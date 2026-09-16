//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := "feature/no-version-transitions"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "no-version.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	prID := createLifecyclePR(t, branch, "Transitions without --version")

	// Every call below deliberately omits --version. Before the fix each one
	// answered 409 with expectedVersion -1.
	//
	// pr update is here for the same reason and arrived later: #532 fixed the
	// transitions and left --version as the one required flag on the command
	// that edits a title, so changing a title took a read first and a stale
	// read turned the edit into a 409.
	t.Run("update", func(t *testing.T) {
		retitled := "Retitled without a version"

		output, err := executeLiveCLI(t, "--json", "pr", "update", prID, "--title", retitled)
		if err != nil {
			t.Fatalf("pr update without --version failed: %v\noutput: %s", err, output)
		}

		pr := extractPRData(decodeJSONMap(t, output))
		if title, _ := pr["title"].(string); title != retitled {
			t.Fatalf("the title is %v, want %q\noutput: %s", pr["title"], retitled, output)
		}

		assertLifecyclePRStored(t, readLifecyclePR(t, prID), map[string]any{"title": retitled})
	})

	// The update above moved the version past 0, so this names one the pull
	// request has genuinely moved on from.
	t.Run("an explicit stale version still conflicts on update", func(t *testing.T) {
		before := readLifecyclePR(t, prID)

		output, err := executeLiveCLI(t, "--json", "pr", "update", prID, "--version", "0", "--title", "Should not land")
		if err == nil {
			t.Fatalf("expected a conflict for a stale version, got success:\n%s", output)
		}
		if !strings.Contains(output, "out-of-date") && !strings.Contains(err.Error(), "conflict") {
			t.Errorf("expected an out-of-date conflict, got: %v\noutput: %s", err, output)
		}
		assertLifecycleOutOfDate(t, err)

		assertLifecyclePRStored(t, readLifecyclePR(t, prID), map[string]any{"title": before["title"], "version": before["version"]})
	})

	t.Run("decline", func(t *testing.T) {
		assertLivePRState(t, prID, "decline", "DECLINED")
		assertLifecyclePRStored(t, readLifecyclePR(t, prID), map[string]any{"state": "DECLINED"})
	})

	t.Run("reopen", func(t *testing.T) {
		assertLivePRState(t, prID, "reopen", "OPEN")
		assertLifecyclePRStored(t, readLifecyclePR(t, prID), map[string]any{"state": "OPEN"})
	})

	// A stale version must still be refused: resolving the current one when the
	// caller gave none must not have disarmed the optimistic lock for callers
	// who do give one.
	t.Run("an explicit stale version still conflicts", func(t *testing.T) {
		before := readLifecyclePR(t, prID)

		output, err := executeLiveCLI(t, "--json", "pr", "decline", prID, "--version", "0")
		if err == nil {
			t.Fatalf("expected a conflict for a stale version, got success:\n%s", output)
		}
		if !strings.Contains(output, "out-of-date") && !strings.Contains(err.Error(), "conflict") {
			t.Errorf("expected an out-of-date conflict, got: %v\noutput: %s", err, output)
		}
		assertLifecycleOutOfDate(t, err)

		assertLifecyclePRStored(t, readLifecyclePR(t, prID), map[string]any{"state": before["state"], "version": before["version"]})
	})

	// Merge closes the pull request, so it goes last.
	t.Run("merge", func(t *testing.T) {
		assertLivePRState(t, prID, "merge", "MERGED")
		assertLifecyclePRStored(t, readLifecyclePR(t, prID), map[string]any{"state": "MERGED"})
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

// createLifecyclePR is createLivePRForRegression followed by a read of what
// Bitbucket stored for the title and both branches.
//
// A read of its own, because Bitbucket answers 2xx to a property it does not
// know and drops it: the create's answer cannot tell a stored value from a
// dropped one, and createLivePRForRegression keeps only the id. master is the
// default branch, but not one Bitbucket fills in: a create without a toRef is
// refused with 400.
func createLifecyclePR(t *testing.T, fromBranch, title string, extraArgs ...string) string {
	t.Helper()

	prID := createLivePRForRegression(t, fromBranch, title, extraArgs...)
	assertLifecyclePRStored(t, readLifecyclePR(t, prID), map[string]any{
		"title":        title,
		"sourceBranch": fromBranch,
		"targetBranch": "master",
	})

	return prID
}

// readLifecyclePR reads a pull request through bb pr get, in the repository the
// test is scoped to.
func readLifecyclePR(t *testing.T, prID string) map[string]any {
	t.Helper()

	return extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID)))
}

