//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLiveApiRawPassthrough(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// Test 1: GET /rest/api/1.0/projects
	output, err := executeLiveCLI(t, "--json", "api", "/rest/api/1.0/projects")
	if err != nil {
		t.Fatalf("bb api /rest/api/1.0/projects failed: %v\noutput: %s", err, output)
	}

	payload := decodeJSONMap(t, output)
	values, ok := payload["values"].([]any)
	if !ok || len(values) == 0 {
		t.Fatalf("expected non-empty values in projects list: %s", output)
	}

	// An endpoint given as the instance's own URL is resolved for that host and
	// still carries its credential. The seeded project is private, so only a
	// caller who is signed in gets it back.
	endpoint := strings.TrimRight(harness.config.BitbucketURL, "/") + "/rest/api/1.0/projects/" + seeded.Key
	output, err = executeLiveCLI(t, "--json", "api", endpoint)
	if err != nil {
		t.Fatalf("bb api %s failed: %v\noutput: %s", endpoint, err, output)
	}
	if key := decodeJSONMap(t, output)["key"]; key != seeded.Key {
		t.Fatalf("bb api %s answered project %v, want %s: %s", endpoint, key, seeded.Key, output)
	}
}
