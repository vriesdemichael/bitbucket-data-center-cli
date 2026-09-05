package bulkcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
	bulkworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/bulk"
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

	// Nothing on stdout. This test used to require the status envelope here,
	// which is what made the failure path write two documents: cmd/bb writes
	// the error envelope for the returned error, and a consumer then reads one
	// document too many -- a strict decoder errors, jq quietly returns a result
	// per document and exits 0. Under --json the failure envelope is the one
	// document (ADR-075).
	if written := strings.TrimSpace(buffer.String()); written != "" {
		t.Fatalf("a failing apply wrote to stdout under --json; cmd/bb appends the error envelope after it:\n%s", written)
	}

	// The operation id is in the error, because it is the only handle the
	// caller is left with -- `bb bulk status <id>` returns the artifact.
	operationID := operationIDFrom(t, err)

	statusCommand := New(testDependencies(server.URL))
	statusBuffer := &bytes.Buffer{}
	statusCommand.SetOut(statusBuffer)
	statusCommand.SetErr(statusBuffer)
	statusCommand.SetArgs([]string{"status", operationID})
	if statusErr := statusCommand.Execute(); statusErr != nil {
		t.Fatalf("the id named in the error does not resolve: %v", statusErr)
	}

	var status bulkworkflow.ApplyStatus
	if decodeErr := decodeJSONEnvelopeData(statusBuffer.Bytes(), &status); decodeErr != nil {
		t.Fatalf("expected structured JSON status, got %q (%v)", statusBuffer.String(), decodeErr)
	}
	if status.Status != "failed" {
		t.Fatalf("expected failed apply status, got %s", status.Status)
	}
	if status.Targets[0].Operations[0].Status != "failed" {
		t.Fatalf("expected failed operation, got %#v", status.Targets[0].Operations)
	}
}

