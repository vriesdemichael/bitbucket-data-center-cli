package bulkcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
	bulkworkflow "github.com/vriesdemichael/bitbucket-server-cli/internal/workflows/bulk"
)

type fakeCatalog struct {
	repositories map[string][]repository.Repository
}

func (catalog fakeCatalog) ListByProject(_ context.Context, projectKey string, _ repository.ListOptions) ([]repository.Repository, error) {
	return append([]repository.Repository(nil), catalog.repositories[projectKey]...), nil
}

func testDependencies(serverURL string) Dependencies {
	return Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL:       serverURL,
				ProjectKey:         "PRJ",
				RequestTimeout:     5 * time.Second,
				RetryCount:         0,
				RetryBackoff:       time.Millisecond,
				LogLevel:           "error",
				LogFormat:          "text",
				DiagnosticsEnabled: false,
			}, nil
		},
	}
}

func TestBulkPlanApplyAndStatusCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/1.0/projects/PRJ/repos":
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"slug":"repo-a","name":"Repo A","public":false,"project":{"key":"PRJ"}},{"slug":"repo-b","name":"Repo B","public":false,"project":{"key":"PRJ"}}]}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo-a/settings/pull-requests":
			_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":true}`))
		case request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo-b/settings/pull-requests":
			_, _ = writer.Write([]byte(`{"requiredAllTasksComplete":true}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	defer server.Close()

	tempDir := t.TempDir()
	statusDir := filepath.Join(tempDir, "status")
	t.Setenv("BB_BULK_STATUS_DIR", statusDir)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "config.yaml"))

	policyPath := filepath.Join(tempDir, "policy.yaml")
	planPath := filepath.Join(tempDir, "plan.json")
	policy := strings.Join([]string{
		"apiVersion: bb.io/v1alpha1",
		"selector:",
		"  projectKey: PRJ",
		"  repoPattern: repo-*",
		"operations:",
		"  - type: repo.pull-request-settings.required-all-tasks-complete",
		"    requiredAllTasksComplete: true",
	}, "\n")
	if err := os.WriteFile(policyPath, []byte(policy), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	planCommand := New(testDependencies(server.URL))
	planOutput := &bytes.Buffer{}
	planCommand.SetOut(planOutput)
	planCommand.SetErr(planOutput)
	planCommand.SetArgs([]string{"plan", "-f", policyPath, "-o", planPath})
	if err := planCommand.Execute(); err != nil {
		t.Fatalf("plan execute failed: %v", err)
	}

	var plan bulkworkflow.Plan
	if err := decodeJSONEnvelopeData(planOutput.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan output: %v", err)
	}
	if plan.Summary.TargetCount != 2 || plan.Summary.OperationCount != 2 {
		t.Fatalf("unexpected plan summary: %#v", plan.Summary)
	}
	if strings.TrimSpace(plan.PlanHash) == "" {
		t.Fatal("expected plan hash to be populated")
	}

	rawPlan, err := os.ReadFile(planPath)
	if err != nil {
		t.Fatalf("read plan artifact: %v", err)
	}
	var persistedPlan bulkworkflow.Plan
	if err := json.Unmarshal(rawPlan, &persistedPlan); err != nil {
		t.Fatalf("decode persisted plan: %v", err)
	}
	if persistedPlan.PlanHash != plan.PlanHash {
		t.Fatalf("expected persisted plan hash %s, got %s", plan.PlanHash, persistedPlan.PlanHash)
	}

	applyCommand := New(testDependencies(server.URL))
	applyOutput := &bytes.Buffer{}
	applyCommand.SetOut(applyOutput)
	applyCommand.SetErr(applyOutput)
	applyCommand.SetArgs([]string{"apply", "--from-plan", planPath})
	if err := applyCommand.Execute(); err != nil {
		t.Fatalf("apply execute failed: %v", err)
	}

	var status bulkworkflow.ApplyStatus
	if err := decodeJSONEnvelopeData(applyOutput.Bytes(), &status); err != nil {
		t.Fatalf("decode apply output: %v", err)
	}
	if status.Summary.SuccessfulTargets != 2 || status.Summary.FailedTargets != 0 {
		t.Fatalf("unexpected apply summary: %#v", status.Summary)
	}
	if strings.TrimSpace(status.OperationID) == "" {
		t.Fatal("expected operation id")
	}

	statusCommand := New(testDependencies(server.URL))
	statusOutput := &bytes.Buffer{}
	statusCommand.SetOut(statusOutput)
	statusCommand.SetErr(statusOutput)
	statusCommand.SetArgs([]string{"status", status.OperationID})
	if err := statusCommand.Execute(); err != nil {
		t.Fatalf("status execute failed: %v", err)
	}

	var loaded bulkworkflow.ApplyStatus
	if err := decodeJSONEnvelopeData(statusOutput.Bytes(), &loaded); err != nil {
		t.Fatalf("decode status output: %v", err)
	}
	if loaded.OperationID != status.OperationID {
		t.Fatalf("expected operation id %s, got %s", status.OperationID, loaded.OperationID)
	}
}

func TestBulkApplyReturnsStructuredFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo-a/settings/pull-requests" {
			writer.WriteHeader(http.StatusConflict)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"conflict"}]}`))
			return
		}
		http.NotFound(writer, request)
	}))
	defer server.Close()

	tempDir := t.TempDir()
	t.Setenv("BB_BULK_STATUS_DIR", filepath.Join(tempDir, "status"))
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "config.yaml"))

	planner := bulkworkflow.NewPlanner(fakeCatalog{repositories: map[string][]repository.Repository{
		"PRJ": {{ProjectKey: "PRJ", Slug: "repo-a", Name: "Repo A"}},
	}})
	plan, err := planner.Plan(context.TODO(), bulkworkflow.Policy{
		APIVersion: bulkworkflow.APIVersion,
		Selector:   bulkworkflow.Selector{ProjectKey: "PRJ", Repositories: []string{"repo-a"}},
		Operations: []bulkworkflow.OperationSpec{{Type: bulkworkflow.OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPointer(true)}},
	})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}

	planPath := filepath.Join(tempDir, "plan.json")
	encodedPlan, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}
	if err := os.WriteFile(planPath, encodedPlan, 0o600); err != nil {
		t.Fatalf("write plan: %v", err)
	}

	command := New(testDependencies(server.URL))
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"apply", "--from-plan", planPath})

	err = command.Execute()
	if err == nil {
		t.Fatal("expected apply failure")
	}
	if apperrors.ExitCode(err) != 5 {
		t.Fatalf("expected conflict exit code, got %d (%v)", apperrors.ExitCode(err), err)
	}

	var status bulkworkflow.ApplyStatus
	if decodeErr := decodeJSONEnvelopeData(buffer.Bytes(), &status); decodeErr != nil {
		t.Fatalf("expected structured JSON status, got %q (%v)", buffer.String(), decodeErr)
	}
	if status.Status != "failed" {
		t.Fatalf("expected failed apply status, got %s", status.Status)
	}
	if status.Targets[0].Operations[0].Status != "failed" {
		t.Fatalf("expected failed operation, got %#v", status.Targets[0].Operations)
	}
}

