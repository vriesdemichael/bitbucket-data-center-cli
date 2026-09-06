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

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
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

// TestLiveWebhookUpdatePreservesUnchangedFields is the #511 question asked of
// webhooks: does changing one field quietly change another.
//
// The mock it replaces read the request body and asserted the untouched fields
// were still in it, which only proves bb sent them. Whether the server keeps
// what it was sent -- the events, the active flag, and the secret it never
// echoes back -- is a different question, and the secret is the one that
// matters: a webhook that silently loses it starts failing signature checks at
// the far end, with nothing in bb's output to say why.
func TestLiveWebhookUpdatePreservesUnchangedFields(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Created through the API so it can carry a secret, which bb has no flag
	// for and which the server never sends back in a listing.
	created, err := harness.liveJSON(ctx, "POST",
		"/rest/api/latest/projects/"+seeded.Key+"/repos/"+repo.Slug+"/webhooks",
		map[string]any{
			"name":          "preserve-me",
			"url":           "http://example.invalid/hook",
			"events":        []string{"repo:refs_changed", "pr:opened"},
			"active":        true,
			"configuration": map[string]any{"secret": "s3cr3t"},
		})
	if err != nil {
		t.Fatalf("create the webhook: %v", err)
	}
	id := trimNumeric(created["id"])

	mustLiveCLI(t, "webhook", "update", id, "--name", "renamed", "--repo", repoRef)

	after, err := harness.liveJSON(ctx, "GET",
		"/rest/api/latest/projects/"+seeded.Key+"/repos/"+repo.Slug+"/webhooks/"+id, nil)
	if err != nil {
		t.Fatalf("read the webhook back: %v", err)
	}

	if name, _ := after["name"].(string); name != "renamed" {
		t.Errorf("name = %q, want renamed", name)
	}
	if url, _ := after["url"].(string); url != "http://example.invalid/hook" {
		t.Errorf("the url changed to %q", url)
	}
	if active, _ := after["active"].(bool); !active {
		t.Error("the webhook was deactivated by a rename")
	}
	if events, _ := after["events"].([]any); len(events) != 2 {
		t.Errorf("events = %v, want both to survive", events)
	}

	// The one a listing cannot show and a mock cannot check.
	configuration, _ := after["configuration"].(map[string]any)
	if secret, _ := configuration["secret"].(string); secret != "s3cr3t" {
		t.Errorf("the secret did not survive the rename: %q", secret)
	}
}

// TestLiveRepoSettingsReadSurfaces covers the settings and permission listings
// through the CLI.
//
// The mocks these replace served a settings object their author wrote and
// asserted bb rendered its fields. Reading the settings a real repository
// actually has is the same assertion without the fixture deciding the answer,
// and it catches the case a fixture cannot: a field Bitbucket stopped sending.
func TestLiveRepoSettingsReadSurfaces(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	repoRef := seeded.Key + "/" + repo.Slug
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	user, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	t.Run("pull request settings carry the fields a caller reads", func(t *testing.T) {
		settings := decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get", "--repo", repoRef))

		// requiredApprovers is what update-approvers writes, so a get that does
		// not carry it makes the pair unusable.
		if _, ok := settings["requiredApprovers"]; !ok {
			t.Errorf("no requiredApprovers in the settings: %v", settings)
		}
	})

	t.Run("merge checks list", func(t *testing.T) {
		output := mustLiveCLI(t, "repo", "settings", "pull-requests", "merge-checks", "list", "--repo", repoRef)
		if strings.TrimSpace(output) == "" {
			t.Fatal("the merge checks listing printed nothing at all")
		}
	})

	t.Run("a granted user appears in the security permission listing", func(t *testing.T) {
		mustLiveCLI(t, "repo", "permissions", "grant", user.Username, "REPO_READ", "--repo", repoRef)

		listing := mustLiveCLI(t, "repo", "settings", "security", "permissions", "users", "list", "--repo", repoRef)
		if !strings.Contains(listing, user.Username) {
			t.Fatalf("expected %s in the permission listing:\n%s", user.Username, listing)
		}
	})

	t.Run("a granted user appears in the project permission listing", func(t *testing.T) {
		mustLiveCLI(t, "project", "permissions", "grant", seeded.Key, user.Username, "PROJECT_READ")

		listing := mustLiveCLI(t, "project", "permissions", "list", seeded.Key, "--all")
		if !strings.Contains(listing, user.Username) {
			t.Fatalf("expected %s in the project permission listing:\n%s", user.Username, listing)
		}
	})
}
