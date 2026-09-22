//go:build live

package live_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// codeOwnersSpelling is how Bitbucket's Code Owners syntax names a reviewer
// group, which --reviewers accepts behind its at-sign as well as a bare name.
const codeOwnersSpelling = "reviewer-group/"

// TestLiveCompletionPeople proves what the people sources actually offer.
//
// The assertions are on the values, not on the call succeeding, for the reason
// the pull request source is tested that way: a source that answers with
// nothing is indistinguishable, in every shell, from an instance with nobody
// to offer.
//
// Two of them are negative on purpose. Bitbucket drops a query parameter it
// does not recognise instead of refusing it, so a permission filter that is
// misspelled or misnumbered answers with the whole instance -- which looks
// exactly like a filter that worked. The only way to see the difference is to
// seed somebody the filter must exclude and watch them not come back.
func TestLiveCompletionPeople(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	selector := seeded.Key + "/" + repo.Slug

	// Somebody who can review: a licence, and read access to the repository.
	reviewer, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed reviewer failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, reviewer.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant repository read to the reviewer failed: %v", err)
	}

	// Licensed, and cannot see this repository. Bitbucket's permission filter
	// is what leaves them out.
	outsider, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create licensed outsider failed: %v", err)
	}

	// Reads the repository, holds no licence. Bitbucket refuses them as a
	// participant with "they are not a licensed user", so offering them would
	// be offering a value the command fails on.
	unlicensed, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create unlicensed user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, unlicensed.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant repository read to the unlicensed user failed: %v", err)
	}

	groupName := testsupport.UniqueName("lt-reviewers-")
	if err := harness.createReviewerGroup(ctx, seeded.Key, repo.Slug, groupName, reviewer.Username); err != nil {
		t.Fatalf("create reviewer group failed: %v", err)
	}

	branch := testsupport.UniqueName("lt-completion-people-")
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "completion-people.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}
	if _, err := harness.liveJSON(ctx, http.MethodPost,
		"/rest/api/latest/projects/"+seeded.Key+"/repos/"+repo.Slug+"/pull-requests/"+pullRequestID+"/participants",
		map[string]any{"user": map[string]any{"name": reviewer.Username}, "role": "REVIEWER"}); err != nil {
		t.Fatalf("add reviewer to pull request failed: %v", err)
	}

	t.Run("a user slot offers a user with their display name and address", func(t *testing.T) {
		candidates, directive := completeLive(t,
			"project", "permissions", "users", "grant", seeded.Key, outsider.Username)

		description, offered := candidates[outsider.Username]
		if !offered {
			t.Fatalf("user %s was not offered for `bb project permissions users grant`; got %v", outsider.Username, candidates)
		}
		if !strings.Contains(description, "Live Test User") {
			t.Errorf("expected the display name beside the username, got %q", description)
		}
		if !strings.Contains(description, outsider.Username+"@example.local") {
			t.Errorf("expected the email address beside the username, got %q", description)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the shell to be told not to fall back to file names, got directive %d", directive)
		}
	})

	t.Run("a reviewer slot offers only people the pull request can carry", func(t *testing.T) {
		candidates, _ := completeLive(t, "pr", "create", "--repo", selector, "--reviewers", "")

		if _, offered := candidates[reviewer.Username]; !offered {
			t.Fatalf("the licensed repository reader %s was not offered to `bb pr create --reviewers`; got %v",
				reviewer.Username, candidates)
		}

		// The proof that a server-side filter filtered. Both users are in the
		// unfiltered listing -- the subtest above reads one of them out of it
		// -- so their absence here cannot come from the prefix match the shell
		// applies, which does nothing at all to an empty word.
		if _, offered := candidates[outsider.Username]; offered {
			t.Errorf("%s holds a licence and no permission on the repository, and was offered as a reviewer: %v",
				outsider.Username, candidates)
		}
		if _, offered := candidates[unlicensed.Username]; offered {
			t.Errorf("%s reads the repository without a licence, which Bitbucket refuses as a reviewer, and was offered: %v",
				unlicensed.Username, candidates)
		}
	})

	t.Run("the licensed outsider is in the unfiltered listing the reviewer slot narrowed", func(t *testing.T) {
		// The other half of the pairing above: without it, a reviewer slot
		// that offered nobody at all would pass.
		candidates, _ := completeLive(t,
			"project", "permissions", "users", "grant", seeded.Key, outsider.Username)

		if _, offered := candidates[outsider.Username]; !offered {
			t.Fatalf("%s was not offered by an unnarrowed user slot either, so the reviewer assertion proves nothing: %v",
				outsider.Username, candidates)
		}
	})

	t.Run("an at-sign inside a reviewer slot names a group", func(t *testing.T) {
		candidates, _ := completeLive(t, "pr", "create", "--repo", selector, "--reviewers", "@")

		if _, offered := candidates["@"+groupName]; !offered {
			t.Fatalf("reviewer group %s was not offered as @%s; got %v", groupName, groupName, candidates)
		}
		// One spelling per press. Both forms survive the prefix match on "@",
		// so offering the long one here would be a second copy of every group
		// rather than a second way to reach one.
		if _, offered := candidates["@"+codeOwnersSpelling+groupName]; offered {
			t.Errorf("a bare at-sign offered the code owners spelling as well: %v", candidates)
		}
	})

	t.Run("the code owners spelling of the same group keeps completing", func(t *testing.T) {
		candidates, _ := completeLive(t, "pr", "create", "--repo", selector, "--reviewers", "@"+codeOwnersSpelling)

		if _, offered := candidates["@"+codeOwnersSpelling+groupName]; !offered {
			t.Fatalf("reviewer group %s was not offered as @%s%s; got %v",
				groupName, codeOwnersSpelling, groupName, candidates)
		}
	})

	t.Run("removing a reviewer offers this pull request's reviewers", func(t *testing.T) {
		candidates, directive := completeLive(t,
			"pr", "review", "reviewer", "remove", pullRequestID, "--repo", selector, "--user", "")

		if _, offered := candidates[reviewer.Username]; !offered {
			t.Fatalf("the pull request's own reviewer %s was not offered to `reviewer remove`; got %v",
				reviewer.Username, candidates)
		}
		// The administrator can review this repository and is not reviewing
		// this pull request, which is the difference between the two slots.
		if _, offered := candidates[harness.username()]; offered {
			t.Errorf("%s is not a reviewer of pull request %s and was offered for removal: %v",
				harness.username(), pullRequestID, candidates)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}
	})

	t.Run("a reviewer group argument is offered by name", func(t *testing.T) {
		// The placeholder is <reviewer-group-id> and the command resolves an
		// exact name before it reads the argument as an id, so the name is the
		// value worth offering.
		candidates, directive := completeLive(t, "reviewer-group", "delete", "--repo", selector, "")

		if _, offered := candidates[groupName]; !offered {
			t.Fatalf("reviewer group %s was not offered to `bb reviewer-group delete`; got %v", groupName, candidates)
		}
		if _, offered := candidates["@"+groupName]; offered {
			t.Errorf("the at-sign spelling leaked into an argument that takes a bare name: %v", candidates)
		}
		if !forbidsFileNames(directive) {
			t.Errorf("expected the no-file-completion bit to be set, got directive %d", directive)
		}
	})

	t.Run("a group slot offers the instance's groups", func(t *testing.T) {
		candidates, _ := completeLive(t, "project", "permissions", "groups", "grant", seeded.Key, "stash")

		if _, offered := candidates["stash-users"]; !offered {
			t.Fatalf("the licensed group was not offered to `bb project permissions groups grant`; got %v", candidates)
		}
	})
}
