//go:build live

package live_test

import (
	"context"
	"testing"
	"time"
)

func TestLiveApiRawPassthrough(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
}
