//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestLiveRepositoryWebhookLifecycle covers bb webhook list, get, update, test
// and stats — the read and edit half of the repository webhook surface, none of
// which had ever run against a real Bitbucket.
//
// Creation is already covered elsewhere; this starts from a webhook it creates
// so the identifiers are real, then drives every uncovered verb against it.
func TestLiveRepositoryWebhookLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	name := fmt.Sprintf("live-webhook-%d", time.Now().UnixNano()%100000)
	createOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "create",
		name, "http://localhost:7990/status", "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("webhook create failed: %v\noutput: %s", err, createOutput)
	}

	webhookID, ok := webhookIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected a webhook id in the create output: %s", createOutput)
	}
	defer func() {
		_, _ = executeLiveCLI(t, "repo", "settings", "workflow", "webhooks", "delete", webhookID)
	}()

	listOutput, err := executeLiveCLI(t, "--json", "webhook", "list", "--limit", "50")
	if err != nil {
		t.Fatalf("webhook list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, name) {
		t.Fatalf("expected the created webhook in the listing, got: %s", listOutput)
	}

	getOutput, err := executeLiveCLI(t, "--json", "webhook", "get", webhookID)
	if err != nil {
		t.Fatalf("webhook get failed: %v\noutput: %s", err, getOutput)
	}
	if !strings.Contains(getOutput, name) {
		t.Fatalf("expected the webhook name in get output, got: %s", getOutput)
	}

	// The server requires url and events on every update, even when only the
	// name changes — a partial update is rejected outright. That is a real
	// limitation of bb update rather than of this test, and is filed separately.
	renamed := name + "-renamed"
	updateOutput, err := executeLiveCLI(t, "--json", "webhook", "update", webhookID,
		"--name", renamed, "--url", "http://localhost:7990/status", "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("webhook update failed: %v\noutput: %s", err, updateOutput)
	}

	// Read back rather than trusting the update response.
	afterUpdate, err := executeLiveCLI(t, "--json", "webhook", "get", webhookID)
	if err != nil {
		t.Fatalf("webhook get after update failed: %v\noutput: %s", err, afterUpdate)
	}
	if !strings.Contains(afterUpdate, renamed) {
		t.Fatalf("expected the rename to persist, got: %s", afterUpdate)
	}

	// bb webhook test is deliberately not asserted here. The request matches the
	// 10.2 spec — webhookId goes in the query string, and bb sends it — but the
	// server answers with an unhandled exception and a 500 whatever URL the
	// webhook points at. Asserting the failure would pin a server bug as if it
	// were intended behaviour, so the command stays uncovered and the finding is
	// filed instead.

	if _, err := executeLiveCLI(t, "--json", "webhook", "stats", webhookID, "--summary"); err != nil {
		t.Fatalf("webhook stats failed: %v", err)
	}
}

// TestLiveProjectWebhookLifecycle is the project-level twin: create, list, get
// via update, test, stats and delete.
func TestLiveProjectWebhookLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	name := fmt.Sprintf("live-project-webhook-%d", time.Now().UnixNano()%100000)
	createOutput, err := executeLiveCLI(t, "--json", "project", "webhook", "create",
		seeded.Key, name, "http://localhost:7990/status", "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("project webhook create failed: %v\noutput: %s", err, createOutput)
	}

	// The project variant returns the webhook flat in data, where the
	// repository one nests it under a webhook key. That inconsistency is filed
	// separately; this reads whichever shape is present.
	webhookID, ok := flatWebhookIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected a webhook id in the create output: %s", createOutput)
	}

	listOutput, err := executeLiveCLI(t, "--json", "project", "webhook", "list", seeded.Key, "--limit", "50")
	if err != nil {
		t.Fatalf("project webhook list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, name) {
		t.Fatalf("expected the created webhook in the listing, got: %s", listOutput)
	}

	renamed := name + "-renamed"
	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "update", seeded.Key, webhookID,
		"--name", renamed, "--url", "http://localhost:7990/status", "--event", "repo:refs_changed"); err != nil {
		t.Fatalf("project webhook update failed: %v", err)
	}

	afterUpdate, err := executeLiveCLI(t, "--json", "project", "webhook", "list", seeded.Key, "--limit", "50")
	if err != nil {
		t.Fatalf("project webhook list after update failed: %v\noutput: %s", err, afterUpdate)
	}
	if !strings.Contains(afterUpdate, renamed) {
		t.Fatalf("expected the rename to persist, got: %s", afterUpdate)
	}

	// Same server-side 500 as the repository variant; see the note there.

	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "stats", seeded.Key, webhookID, "--summary"); err != nil {
		t.Fatalf("project webhook stats failed: %v", err)
	}

	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "delete", seeded.Key, webhookID); err != nil {
		t.Fatalf("project webhook delete failed: %v", err)
	}
}

// flatWebhookIDFromCreateOutput reads an id from a payload that carries the
// webhook fields directly, which is what the project variant returns.
func flatWebhookIDFromCreateOutput(output string) (string, bool) {
	payload := map[string]any{}
	if err := unmarshalJSONObject(output, &payload); err != nil {
		return "", false
	}

	if nested, ok := payload["webhook"].(map[string]any); ok {
		return numericOrStringID(nested["id"])
	}

	return numericOrStringID(payload["id"])
}