func TestBulkCommandErrorPaths(t *testing.T) {
	tempDir := t.TempDir()
	deps := testDependencies("http://localhost")

	t.Run("plan missing file", func(t *testing.T) {
		cmd := New(deps)
		cmd.SetArgs([]string{"plan", "-f", filepath.Join(tempDir, "missing.yaml")})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("apply missing plan", func(t *testing.T) {
		cmd := New(deps)
		cmd.SetArgs([]string{"apply", "--from-plan", filepath.Join(tempDir, "missing.json")})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("status missing operation", func(t *testing.T) {
		t.Setenv("BB_BULK_STATUS_DIR", tempDir)
		cmd := New(deps)
		cmd.SetArgs([]string{"status", "missing-op"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestBulkHumanOutput(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("BB_BULK_STATUS_DIR", tempDir)

	deps := Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{BitbucketURL: "http://loc", ProjectKey: "P"}, nil
		},
	}

	t.Run("plan human", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"slug":"s","project":{"key":"P"}}]}`))
		}))
		defer server.Close()
		deps.LoadConfig = func() (config.AppConfig, error) {
			return config.AppConfig{BitbucketURL: server.URL, ProjectKey: "P"}, nil
		}

		policyPath := filepath.Join(tempDir, "p.yaml")
		_ = os.WriteFile(policyPath, []byte(`
apiVersion: bb.io/v1alpha1
selector:
  projectKey: P
operations:
  - type: repo.pull-request-settings.required-all-tasks-complete
    requiredAllTasksComplete: true
`), 0o600)

		cmd := New(deps)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"plan", "-f", policyPath})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("failed: %v", err)
		}
		if !strings.Contains(buf.String(), "Bulk plan ready") {
			t.Fatalf("expected human plan output, got: %s", buf.String())
		}
	})

	t.Run("apply and status human", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"requiredAllTasksComplete":true}`))
		}))
		defer server.Close()
		deps.LoadConfig = func() (config.AppConfig, error) {
			return config.AppConfig{BitbucketURL: server.URL, ProjectKey: "P"}, nil
		}

		plan := bulkworkflow.Plan{
			APIVersion: bulkworkflow.APIVersion, Kind: bulkworkflow.PlanKind,
			Policy: bulkworkflow.Policy{
				APIVersion: bulkworkflow.APIVersion, Selector: bulkworkflow.Selector{ProjectKey: "P"},
				Operations: []bulkworkflow.OperationSpec{{Type: bulkworkflow.OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPointer(true)}},
			},
			Validation: bulkworkflow.ValidationResult{Valid: true, Errors: []string{}},
			Summary:    bulkworkflow.PlanSummary{TargetCount: 1, OperationCount: 1},
			Targets: []bulkworkflow.TargetPlan{{
				Repository: bulkworkflow.RepositoryTarget{ProjectKey: "P", Slug: "s"},
				Validation: bulkworkflow.ValidationResult{Valid: true, Errors: []string{}},
				Operations: []bulkworkflow.OperationSpec{{Type: bulkworkflow.OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPointer(true)}},
			}},
		}
		hash, _ := bulkworkflow.ComputePlanHash(plan)
		plan.PlanHash = hash

		planPath := filepath.Join(tempDir, "plan.json")
		encoded, _ := json.Marshal(plan)
		_ = os.WriteFile(planPath, encoded, 0o600)

		cmd := New(deps)
		buf := &bytes.Buffer{}
		cmd.SetOut(buf)
		cmd.SetArgs([]string{"apply", "--from-plan", planPath})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("failed: %v", err)
		}
		if !strings.Contains(buf.String(), "Bulk apply") {
			t.Fatalf("expected human apply output, got: %s", buf.String())
		}

		// Extract operation id from output
		lines := strings.Split(buf.String(), "\n")
		parts := strings.Fields(lines[0])
		opID := strings.TrimSuffix(parts[2], ":")

		buf.Reset()
		cmd.SetArgs([]string{"status", opID})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("status failed: %v", err)
		}
		if !strings.Contains(buf.String(), opID) {
			t.Fatalf("expected status output for %s, got: %s", opID, buf.String())
		}
	})
}

