//go:build live

package live_test

import (
	"context"
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
		name        string
		create, get []string
		// keyed says a project key follows the command words.
		keyed bool
		// accessKeyScope names where an SSH access key this scope's restriction
		// can exempt is added.
		accessKeyScope string
	}

	scopes := []scope{
		{
			name:           "repository",
			create:         []string{"branch", "restriction", "create"},
			get:            []string{"branch", "restriction", "get"},
			accessKeyScope: "--repo",
		},
		{
			name:           "project",
			create:         []string{"project", "branch-restriction", "create"},
			get:            []string{"project", "branch-restriction", "get"},
			keyed:          true,
			accessKeyScope: "--project",
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
			// Unscoped: the key's scope is named explicitly, and an injected --repo
			// would contradict a --project.
			added := decodeJSONMap(t, mustLiveCLIUnscoped(t, append([]string{"repo", "ssh-key", "add"},
				generateSSHPublicKey(t, label), "--label", label, "--read-only", scope.accessKeyScope, accessKeyTarget)...))
			keyObject, ok := added["key"].(map[string]any)
			if !ok {
				keyObject = added
			}
			accessKeyID, ok := numericOrStringID(keyObject["id"])
			if !ok {
				t.Fatalf("no key id in the add output: %v", added)
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
