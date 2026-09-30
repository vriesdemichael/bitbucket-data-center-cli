//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// webhookCreateRun runs one scope's webhook create with the flags given and
// stdin, which carries the shared secret.
type webhookCreateRun func(stdin string, flags ...string) (string, string, error)

// TestLiveWebhookCreateIsSafeToRunAgain is #729.
//
// Bitbucket stores a second webhook with the name, URL and settings of one it
// already has, and bb passed that on: a setup script run twice made two
// webhooks and reported success both times, and the removal of bb bulk tells
// people to loop over their repositories with exactly that script. A create
// now finds the webhook it would make and reports it rather than adding
// another. One with the same name and URL and other settings is refused, so a
// create never changes a webhook it did not make.
//
// Each of the three commands that create a webhook is run, since each has its
// own route to the same service, and every outcome is read back from
// Bitbucket's listing rather than taken from bb's answer.
func TestLiveWebhookCreateIsSafeToRunAgain(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	repoHooks := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/webhooks", seeded.Key, repo.Slug)
	projectHooks := fmt.Sprintf("/rest/api/latest/projects/%s/webhooks", seeded.Key)

	t.Run("bb webhook create", func(t *testing.T) {
		const name = "again-webhook"
		assertWebhookCreateConverges(t, ctx, harness, repoHooks, name, func(stdin string, flags ...string) (string, string, error) {
			return executeLiveCLISplit(t, stdin, append([]string{"--json", "webhook", "create", name, "https://ci.example.com/again/webhook", "--repo", repoRef}, flags...)...)
		})
	})

	t.Run("bb repo settings workflow webhooks create", func(t *testing.T) {
		const name = "again-settings"
		assertWebhookCreateConverges(t, ctx, harness, repoHooks, name, func(stdin string, flags ...string) (string, string, error) {
			return executeLiveCLISplit(t, stdin, append([]string{"--json", "repo", "settings", "workflow", "webhooks", "create", name, "https://ci.example.com/again/settings", "--repo", repoRef}, flags...)...)
		})
	})

	t.Run("bb project webhook create", func(t *testing.T) {
		const name = "again-project"
		assertWebhookCreateConverges(t, ctx, harness, projectHooks, name, func(stdin string, flags ...string) (string, string, error) {
			return executeLiveCLISplit(t, stdin, append([]string{"--json", "project", "webhook", "create", seeded.Key, name, "https://ci.example.com/again/project"}, flags...)...)
		})
	})
}

