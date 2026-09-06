//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLiveWebhookCreateAndDelete covers the top-level `bb webhook create` and
// `bb webhook delete`.
//
// Delete is the reason the pair exists: decommissioning a cluster leaves dead
// webhooks firing on every repository that pointed at it, and `webhook update
// --active false` leaves them in the listing, so the repository still reads as
// configured for two CI systems. The assertion that matters is therefore the
// last one — the webhook is gone from the list, not merely disabled.
func TestLiveWebhookCreateAndDelete(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	name := fmt.Sprintf("live-wh-create-%d", time.Now().UnixNano()%100000)
	createOutput, err := executeLiveCLI(t, "--json", "webhook", "create",
		name, "http://localhost:7990/status", "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("webhook create failed: %v\noutput: %s", err, createOutput)
	}

	webhookID, ok := webhookIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected a webhook id in the create output: %s", createOutput)
	}

	listOutput, err := executeLiveCLI(t, "--json", "webhook", "list", "--limit", "50")
	if err != nil {
		t.Fatalf("webhook list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, name) {
		t.Fatalf("expected the created webhook in the listing, got: %s", listOutput)
	}

	deleteOutput, err := executeLiveCLI(t, "--json", "webhook", "delete", webhookID)
	if err != nil {
		t.Fatalf("webhook delete failed: %v\noutput: %s", err, deleteOutput)
	}

	afterDelete, err := executeLiveCLI(t, "--json", "webhook", "list", "--limit", "50")
	if err != nil {
		t.Fatalf("webhook list after delete failed: %v\noutput: %s", err, afterDelete)
	}
	if strings.Contains(afterDelete, name) {
		t.Fatalf("expected the webhook to be gone from the listing after delete, got: %s", afterDelete)
	}
}
