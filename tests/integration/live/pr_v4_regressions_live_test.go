//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The four defects fixed for v4.0.0 in the pull-request commands. Each had a
// unit test that passed against a mock while the real server rejected the same
// call, so each is pinned here against Bitbucket itself.

// TestLivePRTransitionsWithoutAnExplicitVersion is #505.
//
// Bitbucket does not read an absent version as "whatever is current". It
// defaults expectedVersion to -1, compares it strictly, and answers 409. The
// existing lifecycle test passed --version on every call, so it only ever
// exercised the path that already worked; the default path -- the one every
// real caller uses -- was never tried against a server.
//
// The issue reported this for decline. merge and reopen share the same helper
// and were equally unusable, so all three are covered.
func TestLivePRTransitionsWithoutAnExplicitVersion(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := "feature/no-version-transitions"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "no-version.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Transitions without --version")

	// Every call below deliberately omits --version. Before the fix each one
	// answered 409 with expectedVersion -1.
	t.Run("decline", func(t *testing.T) {
		assertLivePRState(t, prID, "decline", "DECLINED")
	})

	t.Run("reopen", func(t *testing.T) {
		assertLivePRState(t, prID, "reopen", "OPEN")
	})

	// A stale version must still be refused: resolving the current one when the
	// caller gave none must not have disarmed the optimistic lock for callers
	// who do give one.
	t.Run("an explicit stale version still conflicts", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "pr", "decline", prID, "--version", "0")
		if err == nil {
			t.Fatalf("expected a conflict for a stale version, got success:\n%s", output)
		}
		if !strings.Contains(output, "out-of-date") && !strings.Contains(err.Error(), "conflict") {
			t.Errorf("expected an out-of-date conflict, got: %v\noutput: %s", err, output)
		}
	})

	// Merge closes the pull request, so it goes last.
	t.Run("merge", func(t *testing.T) {
		assertLivePRState(t, prID, "merge", "MERGED")
	})
}

// createLivePRForRegression opens a pull request through the CLI and returns
// its ID.
func createLivePRForRegression(t *testing.T, fromBranch, title string, extraArgs ...string) string {
	t.Helper()

	args := append([]string{"--json", "pr", "create",
		"--from-ref", fromBranch,
		"--to-ref", "refs/heads/master",
		"--title", title,
	}, extraArgs...)

	output, err := executeLiveCLI(t, args...)
	if err != nil {
		t.Fatalf("pr create failed: %v\noutput: %s", err, output)
	}

	pr := extractPRData(decodeJSONMap(t, output))
	id, ok := pr["id"]
	if !ok {
		t.Fatalf("pull request id missing from create output: %s", output)
	}

	return fmt.Sprintf("%v", id)
}

// assertLivePRState runs a transition with no --version and checks the state it
// lands in.
func assertLivePRState(t *testing.T, prID, action, wantState string) {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "pr", action, prID)
	if err != nil {
		t.Fatalf("pr %s without --version failed: %v\noutput: %s", action, err, output)
	}

	pr := extractPRData(decodeJSONMap(t, output))
	if state, _ := pr["state"].(string); state != wantState {
		t.Fatalf("after %s the state is %v, want %s\noutput: %s", action, pr["state"], wantState, output)
	}
}

// decodeLivePRReviewers pulls the reviewer usernames out of a pull request
// payload, whatever shape the reviewer entries take.
func decodeLivePRReviewers(t *testing.T, data map[string]any) []string {
	t.Helper()

	raw, err := json.Marshal(extractPRData(data)["reviewers"])
	if err != nil {
		t.Fatalf("re-encode reviewers failed: %v", err)
	}

	var entries []struct {
		Name string `json:"name"`
		User struct {
			Name string `json:"name"`
		} `json:"user"`
	}
	if err := json.Unmarshal(raw, &entries); err != nil {
		t.Fatalf("decode reviewers failed: %v (from %s)", err, raw)
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if name := entry.Name; name != "" {
			names = append(names, name)
			continue
		}
		if name := entry.User.Name; name != "" {
			names = append(names, name)
		}
	}

	return names
}

