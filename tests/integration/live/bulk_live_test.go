//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
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

// TestLiveBulkEveryOperationType is the runner's dispatch table, asked of a
// real Bitbucket.
//
// A unit test ran all nine operation types past a handler that answered
// `{"status":"ok"}` to every request and checked only that Run returned no
// error. That says an operation was dispatched somewhere; it cannot say the
// request it built was one Bitbucket accepts, and it passes just as well for
// an operation that sends nonsense to the wrong route.
//
// Here the apply status is Bitbucket's verdict on each of the nine, one
// operation result at a time, and the settings are read back afterwards.
func TestLiveBulkEveryOperationType(t *testing.T) {
	harness := newLiveHarness(t)
	service := reposettings.NewService(harness.client)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A real user to grant to: a username Bitbucket does not know is refused,
	// which would make the grant fail for a reason that is not the runner's.
	grantee, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create user failed: %v", err)
	}

	hookName := fmt.Sprintf("bulk-hook-%d", time.Now().UnixNano()%100000)
	policy := strings.Join([]string{
		"apiVersion: bb.io/v1alpha1",
		"selector:",
		"  projectKey: " + seeded.Key,
		"  repoPattern: " + repo.Slug,
		"operations:",
		"  - type: repo.permission.user.grant",
		"    username: " + grantee.Username,
		"    permission: REPO_READ",
		"  - type: repo.permission.group.grant",
		"    group: stash-users",
		"    permission: REPO_READ",
		"  - type: repo.webhook.create",
		"    name: " + hookName,
		"    url: https://example.invalid/bulk-hook",
		"    events:",
		"      - repo:refs_changed",
		"  - type: repo.pull-request-settings.required-all-tasks-complete",
		"    requiredAllTasksComplete: true",
		"  - type: repo.pull-request-settings.required-approvers-count",
		"    count: 1",
		"  - type: build.required.create",
		"    payload:",
		"      buildParentKeys:",
		"        - ci",
		"      refMatcher:",
		"        id: refs/heads/master",
		"        type:",
		"          id: BRANCH",
		"  - type: repo.settings.auto-merge",
		"    enabled: true",
		"  - type: repo.settings.auto-decline",
		"    enabled: true",
		"    inactivityWeeks: 4",
		"  - type: repo.default-task.create",
		"    description: bulk default task",
	}, "\n")

	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "every-operation.yaml")
	planPath := filepath.Join(tempDir, "every-operation-plan.json")
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
	if plan.Summary.OperationCount != 9 {
		t.Fatalf("expected all nine operation types in the plan, got %d:\n%s", plan.Summary.OperationCount, planOutput)
	}

	applyOutput, err := executeLiveCLI(t, "--json", "bulk", "apply", "--from-plan", planPath)
	if err != nil {
		t.Fatalf("bulk apply failed: %v\noutput: %s", err, applyOutput)
	}

	var status bulkworkflow.ApplyStatus
	if err := decodeJSONEnvelopeData(applyOutput, &status); err != nil {
		t.Fatalf("decode bulk apply output: %v\noutput: %s", err, applyOutput)
	}

	// Per operation rather than per target: a target counted successful hides
	// which of the nine the server actually took.
	applied := map[string]string{}
	for _, target := range status.Targets {
		for _, operation := range target.Operations {
			applied[operation.Type] = operation.Status
			if operation.Error != "" {
				t.Errorf("%s failed: %s", operation.Type, operation.Error)
			}
		}
	}
	for _, operationType := range []string{
		"repo.permission.user.grant",
		"repo.permission.group.grant",
		"repo.webhook.create",
		"repo.pull-request-settings.required-all-tasks-complete",
		"repo.pull-request-settings.required-approvers-count",
		"build.required.create",
		"repo.settings.auto-merge",
		"repo.settings.auto-decline",
		"repo.default-task.create",
	} {
		if applied[operationType] != "success" {
			t.Errorf("%s reported %q, want success", operationType, applied[operationType])
		}
	}

	// And the state Bitbucket now holds, for the operations whose effect is
	// readable: a request the server accepted and then ignored would pass
	// everything above.
	settings, err := service.GetRepositoryPullRequestSettings(ctx, reposettings.RepositoryRef{ProjectKey: seeded.Key, Slug: repo.Slug})
	if err != nil {
		t.Fatalf("read pull request settings failed: %v", err)
	}
	if allTasks, _ := settings["requiredAllTasksComplete"].(bool); !allTasks {
		t.Errorf("requiredAllTasksComplete was reported applied and is not set: %#v", settings["requiredAllTasksComplete"])
	}
	if approvers, _ := settings["requiredApprovers"].(float64); int(approvers) != 1 {
		t.Errorf("requiredApprovers = %#v, want 1", settings["requiredApprovers"])
	}

	hooks := mustLiveCLI(t, "--json", "webhook", "list", "--limit", "50")
	if !strings.Contains(hooks, hookName) {
		t.Errorf("the webhook the policy created is not in the repository's hooks:\n%s", hooks)
	}
}
