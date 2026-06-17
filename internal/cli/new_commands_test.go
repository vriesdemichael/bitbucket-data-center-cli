package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewCLICommandsMock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// Auto-merge
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-merge":
			w.WriteHeader(http.StatusNoContent)

		// Auto-decline
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-decline":
			_, _ = w.Write([]byte(`{"enabled":true,"inactivityWeeks":4}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-decline":
			_, _ = w.Write([]byte(`{"enabled":true,"inactivityWeeks":4}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-decline":
			w.WriteHeader(http.StatusNoContent)

		// Labels
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/labels":
			_, _ = w.Write([]byte(`{"values":[{"name":"label1"},{"name":"label2"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/labels":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/labels/label1":
			w.WriteHeader(http.StatusNoContent)

		// Watch
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/watch":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/watch":
			w.WriteHeader(http.StatusNoContent)

		// Default checklist tasks
		case r.Method == http.MethodGet && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/repos/repo/tasks":
			_, _ = w.Write([]byte(`{"values":[{"id":123,"description":"task1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/repos/repo/tasks":
			_, _ = w.Write([]byte(`{"id":123,"description":"task1"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/repos/repo/tasks/123":
			_, _ = w.Write([]byte(`{"id":123,"description":"task1-updated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/repos/repo/tasks/123":
			w.WriteHeader(http.StatusNoContent)

		// Webhooks
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks":
			_, _ = w.Write([]byte(`{"values":[{"id":1,"name":"hook1","url":"http://test","active":true,"events":["repo:refs_changed"]},{"id":2,"name":"hook2","url":"http://test2","active":false,"events":["repo:modified"]}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/1":
			_, _ = w.Write([]byte(`{"id":1,"name":"hook1"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/1":
			_, _ = w.Write([]byte(`{"id":1,"name":"hook1-updated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/test" && r.URL.RawQuery == "webhookId=1":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/1/statistics":
			_, _ = w.Write([]byte(`{"invocations":10}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/1/statistics/summary":
			_, _ = w.Write([]byte(`{"summary":"ok"}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// 1. Auto-merge
	out, err := executeTestCLI(t, "repo", "settings", "auto-merge", "get")
	if err != nil {
		t.Fatalf("auto-merge get failed: %v", err)
	}
	if !strings.Contains(out, "Auto-merge enabled: true") {
		t.Fatalf("unexpected auto-merge get output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "settings", "auto-merge", "set", "--enabled")
	if err != nil {
		t.Fatalf("auto-merge set failed: %v", err)
	}
	if !strings.Contains(out, "Updated auto-merge settings: enabled=true") {
		t.Fatalf("unexpected auto-merge set output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "settings", "auto-merge", "delete")
	if err != nil {
		t.Fatalf("auto-merge delete failed: %v", err)
	}
	if !strings.Contains(out, "Deleted auto-merge settings") {
		t.Fatalf("unexpected auto-merge delete output: %s", out)
	}

	// 2. Auto-decline
	out, err = executeTestCLI(t, "repo", "settings", "auto-decline", "get")
	if err != nil {
		t.Fatalf("auto-decline get failed: %v", err)
	}
	if !strings.Contains(out, "Auto-decline enabled: true") || !strings.Contains(out, "Inactivity weeks: 4") {
		t.Fatalf("unexpected auto-decline get output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4")
	if err != nil {
		t.Fatalf("auto-decline set failed: %v", err)
	}
	if !strings.Contains(out, "Updated auto-decline settings: enabled=true inactivityWeeks=4") {
		t.Fatalf("unexpected auto-decline set output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "settings", "auto-decline", "delete")
	if err != nil {
		t.Fatalf("auto-decline delete failed: %v", err)
	}
	if !strings.Contains(out, "Deleted auto-decline settings") {
		t.Fatalf("unexpected auto-decline delete output: %s", out)
	}

	// 3. Labels
	out, err = executeTestCLI(t, "repo", "label", "list")
	if err != nil {
		t.Fatalf("label list failed: %v", err)
	}
	if !strings.Contains(out, "label1") || !strings.Contains(out, "label2") {
		t.Fatalf("unexpected label list output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "label", "add", "label3")
	if err != nil {
		t.Fatalf("label add failed: %v", err)
	}
	if !strings.Contains(out, "Added label: label3") {
		t.Fatalf("unexpected label add output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "label", "remove", "label1")
	if err != nil {
		t.Fatalf("label remove failed: %v", err)
	}
	if !strings.Contains(out, "Removed label: label1") {
		t.Fatalf("unexpected label remove output: %s", out)
	}

	// 4. Watch
	out, err = executeTestCLI(t, "repo", "watch")
	if err != nil {
		t.Fatalf("watch failed: %v", err)
	}
	if !strings.Contains(out, "Watching repository PRJ/repo") {
		t.Fatalf("unexpected watch output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "unwatch")
	if err != nil {
		t.Fatalf("unwatch failed: %v", err)
	}
	if !strings.Contains(out, "Unwatched repository PRJ/repo") {
		t.Fatalf("unexpected unwatch output: %s", out)
	}

	// 5. Default checklist tasks
	out, err = executeTestCLI(t, "repo", "default-task", "list")
	if err != nil {
		t.Fatalf("default-task list failed: %v", err)
	}
	if !strings.Contains(out, "task1") || !strings.Contains(out, "123") {
		t.Fatalf("unexpected default-task list output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "default-task", "add", "task1")
	if err != nil {
		t.Fatalf("default-task add failed: %v", err)
	}
	if !strings.Contains(out, "Created default task: 123") {
		t.Fatalf("unexpected default-task add output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "default-task", "update", "123", "--description", "task1-updated")
	if err != nil {
		t.Fatalf("default-task update failed: %v", err)
	}
	if !strings.Contains(out, "Updated default task: 123") {
		t.Fatalf("unexpected default-task update output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "default-task", "delete", "123")
	if err != nil {
		t.Fatalf("default-task delete failed: %v", err)
	}
	if !strings.Contains(out, "Deleted default task: 123") {
		t.Fatalf("unexpected default-task delete output: %s", out)
	}

	// 6. Webhooks
	out, err = executeTestCLI(t, "webhook", "get", "1")
	if err != nil {
		t.Fatalf("webhook get failed: %v", err)
	}
	if !strings.Contains(out, "hook1") {
		t.Fatalf("unexpected webhook get output: %s", out)
	}

	out, err = executeTestCLI(t, "webhook", "update", "1", "--name", "hook1-updated")
	if err != nil {
		t.Fatalf("webhook update failed: %v", err)
	}
	if !strings.Contains(out, "Updated webhook: 1") {
		t.Fatalf("unexpected webhook update output: %s", out)
	}

	out, err = executeTestCLI(t, "webhook", "test", "1")
	if err != nil {
		t.Fatalf("webhook test failed: %v", err)
	}
	if !strings.Contains(out, "success") {
		t.Fatalf("unexpected webhook test output: %s", out)
	}

	out, err = executeTestCLI(t, "webhook", "stats", "1")
	if err != nil {
		t.Fatalf("webhook stats failed: %v", err)
	}
	if !strings.Contains(out, "10") {
		t.Fatalf("unexpected webhook stats output: %s", out)
	}

	out, err = executeTestCLI(t, "webhook", "stats", "1", "--summary")
	if err != nil {
		t.Fatalf("webhook stats summary failed: %v", err)
	}
	if !strings.Contains(out, "ok") {
		t.Fatalf("unexpected webhook stats summary output: %s", out)
	}

	out, err = executeTestCLI(t, "webhook", "list")
	if err != nil {
		t.Fatalf("webhook list failed: %v", err)
	}
	if !strings.Contains(out, "hook1") || !strings.Contains(out, "hook2") {
		t.Fatalf("unexpected webhook list output: %s", out)
	}

	// Test webhook list pagination
	out, err = executeTestCLI(t, "webhook", "list", "--limit", "1", "--start", "0")
	if err != nil {
		t.Fatalf("webhook list pagination failed: %v", err)
	}
	if !strings.Contains(out, "hook1") || strings.Contains(out, "hook2") {
		t.Fatalf("unexpected webhook list pagination output: %s", out)
	}

	out, err = executeTestCLI(t, "webhook", "list", "--limit", "1", "--start", "1")
	if err != nil {
		t.Fatalf("webhook list pagination offset failed: %v", err)
	}
	if strings.Contains(out, "hook1") || !strings.Contains(out, "hook2") {
		t.Fatalf("unexpected webhook list pagination offset output: %s", out)
	}

	// Test webhook list pagination edge cases
	out, err = executeTestCLI(t, "webhook", "list", "--limit", "1", "--start", "-1")
	if err != nil {
		t.Fatalf("webhook list pagination with negative start failed: %v", err)
	}
	if !strings.Contains(out, "hook1") || strings.Contains(out, "hook2") {
		t.Fatalf("unexpected webhook list pagination negative start output: %s", out)
	}

	out, err = executeTestCLI(t, "webhook", "list", "--limit", "1", "--start", "99")
	if err != nil {
		t.Fatalf("webhook list pagination out of bounds failed: %v", err)
	}
	if !strings.Contains(out, "No webhooks found") {
		t.Fatalf("unexpected webhook list pagination out of bounds output: %s", out)
	}

	// Test webhook list error paths
	_, err = executeTestCLI(t, "webhook", "list", "--repo", "PRJ/missing")
	if err == nil {
		t.Fatal("expected error when webhook list fails on non-existent repo")
	}

	_, err = executeTestCLI(t, "webhook", "list", "--repo", "invalid-repo-format")
	if err == nil {
		t.Fatal("expected error when resolveRepositorySettingsReference fails")
	}
}

func TestNewCLICommandsDryRunAndJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// Auto-merge
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-merge":
			_, _ = w.Write([]byte(`{"enabled":true}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-merge":
			w.WriteHeader(http.StatusNoContent)

		// Auto-decline
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-decline":
			_, _ = w.Write([]byte(`{"enabled":true,"inactivityWeeks":4}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-decline":
			_, _ = w.Write([]byte(`{"enabled":true,"inactivityWeeks":4}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/settings/auto-decline":
			w.WriteHeader(http.StatusNoContent)

		// Labels
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/labels":
			_, _ = w.Write([]byte(`{"values":[{"name":"label1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/labels":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/labels/label1":
			w.WriteHeader(http.StatusNoContent)

		// Watch
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/watch":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/watch":
			w.WriteHeader(http.StatusNoContent)

		// Default tasks
		case r.Method == http.MethodGet && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/repos/repo/tasks":
			_, _ = w.Write([]byte(`{"values":[{"id":123,"description":"task1"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/repos/repo/tasks":
			_, _ = w.Write([]byte(`{"id":123,"description":"task1"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/repos/repo/tasks/123":
			_, _ = w.Write([]byte(`{"id":123,"description":"task1-updated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/default-tasks/latest/projects/PRJ/repos/repo/tasks/123":
			w.WriteHeader(http.StatusNoContent)

		// Webhooks
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks":
			_, _ = w.Write([]byte(`{"values":[{"id":1,"name":"hook1","url":"http://test","active":true,"events":["repo:refs_changed"]},{"id":2,"name":"hook2","url":"http://test2","active":false,"events":["repo:modified"]}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/1":
			_, _ = w.Write([]byte(`{"id":1,"name":"hook1"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/1":
			_, _ = w.Write([]byte(`{"id":1,"name":"hook1-updated"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/test" && r.URL.RawQuery == "webhookId=1":
			_, _ = w.Write([]byte(`{"status":"success"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/1/statistics":
			_, _ = w.Write([]byte(`{"invocations":10}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/webhooks/1/statistics/summary":
			_, _ = w.Write([]byte(`{"summary":"ok"}`))

		// Permission checks for dry-run
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"repo","project":{"key":"PRJ"}}],"isLastPage":true}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// 1. Dry run tests for all mutations
	dryRuns := [][]string{
		{"repo", "settings", "auto-merge", "set", "--enabled", "--dry-run"},
		{"repo", "settings", "auto-merge", "delete", "--dry-run"},
		{"repo", "settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4", "--dry-run"},
		{"repo", "settings", "auto-decline", "delete", "--dry-run"},
		{"repo", "label", "add", "label3", "--dry-run"},
		{"repo", "label", "remove", "label1", "--dry-run"},
		{"repo", "watch", "--dry-run"},
		{"repo", "unwatch", "--dry-run"},
		{"repo", "default-task", "add", "task1", "--dry-run"},
		{"repo", "default-task", "update", "123", "--description", "task1-updated", "--dry-run"},
		{"repo", "default-task", "delete", "123", "--dry-run"},
		{"webhook", "update", "1", "--name", "hook1-updated", "--dry-run"},
		{"webhook", "test", "1", "--dry-run"},
	}
	for _, args := range dryRuns {
		out, err := executeTestCLI(t, args...)
		if err != nil {
			t.Fatalf("dry-run failed for %v: %v", args, err)
		}
		if !strings.Contains(strings.ToLower(out), "dry-run") {
			t.Fatalf("unexpected dry-run output for %v: %s", args, out)
		}
	}

	// 2. JSON format tests
	jsonRuns := [][]string{
		{"repo", "settings", "auto-merge", "get", "--json"},
		{"repo", "settings", "auto-merge", "set", "--enabled", "--json"},
		{"repo", "settings", "auto-decline", "get", "--json"},
		{"repo", "settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4", "--json"},
		{"repo", "label", "list", "--json"},
		{"repo", "default-task", "list", "--json"},
		{"repo", "default-task", "add", "task1", "--json"},
		{"repo", "default-task", "update", "123", "--description", "task1", "--json"},
		{"webhook", "get", "1", "--json"},
		{"webhook", "update", "1", "--name", "hook1-updated", "--json"},
		{"webhook", "test", "1", "--json"},
		{"webhook", "stats", "1", "--json"},
		{"webhook", "stats", "1", "--summary", "--json"},
		{"webhook", "list", "--json"},
	}
	for _, args := range jsonRuns {
		out, err := executeTestCLI(t, args...)
		if err != nil {
			t.Fatalf("json run failed for %v: %v", args, err)
		}
		if !strings.HasPrefix(strings.TrimSpace(out), "{") && !strings.HasPrefix(strings.TrimSpace(out), "[") {
			t.Fatalf("unexpected non-json output for %v: %s", args, out)
		}
	}

	// 3. Flags and validation branches
	_, err := executeTestCLI(t, "webhook", "update", "1", "--active", "true")
	if err != nil {
		t.Fatalf("webhook update with --active true failed: %v", err)
	}
	_, err = executeTestCLI(t, "webhook", "update", "1", "--active", "false")
	if err != nil {
		t.Fatalf("webhook update with --active false failed: %v", err)
	}
	_, err = executeTestCLI(t, "webhook", "update", "1", "--active", "invalid")
	if err == nil {
		t.Fatal("expected validation error for invalid --active value")
	}

	_, err = executeTestCLI(t, "webhook", "update", "1", "--event", "repo:refs_changed,repo:modified")
	if err != nil {
		t.Fatalf("webhook update with --event failed: %v", err)
	}

	_, err = executeTestCLI(t, "repo", "default-task", "add", "task1", "--source-ref", "refs/heads/feature", "--target-ref", "refs/heads/master")
	if err != nil {
		t.Fatalf("default-task add with refs failed: %v", err)
	}

	_, err = executeTestCLI(t, "repo", "default-task", "update", "123", "--description", "task1", "--source-ref", "refs/heads/feature", "--target-ref", "refs/heads/master")
	if err != nil {
		t.Fatalf("default-task update with refs failed: %v", err)
	}

	_, err = executeTestCLI(t, "repo", "settings", "auto-decline", "set", "--enabled")
	if err == nil {
		t.Fatal("expected validation error for auto-decline set without inactivity weeks")
	}
}

func TestNewCLICommandsErrorPaths(t *testing.T) {
	// 1. Client configuration failure (BITBUCKET_URL=://invalid)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "://invalid")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	errorCmds := [][]string{
		{"repo", "settings", "auto-merge", "get"},
		{"repo", "settings", "auto-merge", "set", "--enabled"},
		{"repo", "settings", "auto-merge", "delete"},
		{"repo", "settings", "auto-decline", "get"},
		{"repo", "settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4"},
		{"repo", "settings", "auto-decline", "delete"},
		{"repo", "label", "list"},
		{"repo", "label", "add", "label3"},
		{"repo", "label", "remove", "label1"},
		{"repo", "watch"},
		{"repo", "unwatch"},
		{"repo", "default-task", "list"},
		{"repo", "default-task", "add", "task1"},
		{"repo", "default-task", "update", "123", "--description", "task1-updated"},
		{"repo", "default-task", "delete", "123"},
		{"webhook", "get", "1"},
		{"webhook", "update", "1", "--name", "hook1-updated"},
		{"webhook", "test", "1"},
		{"webhook", "stats", "1"},
		{"webhook", "stats", "1", "--summary"},
	}

	for _, args := range errorCmds {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for command %v with invalid URL", args)
		}
	}

	// 2. Invalid repo format (e.g. --repo invalid)
	t.Setenv("BITBUCKET_URL", "http://localhost")
	for _, args := range errorCmds {
		fullArgs := append([]string(nil), args...)
		fullArgs = append(fullArgs, "--repo", "invalid")
		cmd := NewRootCommand()
		cmd.SetArgs(fullArgs)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for command %v with invalid repo format", fullArgs)
		}
	}

	// 3. Server error (HTTP 500)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	for _, args := range errorCmds {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for command %v with HTTP 500 response", args)
		}
	}

	// 4. Dry-run Server error / Permission check failure
	dryRunCmds := [][]string{
		{"repo", "settings", "auto-merge", "set", "--enabled", "--dry-run"},
		{"repo", "settings", "auto-merge", "delete", "--dry-run"},
		{"repo", "settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4", "--dry-run"},
		{"repo", "settings", "auto-decline", "delete", "--dry-run"},
		{"repo", "label", "add", "label3", "--dry-run"},
		{"repo", "label", "remove", "label1", "--dry-run"},
		{"repo", "watch", "--dry-run"},
		{"repo", "unwatch", "--dry-run"},
		{"repo", "default-task", "add", "task1", "--dry-run"},
		{"repo", "default-task", "update", "123", "--description", "task1-updated", "--dry-run"},
		{"repo", "default-task", "delete", "123", "--dry-run"},
		{"webhook", "update", "1", "--name", "hook1-updated", "--dry-run"},
		{"webhook", "test", "1", "--dry-run"},
	}

	for _, args := range dryRunCmds {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Errorf("expected error for dry-run command %v with HTTP 500 response", args)
		}
	}
}

func TestIssue221CLICommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		// Scoped builds
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/commits/abc/builds":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/commits/abc/builds" && r.URL.Query().Get("key") == "ci/main":
			_, _ = w.Write([]byte(`{"key":"ci/main","state":"SUCCESSFUL","url":"https://ci.example"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/commits/abc/builds" && r.URL.Query().Get("key") == "ci/main":
			w.WriteHeader(http.StatusNoContent)

		// Multi-commit stats
		case r.Method == http.MethodPost && r.URL.Path == "/rest/build-status/latest/commits/stats":
			_, _ = w.Write([]byte(`{"abc":{"successful":1,"failed":0,"inProgress":0,"unknown":0,"cancelled":0}}`))

		// Deployments
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/commits/abc/deployments":
			_, _ = w.Write([]byte(`{"key":"dep1","displayName":"deploy1","state":"SUCCESSFUL","url":"https://deploy.example"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/commits/abc/deployments" && r.URL.Query().Get("key") == "dep1":
			_, _ = w.Write([]byte(`{"key":"dep1","displayName":"deploy1","state":"SUCCESSFUL","url":"https://deploy.example"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo/commits/abc/deployments" && r.URL.Query().Get("key") == "dep1":
			w.WriteHeader(http.StatusNoContent)

		// Insights annotations
		case r.Method == http.MethodPut && r.URL.Path == "/rest/insights/latest/projects/PRJ/repos/repo/commits/abc/reports/lint/annotations/a1":
			_, _ = w.Write([]byte(`{"externalId":"a1","message":"fixed","severity":"LOW"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/insights/latest/projects/PRJ/repos/repo/commits/abc/reports/lint/annotations":
			_, _ = w.Write([]byte(`{"annotations":[{"externalId":"a1","message":"fixed","severity":"LOW"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/insights/latest/projects/PRJ/repos/repo/commits/abc/annotations":
			_, _ = w.Write([]byte(`{"annotations":[{"externalId":"a1","message":"fixed","severity":"LOW"}]}`))

		// Permission checks (for dry-run)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"values":[{"slug":"repo","project":{"key":"PRJ"}}],"isLastPage":true}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// 1. Scoped Builds
	out, err := executeTestCLI(t, "build", "set", "abc", "--key", "ci/main", "--state", "SUCCESSFUL", "--url", "https://ci.example")
	if err != nil {
		t.Fatalf("build set failed: %v", err)
	}
	if !strings.Contains(out, "Repository-scoped build status ci/main set") {
		t.Fatalf("unexpected build set output: %s", out)
	}

	out, err = executeTestCLI(t, "build", "get", "abc", "--key", "ci/main")
	if err != nil {
		t.Fatalf("build get failed: %v", err)
	}
	if !strings.Contains(out, "ci/main") {
		t.Fatalf("unexpected build get output: %s", out)
	}

	out, err = executeTestCLI(t, "build", "delete", "abc", "--key", "ci/main")
	if err != nil {
		t.Fatalf("build delete failed: %v", err)
	}
	if !strings.Contains(out, "Deleted repository-scoped build status ci/main") {
		t.Fatalf("unexpected build delete output: %s", out)
	}

	// 2. Multi-commit stats
	out, err = executeTestCLI(t, "build", "status", "stats", "abc", "def")
	if err != nil {
		t.Fatalf("build status stats failed: %v", err)
	}
	if !strings.Contains(out, "COMMIT") || !strings.Contains(out, "abc") {
		t.Fatalf("unexpected stats table: %s", out)
	}

	// 3. Deployments
	out, err = executeTestCLI(t, "deployment", "create", "abc",
		"--deployment-sequence-number", "1",
		"--display-name", "deploy1",
		"--key", "dep1",
		"--state", "SUCCESSFUL",
		"--url", "https://deploy.example",
		"--env-key", "prod",
		"--env-name", "Production",
	)
	if err != nil {
		t.Fatalf("deployment create failed: %v", err)
	}
	if !strings.Contains(out, "Deployment dep1 (deploy1) set") {
		t.Fatalf("unexpected deployment create output: %s", out)
	}

	out, err = executeTestCLI(t, "deployment", "get", "abc", "--key", "dep1")
	if err != nil {
		t.Fatalf("deployment get failed: %v", err)
	}
	if !strings.Contains(out, "dep1") {
		t.Fatalf("unexpected deployment get output: %s", out)
	}

	out, err = executeTestCLI(t, "deployment", "delete", "abc", "--key", "dep1")
	if err != nil {
		t.Fatalf("deployment delete failed: %v", err)
	}
	if !strings.Contains(out, "Deleted deployment") {
		t.Fatalf("unexpected deployment delete output: %s", out)
	}

	// 4. Insights Annotations
	out, err = executeTestCLI(t, "insights", "annotation", "set", "abc", "lint", "a1", "--message", "fixed", "--severity", "LOW")
	if err != nil {
		t.Fatalf("insights annotation set failed: %v", err)
	}
	if !strings.Contains(out, "Annotation a1 set on report lint") {
		t.Fatalf("unexpected annotation set output: %s", out)
	}

	out, err = executeTestCLI(t, "insights", "annotation", "list", "abc")
	if err != nil {
		t.Fatalf("insights annotation list commit-level failed: %v", err)
	}
	if !strings.Contains(out, "fixed") {
		t.Fatalf("unexpected annotation list commit-level output: %s", out)
	}

	// 5. Dry-run support
	dryRunCmds := [][]string{
		{"build", "set", "abc", "--key", "ci/main", "--state", "SUCCESSFUL", "--url", "https://ci.example", "--dry-run"},
		{"build", "set", "abc", "--key", "ci/other", "--state", "SUCCESSFUL", "--url", "https://ci.example", "--dry-run"}, // predicted: create
		{"build", "delete", "abc", "--key", "ci/main", "--dry-run"}, // predicted: delete
		{"build", "delete", "abc", "--key", "ci/other", "--dry-run"}, // predicted: no-op
		{"deployment", "create", "abc",
			"--deployment-sequence-number", "1",
			"--display-name", "deploy1",
			"--key", "dep1",
			"--state", "SUCCESSFUL",
			"--url", "https://deploy.example",
			"--env-key", "prod",
			"--env-name", "Production",
			"--dry-run",
		}, // predicted: update
		{"deployment", "create", "abc",
			"--deployment-sequence-number", "1",
			"--display-name", "deploy1",
			"--key", "dep-new",
			"--state", "SUCCESSFUL",
			"--url", "https://deploy.example",
			"--env-key", "prod",
			"--env-name", "Production",
			"--dry-run",
		}, // predicted: create
		{"deployment", "delete", "abc", "--key", "dep1", "--dry-run"}, // predicted: delete
		{"deployment", "delete", "abc", "--key", "dep-new", "--dry-run"}, // predicted: no-op
		{"insights", "annotation", "set", "abc", "lint", "a1", "--message", "fixed", "--severity", "LOW", "--dry-run"}, // predicted: update
		{"insights", "annotation", "set", "abc", "lint", "a2", "--message", "fixed", "--severity", "LOW", "--dry-run"}, // predicted: create
	}

	for _, args := range dryRunCmds {
		cmd := NewRootCommand()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err != nil {
			t.Errorf("expected success for dry-run command %v, got %v", args, err)
		}
	}

	// 6. JSON output testing for scoped builds, stats, deployments, annotations
	jsonCmds := [][]string{
		{"build", "set", "abc", "--key", "ci/main", "--state", "SUCCESSFUL", "--url", "https://ci.example", "--json"},
		{"build", "get", "abc", "--key", "ci/main", "--json"},
		{"build", "delete", "abc", "--key", "ci/main", "--json"},
		{"build", "status", "stats", "abc", "def", "--json"},
		{"deployment", "create", "abc",
			"--deployment-sequence-number", "1",
			"--display-name", "deploy1",
			"--key", "dep1",
			"--state", "SUCCESSFUL",
			"--url", "https://deploy.example",
			"--env-key", "prod",
			"--env-name", "Production",
			"--json",
		},
		{"deployment", "get", "abc", "--key", "dep1", "--json"},
		{"deployment", "delete", "abc", "--key", "dep1", "--json"},
		{"insights", "annotation", "set", "abc", "lint", "a1", "--message", "fixed", "--severity", "LOW", "--json"},
		{"insights", "annotation", "list", "abc", "--json"},
	}
	for _, args := range jsonCmds {
		out, err := executeTestCLI(t, args...)
		if err != nil {
			t.Fatalf("json run failed for %v: %v", args, err)
		}
		if !strings.HasPrefix(strings.TrimSpace(out), "{") && !strings.HasPrefix(strings.TrimSpace(out), "[") {
			t.Fatalf("unexpected non-json output for %v: %s", args, out)
		}
	}

	// 7. Optional flags testing for build set and insights annotation set
	_, err = executeTestCLI(t, "build", "set", "abc",
		"--key", "ci/main",
		"--state", "SUCCESSFUL",
		"--url", "https://ci.example",
		"--name", "Build Name",
		"--description", "Description",
		"--ref", "refs/heads/main",
		"--parent", "ci",
		"--build-number", "123",
		"--duration-ms", "1000",
	)
	if err != nil {
		t.Fatalf("build set with optional flags failed: %v", err)
	}

	_, err = executeTestCLI(t, "insights", "annotation", "set", "abc", "lint", "a1",
		"--message", "fixed",
		"--severity", "LOW",
		"--path", "main.go",
		"--line", "42",
		"--link", "https://violation.example",
		"--type", "BUG",
	)
	if err != nil {
		t.Fatalf("insights annotation set with optional flags failed: %v", err)
	}

	// 8. Error/Validation paths for CLI commands (to cover error checking code)
	invalidCmds := [][]string{
		{"build", "set", "abc", "--key", "", "--state", "SUCCESSFUL", "--url", "u"},
		{"build", "set", "abc", "--key", "k", "--state", "", "--url", "u"},
		{"build", "set", "abc", "--key", "k", "--state", "s", "--url", ""},
		{"build", "get", "abc", "--key", ""},
		{"build", "delete", "abc", "--key", ""},
		{"build", "status", "stats"}, // missing args
		{"deployment", "create", "abc", "--key", ""},
		{"deployment", "get", "abc", "--key", ""},
		{"deployment", "delete", "abc", "--key", ""},
		{"insights", "annotation", "set", "abc", "lint", ""},
	}
	for _, args := range invalidCmds {
		_, err := executeTestCLI(t, args...)
		if err == nil {
			t.Errorf("expected validation error for invalid command args: %v", args)
		}
	}
}