// TestLivePRUpdateKeepsReviewers is #511.
//
// The update payload carried no reviewers key, and Bitbucket reads an absent
// key as "no reviewers" rather than "leave them alone", so changing a title
// dropped everyone from the review. The issue guessed that omitting it "may
// also work"; the first subtest here is what proves it does not, against the
// server, and is why the fix echoes the current list back.
func TestLivePRUpdateKeepsReviewers(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	// A pull request author cannot review their own work, so the reviewer has
	// to be somebody else.
	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create reviewer user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant reviewer read access failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := "feature/keep-reviewers"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "keep-reviewers.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLivePRForRegression(t, branch, "Reviewers must survive an update",
		"--reviewers", reviewer.Username, "--no-default-reviewers", "--no-codeowners")

	before := currentLivePRReviewers(t, prID)
	if len(before) != 1 || !strings.EqualFold(before[0], reviewer.Username) {
		t.Fatalf("expected the pull request to start with reviewer %s, got %v", reviewer.Username, before)
	}

	version := currentLivePRVersion(t, prID)

	// The defect, exactly as reported: change one unrelated field.
	updateOutput, err := executeLiveCLI(t, "--json", "pr", "update", prID,
		"--version", version, "--description", "touched by the live regression test")
	if err != nil {
		t.Fatalf("pr update failed: %v\noutput: %s", err, updateOutput)
	}

	after := currentLivePRReviewers(t, prID)
	if len(after) != 1 || !strings.EqualFold(after[0], reviewer.Username) {
		t.Fatalf("updating the description dropped the reviewers: before=%v after=%v", before, after)
	}

	// --reviewers still replaces the list, so the echo must not have turned the
	// flag into an append.
	replaceVersion := currentLivePRVersion(t, prID)
	replaceOutput, err := executeLiveCLI(t, "--json", "pr", "update", prID,
		"--version", replaceVersion, "--reviewers", "")
	if err != nil {
		t.Fatalf("clearing the reviewers failed: %v\noutput: %s", err, replaceOutput)
	}
	if cleared := currentLivePRReviewers(t, prID); len(cleared) != 0 {
		t.Fatalf("--reviewers \"\" must clear the list, got %v", cleared)
	}
}

func currentLivePRReviewers(t *testing.T, prID string) []string {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "pr", "get", prID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	return decodeLivePRReviewers(t, decodeJSONMap(t, output))
}

func currentLivePRVersion(t *testing.T, prID string) string {
	t.Helper()

	output, err := executeLiveCLI(t, "--json", "pr", "get", prID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	return extractPRVersion(decodeJSONMap(t, output))
}

// TestLiveCodeOwnersExpandsAReviewerGroup is #503.
//
// "@reviewer-group/<name>" is how Bitbucket's own Code Owners plugin spells a
// reviewer group. The whole token went into the lookup, missed, and the
// unknown-group fallback then offered it to the reviewers API as a username --
// answered with the 409 the issue reported. Nothing in the live suite touched
// CODEOWNERS at all, so only a server could have caught it.
func TestLiveCodeOwnersExpandsAReviewerGroup(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	member, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create group member failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, member.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant member read access failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const groupName = "cog_product"
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, groupName, member.Username); err != nil {
		t.Fatalf("reviewer group create failed: %v", err)
	}

	// The file has to be on the target branch, which is where the lookup reads
	// it from.
	codeOwners := fmt.Sprintf("*.txt @reviewer-group/%s\n", groupName)
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", ".bitbucket/CODEOWNERS", codeOwners); err != nil {
		t.Fatalf("push CODEOWNERS failed: %v", err)
	}

	branch := "feature/codeowners-reviewer-group"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "owned.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	// --codeowners explicitly, so an entry that cannot be resolved is fatal
	// rather than a warning: this must resolve, not merely fail to complain.
	output, err := createLivePRWithOutput(t, branch, "CODEOWNERS reviewer group", "--codeowners", "--no-default-reviewers")
	if err != nil {
		t.Fatalf("pr create with a CODEOWNERS reviewer group failed: %v\noutput: %s", err, output)
	}

	reviewers := decodeLivePRReviewers(t, decodeJSONMap(t, output))
	if !containsFold(reviewers, member.Username) {
		t.Errorf("expected the reviewer group to expand to %s, got %v\noutput: %s", member.Username, reviewers, output)
	}
	for _, name := range reviewers {
		if strings.Contains(name, "reviewer-group/") {
			t.Errorf("the group token was assigned as a username: %v", reviewers)
		}
	}
}