// readLifecyclePRIn is readLifecyclePR for a pull request in a repository other
// than the test's own, such as the upstream of a fork.
func readLifecyclePRIn(t *testing.T, repoRef, prID string) map[string]any {
	t.Helper()

	return extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "get", prID, "--repo", repoRef)))
}

// assertLifecyclePRStored compares fields of a pull request that was read back
// with the values sent, exactly. The values are what JSON decodes to: a version
// is a float64 and a repository a map.
func assertLifecyclePRStored(t *testing.T, stored, want map[string]any) {
	t.Helper()

	for _, field := range slices.Sorted(maps.Keys(want)) {
		if !reflect.DeepEqual(stored[field], want[field]) {
			t.Errorf("pull request %v reads back %s = %#v, want %#v", stored["id"], field, stored[field], want[field])
		}
	}
}

// assertLifecyclePRHarnessStored reads back a pull request that
// harness.createPullRequest opened: the title and description it always sends,
// and the branches it was given.
func assertLifecyclePRHarnessStored(t *testing.T, prID, fromBranch, toBranch string) {
	t.Helper()

	assertLifecyclePRStored(t, readLifecyclePR(t, prID), map[string]any{
		"title":        "Live test PR",
		"description":  "PR seeded by live harness",
		"sourceBranch": fromBranch,
		"targetBranch": toBranch,
	})
}

// assertLifecycleOutOfDate checks that a refusal is Bitbucket's own answer to a
// stale version, which only a version that reached it can produce.
func assertLifecycleOutOfDate(t *testing.T, err error) {
	t.Helper()

	details := apperrors.DetailsOf(err)
	if details["upstreamStatus"] != "409" || details["upstreamException"] != "com.atlassian.bitbucket.pull.PullRequestOutOfDateException" {
		t.Errorf("expected Bitbucket's 409 PullRequestOutOfDateException, got %v: %v", details, err)
	}
}

