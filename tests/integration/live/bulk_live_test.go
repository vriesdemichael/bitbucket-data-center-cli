//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	reposettings "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
	bulkworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/bulk"
)

func TestLiveBulkPolicyPlanApplyStatus(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 2, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	currentSettings, err := service.GetRepositoryPullRequestSettings(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: seeded.Repos[0].Slug})
	if err != nil {
		t.Fatalf("get baseline pull request settings failed: %v", err)
	}

	currentValue, _ := currentSettings["requiredAllTasksComplete"].(bool)
	targetValue := !currentValue

	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "bulk-policy.yaml")
	planPath := filepath.Join(tempDir, "bulk-plan.json")
	policy := strings.Join([]string{
		"apiVersion: bb.io/v1alpha1",
		"selector:",
		"  projectKey: " + seeded.Key,
		"  repoPattern: lt-repo-*",
		"operations:",
		"  - type: repo.pull-request-settings.required-all-tasks-complete",
		"    requiredAllTasksComplete: " + strings.ToLower(strconvBool(targetValue)),
	}, "\n")
	if err := os.WriteFile(policyPath, []byte(policy), 0o600); err != nil {
		t.Fatalf("write bulk policy: %v", err)
	}

	planOutput, err := executeLiveCLI(t, "--json", "bulk", "plan", "-f", policyPath, "-o", planPath)
	if err != nil {
		t.Fatalf("bulk plan failed: %v\noutput: %s", err, planOutput)
	}

	var plan bulkworkflow.Plan
	if err := decodeJSONEnvelopeData(planOutput, &plan); err != nil {
		t.Fatalf("decode bulk plan output: %v\noutput: %s", err, planOutput)
	}
	if plan.Summary.TargetCount != 2 || len(plan.Targets) != 2 {
		t.Fatalf("expected 2 targets in bulk plan, got %#v", plan.Summary)
	}
	if strings.TrimSpace(plan.PlanHash) == "" {
		t.Fatal("expected bulk plan hash")
	}

	applyOutput, err := executeLiveCLI(t, "--json", "bulk", "apply", "--from-plan", planPath)
	if err != nil {
		t.Fatalf("bulk apply failed: %v\noutput: %s", err, applyOutput)
	}

	var status bulkworkflow.ApplyStatus
	if err := decodeJSONEnvelopeData(applyOutput, &status); err != nil {
		t.Fatalf("decode bulk apply output: %v\noutput: %s", err, applyOutput)
	}
	if status.Summary.SuccessfulTargets != 2 || status.Summary.FailedTargets != 0 {
		t.Fatalf("unexpected bulk apply summary: %#v", status.Summary)
	}
	if strings.TrimSpace(status.OperationID) == "" {
		t.Fatal("expected bulk operation id")
	}

	statusOutput, err := executeLiveCLI(t, "--json", "bulk", "status", status.OperationID)
	if err != nil {
		t.Fatalf("bulk status failed: %v\noutput: %s", err, statusOutput)
	}

	var loaded bulkworkflow.ApplyStatus
	if err := decodeJSONEnvelopeData(statusOutput, &loaded); err != nil {
		t.Fatalf("decode bulk status output: %v\noutput: %s", err, statusOutput)
	}
	if loaded.OperationID != status.OperationID {
		t.Fatalf("expected status operation id %s, got %s", status.OperationID, loaded.OperationID)
	}

	// The human renderings, against the same real project. A unit test held
	// these against a repository listing and a settings reply it wrote itself,
	// so the target count it printed was the count the fixture contained.
	humanPlan := mustLiveHumanCLI(t, "bulk", "plan", "-f", policyPath)
	if !strings.Contains(humanPlan, "Bulk plan ready") {
		t.Fatalf("expected the human plan summary, got: %s", humanPlan)
	}
	for _, repo := range seeded.Repos {
		if !strings.Contains(humanPlan, seeded.Key+"/"+repo.Slug) {
			t.Fatalf("the human plan named no target %s/%s:\n%s", seeded.Key, repo.Slug, humanPlan)
		}
	}

	humanStatus := mustLiveHumanCLI(t, "bulk", "status", status.OperationID)
	if !strings.Contains(humanStatus, "Plan hash:") || !strings.Contains(humanStatus, status.OperationID) {
		t.Fatalf("expected the human status to name the plan hash and operation, got: %s", humanStatus)
	}

	for _, repo := range seeded.Repos {
		settings, err := service.GetRepositoryPullRequestSettings(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug})
		if err != nil {
			t.Fatalf("get updated pull request settings for %s failed: %v", repo.Slug, err)
		}
		value, ok := settings["requiredAllTasksComplete"].(bool)
		if !ok {
			t.Fatalf("expected requiredAllTasksComplete to be a boolean setting for %s", repo.Slug)
		}
		if value != targetValue {
			t.Fatalf("expected requiredAllTasksComplete=%t for %s, got %t", targetValue, repo.Slug, value)
		}
	}
}

func strconvBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func decodeJSONEnvelopeData(value string, target any) error {
	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
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