// TestLiveReviewerFlagsAcceptTheReviewerGroupPrefix covers the two neighbours
// of #503: the same prefix reaches the same lookup from --reviewers and from
// --reviewer-group, and carried the same defect.
func TestLiveReviewerFlagsAcceptTheReviewerGroupPrefix(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	member, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create group member failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, member.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant member read access failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const groupName = "cog_platform"
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, groupName, member.Username); err != nil {
		t.Fatalf("reviewer group create failed: %v", err)
	}

	for index, flags := range [][]string{
		{"--reviewers", "@reviewer-group/" + groupName},
		{"--reviewer-group", "reviewer-group/" + groupName},
		{"--reviewer-group", "@reviewer-group/" + groupName},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			branch := fmt.Sprintf("feature/prefix-flag-%d", index)
			if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, fmt.Sprintf("prefix-%d.txt", index)); err != nil {
				t.Fatalf("push commit on branch failed: %v", err)
			}

			args := append([]string{"--no-default-reviewers", "--no-codeowners"}, flags...)
			output, err := createLivePRWithOutput(t, branch, "Reviewer group prefix "+fmt.Sprint(index), args...)
			if err != nil {
				t.Fatalf("pr create with %v failed: %v\noutput: %s", flags, err, output)
			}

			reviewers := decodeLivePRReviewers(t, decodeJSONMap(t, output))
			if !containsFold(reviewers, member.Username) {
				t.Errorf("expected %v to expand to %s, got %v", flags, member.Username, reviewers)
			}
		})
	}
}

// createLivePRWithOutput opens a pull request and hands back the raw output so
// the caller can assert on what came back, error included.
func createLivePRWithOutput(t *testing.T, fromBranch, title string, extraArgs ...string) (string, error) {
	t.Helper()

	args := append([]string{"--json", "pr", "create",
		"--from-ref", fromBranch,
		"--to-ref", "refs/heads/master",
		"--title", title,
	}, extraArgs...)

	return executeLiveCLI(t, args...)
}

func containsFold(values []string, want string) bool {
	for _, value := range values {
		if strings.EqualFold(value, want) {
			return true
		}
	}

	return false
}

// TestLivePRCreateFromAFork is #506.
//
// A fork-to-upstream pull request was impossible twice over: the pre-flight
// demanded REPO_WRITE on the target, which a fork contributor does not have,
// and the payload named the target repository as the source, so Bitbucket
// looked for the branch in the wrong place.
//
// Both halves need a real server and a real second user to show up at all: the
// permission check is the server's, and the "branch not found" is the server's
// reading of a payload that a mock would have accepted.
func TestLivePRCreateFromAFork(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	upstream := seeded.Repos[0]

	forkSlug := upstream.Slug + "-contributor-fork"
	postLiveJSON(t, fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s", seeded.Key, upstream.Slug), map[string]any{
		"name":    forkSlug,
		"slug":    forkSlug,
		"project": map[string]any{"key": seeded.Key},
	})

	configureLiveCLIEnv(t, harness, seeded.Key, forkSlug)

	branch := "feature/from-the-fork"
	if err := harness.pushCommitOnBranch(seeded.Key, forkSlug, branch, "contributed.txt"); err != nil {
		t.Fatalf("push commit on the fork failed: %v", err)
	}

	// The pull request targets the upstream and names the fork as its source.
	output, err := executeLiveCLI(t, "--json", "pr", "create",
		"--repo", seeded.Key+"/"+upstream.Slug,
		"--from-repo", seeded.Key+"/"+forkSlug,
		"--from-ref", branch,
		"--to-ref", "refs/heads/master",
		"--title", "From the fork",
		"--no-default-reviewers", "--no-codeowners",
	)
	if err != nil {
		t.Fatalf("fork to upstream pull request failed: %v\noutput: %s", err, output)
	}

	pr := extractPRData(decodeJSONMap(t, output))

	// The pull request has to live on the upstream and read its source from the
	// fork. Getting this wrong is what made the server answer "branch not
	// found" before the fix.
	// The pull request has to live on the upstream and read its source from the
	// fork. Getting this second one wrong is what made the server answer
	// "branch not found" before the fix.
	assertLivePRRepository(t, pr, "sourceRepository", forkSlug)
	assertLivePRRepository(t, pr, "repository", upstream.Slug)

	// The other side of the flag: a pull request that is not from a fork must
	// not become one.
	//
	// A unit test asserted this by reading the POST body and checking fromRef
	// carried no repository. That says the payload was built a certain way; it
	// does not say Bitbucket read it the same way, and the field it watches is
	// the one whose absence is the whole signal. Here the answer comes back from
	// the server: source and target are the same repository.
	t.Run("without --from-repo the pull request stays same-repository", func(t *testing.T) {
		const branch = "feature/not-from-a-fork"
		if err := harness.pushCommitOnBranch(seeded.Key, upstream.Slug, branch, "same-repo.txt"); err != nil {
			t.Fatalf("push commit on the upstream failed: %v", err)
		}

		output := mustLiveCLI(t, "pr", "create",
			"--repo", seeded.Key+"/"+upstream.Slug,
			"--from-ref", branch,
			"--to-ref", "refs/heads/master",
			"--title", "Not from a fork",
			"--no-default-reviewers", "--no-codeowners",
		)

		created := extractPRData(decodeJSONMap(t, output))
		assertLivePRRepository(t, created, "repository", upstream.Slug)

		// Absent or equal to the target are both "not from a fork"; a different
		// repository is the failure.
		if _, present := created["sourceRepository"]; present {
			assertLivePRRepository(t, created, "sourceRepository", upstream.Slug)
		}
	})

	// Naming the target as the source is the same-repository case spelled out,
	// and has to behave as though the flag were absent.
	t.Run("--from-repo naming the target is still same-repository", func(t *testing.T) {
		const branch = "feature/from-repo-is-the-target"
		if err := harness.pushCommitOnBranch(seeded.Key, upstream.Slug, branch, "target-as-source.txt"); err != nil {
			t.Fatalf("push commit on the upstream failed: %v", err)
		}

		output := mustLiveCLI(t, "pr", "create",
			"--repo", seeded.Key+"/"+upstream.Slug,
			"--from-repo", seeded.Key+"/"+upstream.Slug,
			"--from-ref", branch,
			"--to-ref", "refs/heads/master",
			"--title", "Target named as source",
			"--no-default-reviewers", "--no-codeowners",
		)

		assertLivePRRepository(t, extractPRData(decodeJSONMap(t, output)), "repository", upstream.Slug)
	})
}

