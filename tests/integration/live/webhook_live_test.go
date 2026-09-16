//go:build live

package live_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLiveRepositoryWebhookLifecycle covers bb webhook list, get, update, test
// and stats — the read and edit half of the repository webhook surface, none of
// which had ever run against a real Bitbucket.
//
// Creation is already covered elsewhere; this starts from a webhook it creates
// so the identifiers are real, then drives every uncovered verb against it.
func TestLiveRepositoryWebhookLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A receiver, so where each test ping went is seen rather than reported;
	// the path tells the stored url from an override.
	pings, target := newWebhookPingReceiver(t)
	storedURL := target + "/stored"

	// Not bb's default event, so an --event that never arrived cannot read back
	// as one that did, and a username for the test pings to carry
	// (expectWebhookPing).
	name := testsupport.UniqueName("live-webhook-")
	createOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "create",
		name, storedURL, "--event", "pr:opened", "--credentials-username", "pinguser")
	if err != nil {
		t.Fatalf("webhook create failed: %v\noutput: %s", err, createOutput)
	}

	webhookID, ok := webhookIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected a webhook id in the create output: %s", createOutput)
	}
	defer func() {
		_, _ = executeLiveCLI(t, "repo", "settings", "workflow", "webhooks", "delete", webhookID, "--yes")
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
	created := webhookAsStored(t, webhookID)
	expectWebhookStoredAsSent(t, created, sentWebhook{name: name, url: storedURL, events: []string{"pr:opened"}})
	if created["credentialsUsername"] != "pinguser" {
		t.Errorf("credentialsUsername = %v, want pinguser", created["credentialsUsername"])
	}

	// A name-only update. The endpoint replaces the webhook rather than patching
	// it, so bb reads the current one and merges -- without that, this call is
	// rejected for the url and events it never mentioned, and any field the
	// server does not validate is silently cleared.
	renamed := name + "-renamed"
	updateOutput, err := executeLiveCLI(t, "--json", "webhook", "update", webhookID, "--name", renamed)
	if err != nil {
		t.Fatalf("webhook update with only a name failed: %v\noutput: %s", err, updateOutput)
	}

	// Read back rather than trusting the update response.
	afterUpdate, err := executeLiveCLI(t, "--json", "webhook", "get", webhookID)
	if err != nil {
		t.Fatalf("webhook get after update failed: %v\noutput: %s", err, afterUpdate)
	}
	if !strings.Contains(afterUpdate, renamed) {
		t.Fatalf("expected the rename to persist, got: %s", afterUpdate)
	}
	// The url and events the update never mentioned have to still be there.
	if !strings.Contains(afterUpdate, storedURL) {
		t.Fatalf("expected the url to survive a name-only update, got: %s", afterUpdate)
	}
	if !strings.Contains(afterUpdate, "pr:opened") {
		t.Fatalf("expected the events to survive a name-only update, got: %s", afterUpdate)
	}
	updated := webhookAsStored(t, webhookID)
	expectWebhookStoredAsSent(t, updated, sentWebhook{name: renamed, url: storedURL, events: []string{"pr:opened"}})
	if updated["credentialsUsername"] != "pinguser" {
		t.Errorf("credentialsUsername = %v after a name-only update, want pinguser", updated["credentialsUsername"])
	}

	// Fixed in this branch: bb now sends the webhook's url alongside webhookId,
	// which the server requires despite the spec marking it optional. Verified
	// directly — webhookId alone returns 500, webhookId with url returns 200.
	if _, err := executeLiveCLI(t, "--json", "webhook", "test", webhookID); err != nil {
		t.Fatalf("webhook test failed: %v", err)
	}
	expectWebhookPing(t, awaitWebhookPing(t, pings, target), "/stored", "pinguser")

	// The override is what the endpoint is documented for: testing connectivity
	// to a candidate url before saving it.
	if _, err := executeLiveCLI(t, "--json", "webhook", "test", webhookID, "--url", target+"/candidate"); err != nil {
		t.Fatalf("webhook test with an explicit url failed: %v", err)
	}
	expectWebhookPing(t, awaitWebhookPing(t, pings, target), "/candidate", "pinguser")

	if _, err := executeLiveCLI(t, "--json", "webhook", "stats", webhookID, "--summary"); err != nil {
		t.Fatalf("webhook stats failed: %v", err)
	}
}

