//go:build live

package live_test

import (
	"context"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/compat"
)

// noCreatesSince is the first release with the no-creates branch restriction:
// 9.3.2 refused the type with a 400 "Invalid type", as a restriction and as a
// listing filter, and 9.4.24 stored it. Stated here rather than read from
// internal/compat, so a boundary set wrong there fails on a release.
var noCreatesSince = compat.Release{Major: 9, Minor: 4}

// TestLiveNoCreatesRestriction covers the no-creates restriction type, for a
// repository and for a project, on the release under test.
//
// From 9.4 a restriction of the type is stored, found by a listing filtered by
// it, and made by an update. Before it bb refuses the create, the update and
// their dry runs as unsupported rather than pass on Bitbucket's "Invalid type",
// and a listing filtered by the type answers with none -- what such a release
// holds -- rather than refusing the filter. A read-only restriction sits beside
// it throughout, so a filter that was not applied has something to show.
func TestLiveNoCreatesRestriction(t *testing.T) {
	t.Parallel()

	type scope struct {
		name                      string
		create, get, list, update []string
		keyed                     bool
	}
	scopes := []scope{
		{
			name:   "repository",
			create: []string{"branch", "restriction", "create"},
			get:    []string{"branch", "restriction", "get"},
			list:   []string{"branch", "restriction", "list"},
			update: []string{"branch", "restriction", "update"},
		},
		{
			name:   "project",
			create: []string{"project", "branch-restriction", "create"},
			get:    []string{"project", "branch-restriction", "get"},
			list:   []string{"project", "branch-restriction", "list"},
			update: []string{"project", "branch-restriction", "update"},
			keyed:  true,
		},
	}

	for _, scope := range scopes {
		t.Run(scope.name, func(t *testing.T) {
			t.Parallel()

			harness := newLiveHarness(t)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()

			seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
			if err != nil {
				t.Fatalf("seed project failed: %v", err)
			}
			configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

			then := func(rest ...string) []string {
				if scope.keyed {
					return append([]string{seeded.Key}, rest...)
				}
				return rest
			}
			readOnlyID := restrictionID(t, mustLiveCLI(t, append(scope.create,
				then("--type", "read-only", "--matcher-type", "PATTERN", "--matcher-id", "refs/heads/ro-*")...)...))
			noCreates := []string{"--type", "no-creates", "--matcher-type", "PATTERN", "--matcher-id", "refs/heads/nc-*"}

			release := harness.release(t)
			if release.Before(noCreatesSince) {
				for _, args := range [][]string{
					append(append([]string{"--json", "--dry-run"}, scope.create...), then(noCreates...)...),
					append(append([]string{"--json"}, scope.create...), then(noCreates...)...),
					append(append([]string{"--json", "--dry-run"}, scope.update...), then(append([]string{readOnlyID}, noCreates...)...)...),
					append(append([]string{"--json"}, scope.update...), then(append([]string{readOnlyID}, noCreates...)...)...),
				} {
					output, err := executeLiveCLI(t, args...)
					assertUnsupportedOn(t, release, err, output)
				}

				// The update refused before it replaced anything.
				assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, append(scope.get, then(readOnlyID)...)...)),
					storedRestriction{scope: scopeOfRestriction(scope.keyed), restrictionType: "read-only", matcherType: "PATTERN", matcherID: "refs/heads/ro-*"})
				if listed := restrictionsMatching(t, mustLiveCLI(t, append(scope.list, then("--type", "no-creates")...)...), "read-only", "refs/heads/ro-*"); len(listed) != 0 {
					t.Errorf("a listing filtered by no-creates returned the read-only restriction: %v", listed)
				}
				if all := restrictionsMatching(t, mustLiveCLI(t, append(scope.list, then()...)...), "read-only", "refs/heads/ro-*"); len(all) != 1 {
					t.Errorf("want the read-only restriction alone in the unfiltered listing, got %v", all)
				}

				return
			}

			createdID := restrictionID(t, mustLiveCLI(t, append(scope.create, then(noCreates...)...)...))
			assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, append(scope.get, then(createdID)...)...)),
				storedRestriction{scope: scopeOfRestriction(scope.keyed), restrictionType: "no-creates", matcherType: "PATTERN", matcherID: "refs/heads/nc-*"})

			// Filtered, the listing holds the no-creates restriction and not the
			// read-only one.
			filtered := mustLiveCLI(t, append(scope.list, then("--type", "no-creates")...)...)
			if found := restrictionsMatching(t, filtered, "no-creates", "refs/heads/nc-*"); len(found) != 1 {
				t.Errorf("a listing filtered by no-creates holds %d no-creates restrictions on nc-*, want 1:\n%s", len(found), filtered)
			}
			if found := restrictionsMatching(t, filtered, "read-only", "refs/heads/ro-*"); len(found) != 0 {
				t.Errorf("a listing filtered by no-creates returned the read-only restriction:\n%s", filtered)
			}

			// An update to the type makes a no-creates restriction on the
			// read-only one's branches, and the read-only one is gone.
			updatedID := restrictionID(t, mustLiveCLI(t, append(scope.update,
				then(readOnlyID, "--type", "no-creates", "--matcher-type", "PATTERN", "--matcher-id", "refs/heads/ro-*")...)...))
			assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, append(scope.get, then(updatedID)...)...)),
				storedRestriction{scope: scopeOfRestriction(scope.keyed), restrictionType: "no-creates", matcherType: "PATTERN", matcherID: "refs/heads/ro-*"})
			if left := restrictionsMatching(t, mustLiveCLI(t, append(scope.list, then()...)...), "read-only", "refs/heads/ro-*"); len(left) != 0 {
				t.Errorf("the update left the read-only restriction behind: %v", left)
			}
		})
	}
}

// scopeOfRestriction is the scope Bitbucket reports for a restriction made
// through a project's commands or a repository's.
func scopeOfRestriction(project bool) string {
	if project {
		return "PROJECT"
	}

	return "REPOSITORY"
}