func TestParseErrorKindCoverage(t *testing.T) {
	kinds := []string{
		"authentication", "authorization", "validation", "not_found",
		"conflict", "transient", "permanent", "not_implemented", "internal",
		"unknown",
	}
	for _, k := range kinds {
		_ = parseErrorKind(k)
	}
}

func TestStatusStoreDirDefault(t *testing.T) {
	t.Setenv("BB_BULK_STATUS_DIR", "")
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))
	dir, err := statusStoreDir()
	if err != nil || !strings.Contains(dir, "bulk-status") {
		t.Fatalf("expected bulk-status path, got %s (%v)", dir, err)
	}
}

func TestStatusStoreDirEnv(t *testing.T) {
	t.Setenv("BB_BULK_STATUS_DIR", "/tmp/bulk")
	dir, err := statusStoreDir()
	if err != nil || dir != "/tmp/bulk" {
		t.Fatalf("expected /tmp/bulk, got %s (%v)", dir, err)
	}
}

func TestWriteJSONFileErrors(t *testing.T) {
	// Use a path that is a file to trigger error in MkdirAll
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "iamfile")
	_ = os.WriteFile(filePath, []byte("iamfile"), 0o600)

	err := writeJSONFile(filepath.Join(filePath, "too", "deep"), map[string]string{"foo": "bar"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadFileEmpty(t *testing.T) {
	_, err := readFile("", "label")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyFailureErrorHandling(t *testing.T) {
	status := bulkworkflow.ApplyStatus{
		Status:  "failed",
		Summary: bulkworkflow.ApplySummary{FailedOperations: 1},
		Targets: []bulkworkflow.TargetResult{
			{
				Status: "failed",
				Operations: []bulkworkflow.OperationResult{
					{Status: "failed", ErrorKind: "conflict"},
				},
			},
		},
	}
	err := applyFailureError(status)
	if err == nil || apperrors.ExitCode(err) != 5 {
		t.Fatalf("expected conflict exit code, got %v", err)
	}
}

func TestNewCommandDefaults(t *testing.T) {
	cmd := New(Dependencies{})
	if cmd.Use != "bulk" {
		t.Fatal("expected bulk command")
	}
}

func TestWriteJSONFileWriteError(t *testing.T) {
	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "readonly")
	_ = os.WriteFile(filePath, []byte("iamfile"), 0o400)

	err := writeJSONFile(filePath, map[string]string{"foo": "bar"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func boolPointer(value bool) *bool {
	return &value
}

func decodeJSONEnvelopeData(raw []byte, target any) error {
	envelope := map[string]any{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return err
	}

	rawData, ok := envelope["data"]
	if !ok {
		return os.ErrInvalid
	}

	encodedData, err := json.Marshal(rawData)
	if err != nil {
		return err
	}

	return json.Unmarshal(encodedData, target)
}
