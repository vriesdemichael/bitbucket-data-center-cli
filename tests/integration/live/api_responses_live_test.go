//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// What `bb api` does with what comes back, against a real server.
//
// The passthrough is deliberately thin, so most of its unit tests are about the
// request it builds -- which host, which path, which fields become query
// parameters -- and those assume nothing about Bitbucket. The response half
// does: pagination follows Bitbucket's isLastPage and nextPageStart convention,
// an error body has a shape, a 204 carries nothing to decode. Those were mocks
// describing what the author believed, and they are what moves here.
func TestLiveApiResponseHandling(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 3)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	t.Run("--paginate follows the pages Bitbucket advertises", func(t *testing.T) {
		// A page size of one forces several round trips, so following the
		// convention is what decides whether everything comes back.
		path := "/rest/api/latest/projects/" + seeded.Key + "/repos/" + repo.Slug + "/commits?limit=1"

		single := mustLiveCLI(t, "api", path)
		all := mustLiveCLI(t, "api", path, "--paginate")

		if len(all) <= len(single) {
			t.Fatalf("--paginate returned no more than one page\none page: %d bytes\nall: %d bytes", len(single), len(all))
		}
	})

	t.Run("an error body comes back as a failure, not as data", func(t *testing.T) {
		output, err := executeLiveCLI(t, "--json", "api",
			"/rest/api/latest/projects/"+seeded.Key+"/repos/no-such-repository")
		if err == nil {
			t.Fatalf("expected a missing repository to fail, got:\n%s", output)
		}
		// The passthrough must not swallow the server's explanation.
		if !strings.Contains(err.Error()+output, "no-such-repository") &&
			!strings.Contains(strings.ToLower(err.Error()+output), "not found") {
			t.Errorf("expected the server's error to survive, got: %v\noutput: %s", err, output)
		}
	})

	t.Run("a body-less response is not an error", func(t *testing.T) {
		// Watching a repository answers 204 with nothing in it. A passthrough
		// that insisted on JSON would report success as a decode failure.
		path := "/rest/api/latest/projects/" + seeded.Key + "/repos/" + repo.Slug + "/watch"

		if output, err := executeLiveCLI(t, "api", path, "-X", "POST"); err != nil {
			t.Fatalf("an empty response body must not fail: %v\noutput: %s", err, output)
		}
		if output, err := executeLiveCLI(t, "api", path, "-X", "DELETE"); err != nil {
			t.Fatalf("an empty response body must not fail: %v\noutput: %s", err, output)
		}
	})

	t.Run("a typed field becomes a query parameter on a GET", func(t *testing.T) {
		// -F alone makes the call a POST, the way gh does, so the method is
		// stated. On a GET the field has nowhere to go but the query string, and
		// the server applying it is what proves it arrived.
		output := mustLiveCLI(t, "api",
			"/rest/api/latest/projects/"+seeded.Key+"/repos/"+repo.Slug+"/commits",
			"-X", "GET", "-F", "limit=1")

		payload := decodeJSONMap(t, output)
		values, _ := payload["values"].([]any)
		if len(values) != 1 {
			t.Fatalf("expected limit=1 to reach the server, got %d values", len(values))
		}
	})
}

// The unauthenticated case is deliberately not here.
//
// Bitbucket answers an unauthenticated REST call with an HTML login page
// rather than JSON, and bb has to recognise that rather than report a parsing
// failure. It cannot be produced through this harness: clearing the credentials
// only makes the CLI fall back to the local defaults the harness supplies, so
// the call succeeds.
//
// Its unit test stays. The assertion there is what bb does when an HTML body
// arrives, not when Bitbucket chooses to send one -- our response handling,
// with the body supplied rather than claimed.