// TestLiveProjectWebhookLifecycle is the project-level twin: create, list, get
// via update, test, stats and delete.
func TestLiveProjectWebhookLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	pings, target := newWebhookPingReceiver(t)
	storedURL := target + "/stored"

	// A non-default event and a username, as on the repository side.
	name := testsupport.UniqueName("live-project-webhook-")
	createOutput, err := executeLiveCLI(t, "--json", "project", "webhook", "create",
		seeded.Key, name, storedURL, "--event", "pr:opened", "--credentials-username", "pinguser")
	if err != nil {
		t.Fatalf("project webhook create failed: %v\noutput: %s", err, createOutput)
	}

	// Both create commands nest the webhook under the same key now. The test
	// used to read whichever shape turned up, which was the tell that they
	// disagreed.
	webhookID, ok := webhookIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected a nested webhook id in the create output: %s", createOutput)
	}

	listOutput, err := executeLiveCLI(t, "--json", "project", "webhook", "list", seeded.Key, "--limit", "50")
	if err != nil {
		t.Fatalf("project webhook list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, name) {
		t.Fatalf("expected the created webhook in the listing, got: %s", listOutput)
	}
	// The listing is the project scope's read: there is no project webhook get.
	created, found := webhookInListing(t, listOutput, webhookID)
	if !found {
		t.Fatalf("project webhook %s is not in the listing: %s", webhookID, listOutput)
	}
	expectWebhookStoredAsSent(t, created, sentWebhook{name: name, url: storedURL, events: []string{"pr:opened"}})
	if created["credentialsUsername"] != "pinguser" {
		t.Errorf("credentialsUsername = %v, want pinguser", created["credentialsUsername"])
	}

	// A name-only update, as on the repository side: the merge is what keeps the
	// url and events the caller never mentioned.
	renamed := name + "-renamed"
	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "update", seeded.Key, webhookID, "--name", renamed); err != nil {
		t.Fatalf("project webhook update with only a name failed: %v", err)
	}

	afterUpdate, err := executeLiveCLI(t, "--json", "project", "webhook", "list", seeded.Key, "--limit", "50")
	if err != nil {
		t.Fatalf("project webhook list after update failed: %v\noutput: %s", err, afterUpdate)
	}
	if !strings.Contains(afterUpdate, renamed) {
		t.Fatalf("expected the rename to persist, got: %s", afterUpdate)
	}
	if !strings.Contains(afterUpdate, storedURL) {
		t.Fatalf("expected the url to survive a name-only update, got: %s", afterUpdate)
	}
	if !strings.Contains(afterUpdate, "pr:opened") {
		t.Fatalf("expected the events to survive a name-only update, got: %s", afterUpdate)
	}
	updated, found := webhookInListing(t, afterUpdate, webhookID)
	if !found {
		t.Fatalf("project webhook %s is not in the listing after the update: %s", webhookID, afterUpdate)
	}
	expectWebhookStoredAsSent(t, updated, sentWebhook{name: renamed, url: storedURL, events: []string{"pr:opened"}})
	if updated["credentialsUsername"] != "pinguser" {
		t.Errorf("credentialsUsername = %v after a name-only update, want pinguser", updated["credentialsUsername"])
	}

	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "test", seeded.Key, webhookID); err != nil {
		t.Fatalf("project webhook test failed: %v", err)
	}
	expectWebhookPing(t, awaitWebhookPing(t, pings, target), "/stored", "pinguser")

	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "stats", seeded.Key, webhookID, "--summary"); err != nil {
		t.Fatalf("project webhook stats failed: %v", err)
	}

	if _, err := executeLiveCLI(t, "--json", "project", "webhook", "delete", seeded.Key, webhookID, "--yes"); err != nil {
		t.Fatalf("project webhook delete failed: %v", err)
	}
	if _, found := webhookInListing(t, mustLiveCLI(t, "project", "webhook", "list", seeded.Key, "--limit", "50"), webhookID); found {
		t.Errorf("project webhook %s is still listed after its delete", webhookID)
	}
}

// sentWebhook is the part of a webhook a create or an update sends and a read
// hands back.
type sentWebhook struct {
	name, url string
	events    []string
}

// expectWebhookStoredAsSent compares a webhook read back with what was sent.
//
// Bitbucket answers 2xx to a property it does not take and stores nothing, so a
// command succeeding says nothing about any one field.
func expectWebhookStoredAsSent(t *testing.T, hook map[string]any, sent sentWebhook) {
	t.Helper()

	events := slices.Clone(sent.events)
	slices.Sort(events)
	if hook["name"] != sent.name || hook["url"] != sent.url || !slices.Equal(webhookEventsOf(hook), events) {
		t.Errorf("webhook stored as name %v, url %v, events %v; want %s, %s, %v",
			hook["name"], hook["url"], hook["events"], sent.name, sent.url, events)
	}
}

// webhookAsStored reads one repository webhook back through bb webhook get.
func webhookAsStored(t *testing.T, id string) map[string]any {
	t.Helper()

	output := mustLiveCLI(t, "webhook", "get", id)
	hook, ok := decodeJSONMap(t, output)["webhook"].(map[string]any)
	if !ok {
		t.Fatalf("no webhook in the get output: %s", output)
	}

	return hook
}

// webhooksInListing decodes the webhooks a --json listing returned. The
// repository, project and settings listings all publish them under webhooks.
func webhooksInListing(t *testing.T, output string) []map[string]any {
	t.Helper()

	listed, ok := decodeJSONMap(t, output)["webhooks"].([]any)
	if !ok {
		t.Fatalf("no webhooks array in the listing: %s", output)
	}

	hooks := make([]map[string]any, 0, len(listed))
	for _, entry := range listed {
		if hook, ok := entry.(map[string]any); ok {
			hooks = append(hooks, hook)
		}
	}

	return hooks
}

// webhookInListing finds one webhook by id in a --json listing.
func webhookInListing(t *testing.T, output, id string) (map[string]any, bool) {
	t.Helper()

	for _, hook := range webhooksInListing(t, output) {
		if listedID, ok := numericOrStringID(hook["id"]); ok && listedID == id {
			return hook, true
		}
	}

	return nil, false
}

// webhookEventsOf reads a decoded webhook's events, sorted: Bitbucket need not
// keep the order they were sent in.
func webhookEventsOf(hook map[string]any) []string {
	listed, _ := hook["events"].([]any)
	events := make([]string, 0, len(listed))
	for _, event := range listed {
		if name, ok := event.(string); ok {
			events = append(events, name)
		}
	}
	slices.Sort(events)

	return events
}