// assertLivePRRepository checks which repository one side of a pull request
// points at. bb flattens both sides to {projectKey, slug}: "repository" is the
// pull request's own repository, which is the target, and "sourceRepository" is
// where the source branch lives.
func assertLivePRRepository(t *testing.T, pr map[string]any, key, wantSlug string) {
	t.Helper()

	entry, ok := pr[key].(map[string]any)
	if !ok {
		raw, _ := json.Marshal(pr)
		t.Errorf("no %s in the pull request payload: %s", key, raw)

		return
	}

	if slug, _ := entry["slug"].(string); slug != wantSlug {
		t.Errorf("%s.slug = %q, want %q", key, slug, wantSlug)
	}
}

// TestLiveCodeOwnersUnknownReviewerGroup is the other half of #503, and the
// half only the parser can answer.
//
// An "@reviewer-group/<name>" entry has said what it is, so the username
// fallback does not apply -- that reading is only for the ambiguous bare
// "@name". Taking it anyway is what sent a group name to the reviewers API and
// produced the 409 the issue reported, naming the server rather than the file
// that caused it. What happens instead is the documented --codeowners contract.
func TestLiveCodeOwnersUnknownReviewerGroup(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A group that does not exist, named beside an owner that does.
	owners := "*.txt @reviewer-group/no_such_group " + harness.username() + "\n"
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", ".bitbucket/CODEOWNERS", owners); err != nil {
		t.Fatalf("push CODEOWNERS failed: %v", err)
	}

	// Explicit --codeowners: the caller asked for these reviewers by name, so
	// an entry that cannot be resolved is fatal.
	t.Run("explicit, so it fails naming the group", func(t *testing.T) {
		branch := "feature/unknown-group-explicit"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "explicit.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}

		output, err := createLivePRWithOutput(t, branch, "Unknown reviewer group, explicit",
			"--codeowners", "--no-default-reviewers")
		if err == nil {
			t.Fatalf("expected an unresolvable entry to be fatal, got:\n%s", output)
		}

		combined := err.Error() + output
		if !strings.Contains(combined, "no_such_group") {
			t.Errorf("the failure must name the group, got: %v\noutput: %s", err, output)
		}
		// The defect was the group name reaching the reviewers API as a user.
		if strings.Contains(combined, "InvalidPullRequestReviewersException") {
			t.Errorf("the group name was still sent as a username: %v\noutput: %s", err, output)
		}
	})

	// Defaulted: code owners are a convenience, so one bad entry warns and the
	// owner named beside it is still assigned.
	t.Run("defaulted, so it warns and keeps the rest", func(t *testing.T) {
		branch := "feature/unknown-group-defaulted"
		if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "defaulted.txt"); err != nil {
			t.Fatalf("push commit on branch failed: %v", err)
		}

		output, err := createLivePRWithOutput(t, branch, "Unknown reviewer group, defaulted", "--no-default-reviewers")
		if err != nil {
			t.Fatalf("a defaulted lookup must not fail on one bad entry: %v\noutput: %s", err, output)
		}
		if !strings.Contains(output, "no_such_group") {
			t.Errorf("expected a warning naming the group, got:\n%s", output)
		}
	})
}
