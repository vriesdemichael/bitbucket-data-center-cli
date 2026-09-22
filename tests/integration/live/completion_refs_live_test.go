//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	tagservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/tag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveCompletionRefs proves the branch, tag, ref and commit sources
// against a repository that really has each of those in it.
//
// The assertions are on the values and their descriptions rather than on the
// press having succeeded, for the reason the pull request test gives: a source
// that returns nothing is indistinguishable, in every shell, from a repository
// with nothing to offer.
func TestLiveCompletionRefs(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	selector := seeded.Key + "/" + repo.Slug
	commitID := repo.CommitIDs[0]

	// Two branches whose names share no prefix, so a filter that narrows to
	// one of them has something to leave out.
	alpha := testsupport.UniqueName("lt-refs-alpha-")
	beta := testsupport.UniqueName("lt-refs-beta-")

	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, alpha, "alpha.txt"); err != nil {
		t.Fatalf("push the alpha branch failed: %v", err)
	}
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, beta, "beta.txt"); err != nil {
		t.Fatalf("push the beta branch failed: %v", err)
	}

	alphaTag := testsupport.UniqueName("lt-refs-alphatag-")
	betaTag := testsupport.UniqueName("lt-refs-betatag-")
	for _, name := range []string{alphaTag, betaTag} {
		output, err := executeLiveCLIUnscoped(t,
			"tag", "create", name, "--repo", selector, "--start-point", commitID, "--message", "completion refs tag")
		if err != nil {
			t.Fatalf("tag create %s failed: %v\noutput: %s", name, err, output)
		}
	}

	t.Run("a branch slot offers the repository's branches and marks the default", func(t *testing.T) {
		offered := completeRefs(t, "branch", "delete", "--repo", selector, "")

		if !offered.has(alpha) || !offered.has(beta) {
			t.Fatalf("a pushed branch was not offered to `bb branch delete`: %v", offered.values)
		}
		if description := offered.descriptions["master"]; description != "default branch" {
			t.Errorf("the default branch was described as %q, want %q", description, "default branch")
		}
		if description := offered.descriptions[alpha]; description != "" {
			t.Errorf("an ordinary branch was marked as %q", description)
		}

		// Bitbucket sends the flag as isDefault and the generated model reads
		// it as default, so RestBranch.Default is nil for the default branch
		// too. The marking above therefore has to come from somewhere else,
		// and this is the assertion that notices if it stops.
		if offered.values[0] != "master" {
			t.Errorf("the default branch was not offered first: %v", offered.values)
		}
		if offered.directive != directiveKeptOrder {
			t.Errorf("expected the shell to be told to keep the order, got directive %q", offered.directive)
		}
	})

	t.Run("a branch slot narrows to what has been typed", func(t *testing.T) {
		offered := completeRefs(t, "branch", "delete", "--repo", selector, alpha[:len("lt-refs-alpha-")])

		if !offered.has(alpha) {
			t.Fatalf("the matching branch was not offered: %v", offered.values)
		}
		if offered.has(beta) || offered.has("master") {
			t.Errorf("a branch that does not start with what was typed was offered: %v", offered.values)
		}
	})

	t.Run("a tag slot offers the tags with the commit each one marks", func(t *testing.T) {
		offered := completeRefs(t, "tag", "view", "--repo", selector, "")

		if !offered.has(alphaTag) {
			t.Fatalf("the seeded tag was not offered to `bb tag view`: %v", offered.values)
		}

		want := "at " + commitID[:7]
		if description := offered.descriptions[alphaTag]; description != want {
			t.Errorf("the tag was described as %q, want %q", description, want)
		}
	})

	t.Run("a ref slot offers both, and says which is which", func(t *testing.T) {
		offered := completeRefs(t, "ref", "resolve", "--repo", selector, "")

		if description := offered.descriptions[alpha]; description != "branch" {
			t.Errorf("a branch in a ref slot was described as %q", description)
		}
		if description := offered.descriptions[alphaTag]; description != "tag" {
			t.Errorf("a tag in a ref slot was described as %q", description)
		}
	})

	t.Run("a commit slot offers commits and the names that resolve to one", func(t *testing.T) {
		offered := completeRefs(t, "commit", "get", "--repo", selector, "")

		if !offered.has(alpha) || !offered.has(alphaTag) {
			t.Errorf("a commit-ish slot was not offered the names that resolve to a commit: %v", offered.values)
		}

		short, description, found := offered.prefixOf(commitID)
		if !found {
			t.Fatalf("the seeded commit %s was not offered: %v", commitID, offered.values)
		}

		want := fmt.Sprintf("seed commit 1 for %s/%s (bb-live-test)", seeded.Key, repo.Slug)
		if description != want {
			t.Errorf("the commit %s was described as %q, want %q", short, description, want)
		}
	})

	// The filters below are asserted through the services the sources call,
	// not through a press. A press cannot tell a filter apart from no filter:
	// the shell only ever sees what starts with the typed word, because
	// internal/cli/completion/run.go drops the rest, so a filterText Bitbucket
	// silently ignored produces exactly the same candidates as one it applied.
	// Bitbucket ignores query parameters it does not recognise rather than
	// refusing them, which is how a misspelled one would otherwise survive
	// every test in this file.
	t.Run("Bitbucket applies the branch filter it is given", func(t *testing.T) {
		service := branchservice.NewService(harness.client)
		repoRef := branchservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}

		everything, err := service.List(ctx, repoRef, branchservice.ListOptions{MaxResults: 100})
		if err != nil {
			t.Fatalf("list branches failed: %v", err)
		}
		if !containsRef(branchNames(everything), beta) {
			t.Fatalf("the unfiltered listing does not hold %s, so its absence below would prove nothing: %v",
				beta, branchNames(everything))
		}

		filtered, err := service.List(ctx, repoRef, branchservice.ListOptions{MaxResults: 100, FilterText: alpha})
		if err != nil {
			t.Fatalf("list branches with a filter failed: %v", err)
		}

		names := branchNames(filtered)
		if !containsRef(names, alpha) {
			t.Fatalf("the filtered listing dropped the branch it was asked for: %v", names)
		}
		if containsRef(names, beta) {
			t.Errorf("filterText=%s came back with %s as well, so Bitbucket ignored it: %v", alpha, beta, names)
		}
	})

	t.Run("Bitbucket applies the tag filter it is given", func(t *testing.T) {
		service := tagservice.NewService(harness.client)
		repoRef := tagservice.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug}

		everything, err := service.List(ctx, repoRef, tagservice.ListOptions{MaxResults: 100})
		if err != nil {
			t.Fatalf("list tags failed: %v", err)
		}
		if !containsRef(tagNames(everything), betaTag) {
			t.Fatalf("the unfiltered listing does not hold %s, so its absence below would prove nothing: %v",
				betaTag, tagNames(everything))
		}

		filtered, err := service.List(ctx, repoRef, tagservice.ListOptions{MaxResults: 100, FilterText: alphaTag})
		if err != nil {
			t.Fatalf("list tags with a filter failed: %v", err)
		}

		names := tagNames(filtered)
		if !containsRef(names, alphaTag) {
			t.Fatalf("the filtered listing dropped the tag it was asked for: %v", names)
		}
		if containsRef(names, betaTag) {
			t.Errorf("filterText=%s came back with %s as well, so Bitbucket ignored it: %v", alphaTag, betaTag, names)
		}
	})
}

