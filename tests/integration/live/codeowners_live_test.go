//go:build live

package live_test

// CODEOWNERS against a real Bitbucket.
//
// The whole feature reads one file out of the repository, matches it against a
// pull request's diff, and resolves what it finds into reviewers. Every one of
// those inputs -- the file, the diff, the reviewer groups, the users -- used to
// be supplied by a handwritten server, which is to say the tests chose the
// answers they then checked. The syntax is wide enough that this matters:
// anchoring, globbing, escaped spaces and the two kinds of at-sign all read
// alike and behave differently, and the reviewer-group form has already been
// wrong once in a way no mock noticed (#503).

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestLiveCodeOwnersPatternSyntax walks the pattern forms a CODEOWNERS file
// may use, one pull request per form, against rules a real repository holds.
//
// Each case names the file it touches and who should end up reviewing it. The
// negative cases are the point of half of them: an anchored rule must not
// match deeper, and a path nobody claims must draw nobody.
func TestLiveCodeOwnersPatternSyntax(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	docs := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)
	root := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)
	deep := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)

	// A file with a comment, a blank line, a line that is not a rule, and one
	// rule that is deliberately shadowed by a later one.
	codeOwners := strings.Join([]string{
		"# ownership, as the live suite understands it",
		"",
		"*.md                   @" + docs.Username,
		"/root-only.txt         @" + root.Username,
		"src/**/deep/           @" + deep.Username,
		"**/*.rst               @" + deep.Username,
		`docs\ site/            @` + docs.Username,
		"a-line-with-no-owners",
		"docs/final.md          @" + root.Username,
		"",
	}, "\n")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", ".bitbucket/CODEOWNERS", codeOwners); err != nil {
		t.Fatalf("push CODEOWNERS failed: %v", err)
	}

	for _, testCase := range []struct {
		name string
		file string
		want []string
		deny []string
	}{
		{
			name: "an unanchored glob matches at the root",
			file: "notes.md",
			want: []string{docs.Username},
		},
		{
			// Not GitHub's rule: an unanchored glob is anchored to the root
			// here, so the same name deeper down is not owned.
			name: "and does not match further down",
			file: "sub/dir/notes.md",
			deny: []string{docs.Username},
		},
		{
			name: "a leading double star is how any depth is spelled",
			file: "sub/dir/notes.rst",
			want: []string{deep.Username},
		},
		{
			name: "a leading slash anchors the rule to the root",
			file: "root-only.txt",
			want: []string{root.Username},
		},
		{
			name: "so the same name deeper down is not owned",
			file: "sub/root-only.txt",
			deny: []string{root.Username, docs.Username, deep.Username},
		},
		{
			name: "a double star spans any number of directories",
			file: "src/one/two/deep/thing.txt",
			want: []string{deep.Username},
		},
		{
			name: "and none at all",
			file: "src/deep/thing.txt",
			want: []string{deep.Username},
		},
		{
			name: "an escaped space is part of the directory name",
			file: "docs site/guide.txt",
			want: []string{docs.Username},
		},
		{
			name: "the last matching rule wins outright",
			file: "docs/final.md",
			want: []string{root.Username},
			// *.md matches this too, and is listed first. Last wins means
			// replaced, not added to.
			deny: []string{docs.Username},
		},
		{
			name: "a path no rule claims draws nobody",
			file: "unclaimed/thing.txt",
			deny: []string{docs.Username, root.Username, deep.Username},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			branch := fmt.Sprintf("feature/co-%d", time.Now().UnixNano()%1000000)
			if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, testCase.file, "content\n"); err != nil {
				t.Fatalf("push %s failed: %v", testCase.file, err)
			}

			output := mustLiveCLI(t, "pr", "create",
				"--from-ref", branch, "--to-ref", "refs/heads/master",
				"--title", testCase.name, "--codeowners", "--no-default-reviewers")

			reviewers := decodeLivePRReviewers(t, decodeJSONMap(t, output))
			for _, want := range testCase.want {
				if !containsFold(reviewers, want) {
					t.Errorf("%s: expected %s among the reviewers, got %v", testCase.file, want, reviewers)
				}
			}
			for _, deny := range testCase.deny {
				if containsFold(reviewers, deny) {
					t.Errorf("%s: %s should not own this path, got %v", testCase.file, deny, reviewers)
				}
			}
		})
	}
}

// liveCodeOwner makes a user who can be named in CODEOWNERS: licensed, and
// able to read the repository, which Bitbucket requires before it will accept
// them as a reviewer.
func liveCodeOwner(t *testing.T, ctx context.Context, harness *liveHarness, projectKey, repositorySlug string) restrictedUser {
	t.Helper()

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create code owner failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, projectKey, repositorySlug, user.Username,
		openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
		t.Fatalf("grant the code owner read access failed: %v", err)
	}

	return user
}

