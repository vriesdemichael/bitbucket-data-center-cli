//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/completion"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveCompletionVocabulary is what makes the two vocabularies in
// source_vocabulary.go that belong to Bitbucket rather than to bb worth
// trusting.
//
// A token permission and a webhook event key are both validated by the server
// and by nothing else: RestAccessTokenRequest types permissions as a bare
// []string, and there is no endpoint that lists the webhook events at all. So
// the only way to know the completion offers a value the command can use is to
// use it -- create the thing, then read it back with a separate request and
// find the value on it.
//
// That the server validates is the point. `--event bb:not_an_event` is refused
// with "the event ${validatedValue} is unknown", and a permission it does not
// know with "ACCOUNT_READ is not a valid permission", so a plausible-looking
// value offered here does not merely go unused: it fails the whole call. Both
// refusals are asserted below, because a list checked only for acceptance
// would pass just as well against a server that accepted anything.
//
// Transcribing the documentation would not have done. Atlassian's event
// payload page names pr:to_ref_updated and pr:reviewer:changes_requested, and
// the instance rejects both.
func TestLiveCompletionVocabulary(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	selector := seeded.Key + "/" + repo.Slug

	t.Run("every webhook event completion offers is accepted and stored", func(t *testing.T) {
		offered := completeLiveValues(t, "webhook", "create", "--repo", selector, "--event", "")
		if len(offered) != len(completion.WebhookEvents) {
			t.Fatalf("completion offered %d events, the vocabulary holds %d: %v",
				len(offered), len(completion.WebhookEvents), offered)
		}

		for _, event := range offered {
			name := testsupport.UniqueName("lt-vocab-wh-")
			output, err := executeLiveCLI(t, "--json", "webhook", "create", name,
				"http://localhost:7990/status", "--event", event)
			if err != nil {
				t.Errorf("Bitbucket refused --event %s, which completion offers: %v\noutput: %s", event, err, output)
				continue
			}

			webhookID, ok := webhookIDFromCreateOutput(output)
			if !ok {
				t.Errorf("--event %s: no webhook id in the create output: %s", event, output)
				continue
			}

			// A separate read, not the create's own answer: a 2xx says the
			// request was accepted, and a field Bitbucket ignored looks
			// exactly the same on the way out as one it stored.
			stored, err := harness.liveJSON(ctx, "GET",
				fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/webhooks/%s", seeded.Key, repo.Slug, webhookID), nil)
			if err != nil {
				t.Errorf("--event %s: reading the webhook back failed: %v", event, err)
			} else if events := stringsFrom(stored["events"]); !containsValue(events, event) {
				t.Errorf("--event %s was accepted but the webhook came back subscribed to %v", event, events)
			}

			if _, err := executeLiveCLI(t, "--json", "webhook", "delete", webhookID, "--yes"); err != nil {
				t.Errorf("cleaning up the webhook for %s failed: %v", event, err)
			}
		}
	})

	t.Run("an event key completion does not offer is refused", func(t *testing.T) {
		name := testsupport.UniqueName("lt-vocab-wh-bad-")
		output, err := executeLiveCLI(t, "--json", "webhook", "create", name,
			"http://localhost:7990/status", "--event", "bb:not_an_event")
		if err == nil {
			t.Fatalf("Bitbucket accepted an invented event key, so this vocabulary guards nothing: %s", output)
		}
	})

	t.Run("every user-scoped token permission completion offers is accepted and stored", func(t *testing.T) {
		user := harness.username()

		offered := completeLiveValues(t, "auth", "token", "create", "--user", user, "--permission", "")
		if len(offered) == 0 {
			t.Fatal("completion offered no token permission")
		}

		for _, permission := range offered {
			assertTokenPermissionAccepted(ctx, t, harness,
				fmt.Sprintf("/rest/access-tokens/latest/users/%s", user), permission,
				"--user", user)
		}
	})

	t.Run("a repository-scoped token takes the three completion narrows to", func(t *testing.T) {
		offered := completeLiveValues(t, "auth", "token", "create", "--repo", selector, "--permission", "")
		if len(offered) != len(completion.TokenRepositoryPermissions) {
			t.Fatalf("a repository-scoped token was offered %v; the narrowed set is %v",
				offered, completion.TokenRepositoryPermissions)
		}

		for _, permission := range offered {
			assertTokenPermissionAccepted(ctx, t, harness,
				fmt.Sprintf("/rest/access-tokens/latest/projects/%s/repos/%s", seeded.Key, repo.Slug), permission,
				"--repo", selector)
		}
	})

	t.Run("a project permission on a repository-scoped token is refused", func(t *testing.T) {
		// The reason the set above is narrowed. Without this the narrowing
		// could be cosmetic -- three values withheld from a server that would
		// have taken all six.
		name := testsupport.UniqueName("lt-vocab-token-bad-")
		output, err := executeLiveCLIUnscoped(t, "--json", "auth", "token", "create", name,
			"--repo", selector, "--permission", "PROJECT_READ", "--expiry-days", "1")
		if err == nil {
			t.Fatalf("a repository-scoped token took PROJECT_READ, so withholding the project permissions is wrong: %s", output)
		}
	})
}

// assertTokenPermissionAccepted creates a token holding one permission, reads
// it back through a request of its own and revokes it.
//
// Reading back matters more here than anywhere: the create response is the
// only time the secret is returned, and a caller reading the permissions off
// that same answer would be reading what they sent.
func assertTokenPermissionAccepted(
	ctx context.Context,
	t *testing.T,
	harness *liveHarness,
	scopePath string,
	permission string,
	scope ...string,
) {
	t.Helper()

	name := testsupport.UniqueName("lt-vocab-token-")
	args := append([]string{"--json", "auth", "token", "create", name}, scope...)
	args = append(args, "--permission", permission, "--expiry-days", "1")

	output, err := executeLiveCLIUnscoped(t, args...)
	if err != nil {
		t.Errorf("Bitbucket refused --permission %s, which completion offers: %v\noutput: %s", permission, err, output)

		return
	}

	created := decodeJSONMap(t, output)
	tokenID, ok := numericOrStringID(created["id"])
	if !ok {
		t.Errorf("--permission %s: no token id in the create output: %s", permission, output)

		return
	}

	defer func() {
		if _, err := harness.liveJSON(ctx, "DELETE", scopePath+"/"+tokenID, nil); err != nil {
			t.Errorf("revoking the token for %s failed: %v", permission, err)
		}
	}()

	stored, err := harness.liveJSON(ctx, "GET", scopePath+"/"+tokenID, nil)
	if err != nil {
		t.Errorf("--permission %s: reading the token back failed: %v", permission, err)

		return
	}

	if held := stringsFrom(stored["permissions"]); !containsValue(held, permission) {
		t.Errorf("--permission %s was accepted but the token came back holding %v", permission, held)
	}
}

// completeLiveValues is completeLive without the descriptions, in no
// particular order -- these assertions are about which values are offered.
func completeLiveValues(t *testing.T, words ...string) []string {
	t.Helper()

	candidates, _ := completeLive(t, words...)

	values := make([]string, 0, len(candidates))
	for value := range candidates {
		values = append(values, value)
	}

	return values
}

func stringsFrom(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}

	collected := make([]string, 0, len(items))
	for _, item := range items {
		if text, ok := item.(string); ok {
			collected = append(collected, strings.TrimSpace(text))
		}
	}

	return collected
}

func containsValue(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}