// TestLiveCompletionRefsStayInsideTheRepositoryNamed is the rule that keeps
// one repository's branches out of another's completion.
//
// `bb pr create --from-repo` is the one place a ref slot means a branch of a
// repository other than the one the command acts on, and only --from-ref
// moves with it. Getting that backwards offers a branch the fork does not have
// to a pull request that will be refused, or the fork's branches as the
// target of one.
func TestLiveCompletionRefsFollowFromRepo(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{Repos: 2, WithCommitIDs: true})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	target, source := seeded.Repos[0], seeded.Repos[1]
	targetSelector := seeded.Key + "/" + target.Slug
	sourceSelector := seeded.Key + "/" + source.Slug

	targetBranch := testsupport.UniqueName("lt-refs-target-")
	sourceBranch := testsupport.UniqueName("lt-refs-source-")

	if err := harness.pushCommitOnBranch(seeded.Key, target.Slug, targetBranch, "target.txt"); err != nil {
		t.Fatalf("push the target branch failed: %v", err)
	}
	if err := harness.pushCommitOnBranch(seeded.Key, source.Slug, sourceBranch, "source.txt"); err != nil {
		t.Fatalf("push the source branch failed: %v", err)
	}

	t.Run("--from-ref names a branch of --from-repo", func(t *testing.T) {
		offered := completeRefs(t, "pr", "create", "--repo", targetSelector, "--from-repo", sourceSelector, "--from-ref", "")

		if !offered.has(sourceBranch) {
			t.Fatalf("the source repository's branch was not offered: %v", offered.values)
		}
		if offered.has(targetBranch) {
			t.Errorf("the target repository's branch was offered for --from-ref: %v", offered.values)
		}
	})

	t.Run("--to-ref stays with the repository the pull request targets", func(t *testing.T) {
		offered := completeRefs(t, "pr", "create", "--repo", targetSelector, "--from-repo", sourceSelector, "--to-ref", "")

		if !offered.has(targetBranch) {
			t.Fatalf("the target repository's branch was not offered for --to-ref: %v", offered.values)
		}
		if offered.has(sourceBranch) {
			t.Errorf("--from-repo moved --to-ref as well: %v", offered.values)
		}
	})
}

