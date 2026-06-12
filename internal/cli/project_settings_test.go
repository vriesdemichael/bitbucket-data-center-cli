package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProjectSettingsCLI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// Project Webhooks Mock
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks":
			_, _ = w.Write([]byte(`[{"id":123,"name":"wh","url":"http://url","active":true,"events":["repo:refs_changed"]}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":123,"name":"wh","url":"http://url","active":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/123":
			_, _ = w.Write([]byte(`{"id":123,"name":"wh-new","url":"http://url","active":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/123":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/test":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/123/statistics":
			_, _ = w.Write([]byte(`{"invocations":[]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/webhooks/123/statistics/summary":
			_, _ = w.Write([]byte(`{"successCount":5}`))

		// Project Branch Restrictions Mock
		case r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":456,"type":"read-only","matcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions/456":
			_, _ = w.Write([]byte(`{"id":456,"type":"read-only","matcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}},"users":[{"name":"user1"}],"groups":["group1"],"accessKeys":[{"key":{"id":777}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions":
			_, _ = w.Write([]byte(`[{"id":456,"type":"read-only","matcher":{"id":"refs/heads/master","type":{"id":"BRANCH"}}}]`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/branch-permissions/latest/projects/PRJ/restrictions/456":
			w.WriteHeader(http.StatusNoContent)

		// Project Default Tasks Mock
		case r.Method == http.MethodGet && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/tasks":
			_, _ = w.Write([]byte(`{"values":[{"id":789,"description":"task1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/tasks":
			_, _ = w.Write([]byte(`{"id":789,"description":"task1"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/tasks/789":
			_, _ = w.Write([]byte(`{"id":789,"description":"task1-updated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/tasks/789":
			w.WriteHeader(http.StatusNoContent)

		// Project Admin Check Mock
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"key":"PRJ","name":"Project"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	t.Run("Webhook CLI", func(t *testing.T) {
		// list
		out, err := executeTestCLI(t, "project", "webhook", "list", "PRJ")
		if err != nil {
			t.Fatalf("webhook list failed: %v", err)
		}
		if !strings.Contains(out, "123") || !strings.Contains(out, "wh") {
			t.Fatalf("unexpected webhook list output: %s", out)
		}

		// create
		out, err = executeTestCLI(t, "project", "webhook", "create", "PRJ", "wh", "http://url")
		if err != nil {
			t.Fatalf("webhook create failed: %v", err)
		}
		if !strings.Contains(out, "Created webhook: 123") {
			t.Fatalf("unexpected webhook create output: %s", out)
		}

		// update
		out, err = executeTestCLI(t, "project", "webhook", "update", "PRJ", "123", "--name", "wh-new")
		if err != nil {
			t.Fatalf("webhook update failed: %v", err)
		}
		if !strings.Contains(out, "Updated webhook: 123") {
			t.Fatalf("unexpected webhook update output: %s", out)
		}

		// test
		out, err = executeTestCLI(t, "project", "webhook", "test", "PRJ", "123")
		if err != nil {
			t.Fatalf("webhook test failed: %v", err)
		}
		if !strings.Contains(out, "status") {
			t.Fatalf("unexpected webhook test output: %s", out)
		}

		// stats detailed
		out, err = executeTestCLI(t, "project", "webhook", "stats", "PRJ", "123")
		if err != nil {
			t.Fatalf("webhook stats detailed failed: %v", err)
		}
		if !strings.Contains(out, "invocations") {
			t.Fatalf("unexpected webhook stats detailed output: %s", out)
		}

		// stats summary
		out, err = executeTestCLI(t, "project", "webhook", "stats", "PRJ", "123", "--summary")
		if err != nil {
			t.Fatalf("webhook stats summary failed: %v", err)
		}
		if !strings.Contains(out, "successCount") {
			t.Fatalf("unexpected webhook stats summary output: %s", out)
		}

		// delete
		out, err = executeTestCLI(t, "project", "webhook", "delete", "PRJ", "123")
		if err != nil {
			t.Fatalf("webhook delete failed: %v", err)
		}
		if !strings.Contains(out, "Deleted webhook: 123") {
			t.Fatalf("unexpected webhook delete output: %s", out)
		}

		// Dry-runs
		out, err = executeTestCLI(t, "project", "webhook", "create", "PRJ", "wh", "http://url", "--dry-run")
		if err != nil {
			t.Fatalf("webhook create dry-run failed: %v", err)
		}
		if !strings.Contains(out, "project.webhook.create") {
			t.Fatalf("unexpected dryrun webhook create output: %s", out)
		}
	})

	t.Run("Branch Restriction CLI", func(t *testing.T) {
		// list
		out, err := executeTestCLI(t, "project", "branch-restriction", "list", "PRJ")
		if err != nil {
			t.Fatalf("restriction list failed: %v", err)
		}
		if !strings.Contains(out, "456") || !strings.Contains(out, "read-only") {
			t.Fatalf("unexpected restriction list output: %s", out)
		}

		// get
		out, err = executeTestCLI(t, "project", "branch-restriction", "get", "PRJ", "456")
		if err != nil {
			t.Fatalf("restriction get failed: %v", err)
		}
		if !strings.Contains(out, "id=456") || !strings.Contains(out, "read-only") {
			t.Fatalf("unexpected restriction get output: %s", out)
		}

		// create
		out, err = executeTestCLI(t, "project", "branch-restriction", "create", "PRJ", "--type", "read-only", "--matcher-id", "refs/heads/master")
		if err != nil {
			t.Fatalf("restriction create failed: %v", err)
		}
		if !strings.Contains(out, "Created restriction: 456") {
			t.Fatalf("unexpected restriction create output: %s", out)
		}

		// update
		out, err = executeTestCLI(t, "project", "branch-restriction", "update", "PRJ", "456", "--type", "read-only", "--matcher-id", "refs/heads/master")
		if err != nil {
			t.Fatalf("restriction update failed: %v", err)
		}
		if !strings.Contains(out, "Updated restriction: 456") {
			t.Fatalf("unexpected restriction update output: %s", out)
		}

		// delete
		out, err = executeTestCLI(t, "project", "branch-restriction", "delete", "PRJ", "456")
		if err != nil {
			t.Fatalf("restriction delete failed: %v", err)
		}
		if !strings.Contains(out, "Deleted restriction: 456") {
			t.Fatalf("unexpected restriction delete output: %s", out)
		}

		// Dry-runs
		out, err = executeTestCLI(t, "project", "branch-restriction", "create", "PRJ", "--type", "read-only", "--matcher-id", "refs/heads/master", "--dry-run")
		if err != nil {
			t.Fatalf("restriction create dry-run failed: %v", err)
		}
		if !strings.Contains(out, "project.branch-restriction.create") {
			t.Fatalf("unexpected dryrun restriction create output: %s", out)
		}
	})

	t.Run("Default Task CLI", func(t *testing.T) {
		// list
		out, err := executeTestCLI(t, "project", "default-task", "list", "PRJ")
		if err != nil {
			t.Fatalf("default-task list failed: %v", err)
		}
		if !strings.Contains(out, "789") || !strings.Contains(out, "task1") {
			t.Fatalf("unexpected default-task list output: %s", out)
		}

		// add
		out, err = executeTestCLI(t, "project", "default-task", "add", "PRJ", "task1")
		if err != nil {
			t.Fatalf("default-task add failed: %v", err)
		}
		if !strings.Contains(out, "Created default task: 789") {
			t.Fatalf("unexpected default-task add output: %s", out)
		}

		// update
		out, err = executeTestCLI(t, "project", "default-task", "update", "PRJ", "789", "--description", "task1-updated")
		if err != nil {
			t.Fatalf("default-task update failed: %v", err)
		}
		if !strings.Contains(out, "Updated default task: 789") {
			t.Fatalf("unexpected default-task update output: %s", out)
		}

		// delete
		out, err = executeTestCLI(t, "project", "default-task", "delete", "PRJ", "789")
		if err != nil {
			t.Fatalf("default-task delete failed: %v", err)
		}
		if !strings.Contains(out, "Deleted default task: 789") {
			t.Fatalf("unexpected default-task delete output: %s", out)
		}

		// Dry-runs
		out, err = executeTestCLI(t, "project", "default-task", "add", "PRJ", "task1", "--dry-run")
		if err != nil {
			t.Fatalf("default-task add dry-run failed: %v", err)
		}
		if !strings.Contains(out, "project.default-task.create") {
			t.Fatalf("unexpected dryrun default-task add output: %s", out)
		}

		// JSON Output Tests
		// Webhook list JSON
		out, err = executeTestCLI(t, "project", "webhook", "list", "PRJ", "--json")
		if err != nil {
			t.Fatalf("webhook list --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 123`) {
			t.Fatalf("unexpected webhook list --json output: %s", out)
		}

		// Webhook create JSON
		out, err = executeTestCLI(t, "project", "webhook", "create", "PRJ", "wh", "http://url", "--json")
		if err != nil {
			t.Fatalf("webhook create --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 123`) {
			t.Fatalf("unexpected webhook create --json output: %s", out)
		}

		// Webhook update JSON
		out, err = executeTestCLI(t, "project", "webhook", "update", "PRJ", "123", "--name", "wh-new", "--json")
		if err != nil {
			t.Fatalf("webhook update --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 123`) {
			t.Fatalf("unexpected webhook update --json output: %s", out)
		}

		// Webhook delete JSON
		out, err = executeTestCLI(t, "project", "webhook", "delete", "PRJ", "123", "--json")
		if err != nil {
			t.Fatalf("webhook delete --json failed: %v", err)
		}
		if !strings.Contains(out, `"status": "ok"`) {
			t.Fatalf("unexpected webhook delete --json output: %s", out)
		}

		// Webhook test JSON
		out, err = executeTestCLI(t, "project", "webhook", "test", "PRJ", "123", "--json")
		if err != nil {
			t.Fatalf("webhook test --json failed: %v", err)
		}
		if !strings.Contains(out, `"status": "ok"`) {
			t.Fatalf("unexpected webhook test --json output: %s", out)
		}

		// Webhook stats JSON
		out, err = executeTestCLI(t, "project", "webhook", "stats", "PRJ", "123", "--json")
		if err != nil {
			t.Fatalf("webhook stats --json failed: %v", err)
		}
		if !strings.Contains(out, `"invocations": []`) && !strings.Contains(out, `"invocations":[]`) {
			t.Fatalf("unexpected webhook stats --json output: %s", out)
		}

		// Webhook stats summary JSON
		out, err = executeTestCLI(t, "project", "webhook", "stats", "PRJ", "123", "--summary", "--json")
		if err != nil {
			t.Fatalf("webhook stats summary --json failed: %v", err)
		}
		if !strings.Contains(out, `"successCount": 5`) && !strings.Contains(out, `"successCount":5`) {
			t.Fatalf("unexpected webhook stats summary --json output: %s", out)
		}

		// Webhook dry-runs
		out, err = executeTestCLI(t, "project", "webhook", "update", "PRJ", "123", "--name", "wh-new", "--dry-run")
		if err != nil {
			t.Fatalf("webhook update dry-run failed: %v", err)
		}
		if !strings.Contains(out, "project.webhook.update") {
			t.Fatalf("unexpected webhook update dry-run: %s", out)
		}

		out, err = executeTestCLI(t, "project", "webhook", "delete", "PRJ", "123", "--dry-run")
		if err != nil {
			t.Fatalf("webhook delete dry-run failed: %v", err)
		}
		if !strings.Contains(out, "project.webhook.delete") {
			t.Fatalf("unexpected webhook delete dry-run: %s", out)
		}

		out, err = executeTestCLI(t, "project", "webhook", "test", "PRJ", "123", "--dry-run")
		if err != nil {
			t.Fatalf("webhook test dry-run failed: %v", err)
		}
		if !strings.Contains(out, "project.webhook.test") {
			t.Fatalf("unexpected webhook test dry-run: %s", out)
		}

		// Webhook validation error
		_, err = executeTestCLI(t, "project", "webhook", "update", "PRJ", "123", "--active", "invalid")
		if err == nil || !strings.Contains(err.Error(), "active must be true or false") {
			t.Fatalf("expected active validation error, got: %v", err)
		}

		// Branch restriction JSON
		out, err = executeTestCLI(t, "project", "branch-restriction", "list", "PRJ", "--json")
		if err != nil {
			t.Fatalf("restriction list --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 456`) && !strings.Contains(out, `"id":456`) {
			t.Fatalf("unexpected restriction list --json output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "branch-restriction", "get", "PRJ", "456", "--json")
		if err != nil {
			t.Fatalf("restriction get --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 456`) && !strings.Contains(out, `"id":456`) {
			t.Fatalf("unexpected restriction get --json output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "branch-restriction", "create", "PRJ", "--type", "read-only", "--matcher-id", "refs/heads/master", "--json")
		if err != nil {
			t.Fatalf("restriction create --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 456`) && !strings.Contains(out, `"id":456`) {
			t.Fatalf("unexpected restriction create --json output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "branch-restriction", "update", "PRJ", "456", "--type", "read-only", "--matcher-id", "refs/heads/master", "--json")
		if err != nil {
			t.Fatalf("restriction update --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 456`) && !strings.Contains(out, `"id":456`) {
			t.Fatalf("unexpected restriction update --json output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "branch-restriction", "delete", "PRJ", "456", "--json")
		if err != nil {
			t.Fatalf("restriction delete --json failed: %v", err)
		}
		if !strings.Contains(out, `"status": "ok"`) {
			t.Fatalf("unexpected restriction delete --json output: %s", out)
		}

		// Branch restriction dry-runs
		// update (no-op: matches the mock config exactly)
		out, err = executeTestCLI(t, "project", "branch-restriction", "update", "PRJ", "456", "--type", "read-only", "--matcher-id", "refs/heads/master", "--user", "user1", "--group", "group1", "--access-key-id", "777", "--dry-run")
		if err != nil {
			t.Fatalf("restriction update dry-run no-op failed: %v", err)
		}
		if !strings.Contains(out, "no-op") || !strings.Contains(out, "branch restriction already matches") {
			t.Fatalf("unexpected restriction update dry-run no-op: %s", out)
		}

		// update (not no-op: different user)
		out, err = executeTestCLI(t, "project", "branch-restriction", "update", "PRJ", "456", "--type", "read-only", "--matcher-id", "refs/heads/master", "--user", "user2", "--group", "group1", "--access-key-id", "777", "--dry-run")
		if err != nil {
			t.Fatalf("restriction update dry-run failed: %v", err)
		}
		if !strings.Contains(out, "update") || !strings.Contains(out, "branch restriction will be updated") {
			t.Fatalf("unexpected restriction update dry-run: %s", out)
		}

		out, err = executeTestCLI(t, "project", "branch-restriction", "delete", "PRJ", "456", "--dry-run")
		if err != nil {
			t.Fatalf("restriction delete dry-run failed: %v", err)
		}
		if !strings.Contains(out, "project.branch-restriction.delete") {
			t.Fatalf("unexpected restriction delete dry-run: %s", out)
		}

		// Default task JSON
		out, err = executeTestCLI(t, "project", "default-task", "list", "PRJ", "--json")
		if err != nil {
			t.Fatalf("default-task list --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 789`) && !strings.Contains(out, `"id":789`) {
			t.Fatalf("unexpected default-task list --json output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "default-task", "add", "PRJ", "task1", "--json")
		if err != nil {
			t.Fatalf("default-task add --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 789`) && !strings.Contains(out, `"id":789`) {
			t.Fatalf("unexpected default-task add --json output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "default-task", "update", "PRJ", "789", "--description", "task1-updated", "--json")
		if err != nil {
			t.Fatalf("default-task update --json failed: %v", err)
		}
		if !strings.Contains(out, `"id": 789`) && !strings.Contains(out, `"id":789`) {
			t.Fatalf("unexpected default-task update --json output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "default-task", "delete", "PRJ", "789", "--json")
		if err != nil {
			t.Fatalf("default-task delete --json failed: %v", err)
		}
		if !strings.Contains(out, `"status": "ok"`) {
			t.Fatalf("unexpected default-task delete --json output: %s", out)
		}

		// Default task dry-runs
		out, err = executeTestCLI(t, "project", "default-task", "update", "PRJ", "789", "--description", "task1-updated", "--dry-run")
		if err != nil {
			t.Fatalf("default-task update dry-run failed: %v", err)
		}
		if !strings.Contains(out, "project.default-task.update") {
			t.Fatalf("unexpected default-task update dry-run: %s", out)
		}

		out, err = executeTestCLI(t, "project", "default-task", "delete", "PRJ", "789", "--dry-run")
		if err != nil {
			t.Fatalf("default-task delete dry-run failed: %v", err)
		}
		if !strings.Contains(out, "project.default-task.delete") {
			t.Fatalf("unexpected default-task delete dry-run: %s", out)
		}

		// Webhook active true/false updates
		out, err = executeTestCLI(t, "project", "webhook", "update", "PRJ", "123", "--active", "true")
		if err != nil {
			t.Fatalf("webhook update active=true failed: %v", err)
		}
		if !strings.Contains(out, "Updated webhook: 123") {
			t.Fatalf("unexpected webhook update active=true output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "webhook", "update", "PRJ", "123", "--active", "false")
		if err != nil {
			t.Fatalf("webhook update active=false failed: %v", err)
		}
		if !strings.Contains(out, "Updated webhook: 123") {
			t.Fatalf("unexpected webhook update active=false output: %s", out)
		}

		// Branch restriction create/update with users, groups, access keys
		out, err = executeTestCLI(t, "project", "branch-restriction", "create", "PRJ", "--type", "read-only", "--matcher-id", "refs/heads/master", "--user", "user1", "--group", "group1", "--access-key-id", "777")
		if err != nil {
			t.Fatalf("restriction create with users/groups/keys failed: %v", err)
		}
		if !strings.Contains(out, "Created restriction: 456") {
			t.Fatalf("unexpected restriction create output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "branch-restriction", "update", "PRJ", "456", "--type", "read-only", "--matcher-id", "refs/heads/master", "--user", "user1", "--group", "group1", "--access-key-id", "777")
		if err != nil {
			t.Fatalf("restriction update with users/groups/keys failed: %v", err)
		}
		if !strings.Contains(out, "Updated restriction: 456") {
			t.Fatalf("unexpected restriction update output: %s", out)
		}

		// Default task add/update with source and target refs
		out, err = executeTestCLI(t, "project", "default-task", "add", "PRJ", "task1", "--source-ref", "refs/heads/feature/*", "--target-ref", "refs/heads/master")
		if err != nil {
			t.Fatalf("default-task add with refs failed: %v", err)
		}
		if !strings.Contains(out, "Created default task: 789") {
			t.Fatalf("unexpected default-task add with refs output: %s", out)
		}

		out, err = executeTestCLI(t, "project", "default-task", "update", "PRJ", "789", "--description", "task1-updated", "--source-ref", "refs/heads/feature/*", "--target-ref", "refs/heads/master")
		if err != nil {
			t.Fatalf("default-task update with refs failed: %v", err)
		}
		if !strings.Contains(out, "Updated default task: 789") {
			t.Fatalf("unexpected default-task update with refs output: %s", out)
		}
	})
}
