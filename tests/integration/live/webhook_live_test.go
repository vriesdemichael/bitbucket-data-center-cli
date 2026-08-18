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
		name, "http://localhost:65535/hook", "--event", "repo:refs_changed")
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

	renamed := name + "-renamed"
	updateOutput, err := executeLiveCLI(t, "--json", "webhook", "update", webhookID, "--name", renamed)
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

	// The endpoint is what is under test, not whether the unreachable URL
	// answers: bb must report the server's result rather than fail on it.
	if _, err := executeLiveCLI(t, "--json", "webhook", "test", webhookID); err != nil {
		t.Fatalf("webhook test failed: %v", err)
	}

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
		seeded.Key, name, "http://localhost:65535/project-hook", "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("project webhook create failed: %v\noutput: %s", err, createOutput)
	}

	webhookID, ok := webhookIDFromCreateOutput(createOutput)
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
	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "update", seeded.Key, webhookID, "--name", renamed); err != nil {
		t.Fatalf("project webhook update failed: %v", err)
	}

	afterUpdate, err := executeLiveCLI(t, "--json", "project", "webhook", "list", seeded.Key, "--limit", "50")
	if err != nil {
		t.Fatalf("project webhook list after update failed: %v\noutput: %s", err, afterUpdate)
	}
	if !strings.Contains(afterUpdate, renamed) {
		t.Fatalf("expected the rename to persist, got: %s", afterUpdate)
	}

	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "test", seeded.Key, webhookID); err != nil {
		t.Fatalf("project webhook test failed: %v", err)
	}

	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "stats", seeded.Key, webhookID, "--summary"); err != nil {
		t.Fatalf("project webhook stats failed: %v", err)
	}

	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "delete", seeded.Key, webhookID); err != nil {
		t.Fatalf("project webhook delete failed: %v", err)
	}
}