// directiveKeptOrder is ShellCompDirectiveNoFileComp with
// ShellCompDirectiveKeepOrder: no file names, and the order as given.
const directiveKeptOrder = ":36"

// refCompletion is one press, keeping the order the source answered in.
//
// parseCompletionOutput answers with a map, which is the right shape for
// asking whether a value was offered and the wrong one for a source whose
// ranking is part of the answer.
type refCompletion struct {
	values       []string
	descriptions map[string]string
	directive    string
}

func (completion refCompletion) has(value string) bool {
	_, offered := completion.descriptions[value]

	return offered
}

// prefixOf finds the candidate that abbreviates a commit. The value offered is
// Bitbucket's display id, which is the hash cut to whatever length it shows.
func (completion refCompletion) prefixOf(commitID string) (value, description string, found bool) {
	for _, candidate := range completion.values {
		if len(candidate) >= 7 && strings.HasPrefix(commitID, candidate) {
			return candidate, completion.descriptions[candidate], true
		}
	}

	return "", "", false
}

func completeRefs(t *testing.T, words ...string) refCompletion {
	t.Helper()

	output, err := executeLiveCLIUnscoped(t, append([]string{"__complete"}, words...)...)
	if err != nil {
		t.Fatalf("completion failed: %v\noutput: %s", err, output)
	}

	completion := refCompletion{descriptions: map[string]string{}}

	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.TrimSpace(line) == "":
			continue
		case strings.HasPrefix(line, ":"):
			completion.directive = strings.TrimSpace(line)
		case strings.HasPrefix(line, "Completion ended with directive:"):
			continue
		default:
			value, description, _ := strings.Cut(line, "\t")
			value = strings.TrimSpace(value)
			completion.values = append(completion.values, value)
			completion.descriptions[value] = description
		}
	}

	if completion.directive == "" {
		t.Fatalf("completion output carried no directive line: %q", output)
	}

	return completion
}

func branchNames(branches []openapigenerated.RestBranch) []string {
	names := make([]string, 0, len(branches))
	for _, branch := range branches {
		names = append(names, safederef.String(branch.DisplayId))
	}

	return names
}

func tagNames(tags []openapigenerated.RestTag) []string {
	names := make([]string, 0, len(tags))
	for _, tag := range tags {
		names = append(names, safederef.String(tag.DisplayId))
	}

	return names
}

func containsRef(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}

	return false
}