// assertWebhookCreateConverges runs one scope's create through the cases, and
// reads the scope's webhooks called name back from listPath after each.
func assertWebhookCreateConverges(t *testing.T, ctx context.Context, harness *liveHarness, listPath, name string, create webhookCreateRun) {
	t.Helper()

	const secret = "again-secret-729"

	first, stderr, err := create(secret, "--event", "pr:opened", "--active=false", "--secret-stdin")
	if err != nil {
		t.Fatalf("the first create failed: %v\n%s%s", err, first, stderr)
	}
	id := webhookCreateOutcome(t, first, true)
	stored := webhooksNamed(t, ctx, harness, listPath, name)
	if len(stored) != 1 || fmt.Sprintf("%v", stored[0]["id"]) != id {
		t.Fatalf("want the one webhook the create reported, %s, got %v", id, stored)
	}

	// The same create again: nothing made, the webhook there reported.
	again, stderr, err := create(secret, "--event", "pr:opened", "--active=false", "--secret-stdin")
	if err != nil {
		t.Fatalf("the same create again failed: %v\n%s%s", err, again, stderr)
	}
	if got := webhookCreateOutcome(t, again, false); got != id {
		t.Errorf("the create run again reported webhook %s, want the one already there, %s", got, id)
	}
	if stored := webhooksNamed(t, ctx, harness, listPath, name); len(stored) != 1 {
		t.Fatalf("the create run again left %d webhooks called %s, want 1: %v", len(stored), name, stored)
	}

	preview, _, err := create(secret, "--dry-run", "--event", "pr:opened", "--active=false", "--secret-stdin")
	if err != nil {
		t.Fatalf("the preview of the same create failed: %v\n%s", err, preview)
	}
	assertLivePreview(t, preview, jsonoutput.OutcomeNoOp, "already exists")

	// The same name and URL with anything else different is not this create
	// to make, and not an update to guess at.
	for _, differing := range []struct {
		what  string
		stdin string
		flags []string
	}{
		{"other events", secret, []string{"--event", "pr:merged", "--active=false", "--secret-stdin"}},
		{"active", secret, []string{"--event", "pr:opened", "--secret-stdin"}},
		{"another secret", "another-secret-729", []string{"--event", "pr:opened", "--active=false", "--secret-stdin"}},
		{"no secret", "", []string{"--event", "pr:opened", "--active=false"}},
	} {
		output, stderr, err := create(differing.stdin, differing.flags...)
		if !apperrors.IsKind(err, apperrors.KindConflict) {
			t.Fatalf("a create with %s was not refused as a conflict: %v\n%s%s", differing.what, err, output, stderr)
		}
		refusal := err.Error() + output + stderr
		if !strings.Contains(refusal, "webhook "+id+" has") {
			t.Errorf("the refusal of a create with %s does not name webhook %s:\n%s", differing.what, id, refusal)
		}
		if strings.Contains(refusal, secret) || strings.Contains(refusal, "another-secret-729") {
			t.Errorf("the refusal of a create with %s printed a secret:\n%s", differing.what, refusal)
		}

		preview, _, _ := create(differing.stdin, append([]string{"--dry-run"}, differing.flags...)...)
		assertLiveRefusal(t, preview, apperrors.KindConflict, "already exists")
	}

	stored = webhooksNamed(t, ctx, harness, listPath, name)
	if len(stored) != 1 {
		t.Fatalf("the refused creates left %d webhooks called %s, want 1: %v", len(stored), name, stored)
	}
	configuration, _ := stored[0]["configuration"].(map[string]any)
	if !slices.Equal(webhookEventsOf(stored[0]), []string{"pr:opened"}) || stored[0]["active"] != false || configuration["secret"] != secret {
		t.Errorf("a refused create changed the webhook: %v", stored[0])
	}
}

// webhookCreateOutcome reads a create's --json answer: the webhook's id, and
// whether the create made it, which has to be want.
func webhookCreateOutcome(t *testing.T, output string, want bool) string {
	t.Helper()

	var payload struct {
		Created *bool          `json:"created"`
		Webhook map[string]any `json:"webhook"`
	}
	decodeJSONData(t, output, &payload)
	if payload.Created == nil || *payload.Created != want {
		t.Errorf("want created %v in the answer, got %v:\n%s", want, payload.Created, output)
	}
	id := fmt.Sprintf("%v", payload.Webhook["id"])
	if id == "" || id == "<nil>" {
		t.Fatalf("the answer names no webhook:\n%s", output)
	}

	return id
}

// webhooksNamed reads a scope's webhooks from Bitbucket and keeps those called
// name.
func webhooksNamed(t *testing.T, ctx context.Context, harness *liveHarness, listPath, name string) []map[string]any {
	t.Helper()

	listing, err := harness.liveJSON(ctx, http.MethodGet, listPath+"?limit=1000", nil)
	if err != nil {
		t.Fatalf("list the webhooks at %s: %v", listPath, err)
	}
	if listing["isLastPage"] != true {
		t.Fatalf("the webhooks at %s do not fit one page, so the count below would be short: %v", listPath, listing)
	}

	values, _ := listing["values"].([]any)
	var named []map[string]any
	for _, value := range values {
		if hook, ok := value.(map[string]any); ok && hook["name"] == name {
			named = append(named, hook)
		}
	}

	return named
}