// operationIDFrom reads the operation id off the error as a field.
//
// Deliberately not by scanning the message. An earlier version did exactly
// that, which meant the test agreed with the skill in telling callers to scrape
// a sentence for an identifier no schema described -- the failure #474 was
// about. Reading error.details is what a consumer does, so it is what the test
// does.
func operationIDFrom(t *testing.T, err error) string {
	t.Helper()

	details := apperrors.DetailsOf(err)
	if id := details["operationId"]; id != "" {
		return id
	}

	t.Fatalf("the error carries no operationId detail, so the artifact cannot be reached: %v (details: %v)", err, details)
	return ""
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

// TestBulkApplyReportsCancellationWithoutLosingTheArtifact covers the two
// halves of ADR-075 for an interrupted run.
//
// Under --json the failure envelope is the one document cmd/bb writes, so the
// command must put nothing on stdout and must name the operation id in the
// error -- otherwise the caller is holding a saved artifact with no handle to
// fetch it with. Under human output the status is printed instead, because the
// error line goes to stderr and the two do not collide.
//
// The context is cancelled before the command runs. That is the between-target
// check firing on the first target, which is deterministic; the in-flight case
// is covered where it belongs, in the workflow package.
func TestBulkApplyReportsCancellationWithoutLosingTheArtifact(t *testing.T) {
	tempDir := t.TempDir()
	statusDir := filepath.Join(tempDir, "status")
	t.Setenv("BB_BULK_STATUS_DIR", statusDir)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "config.yaml"))

	planner := bulkworkflow.NewPlanner(fakeCatalog{repositories: map[string][]repository.Repository{
		"PRJ": {
			{ProjectKey: "PRJ", Slug: "repo-a", Name: "Repo A"},
			{ProjectKey: "PRJ", Slug: "repo-b", Name: "Repo B"},
		},
	}})
	plan, err := planner.Plan(context.TODO(), bulkworkflow.Policy{
		APIVersion: bulkworkflow.APIVersion,
		Selector:   bulkworkflow.Selector{ProjectKey: "PRJ"},
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

	// No server: an interrupted run must not reach one.
	runCancelled := func(t *testing.T, jsonEnabled bool) (string, error) {
		t.Helper()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		deps := testDependencies("http://127.0.0.1:1")
		deps.JSONEnabled = func() bool { return jsonEnabled }

		command := New(deps)
		buffer := &bytes.Buffer{}
		command.SetOut(buffer)
		command.SetErr(&bytes.Buffer{})
		command.SetArgs([]string{"apply", "--from-plan", planPath})

		// Run first: Go evaluates return operands left to right, so reading the
		// buffer in the return statement reads it before the command has run.
		executeErr := command.ExecuteContext(ctx)

		return buffer.String(), executeErr
	}

	t.Run("machine output writes nothing and names the id", func(t *testing.T) {
		stdout, err := runCancelled(t, true)
		if err == nil {
			t.Fatal("an interrupted run reported success")
		}

		// Exit 12, not 10. Transient is documented to agents as "retry later",
		// and retrying a bulk apply replays mutations across the whole plan.
		if code := apperrors.ExitCode(err); code != 12 {
			t.Errorf("exit code = %d, want 12 (cancelled); %v", code, err)
		}
		if kind := apperrors.KindOf(err); kind != apperrors.KindCancelled {
			t.Errorf("kind = %q, want %q", kind, apperrors.KindCancelled)
		}

		if written := strings.TrimSpace(stdout); written != "" {
			t.Fatalf("wrote to stdout under --json; cmd/bb appends the error envelope after it (ADR-075):\n%s", written)
		}

		operationID := operationIDFrom(t, err)

		// The handle has to actually resolve, which is the whole reason it is
		// in the message.
		statusCommand := New(testDependencies("http://127.0.0.1:1"))
		statusBuffer := &bytes.Buffer{}
		statusCommand.SetOut(statusBuffer)
		statusCommand.SetErr(statusBuffer)
		statusCommand.SetArgs([]string{"status", operationID})
		if statusErr := statusCommand.Execute(); statusErr != nil {
			t.Fatalf("the id named in the error does not resolve: %v", statusErr)
		}

		var status bulkworkflow.ApplyStatus
		if decodeErr := decodeJSONEnvelopeData(statusBuffer.Bytes(), &status); decodeErr != nil {
			t.Fatalf("saved artifact is not readable: %v\n%s", decodeErr, statusBuffer.String())
		}
		if status.Status != "cancelled" {
			t.Errorf("saved status = %q, want cancelled", status.Status)
		}
		if status.Summary.CancelledTargets != len(plan.Targets) {
			t.Errorf("cancelled targets = %d, want %d; the artifact must account for every repository",
				status.Summary.CancelledTargets, len(plan.Targets))
		}
		if status.Summary.FailedTargets != 0 {
			t.Errorf("failed targets = %d, want 0; nothing was tried", status.Summary.FailedTargets)
		}
	})

	t.Run("human output prints the status", func(t *testing.T) {
		stdout, err := runCancelled(t, false)
		if err == nil {
			t.Fatal("an interrupted run reported success")
		}

		// stderr carries the error line, so stdout keeps the detail and the
		// reader needs no handle to recover it.
		if !strings.Contains(stdout, "cancelled") {
			t.Errorf("the human status does not say the run was cancelled:\n%s", stdout)
		}
		if !strings.Contains(stdout, "bb bulk status ") {
			t.Errorf("the human status does not say how to reach the artifact:\n%s", stdout)
		}
	})
}

// TestEveryNonZeroSummaryCounterReachesTheHumanOutput is the guard on the
// second renderer.
//
// bb has two renderings of the same result: the JSON payload and the human
// summary. Nothing tied them together, so cancelledTargets and
// cancelledOperations were added to the model, the JSON and the published
// schema -- with tests, through four rounds of review -- and the human summary
// was never updated. An interrupted run printed "successful=0 failed=0" out of
// three targets, which reads as nothing having happened.
//
// Reflecting over the summary is what makes this hold for the next counter
// rather than only for that one. A field added to ApplySummary and left out of
// writeStatusHuman fails here.
func TestEveryNonZeroSummaryCounterReachesTheHumanOutput(t *testing.T) {
	summary := bulkworkflow.ApplySummary{}

	// Distinct values, so a counter rendered in the wrong place is still
	// caught: two fields sharing a value could cover for each other.
	summaryValue := reflect.ValueOf(&summary).Elem()
	for index := 0; index < summaryValue.NumField(); index++ {
		if field := summaryValue.Field(index); field.Kind() == reflect.Int {
			field.SetInt(int64(index + 11))
		}
	}

	buffer := &bytes.Buffer{}
	writeStatusHuman(buffer, bulkworkflow.ApplyStatus{
		OperationID: "op-1",
		Status:      "cancelled",
		Summary:     summary,
	})
	rendered := buffer.String()

	summaryType := summaryValue.Type()
	for index := 0; index < summaryValue.NumField(); index++ {
		field := summaryValue.Field(index)
		if field.Kind() != reflect.Int {
			continue
		}
		value := fmt.Sprintf("%d", field.Int())
		if !strings.Contains(rendered, value) {
			t.Errorf("summary field %s = %s never reaches the human output, so a reader cannot see it:\n%s",
				summaryType.Field(index).Name, value, rendered)
		}
	}
}

// TestTheHumanSummaryOmitsCancelledWhenThereIsNone keeps the other direction:
// `cancelled=0` on every successful apply is noise, and the JSON omits it too.
func TestTheHumanSummaryOmitsCancelledWhenThereIsNone(t *testing.T) {
	buffer := &bytes.Buffer{}
	writeStatusHuman(buffer, bulkworkflow.ApplyStatus{
		OperationID: "op-1",
		Status:      "success",
		Summary: bulkworkflow.ApplySummary{
			TargetCount:       2,
			OperationCount:    2,
			SuccessfulTargets: 2,
		},
	})

	if strings.Contains(buffer.String(), "cancelled") {
		t.Errorf("an uninterrupted run mentions cancellation:\n%s", buffer.String())
	}
}

// TestBulkHumanOutput is live now, in TestLiveBulkPolicyPlanApplyStatus: the
// same plan and status commands in human mode, against the two repositories
// the harness seeds. The unit version rendered a plan whose targets came from
// a repository listing it wrote, so "1 target" was the fixture's number and
// not the selector's.