// TestLiveCodeOwnersOwnerSyntax walks the owner forms, which is where the
// syntax has actually gone wrong.
//
// A bare name, "@name" and "@reviewer-group/name" all look alike and resolve
// through different lookups, and the middle one is deliberately ambiguous: it
// means a reviewer group when one exists and a username when one does not.
// That ambiguity is why #503 was possible -- "@reviewer-group/x" missed the
// group and then went out as a username, which Bitbucket answered with 409.
func TestLiveCodeOwnersOwnerSyntax(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	bare := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)
	atSign := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)
	first := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)
	second := liveCodeOwner(t, ctx, harness, seeded.Key, repo.Slug)

	// A real reviewer group with two members, so a selection strategy has
	// something to choose between.
	const groupName = "platform-reviewers"
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, groupName, first.Username, second.Username); err != nil {
		t.Fatalf("create reviewer group failed: %v", err)
	}

	codeOwners := strings.Join([]string{
		"bare/       " + bare.Username,
		"at/         @" + atSign.Username,
		"group/      @reviewer-group/" + groupName,
		"plain/      @" + groupName,
		"several/    @" + bare.Username + " @" + atSign.Username,
		"all/        @reviewer-group/" + groupName + ":all",
		"one/        @reviewer-group/" + groupName + ":random(1)",
		"skip/       @reviewer-group/no_such_group @" + atSign.Username,
		"",
	}, "\n")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, "master", ".bitbucket/CODEOWNERS", codeOwners); err != nil {
		t.Fatalf("push CODEOWNERS failed: %v", err)
	}

	reviewersFor := func(t *testing.T, directory string) []string {
		t.Helper()

		branch := fmt.Sprintf("feature/owner-%d", time.Now().UnixNano()%1000000)
		if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, directory+"/file.txt", "x\n"); err != nil {
			t.Fatalf("push %s failed: %v", directory, err)
		}

		output := mustLiveCLI(t, "pr", "create",
			"--from-ref", branch, "--to-ref", "refs/heads/master",
			"--title", "owners for "+directory, "--codeowners", "--no-default-reviewers")

		return decodeLivePRReviewers(t, decodeJSONMap(t, output))
	}

	// Bitbucket wants the at-sign. bb used to take a bare name as a user, so
	// this line assigned somebody through the CLI and nobody through the
	// button -- one of the divergences that ended the local evaluation.
	t.Run("a bare name is nobody", func(t *testing.T) {
		if names := reviewersFor(t, "bare"); containsFold(names, bare.Username) {
			t.Fatalf("a name with no at-sign was read as an owner, got %v", names)
		}
	})

	t.Run("an at-sign with no such group falls back to a username", func(t *testing.T) {
		// This is the ambiguity the reviewer-group prefix exists to remove:
		// "@name" is a group if one answers to it and a person otherwise.
		if names := reviewersFor(t, "at"); !containsFold(names, atSign.Username) {
			t.Fatalf("expected %s, got %v", atSign.Username, names)
		}
	})

	t.Run("the reviewer-group prefix reaches a reviewer group", func(t *testing.T) {
		// #503: the prefix was carried into the lookup, so the group was never
		// found, and the fallback then sent "reviewer-group/platform-reviewers"
		// as a username. Every member has to arrive.
		names := reviewersFor(t, "group")
		for _, member := range []string{first.Username, second.Username} {
			if !containsFold(names, member) {
				t.Errorf("expected group member %s among the reviewers, got %v", member, names)
			}
		}
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), "reviewer-group/") {
				t.Errorf("the prefix was sent as a username: %v", names)
			}
		}
	})

	// And the prefix is not decoration. "@platform" names a Bitbucket group,
	// not a reviewer group, so a reviewer group of that name is not found --
	// where bb used to expand it and assign both members.
	t.Run("a bare at-sign does not reach a reviewer group", func(t *testing.T) {
		names := reviewersFor(t, "plain")
		for _, member := range []string{first.Username, second.Username} {
			if containsFold(names, member) {
				t.Errorf("a bare at-sign expanded a reviewer group, got %v", names)
			}
		}
	})

	t.Run("several owners on one line all arrive", func(t *testing.T) {
		names := reviewersFor(t, "several")
		for _, want := range []string{bare.Username, atSign.Username} {
			if !containsFold(names, want) {
				t.Errorf("expected %s among the reviewers, got %v", want, names)
			}
		}
	})

	t.Run("the all strategy takes every member", func(t *testing.T) {
		names := reviewersFor(t, "all")
		for _, member := range []string{first.Username, second.Username} {
			if !containsFold(names, member) {
				t.Errorf("expected group member %s among the reviewers, got %v", member, names)
			}
		}
	})

	// An entry Bitbucket cannot resolve costs the owners named beside it
	// nothing. bb used to make this fatal under an explicit --codeowners and a
	// warning otherwise, which is a contract the server does not have: it
	// skips the entry and answers with the rest.
	t.Run("an unresolvable entry is skipped, not fatal", func(t *testing.T) {
		names := reviewersFor(t, "skip")
		if !containsFold(names, atSign.Username) {
			t.Errorf("the owner named beside an unresolvable one was lost, got %v", names)
		}
		for _, name := range names {
			if strings.Contains(strings.ToLower(name), "no_such_group") {
				t.Errorf("the unresolvable group was sent as a username: %v", names)
			}
		}
	})

	t.Run("a bounded strategy takes that many and no more", func(t *testing.T) {
		names := reviewersFor(t, "one")

		chosen := 0
		for _, member := range []string{first.Username, second.Username} {
			if containsFold(names, member) {
				chosen++
			}
		}
		if chosen != 1 {
			t.Fatalf("random(1) took %d of the group's two members, got %v", chosen, names)
		}
	})
}