// assertLifecycleForkStored reads a fork back through bb repo get: its name, the
// project it was made in, and the repository it was forked from.
func assertLifecycleForkStored(t *testing.T, projectKey, forkSlug, forkName, originSlug string) {
	t.Helper()

	stored, ok := decodeJSONMap(t, mustLiveCLI(t, "repo", "get", "--repo", projectKey+"/"+forkSlug, "--readme=false"))["repository"].(map[string]any)
	if !ok {
		t.Fatalf("repo get of the fork %s/%s carries no repository", projectKey, forkSlug)
	}

	want := map[string]any{
		"projectKey": projectKey,
		"slug":       forkSlug,
		"name":       forkName,
		"origin":     map[string]any{"projectKey": projectKey, "slug": originSlug},
	}
	for _, field := range slices.Sorted(maps.Keys(want)) {
		if !reflect.DeepEqual(stored[field], want[field]) {
			t.Errorf("the fork reads back %s = %#v, want %#v", field, stored[field], want[field])
		}
	}
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
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

	prID := createLifecyclePR(t, branch, "Reviewers must survive an update",
		"--reviewers", reviewer.Username, "--no-default-reviewers", "--no-codeowners")

	before := currentLivePRReviewers(t, prID)
	if len(before) != 1 || !strings.EqualFold(before[0], reviewer.Username) {
		t.Fatalf("expected the pull request to start with reviewer %s, got %v", reviewer.Username, before)
	}

	version := currentLivePRVersion(t, prID)

	// The defect, exactly as reported: change one unrelated field.
	const description = "touched by the live regression test"
	updateOutput, err := executeLiveCLI(t, "--json", "pr", "update", prID,
		"--version", version, "--description", description)
	if err != nil {
		t.Fatalf("pr update failed: %v\noutput: %s", err, updateOutput)
	}

	after := currentLivePRReviewers(t, prID)
	if len(after) != 1 || !strings.EqualFold(after[0], reviewer.Username) {
		t.Fatalf("updating the description dropped the reviewers: before=%v after=%v", before, after)
	}

	// The field the update was for. The pull request had no description, so a
	// dropped one reads back absent.
	assertLifecyclePRStored(t, readLifecyclePR(t, prID), map[string]any{"description": description})

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

// TestLiveReviewerFlagsAcceptTheReviewerGroupPrefix covers the two neighbours
// of #503: the same prefix reaches the same lookup from --reviewers and from
// --reviewer-group, and carried the same defect.
func TestLiveReviewerFlagsAcceptTheReviewerGroupPrefix(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
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

			title := "Reviewer group prefix " + fmt.Sprint(index)
			args := append([]string{"--no-default-reviewers", "--no-codeowners"}, flags...)
			output, err := createLivePRWithOutput(t, branch, title, args...)
			if err != nil {
				t.Fatalf("pr create with %v failed: %v\noutput: %s", flags, err, output)
			}

			reviewers := decodeLivePRReviewers(t, decodeJSONMap(t, output))
			if !containsFold(reviewers, member.Username) {
				t.Errorf("expected %v to expand to %s, got %v", flags, member.Username, reviewers)
			}

			// The expansion as Bitbucket stored it: the group's one member and
			// nobody beside them, which the create's answer cannot vouch for.
			stored := readLifecyclePR(t, trimNumeric(extractPRData(decodeJSONMap(t, output))["id"]))
			assertLifecyclePRStored(t, stored, map[string]any{"title": title, "sourceBranch": branch, "targetBranch": "master"})
			if names := decodeLivePRReviewers(t, stored); len(names) != 1 || !strings.EqualFold(names[0], member.Username) {
				t.Errorf("%v stored the reviewers %v, want exactly [%s]", flags, names, member.Username)
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
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	upstream := seeded.Repos[0]

	// No slug is sent: Bitbucket derives a fork's slug from its name and ignores
	// one beside it, answering 201 either way, so the name is chosen to be the
	// slug the rest of the test uses.
	forkSlug := upstream.Slug + "-contributor-fork"
	postLiveJSON(t, fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s", seeded.Key, upstream.Slug), map[string]any{
		"name":    forkSlug,
		"project": map[string]any{"key": seeded.Key},
	})
	assertLifecycleForkStored(t, seeded.Key, forkSlug, forkSlug, upstream.Slug)

	configureLiveCLIEnv(t, harness, seeded.Key, forkSlug)
	upstreamRef := seeded.Key + "/" + upstream.Slug

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

	// Read back on the upstream, and with the project as well as the slug: the
	// two checks above read the create's own answer.
	assertLifecyclePRStored(t, readLifecyclePRIn(t, upstreamRef, trimNumeric(pr["id"])), map[string]any{
		"title":            "From the fork",
		"sourceBranch":     branch,
		"targetBranch":     "master",
		"sourceRepository": map[string]any{"projectKey": seeded.Key, "slug": forkSlug},
		"repository":       map[string]any{"projectKey": seeded.Key, "slug": upstream.Slug},
	})

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

		// Read back, where the source repository is there to check whether or
		// not the create's answer carried it.
		assertLifecyclePRStored(t, readLifecyclePRIn(t, upstreamRef, trimNumeric(created["id"])), map[string]any{
			"title":            "Not from a fork",
			"sourceBranch":     branch,
			"targetBranch":     "master",
			"sourceRepository": map[string]any{"projectKey": seeded.Key, "slug": upstream.Slug},
			"repository":       map[string]any{"projectKey": seeded.Key, "slug": upstream.Slug},
		})
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

		created := extractPRData(decodeJSONMap(t, output))
		assertLivePRRepository(t, created, "repository", upstream.Slug)

		// The --from-repo sent is the repository Bitbucket assumes without one,
		// by design, so no read can tell it arrived; this one shows naming it
		// made the pull request nothing else.
		assertLifecyclePRStored(t, readLifecyclePRIn(t, upstreamRef, trimNumeric(created["id"])), map[string]any{
			"title":            "Target named as source",
			"sourceBranch":     branch,
			"targetBranch":     "master",
			"sourceRepository": map[string]any{"projectKey": seeded.Key, "slug": upstream.Slug},
			"repository":       map[string]any{"projectKey": seeded.Key, "slug": upstream.Slug},
		})
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

// The two #503 CODEOWNERS tests moved to codeowners_live_test.go, where the
// rest of the syntax is covered.
//
// One of them no longer describes anything bb does: it asserted that an
// unresolvable "@reviewer-group/<name>" is fatal under an explicit
// --codeowners and a warning otherwise. Bitbucket resolves CODEOWNERS now
// (ADR-080) and it has no such contract -- it skips the entry and answers with
// the owners named beside it, which is what the live suite pins.
