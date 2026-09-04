//go:build live

package live_test

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestLiveWorkflowWebhookLifecycle covers creating, listing and deleting a
// webhook through the repository settings tree.
//
// The unit tests these replace read the request body in the handler and
// asserted it carried `"name":"ci-hook"` and the url beside it. That is a claim
// about what Bitbucket accepts, checked against a mock written from the same
// claim. Creating the webhook and reading it back asks the server instead, and
// the read-back is what makes the assertion mean anything: a name the server
// stored is a name the server understood.
func TestLiveWorkflowWebhookLifecycle(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	const (
		name  = "ci-hook"
		url   = "http://example.invalid/hook"
		event = "pr:opened"
	)

	created := mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "create", name, url,
		"--event", event, "--repo", repoRef)

	id := webhookIDFrom(t, created)
	if id == "" {
		t.Fatalf("no webhook id in the create output:\n%s", created)
	}

	t.Run("the webhook is stored as it was described", func(t *testing.T) {
		listing := mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list", "--repo", repoRef)

		for _, want := range []string{name, url, event} {
			if !strings.Contains(listing, want) {
				t.Errorf("expected %q in the webhook listing:\n%s", want, listing)
			}
		}
	})

	t.Run("deleting it removes it", func(t *testing.T) {
		mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "delete", id, "--repo", repoRef)

		listing := mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list", "--repo", repoRef)
		if strings.Contains(listing, url) {
			t.Fatalf("the webhook survived the delete:\n%s", listing)
		}
	})
}

func webhookIDFrom(t *testing.T, output string) string {
	t.Helper()

	data := decodeJSONMap(t, output)
	for _, key := range []string{"webhook", "data"} {
		if nested, ok := data[key].(map[string]any); ok {
			data = nested

			break
		}
	}

	if id, ok := data["id"]; ok {
		return trimNumeric(id)
	}

	return ""
}

// trimNumeric renders a JSON value as the plain string a caller typed. Numbers
// arrive as float64, and 3 has to read as "3" rather than "3e+00".
func trimNumeric(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

// TestLiveRequiredApproversRoundTrip covers the approver count, which the mock
// asserted by reading `"requiredApprovers":2` out of the request body.
//
// The count is branch protection: writing the wrong one lowers it silently,
// which is what #479 was. Reading it back is the only way to know the number
// the server holds is the number that was asked for.
func TestLiveRequiredApproversRoundTrip(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	for _, count := range []string{"3", "1", "0"} {
		t.Run("count "+count, func(t *testing.T) {
			mustLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers",
				"--count", count, "--repo", repoRef)

			settings := decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get", "--repo", repoRef))
			if got := approverCountFrom(t, settings); got != count {
				t.Fatalf("requiredApprovers = %s, want %s", got, count)
			}
		})
	}
}

func approverCountFrom(t *testing.T, settings map[string]any) string {
	t.Helper()

	for _, key := range []string{"requiredApprovers", "required_approvers"} {
		if value, ok := settings[key]; ok {
			return trimNumeric(value)
		}
	}
	if nested, ok := settings["settings"].(map[string]any); ok {
		if value, ok := nested["requiredApprovers"]; ok {
			return trimNumeric(value)
		}
	}

	t.Fatalf("no requiredApprovers in: %v", settings)

	return ""
}
