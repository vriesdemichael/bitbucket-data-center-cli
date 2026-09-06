package bulkcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	_, err := readFile("", "label")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestApplyFailureErrorHandling(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	cmd := New(Dependencies{})
	if cmd.Use != "bulk" {
		t.Fatal("expected bulk command")
	}
}

func TestWriteJSONFileWriteError(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

// Two more suites are live.
//
// TestBulkPlanApplyAndStatusCommands walked plan, apply and status past a
// repository listing and two settings replies this file wrote, so the two
// targets it counted were the two the fixture held.
// TestLiveBulkPolicyPlanApplyStatus does the same walk against a real project
// and reads the settings back afterwards.
//
// TestBulkApplyReturnsStructuredFailure built its failure from a 409 it served
// itself, which decided the exit code it then checked.
// TestLiveBulkApplyReportsAFailedTarget grants to a username Bitbucket does
// not know and keeps all three guarantees: the command fails, --json writes
// nothing to stdout so the error envelope is the only document (ADR-075), and
// the operation id in the error resolves through `bb bulk status`.

// operationIDFrom reads the operation id off a failed apply's error, which is
// the only handle the caller is left with: `bb bulk status <id>` is how they
// find out what did happen before the run stopped.
func operationIDFrom(t *testing.T, err error) string {
	t.Helper()

	details := apperrors.DetailsOf(err)
	if id := details["operationId"]; id != "" {
		return id
	}

	t.Fatalf("the error carries no operationId detail, so the artifact cannot be reached: %v (details: %v)", err, details)

	return ""
}
