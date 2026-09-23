//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	reposettings "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveRepoSettingsSecurityPermissionsUsers(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}

	// Two grants made past the service: something of this repository's for the
	// listing to show, and one entry more than the smaller limit below.
	reader, err := harness.createRestrictedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, repo.ProjectKey, repo.Slug, harness.username(), "REPO_WRITE"); err != nil {
		t.Fatalf("grant REPO_WRITE failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, repo.ProjectKey, repo.Slug, reader.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant REPO_READ failed: %v", err)
	}

	users, err := service.ListRepositoryPermissionUsers(ctx, repo, 100)
	if err != nil {
		t.Fatalf("list permission users failed: %v", err)
	}
	if users == nil {
		t.Fatal("expected non-nil users slice")
	}
	listed := map[string]string{}
	for _, user := range users {
		listed[user.Name] = user.Permission
	}
	if want := map[string]string{harness.username(): "REPO_WRITE", reader.Username: "REPO_READ"}; !maps.Equal(listed, want) {
		t.Fatalf("permission users = %v, want %v", listed, want)
	}

	// The limit has to cut the listing, not merely travel with the request.
	capped, err := service.ListRepositoryPermissionUsers(ctx, repo, 1)
	if err != nil {
		t.Fatalf("list permission users limited to one failed: %v", err)
	}
	if len(capped) != 1 {
		t.Fatalf("a listing limited to one returned %d entries: %v", len(capped), capped)
	}
}

func TestLiveRepoSettingsWorkflowWebhooks(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	// Created past the service, so an empty listing of the wrong repository
	// cannot pass for this one.
	name := testsupport.UniqueName("lt-listed-webhook-")
	created, err := harness.liveJSON(ctx, http.MethodPost,
		"/rest/api/latest/projects/"+seeded.Key+"/repos/"+seeded.Repos[0].Slug+"/webhooks",
		map[string]any{"name": name, "url": "http://example.invalid/listed", "events": []string{"pr:merged"}})
	if err != nil {
		t.Fatalf("create the webhook: %v", err)
	}

	webhooks, err := service.ListRepositoryWebhooks(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug})
	if err != nil {
		t.Fatalf("list repository webhooks failed: %v", err)
	}
	if webhooks.Count < 0 {
		t.Fatalf("expected non-negative webhook count, got %d", webhooks.Count)
	}
	if webhooks.Count != 1 {
		t.Fatalf("webhook count = %d, want the one created", webhooks.Count)
	}
	listed, ok := findByID(webhooks.Payload, trimNumeric(created["id"]))
	if !ok || listed["name"] != name || listed["url"] != "http://example.invalid/listed" {
		t.Fatalf("the created webhook is not listed as it was created: %v", webhooks.Payload)
	}
}

func TestLiveRepoSettingsPullRequestSettings(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	// Set past the service to the value that is not the default, so the get
	// reports it only by reading this repository's settings.
	if _, err := harness.liveJSON(ctx, http.MethodPost,
		"/rest/api/latest/projects/"+seeded.Key+"/repos/"+seeded.Repos[0].Slug+"/settings/pull-requests",
		map[string]any{"requiredAllTasksComplete": true}); err != nil {
		t.Fatalf("set requiredAllTasksComplete: %v", err)
	}

	settings, err := service.GetRepositoryPullRequestSettings(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug})
	if err != nil {
		t.Fatalf("get pull request settings failed: %v", err)
	}
	if settings == nil {
		t.Fatal("expected non-nil pull request settings")
	}
	if settings["requiredAllTasksComplete"] != true {
		t.Fatalf("requiredAllTasksComplete = %v, want the true this repository was given", settings["requiredAllTasksComplete"])
	}
}

func TestLiveRepoSettingsGrantUserPermission(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	username := harness.username()

	if err := service.GrantRepositoryUserPermission(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}, username, "REPO_WRITE"); err != nil {
		t.Fatalf("grant repository permission failed: %v", err)
	}

	// The grant answers 204 and nothing else; the listing says what was stored.
	listing := mustLiveCLI(t, "repo", "settings", "security", "permissions", "users", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if got := repoSettingsPermissionsFrom(t, listing)[username]; got != "REPO_WRITE" {
		t.Fatalf("%s holds %q on the repository, want REPO_WRITE:\n%s", username, got, listing)
	}
}

func TestLiveRepoSettingsCreateWebhook(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	name := testsupport.UniqueName("lt-webhook-")
	written, err := service.CreateRepositoryWebhook(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}, reposettings.WebhookCreateInput{
		Name:   name,
		URL:    "http://localhost:65535/hook",
		Events: []string{"repo:refs_changed"},
		// Inactive: Bitbucket makes a webhook active when told nothing, so an
		// active one reads back the same whether or not the flag arrived.
		Active: false,
	})
	if err != nil {
		t.Fatalf("create repository webhook failed: %v", err)
	}
	// The service reads what it made back by id; nothing stood in the way of
	// that read here, so the answer standing in for it would be a defect.
	if written.Unread != nil {
		t.Fatalf("the created webhook was not read back: %v", written.Unread)
	}

	id, ok := extractWebhookID(written.Webhook)
	if !ok {
		t.Fatalf("created webhook payload did not include a valid id: %#v", written.Webhook)
	}
	stored := repoSettingsWebhookReadBack(t, seeded.Key+"/"+seeded.Repos[0].Slug, id)
	assertRepoSettingsWebhook(t, stored, name, "http://localhost:65535/hook", false, "repo:refs_changed")
}

