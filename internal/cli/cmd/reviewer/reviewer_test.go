package reviewercmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestReviewerConditionCommands(t *testing.T) {
	emptyRepoConditions := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/default-reviewers/latest/projects/PRJ/repos/repo1/conditions":
			if emptyRepoConditions {
				_, _ = w.Write([]byte(`[]`))
				return
			}
			_, _ = w.Write([]byte(`[{"id":101,"requiredApprovals":1}]`))

		case r.Method == http.MethodGet && path == "/rest/default-reviewers/latest/projects/PRJ/conditions":
			_, _ = w.Write([]byte(`[{"id":102,"requiredApprovals":2}]`))

		case r.Method == http.MethodPost && path == "/rest/default-reviewers/latest/projects/PRJ/repos/repo1/condition":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":103,"requiredApprovals":1}`))

		case r.Method == http.MethodPost && path == "/rest/default-reviewers/latest/projects/PRJ/condition":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":104,"requiredApprovals":2}`))

		case r.Method == http.MethodPut && path == "/rest/default-reviewers/latest/projects/PRJ/repos/repo1/condition/101":
			_, _ = w.Write([]byte(`{"id":101,"requiredApprovals":3}`))

		case r.Method == http.MethodPut && path == "/rest/default-reviewers/latest/projects/PRJ/condition/102":
			_, _ = w.Write([]byte(`{"id":102,"requiredApprovals":3}`))

		case r.Method == http.MethodDelete && path == "/rest/default-reviewers/latest/projects/PRJ/repos/repo1/condition/101":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && path == "/rest/default-reviewers/latest/projects/PRJ/condition/102":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{
		BitbucketURL: server.URL,
		ProjectKey:   "PRJ",
	}

	jsonEnabled := false
	dryRunEnabled := false

	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonEnabled },
		DryRunEnabled: func() bool { return dryRunEnabled },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
	}

	// 1. list repo in human mode
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list repo: %v", err)
	}
	if !strings.Contains(buf.String(), "1 conditions") {
		t.Fatalf("expected 1 conditions in list output: %s", buf.String())
	}

	// 2. list repo in JSON mode
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list repo JSON: %v", err)
	}
	if !strings.Contains(buf.String(), `"conditions"`) {
		t.Fatalf("expected conditions in JSON list output: %s", buf.String())
	}
	jsonEnabled = false

	// 3. list project in human mode and JSON mode
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "list", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list project: %v", err)
	}
	if !strings.Contains(buf.String(), "1 conditions") {
		t.Fatalf("expected 1 conditions in list output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "list", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list project JSON: %v", err)
	}
	jsonEnabled = false

	// 4. create condition on repo in dry-run mode (create vs conflict)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "create", `{"requiredApprovals":5}`, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create condition repo dry-run: %v", err)
	}
	dryRunEnabled = false

	// 5. create condition on repo (real and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "create", `{"requiredApprovals":1}`, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create condition repo: %v", err)
	}
	if !strings.Contains(buf.String(), "Created reviewer condition") {
		t.Fatalf("expected Created reviewer condition in output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "create", `{"requiredApprovals":1}`, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create condition repo JSON: %v", err)
	}
	jsonEnabled = false

	// 6. create condition on project in dry-run mode
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "create", `{"requiredApprovals":2}`, "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create condition project dry-run: %v", err)
	}
	dryRunEnabled = false

	// 7. create condition on project (real and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "create", `{"requiredApprovals":2}`, "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create condition project: %v", err)
	}
	if !strings.Contains(buf.String(), "Created reviewer condition") {
		t.Fatalf("expected Created reviewer condition in output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "create", `{"requiredApprovals":2}`, "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create condition project JSON: %v", err)
	}
	jsonEnabled = false

	// 8. create condition with invalid JSON -> validation error
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "create", `invalid-json`, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error for invalid json in create condition")
	}

	// 9. update condition on repo in dry-run mode (update, no-op, blocked)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "101", `{"requiredApprovals":3}`, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update condition repo dry-run: %v", err)
	}
	// No-op preview (matching requiredApprovals: 1)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "101", `{"requiredApprovals":1}`, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update condition repo dry-run no-op: %v", err)
	}
	// Blocked preview (condition not found)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "999", `{"requiredApprovals":3}`, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update non-existent condition dry-run: %v", err)
	}
	dryRunEnabled = false

	// 10. update condition on repo (real and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "101", `{"requiredApprovals":3}`, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update condition repo: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated reviewer condition") {
		t.Fatalf("expected Updated reviewer condition in output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "101", `{"requiredApprovals":3}`, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update condition repo JSON: %v", err)
	}
	jsonEnabled = false

	// 11. update condition on project in dry-run mode (update, no-op, blocked)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "102", `{"requiredApprovals":3}`, "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update condition project dry-run: %v", err)
	}
	// No-op preview (matching requiredApprovals: 2)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "102", `{"requiredApprovals":2}`, "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update condition project dry-run no-op: %v", err)
	}
	// Blocked preview
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "999", `{"requiredApprovals":3}`, "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update non-existent project condition dry-run: %v", err)
	}
	dryRunEnabled = false

	// 12. update condition on project (real and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "102", `{"requiredApprovals":3}`, "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update condition project: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated reviewer condition") {
		t.Fatalf("expected Updated reviewer condition in output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "102", `{"requiredApprovals":3}`, "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update condition project JSON: %v", err)
	}
	jsonEnabled = false

	// 13. delete repo condition in dry-run mode (found vs not found)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "delete", "101", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete repo dry-run: %v", err)
	}
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "delete", "999", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete repo dry-run not found: %v", err)
	}
	dryRunEnabled = false

	// 14. delete repo condition (real and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "delete", "101", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete repo: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted condition") {
		t.Fatalf("expected Deleted condition in delete output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "delete", "101", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete repo JSON: %v", err)
	}
	jsonEnabled = false

	// 15. delete project condition in dry-run mode (found vs not found)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "delete", "102", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete project dry-run: %v", err)
	}
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "delete", "999", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete project dry-run not found: %v", err)
	}
	dryRunEnabled = false

	// 16. delete project condition (real and JSON)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "delete", "102", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete project: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted condition") {
		t.Fatalf("expected Deleted condition in delete output: %s", buf.String())
	}
	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "delete", "102", "--project", "PRJ"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete project JSON: %v", err)
	}
	jsonEnabled = false

	// 17. create & update condition with config file
	tempConfigFile := filepath.Join(t.TempDir(), "condition.json")
	if err := os.WriteFile(tempConfigFile, []byte(`{"requiredApprovals":1}`), 0o644); err != nil {
		t.Fatalf("failed to write temp config file: %v", err)
	}
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "create", "--config-file", tempConfigFile, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error creating condition with config-file: %v", err)
	}
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "101", "--config-file", tempConfigFile, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error updating condition with config-file: %v", err)
	}

	// 18. mutually exclusive config-file and inline arg validation error
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "create", `{"requiredApprovals":1}`, "--config-file", tempConfigFile, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when both arg and --config-file are provided")
	}
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "update", "101", `{"requiredApprovals":1}`, "--config-file", tempConfigFile, "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when both arg and --config-file are provided for update")
	}

	// 19. Empty reviewer conditions list
	emptyRepoConditions = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on empty repo conditions: %v", err)
	}
	if !strings.Contains(buf.String(), "No conditions found") {
		t.Fatalf("expected No conditions found in list output: %s", buf.String())
	}
	emptyRepoConditions = false

	// 20. Missing project key validation error
	cfgNoProject := config.AppConfig{BitbucketURL: server.URL}
	depsNoProject := deps
	depsNoProject.LoadConfig = func() (config.AppConfig, error) { return cfgNoProject, nil }
	depsNoProject.LoadConfigAndClient = func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
		client, err := openapi.NewClientWithResponsesFromConfig(cfgNoProject)
		return cfgNoProject, client, err
	}
	cmd = New(depsNoProject)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"condition", "list"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when project key is missing")
	}
}
