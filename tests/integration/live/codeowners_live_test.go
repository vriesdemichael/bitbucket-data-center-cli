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
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
			name: "and the same glob matches further down",
			file: "sub/dir/notes.md",
			want: []string{docs.Username},
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
