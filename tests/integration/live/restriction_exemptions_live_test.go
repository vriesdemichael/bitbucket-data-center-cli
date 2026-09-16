//go:build live

package live_test

import (
	"context"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveRestrictionExemptions covers the users, groups and SSH access keys a
// branch restriction exempts, for a repository and for a project.
//
// Bitbucket takes users by name and access keys by id. The specification
// declares both as objects and bb sent what it declared: a user answered 400
// "not a valid user" and an access key 500, so --user and --access-key-id had
// never worked, and no live test had passed either (OPENAPI-031).
func TestLiveRestrictionExemptions(t *testing.T) {
	t.Parallel()

	// The command words are literals in the table and every call spreads one of
	// them through append, which is the shape tools/command-reach can read.
	type scope struct {
		name              string
		create, get, list []string
		// keyed says a project key follows the command words.
		keyed bool
		// restrictionScope is the scope Bitbucket reports for a restriction
		// created through these commands.
		restrictionScope string
		// accessKeyScope names where an SSH access key this scope's restriction
		// can exempt is added.
		accessKeyScope string
		// writePermission is what Bitbucket calls read-write access in that
		// scope.
		writePermission string
	}

	scopes := []scope{
		{
			name:             "repository",
			create:           []string{"branch", "restriction", "create"},
			get:              []string{"branch", "restriction", "get"},
			list:             []string{"branch", "restriction", "list"},
			restrictionScope: "REPOSITORY",
			accessKeyScope:   "--repo",
			writePermission:  "REPO_WRITE",
		},
		{
			name:             "project",
			create:           []string{"project", "branch-restriction", "create"},
			get:              []string{"project", "branch-restriction", "get"},
			list:             []string{"project", "branch-restriction", "list"},
			keyed:            true,
			restrictionScope: "PROJECT",
			accessKeyScope:   "--project",
			writePermission:  "PROJECT_WRITE",
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
			accessKeyTarget := seeded.Key + "/" + seeded.Repos[0].Slug
			if scope.keyed {
				accessKeyTarget = seeded.Key
			}

			label := testsupport.UniqueName("restriction-exempt-")
			publicKey := generateSSHPublicKey(t, label)
			// Unscoped: the key's scope is named explicitly, and an injected --repo
			// would contradict a --project. Read-write, because read-only is what
			// bb asks for when no permission is named.
			added := decodeJSONMap(t, mustLiveCLIUnscoped(t, append([]string{"repo", "ssh-key", "add"},
				publicKey, "--label", label, "--read-write", scope.accessKeyScope, accessKeyTarget)...))
			keyObject, ok := added["key"].(map[string]any)
			if !ok {
				keyObject = added
			}
			accessKeyID, ok := numericOrStringID(keyObject["id"])
			if !ok {
				t.Fatalf("no key id in the add output: %v", added)
			}

			var keys struct {
				Keys []struct {
					ID         int32  `json:"id"`
					Label      string `json:"label"`
					Text       string `json:"text"`
					Permission string `json:"permission"`
				} `json:"keys"`
			}
			listedKeys := mustLiveCLIUnscoped(t, append([]string{"repo", "ssh-key", "list"}, scope.accessKeyScope, accessKeyTarget)...)
			if err := decodeJSONEnvelopeData(listedKeys, &keys); err != nil {
				t.Fatalf("repo ssh-key list returned invalid JSON: %v\n%s", err, listedKeys)
			}
			keyListed := false
			for _, key := range keys.Keys {
				if strconv.Itoa(int(key.ID)) != accessKeyID {
					continue
				}
				keyListed = true
				if key.Label != label || key.Permission != scope.writePermission || key.Text != publicKey {
					t.Errorf("access key %s is stored as %q with %s and text %q, want %q with %s and text %q",
						accessKeyID, key.Label, key.Permission, key.Text, label, scope.writePermission, publicKey)
				}
			}
			if !keyListed {
				t.Fatalf("access key %s is not in the listing:\n%s", accessKeyID, listedKeys)
			}

			exempting := []string{"--type", "read-only", "--matcher-type", "PATTERN", "--matcher-id", "refs/heads/release/*",
				"--user", "admin", "--group", "stash-users", "--access-key-id", accessKeyID}
			id := restrictionID(t, mustLiveCLI(t, append(scope.create, then(exempting...)...)...))

			stored := restrictionPayload(t, mustLiveCLI(t, append(scope.get, then(id)...)...))
			if !restrictionExempts(stored, "admin") {
				t.Errorf("restriction %s does not exempt admin: %v", id, stored["users"])
			}
			if groups, _ := stored["groups"].([]any); len(groups) != 1 || groups[0] != "stash-users" {
				t.Errorf("groups = %v, want [stash-users]", stored["groups"])
			}
			if !restrictionExemptsAccessKey(stored, accessKeyID) {
				t.Errorf("restriction %s does not exempt access key %s: %v", id, accessKeyID, stored["accessKeys"])
			}
			assertRestrictionStored(t, stored, storedRestriction{
				scope: scope.restrictionScope, restrictionType: "read-only", matcherType: "PATTERN", matcherID: "refs/heads/release/*",
				users: []string{"admin"}, groups: []string{"stash-users"},
			})
			if exempted, _ := stored["accessKeys"].([]any); len(exempted) != 1 {
				t.Errorf("restriction %s exempts %d access keys, want only %s: %v", id, len(exempted), accessKeyID, stored["accessKeys"])
			}

			// Bitbucket answers a get for a restriction id through any project's
			// or repository's path, so the get cannot show where the restriction
			// was stored. The scope's own listing can.
			listed := restrictionsMatching(t, mustLiveCLI(t, append(scope.list, then()...)...), "read-only", "refs/heads/release/*")
			if len(listed) != 1 {
				t.Fatalf("want restriction %s alone on release/* in the %s listing, got %d: %v", id, scope.name, len(listed), listed)
			}
			if got, _ := numericOrStringID(listed[0]["id"]); got != id {
				t.Errorf("the %s listing holds restriction %s on release/*, the create answered with %s", scope.name, got, id)
			}
		})
	}
}