func TestLiveRepoSettingsUpdatePullRequestRequiredAllTasks(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
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

	// The update's answer is the write describing itself; a get says what was
	// stored.
	stored := repoSettingsPullRequestReadBack(t, repo.ProjectKey+"/"+repo.Slug)
	if stored["requiredAllTasksComplete"] != target {
		t.Fatalf("requiredAllTasksComplete reads back as %v, want %t", stored["requiredAllTasksComplete"], target)
	}
}

func TestLiveRepoSettingsDeleteWebhook(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repoRef := seeded.Key + "/" + seeded.Repos[0].Slug

	written, err := service.CreateRepositoryWebhook(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}, reposettings.WebhookCreateInput{
		Name:   "live-test-webhook",
		URL:    "http://localhost:65535/hook",
		Events: []string{"repo:refs_changed"},
		// Not the default, for the reason the create test gives.
		Active: false,
	})
	if err != nil {
		t.Fatalf("create repository webhook failed: %v", err)
	}

	webhookID, ok := extractWebhookID(written.Webhook)
	if !ok {
		t.Fatalf("created webhook payload did not include a valid id: %#v", written.Webhook)
	}

	// Read before the delete as well as after, so the delete is shown removing
	// a webhook that was stored rather than one that never was.
	assertRepoSettingsWebhook(t, repoSettingsWebhookReadBack(t, repoRef, webhookID), "live-test-webhook", "http://localhost:65535/hook", false, "repo:refs_changed")

	if err := service.DeleteRepositoryWebhook(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug}, webhookID); err != nil {
		t.Fatalf("delete repository webhook failed: %v", err)
	}

	if output, err := executeLiveCLI(t, "--json", "webhook", "get", webhookID, "--repo", repoRef); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("webhook %s did not read back as not found after its delete: %v\n%s", webhookID, err, output)
	}
}

func TestLiveRepoSettingsUpdatePullRequestRequiredApprovers(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
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

	stored := repoSettingsPullRequestReadBack(t, repo.ProjectKey+"/"+repo.Slug)
	if stored["requiredApprovers"] != float64(2) || stored["requiredApproversEnabled"] != true {
		t.Fatalf("required approvers read back as %v (enabled %v), want 2 (enabled true)", stored["requiredApprovers"], stored["requiredApproversEnabled"])
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

// repoSettingsWebhookReadBack reads one repository webhook through bb webhook
// get, a request of its own rather than the answer to the write.
func repoSettingsWebhookReadBack(t *testing.T, repoRef, id string) map[string]any {
	t.Helper()

	output := mustLiveCLI(t, "webhook", "get", id, "--repo", repoRef)
	hook, ok := decodeJSONMap(t, output)["webhook"].(map[string]any)
	if !ok {
		t.Fatalf("no webhook in the get output for %s: %s", id, output)
	}

	return hook
}

// assertRepoSettingsWebhook compares a webhook read back with what it was
// created as, events as a set.
func assertRepoSettingsWebhook(t *testing.T, hook map[string]any, name, url string, active bool, events ...string) {
	t.Helper()

	var stored []string
	listed, _ := hook["events"].([]any)
	for _, event := range listed {
		stored = append(stored, asString(event))
	}
	slices.Sort(stored)
	want := slices.Sorted(slices.Values(events))

	if hook["name"] != name || hook["url"] != url || hook["active"] != active || !slices.Equal(stored, want) {
		t.Fatalf("webhook reads back as %v, want name %q, url %q, active %t, events %v", hook, name, url, active, events)
	}
}

// repoSettingsPullRequestReadBack reads a repository's pull request settings
// through bb's get.
func repoSettingsPullRequestReadBack(t *testing.T, repoRef string) map[string]any {
	t.Helper()

	return decodeJSONMap(t, mustLiveCLI(t, "repo", "settings", "pull-requests", "get", "--repo", repoRef))
}

// repoSettingsPermissionsFrom reads a permission listing's JSON output as a
// map from each subject's name to the permission it holds.
func repoSettingsPermissionsFrom(t *testing.T, output string) map[string]string {
	t.Helper()

	permissions := map[string]string{}
	entries, _ := decodeJSONMap(t, output)["entries"].([]any)
	for _, entry := range entries {
		record, _ := entry.(map[string]any)
		permissions[asString(record["name"])] = asString(record["permission"])
	}

	return permissions
}
