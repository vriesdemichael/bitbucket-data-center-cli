//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	reposettings "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reposettings"
)

func TestLiveRepoSettingsSecurityPermissionsUsers(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	users, err := service.ListRepositoryPermissionUsers(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}, 100)
	if err != nil {
		t.Fatalf("list permission users failed: %v", err)
	}
	if users == nil {
		t.Fatal("expected non-nil users slice")
	}
}

func TestLiveRepoSettingsWorkflowWebhooks(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	webhooks, err := service.ListRepositoryWebhooks(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug})
	if err != nil {
		t.Fatalf("list repository webhooks failed: %v", err)
	}
	if webhooks.Count < 0 {
		t.Fatalf("expected non-negative webhook count, got %d", webhooks.Count)
	}
}

func TestLiveRepoSettingsPullRequestSettings(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	settings, err := service.GetRepositoryPullRequestSettings(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug})
	if err != nil {
		t.Fatalf("get pull request settings failed: %v", err)
	}
	if settings == nil {
		t.Fatal("expected non-nil pull request settings")
	}
}

func TestLiveRepoSettingsGrantUserPermission(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	username := harness.username()

	if err := service.GrantRepositoryUserPermission(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}, username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant repository permission failed: %v", err)
	}
}

func TestLiveRepoSettingsCreateWebhook(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	name := fmt.Sprintf("lt-webhook-%d", time.Now().UnixNano()%100000)
	_, err = service.CreateRepositoryWebhook(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}, reposettings.WebhookCreateInput{
		Name:   name,
		URL:    "http://localhost:65535/hook",
		Events: []string{"repo:refs_changed"},
		Active: true,
	})
	if err != nil {
		t.Fatalf("create repository webhook failed: %v", err)
	}
}

func TestLiveRepoSettingsUpdatePullRequestRequiredAllTasks(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}
	settingsBefore, err := service.GetRepositoryPullRequestSettings(ctx, repo)
	if err != nil {
		t.Fatalf("get pull request settings before update failed: %v", err)
	}

	current, _ := settingsBefore["requiredAllTasksComplete"].(bool)
	target := !current
	updated, err := service.UpdateRepositoryPullRequestRequiredAllTasks(ctx, repo, target)
	if err != nil {
		t.Fatalf("update pull request settings failed: %v", err)
	}

	resultValue, ok := updated["requiredAllTasksComplete"].(bool)
	if ok && resultValue != target {
		t.Fatalf("expected requiredAllTasksComplete=%t, got %t", target, resultValue)
	}
}

func TestLiveRepoSettingsDeleteWebhook(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	payload, err := service.CreateRepositoryWebhook(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}, reposettings.WebhookCreateInput{
		Name:   "live-test-webhook",
		URL:    "http://localhost:65535/hook",
		Events: []string{"repo:refs_changed"},
		Active: true,
	})
	if err != nil {
		t.Fatalf("create repository webhook failed: %v", err)
	}

	webhookID, ok := extractWebhookID(payload)
	if !ok {
		t.Fatalf("created webhook payload did not include a valid id: %#v", payload)
	}

	if err := service.DeleteRepositoryWebhook(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}, webhookID); err != nil {
		t.Fatalf("delete repository webhook failed: %v", err)
	}
}

func TestLiveRepoSettingsUpdatePullRequestRequiredApprovers(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}
	updated, err := service.UpdateRepositoryPullRequestRequiredApproversCount(ctx, repo, 2)
	if err != nil {
		t.Fatalf("update pull request required approvers failed: %v", err)
	}

	requiredApprovers, ok := updated["requiredApprovers"].(map[string]any)
	if ok {
		if value, ok := requiredApprovers["enabled"].(bool); ok && !value {
			t.Fatal("expected requiredApprovers enabled=true")
		}
	}
}

func extractWebhookID(payload any) (string, bool) {
	switch p := payload.(type) {
	case map[string]any:
		switch value := p["id"].(type) {
		case string:
			return strings.TrimSpace(value), strings.TrimSpace(value) != ""
		case float64:
			return strconv.FormatInt(int64(value), 10), true
		case int:
			return strconv.Itoa(value), true
		case int32:
			return strconv.FormatInt(int64(value), 10), true
		case int64:
			return strconv.FormatInt(value, 10), true
		case json.Number:
			return value.String(), value.String() != ""
		default:
			return "", false
		}
	default:
		// Try JSON marshaling/unmarshaling fallback for generated struct types
		data, err := json.Marshal(payload)
		if err != nil {
			return "", false
		}
		var m map[string]any
		if err := json.Unmarshal(data, &m); err != nil {
			return "", false
		}
		return extractWebhookID(m)
	}
}