func restrictionPayload(t *testing.T, output string) map[string]any {
	t.Helper()

	restriction, ok := decodeJSONMap(t, output)["restriction"].(map[string]any)
	if !ok {
		t.Fatalf("no restriction object in the output: %s", output)
	}
	return restriction
}

func restrictionID(t *testing.T, output string) string {
	t.Helper()

	id, ok := numericOrStringID(restrictionPayload(t, output)["id"])
	if !ok {
		t.Fatalf("no restriction id in the output: %s", output)
	}
	return id
}

func restrictionExempts(restriction map[string]any, user string) bool {
	users, _ := restriction["users"].([]any)
	for _, entry := range users {
		if fields, ok := entry.(map[string]any); ok && (fields["name"] == user || fields["slug"] == user) {
			return true
		}
	}
	return false
}

// storedRestriction is a branch restriction as a test expects Bitbucket to hold
// it.
type storedRestriction struct {
	// scope is PROJECT or REPOSITORY, which says which of the two endpoints
	// created the restriction.
	scope                                   string
	restrictionType, matcherType, matcherID string
	// users and groups are the exemptions exactly: one Bitbucket holds that is
	// not named here is as wrong as one it dropped.
	users, groups []string
}

// assertRestrictionStored compares a restriction read back from Bitbucket with
// the one that was sent, field by field.
func assertRestrictionStored(t *testing.T, restriction map[string]any, want storedRestriction) {
	t.Helper()

	matcher, _ := restriction["matcher"].(map[string]any)
	if restriction["scope"] != want.scope || restriction["type"] != want.restrictionType || matcher["type"] != want.matcherType || matcher["id"] != want.matcherID {
		t.Errorf("restriction %v is a %v %v on %v %v, want a %s %s on %s %s", restriction["id"], restriction["scope"],
			restriction["type"], matcher["type"], matcher["id"], want.scope, want.restrictionType, want.matcherType, want.matcherID)
	}

	var users []string
	entries, _ := restriction["users"].([]any)
	for _, entry := range entries {
		fields, _ := entry.(map[string]any)
		users = append(users, asString(fields["name"]))
	}
	slices.Sort(users)
	if wantUsers := slices.Sorted(slices.Values(want.users)); !slices.Equal(users, wantUsers) {
		t.Errorf("restriction %v exempts users %v, want %v", restriction["id"], users, wantUsers)
	}

	var groups []string
	names, _ := restriction["groups"].([]any)
	for _, name := range names {
		groups = append(groups, asString(name))
	}
	slices.Sort(groups)
	if wantGroups := slices.Sorted(slices.Values(want.groups)); !slices.Equal(groups, wantGroups) {
		t.Errorf("restriction %v exempts groups %v, want %v", restriction["id"], groups, wantGroups)
	}
}

func restrictionExemptsAccessKey(restriction map[string]any, keyID string) bool {
	keys, _ := restriction["accessKeys"].([]any)
	for _, entry := range keys {
		if fields, ok := entry.(map[string]any); ok {
			if id, ok := numericOrStringID(fields["id"]); ok && id == keyID {
				return true
			}
		}
	}
	return false
}
