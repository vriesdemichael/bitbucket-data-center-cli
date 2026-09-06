//go:build live

package live_test

import (
	"context"
	"os"
	"path/filepath"
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

	seeded, err := harness.seedIsolatedProject(ctx, 1, 3)
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

// TestLiveApiRequestConstruction covers the request `bb api` builds, against a
// server that has to accept it.
//
// The unit tests these replace read the request off a mock that had just
// received it and asserted its shape. That proves bb sent what the test author
// expected, which is a weaker claim than it looks: the shape being right is
// exactly the thing in question, and the recorder agrees with whatever it is
// given. Here the server accepting the request, and the change showing up
// afterwards, is the assertion.
func TestLiveApiRequestConstruction(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	projectPath := "/rest/api/latest/projects/" + seeded.Key

	t.Run("a path mangled by the shell still reaches the endpoint", func(t *testing.T) {
		// MSYS2 rewrites a leading /rest/... into a Windows path before bb sees
		// the argument. Recovering the string is one thing; the recovered path
		// resolving to the endpoint the caller meant is the claim worth making,
		// and only the server can settle it.
		mangled := "/C:/Program Files/Git" + projectPath

		output := mustLiveCLI(t, "api", mangled)
		if !strings.Contains(output, `"key": "`+seeded.Key+`"`) {
			t.Fatalf("the recovered path did not reach project %s:\n%s", seeded.Key, output)
		}
		// The recovery is silent otherwise, and a caller who does not know
		// their shell rewrote the argument cannot know why it worked.
		if !strings.Contains(output, "MSYS_NO_PATHCONV=1") {
			t.Errorf("expected a warning naming the fix, got:\n%s", output)
		}
	})

	t.Run("a body from a file is sent as the request body", func(t *testing.T) {
		bodyFile := filepath.Join(t.TempDir(), "body.json")
		if err := os.WriteFile(bodyFile, []byte(`{"description":"set from a file"}`), 0o600); err != nil {
			t.Fatalf("write the body file: %v", err)
		}

		mustLiveCLI(t, "api", projectPath, "-X", "PUT", "--input", bodyFile)

		if description, _ := decodeJSONMap(t, mustLiveCLI(t, "api", projectPath))["description"].(string); description != "set from a file" {
			t.Fatalf("the request body did not take, description = %q", description)
		}
	})

	t.Run("a body from stdin is sent as the request body", func(t *testing.T) {
		output, err := executeLiveCLIWithStdin(t, `{"description":"set from stdin"}`,
			"api", projectPath, "-X", "PUT", "--input", "-")
		if err != nil {
			t.Fatalf("api with a stdin body failed: %v\noutput: %s", err, output)
		}

		if description, _ := decodeJSONMap(t, mustLiveCLI(t, "api", projectPath))["description"].(string); description != "set from stdin" {
			t.Fatalf("the stdin body did not take, description = %q", description)
		}
	})

	t.Run("a field read from a file becomes part of the payload", func(t *testing.T) {
		fieldFile := filepath.Join(t.TempDir(), "description.txt")
		if err := os.WriteFile(fieldFile, []byte("set from a field file"), 0o600); err != nil {
			t.Fatalf("write the field file: %v", err)
		}

		mustLiveCLI(t, "api", projectPath, "-X", "PUT", "-F", "description=@"+fieldFile)

		if description, _ := decodeJSONMap(t, mustLiveCLI(t, "api", projectPath))["description"].(string); description != "set from a field file" {
			t.Fatalf("the @file field did not take, description = %q", description)
		}
	})

	t.Run("a custom header reaches the server and is honoured", func(t *testing.T) {
		// A mock recording the header proves bb wrote it down. Whether the
		// server acted on it is the part that matters, so the test asks for a
		// representation Bitbucket does not offer here: the same call succeeds
		// without the header and is refused with it.
		mustLiveCLI(t, "api", projectPath)

		output, err := executeLiveCLI(t, "api", projectPath, "-H", "Accept: application/xml")
		if err == nil {
			t.Fatalf("expected Accept: application/xml to be refused, got:\n%s", output)
		}
		if !strings.Contains(err.Error(), "406") {
			t.Fatalf("expected a 406 proving the header was applied, got: %v", err)
		}
	})

	t.Run("a dry run refuses to mutate and changes nothing", func(t *testing.T) {
		before := mustLiveCLI(t, "api", projectPath)

		if output, err := executeLiveCLI(t, "--dry-run", "api", projectPath, "-X", "DELETE"); err == nil {
			t.Fatalf("expected a dry run to refuse a mutating passthrough, got:\n%s", output)
		}

		// A GET is safe and must still work under --dry-run.
		if output, err := executeLiveCLI(t, "--dry-run", "api", projectPath); err != nil {
			t.Fatalf("a dry run must still allow a read: %v\noutput: %s", err, output)
		}

		if after := mustLiveCLI(t, "api", projectPath); after != before {
			t.Fatalf("the dry run changed the project\nbefore: %s\nafter:  %s", before, after)
		}
	})
}
