//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveRestrictionUpdateNeverLeavesTheBranchUnprotected covers what an
// update does to the restriction it replaces.
//
// Bitbucket has no endpoint that updates one restriction, and its create is an
// upsert keyed by type and matcher: a second create with the same pair replaces
// that restriction's exemptions and answers with its id (OPENAPI-032). bb used
// to delete the old restriction first and create the new one after, so any
// refusal of the create -- a user name that does not exist is enough -- left
// the branch with no restriction at all.
func TestLiveRestrictionUpdateNeverLeavesTheBranchUnprotected(t *testing.T) {
	t.Parallel()

	// The command words are literals in the table and every call spreads one of
	// them through append, which is the shape tools/command-reach can read.
	type scope struct {
		name                      string
		create, get, update, list []string
		// keyed says a project key follows the command words.
		keyed bool
		// restrictionScope is the scope Bitbucket reports for a restriction
		// created through these commands.
		restrictionScope string
	}

	scopes := []scope{
		{
			name:             "repository",
			create:           []string{"branch", "restriction", "create"},
			get:              []string{"branch", "restriction", "get"},
			update:           []string{"branch", "restriction", "update"},
			list:             []string{"branch", "restriction", "list"},
			restrictionScope: "REPOSITORY",
		},
		{
			name:             "project",
			create:           []string{"project", "branch-restriction", "create"},
			get:              []string{"project", "branch-restriction", "get"},
			update:           []string{"project", "branch-restriction", "update"},
			list:             []string{"project", "branch-restriction", "list"},
			keyed:            true,
			restrictionScope: "PROJECT",
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

			// then is what follows the command words: the project key where the
			// scope takes one, and the rest after it.
			then := func(rest ...string) []string {
				if scope.keyed {
					return append([]string{seeded.Key}, rest...)
				}
				return rest
			}

			release := []string{"--type", "read-only", "--matcher-type", "PATTERN", "--matcher-id", "refs/heads/release/*"}
			id := restrictionID(t, mustLiveCLI(t, append(scope.create, then(append(release, "--user", "admin")...)...)...))
			assertRestrictionStored(t, restrictionPayload(t, mustLiveCLI(t, append(scope.get, then(id)...)...)), storedRestriction{
				scope: scope.restrictionScope, restrictionType: "read-only", matcherType: "PATTERN", matcherID: "refs/heads/release/*", users: []string{"admin"},
			})

			t.Run("a refused update keeps the restriction it would have replaced", func(t *testing.T) {
				missingUser := testsupport.UniqueName("no-such-user-")
				refused := append(append([]string{id}, release...), "--user", missingUser)
				output, err := executeLiveCLI(t, append(scope.update, then(refused...)...)...)
				if err == nil {
					t.Fatalf("an update naming a user that does not exist succeeded:\n%s", output)
				}
				// Bitbucket's refusal of the create, not a check bb made before
				// sending it: only a create the server refused shows what that
				// refusal leaves behind.
				if status := apperrors.DetailsOf(err)["upstreamStatus"]; status != "400" || !strings.Contains(err.Error(), missingUser) {
					t.Errorf("the update failed with upstream status %q and %v, want Bitbucket's 400 naming %s", status, err, missingUser)
				}

				kept := restrictionPayload(t, mustLiveCLI(t, append(scope.get, then(id)...)...))
				if !restrictionExempts(kept, "admin") {
					t.Errorf("restriction %s no longer exempts admin after a refused update: %v", id, kept)
				}
				assertRestrictionStored(t, kept, storedRestriction{
					scope: scope.restrictionScope, restrictionType: "read-only", matcherType: "PATTERN", matcherID: "refs/heads/release/*", users: []string{"admin"},
				})
			})

			t.Run("an update of the same type and matcher leaves one restriction", func(t *testing.T) {
				regrouped := append(append([]string{id}, release...), "--group", "stash-users")
				updated := restrictionID(t, mustLiveCLI(t, append(scope.update, then(regrouped...)...)...))

				matching := restrictionsMatching(t, mustLiveCLI(t, append(scope.list, then()...)...), "read-only", "refs/heads/release/*")
				if len(matching) != 1 {
					t.Fatalf("want exactly one read-only restriction on release/*, got %d: %v", len(matching), matching)
				}
				if got, _ := numericOrStringID(matching[0]["id"]); got != updated {
					t.Errorf("the remaining restriction is %s, the update answered with %s", got, updated)
				}
				if got, _ := numericOrStringID(matching[0]["id"]); got != id {
					t.Errorf("the remaining restriction is %s; one that keeps its type and matcher keeps its id, %s", got, id)
				}
				if groups, _ := matching[0]["groups"].([]any); len(groups) != 1 || groups[0] != "stash-users" {
					t.Errorf("groups = %v, want [stash-users]", matching[0]["groups"])
				}
				// No users: the upsert replaces every exemption, and admin was not
				// named again.
				assertRestrictionStored(t, matching[0], storedRestriction{
					scope: scope.restrictionScope, restrictionType: "read-only", matcherType: "PATTERN", matcherID: "refs/heads/release/*", groups: []string{"stash-users"},
				})
				id = updated
			})

			t.Run("an update to another matcher removes the old restriction", func(t *testing.T) {
				moved := []string{id, "--type", "read-only", "--matcher-type", "PATTERN", "--matcher-id", "refs/heads/hotfix/*", "--group", "stash-users"}
				mustLiveCLI(t, append(scope.update, then(moved...)...)...)

				listed := mustLiveCLI(t, append(scope.list, then()...)...)
				if old := restrictionsMatching(t, listed, "read-only", "refs/heads/release/*"); len(old) != 0 {
					t.Errorf("the restriction on release/* survived an update that moved it: %v", old)
				}
				if now := restrictionsMatching(t, listed, "read-only", "refs/heads/hotfix/*"); len(now) != 1 {
					t.Errorf("want one read-only restriction on hotfix/*, got %d: %v", len(now), now)
				} else {
					assertRestrictionStored(t, now[0], storedRestriction{
						scope: scope.restrictionScope, restrictionType: "read-only", matcherType: "PATTERN", matcherID: "refs/heads/hotfix/*", groups: []string{"stash-users"},
					})
				}
			})
		})
	}
}

// restrictionsMatching lists the restrictions of one type on one matcher, from
// a list command's JSON output.
func restrictionsMatching(t *testing.T, output, restrictionType, matcherID string) []map[string]any {
	t.Helper()

	var data any
	if err := decodeJSONEnvelopeData(output, &data); err != nil {
		t.Fatalf("list returned invalid JSON: %v\n%s", err, output)
	}

	var found []map[string]any
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			matcher, isRestriction := typed["matcher"].(map[string]any)
			if isRestriction && typed["type"] == restrictionType && matcher["id"] == matcherID {
				found = append(found, typed)
				return
			}
			for _, child := range typed {
				walk(child)
			}
		case []any:
			for _, child := range typed {
				walk(child)
			}
		}
	}
	walk(data)
	return found
}
