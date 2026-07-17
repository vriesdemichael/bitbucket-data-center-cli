package bulk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
)

type fakeCatalog struct {
	repos map[string][]repository.Repository
}

func (catalog fakeCatalog) ListByProject(_ context.Context, projectKey string, _ repository.ListOptions) ([]repository.Repository, error) {
	return append([]repository.Repository(nil), catalog.repos[projectKey]...), nil
}

type fakeRunner struct {
	results map[string]fakeRunResult
	calls   []string
}

type fakeRunResult struct {
	output any
	err    error
}

func (runner *fakeRunner) Run(_ context.Context, repo RepositoryTarget, operation OperationSpec) (any, error) {
	key := repo.ProjectKey + "/" + repo.Slug + "/" + operation.Type
	runner.calls = append(runner.calls, key)
	result, ok := runner.results[key]
	if !ok {
		return map[string]any{"status": "ok"}, nil
	}
	return result.output, result.err
}

func TestPlannerResolvesSelectorModesDeterministically(t *testing.T) {
	catalog := fakeCatalog{repos: map[string][]repository.Repository{
		"PRJ": {
			{ProjectKey: "PRJ", Slug: "repo-a", Name: "Repo A"},
			{ProjectKey: "PRJ", Slug: "repo-b", Name: "Repo B"},
			{ProjectKey: "PRJ", Slug: "misc", Name: "Misc"},
		},
	}}
	planner := NewPlanner(catalog)

	testCases := []struct {
		name         string
		selector     Selector
		expectTarget []RepositoryTarget
	}{
		{
			name:     "project only",
			selector: Selector{ProjectKey: "PRJ"},
			expectTarget: []RepositoryTarget{
				{ProjectKey: "PRJ", Slug: "misc"},
				{ProjectKey: "PRJ", Slug: "repo-a"},
				{ProjectKey: "PRJ", Slug: "repo-b"},
			},
		},
		{
			name:     "pattern plus explicit list",
			selector: Selector{ProjectKey: "PRJ", RepoPattern: "repo-*", Repositories: []string{"PRJ/misc"}},
			expectTarget: []RepositoryTarget{
				{ProjectKey: "PRJ", Slug: "misc"},
				{ProjectKey: "PRJ", Slug: "repo-a"},
				{ProjectKey: "PRJ", Slug: "repo-b"},
			},
		},
		{
			name:     "explicit repository list with default project",
			selector: Selector{ProjectKey: "PRJ", Repositories: []string{"repo-b", "PRJ/repo-a"}},
			expectTarget: []RepositoryTarget{
				{ProjectKey: "PRJ", Slug: "repo-a"},
				{ProjectKey: "PRJ", Slug: "repo-b"},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			policy := Policy{
				APIVersion: APIVersion,
				Selector:   testCase.selector,
				Operations: []OperationSpec{{Type: OperationRepoPermissionUserGrant, Username: "alice", Permission: "REPO_WRITE"}},
			}

			planOne, err := planner.Plan(context.Background(), policy)
			if err != nil {
				t.Fatalf("plan failed: %v", err)
			}
			planTwo, err := planner.Plan(context.Background(), policy)
			if err != nil {
				t.Fatalf("second plan failed: %v", err)
			}

			targets := make([]RepositoryTarget, 0, len(planOne.Targets))
			for _, target := range planOne.Targets {
				targets = append(targets, target.Repository)
			}

			if !reflect.DeepEqual(targets, testCase.expectTarget) {
				t.Fatalf("unexpected targets: %#v", targets)
			}
			if planOne.PlanHash != planTwo.PlanHash {
				t.Fatalf("expected deterministic plan hash, got %s != %s", planOne.PlanHash, planTwo.PlanHash)
			}
			if planOne.Summary.OperationCount != len(planOne.Targets) {
				t.Fatalf("expected one operation per target, got summary %#v", planOne.Summary)
			}
		})
	}
}

func TestLoadPlanJSONRejectsTamperedPlan(t *testing.T) {
	planner := NewPlanner(fakeCatalog{repos: map[string][]repository.Repository{
		"PRJ": {{ProjectKey: "PRJ", Slug: "repo-a", Name: "Repo A"}},
	}})

	plan, err := planner.Plan(context.Background(), Policy{
		APIVersion: APIVersion,
		Selector:   Selector{ProjectKey: "PRJ", Repositories: []string{"repo-a"}},
		Operations: []OperationSpec{{Type: OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPtr(true)}},
	})
	if err != nil {
		t.Fatalf("plan failed: %v", err)
	}

	encoded, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("marshal plan: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	targets := payload["targets"].([]any)
	firstTarget := targets[0].(map[string]any)
	firstRepo := firstTarget["repository"].(map[string]any)
	firstRepo["slug"] = "repo-b"

	tampered, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal tampered plan: %v", err)
	}

	_, err = LoadPlanJSON(tampered)
	if err == nil || !strings.Contains(err.Error(), "plan hash") {
		t.Fatalf("expected plan hash validation error, got %v", err)
	}
}

func TestExecutorPersistsStatusAndSkipsRemainingOperationsOnTargetFailure(t *testing.T) {
	store := NewStatusStore(filepath.Join(t.TempDir(), "bulk-status"))
	runner := &fakeRunner{results: map[string]fakeRunResult{
		"PRJ/repo-a/" + OperationRepoPermissionUserGrant: {
			err: apperrors.New(apperrors.KindConflict, "grant failed", nil),
		},
		"PRJ/repo-b/" + OperationRepoPermissionUserGrant: {
			output: map[string]any{"status": "ok"},
		},
	}}
	executor := NewExecutor(runner, store)
	plan := Plan{
		APIVersion: APIVersion,
		Kind:       PlanKind,
		Policy: Policy{
			APIVersion: APIVersion,
			Selector:   Selector{ProjectKey: "PRJ"},
			Operations: []OperationSpec{
				{Type: OperationRepoPermissionUserGrant, Username: "alice", Permission: "REPO_WRITE"},
				{Type: OperationRepoWebhookCreate, Name: "ci", URL: "http://example.local/hook", Events: []string{"repo:refs_changed"}, Active: boolPtr(true)},
			},
		},
		Validation: ValidationResult{Valid: true, Errors: []string{}},
		Summary:    PlanSummary{TargetCount: 2, OperationCount: 3},
		Targets: []TargetPlan{
			{
				Repository: RepositoryTarget{ProjectKey: "PRJ", Slug: "repo-a"},
				Validation: ValidationResult{Valid: true, Errors: []string{}},
				Operations: []OperationSpec{
					{Type: OperationRepoPermissionUserGrant, Username: "alice", Permission: "REPO_WRITE"},
					{Type: OperationRepoWebhookCreate, Name: "ci", URL: "http://example.local/hook", Events: []string{"repo:refs_changed"}, Active: boolPtr(true)},
				},
			},
			{
				Repository: RepositoryTarget{ProjectKey: "PRJ", Slug: "repo-b"},
				Validation: ValidationResult{Valid: true, Errors: []string{}},
				Operations: []OperationSpec{{Type: OperationRepoPermissionUserGrant, Username: "alice", Permission: "REPO_WRITE"}},
			},
		},
	}
	planHash, err := computePlanHash(plan)
	if err != nil {
		t.Fatalf("compute plan hash failed: %v", err)
	}
	plan.PlanHash = planHash

	status, err := executor.Apply(context.Background(), plan)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}

	if status.Status != "partial_failure" {
		t.Fatalf("expected partial_failure status, got %s", status.Status)
	}
	if status.Summary.SuccessfulTargets != 1 || status.Summary.FailedTargets != 1 {
		t.Fatalf("unexpected target summary: %#v", status.Summary)
	}
	if status.Summary.SuccessfulOperations != 1 || status.Summary.FailedOperations != 1 || status.Summary.SkippedOperations != 1 {
		t.Fatalf("unexpected operation summary: %#v", status.Summary)
	}
	if len(runner.calls) != 2 {
		t.Fatalf("expected two executed operations, got %#v", runner.calls)
	}

	loaded, err := store.Load(status.OperationID)
	if err != nil {
		t.Fatalf("load status failed: %v", err)
	}
	if !reflect.DeepEqual(status.Summary, loaded.Summary) {
		t.Fatalf("expected stored summary to match apply summary: %#v != %#v", status.Summary, loaded.Summary)
	}
	if loaded.Targets[0].Operations[1].Status != resultStatusSkipped {
		t.Fatalf("expected skipped follow-up operation, got %#v", loaded.Targets[0].Operations)
	}
}

func TestStatusStoreNotFound(t *testing.T) {
	store := NewStatusStore(filepath.Join(t.TempDir(), "bulk-status"))
	_, err := store.Load("missing-operation")
	if err == nil {
		t.Fatal("expected not-found error")
	}
	if apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not-found exit code, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestNormalizePolicyAndOperations(t *testing.T) {
	testCases := []struct {
		name        string
		policy      Policy
		expectError string
	}{
		{
			name: "valid webhook",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type:   OperationRepoWebhookCreate,
					Name:   "hook",
					URL:    "http://hook.local",
					Events: []string{"repo:refs_changed"},
					Active: boolPtr(true),
				}},
			},
		},
		{
			name: "invalid apiVersion",
			policy: Policy{
				APIVersion: "v1",
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationRepoPermissionUserGrant, Username: "u", Permission: "REPO_READ"}},
			},
			expectError: "apiVersion must be",
		},
		{
			name: "missing selector",
			policy: Policy{
				APIVersion: APIVersion,
				Operations: []OperationSpec{{Type: OperationRepoPermissionUserGrant, Username: "u", Permission: "REPO_READ"}},
			},
			expectError: "selector must include",
		},
		{
			name: "repoPattern without projectKey",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{RepoPattern: "foo"},
				Operations: []OperationSpec{{Type: OperationRepoPermissionUserGrant, Username: "u", Permission: "REPO_READ"}},
			},
			expectError: "selector.projectKey is required when selector.repoPattern is set",
		},
		{
			name: "unsupported operation type",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: "invalid"}},
			},
			expectError: "unsupported type \"invalid\"",
		},
		{
			name: "missing username for user grant",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationRepoPermissionUserGrant, Permission: "REPO_READ"}},
			},
			expectError: "username is required",
		},
		{
			name: "invalid permission",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationRepoPermissionUserGrant, Username: "u", Permission: "READ"}},
			},
			expectError: "permission must be one of",
		},
		{
			name: "missing group for group grant",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationRepoPermissionGroupGrant, Permission: "REPO_READ"}},
			},
			expectError: "group is required",
		},
		{
			name: "missing webhook fields",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationRepoWebhookCreate, Name: "h"}},
			},
			expectError: "url is required",
		},
		{
			name: "missing count for approvers count",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationRepoPullRequestRequiredApproversCount}},
			},
			expectError: "count is required",
		},
		{
			name: "negative count",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationRepoPullRequestRequiredApproversCount, Count: intPtr(-1)}},
			},
			expectError: "count must be >= 0",
		},
		{
			name: "missing requiredAllTasksComplete",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationRepoPullRequestRequiredAllTasksComplete}},
			},
			expectError: "requiredAllTasksComplete is required",
		},
		{
			name: "missing payload for build required",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationBuildRequiredCreate}},
			},
			expectError: "payload is required",
		},
		{
			name: "valid auto-merge",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type:    OperationRepoSettingsAutoMerge,
					Enabled: boolPtr(true),
				}},
			},
		},
		{
			name: "valid auto-decline",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type:            OperationRepoSettingsAutoDecline,
					Enabled:         boolPtr(true),
					InactivityWeeks: intPtr(4),
				}},
			},
		},
		{
			name: "valid default-task",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type:        OperationRepoDefaultTaskCreate,
					Description: stringPtr("task description"),
				}},
			},
		},
		{
			name: "missing enabled for auto-merge",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type: OperationRepoSettingsAutoMerge,
				}},
			},
			expectError: "enabled is required",
		},
		{
			name: "missing enabled for auto-decline",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type: OperationRepoSettingsAutoDecline,
				}},
			},
			expectError: "enabled is required",
		},
		{
			name: "missing inactivityWeeks for auto-decline",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type:    OperationRepoSettingsAutoDecline,
					Enabled: boolPtr(true),
				}},
			},
			expectError: "inactivityWeeks is required when enabled is true",
		},
		{
			name: "invalid inactivityWeeks (<= 0) for auto-decline",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type:            OperationRepoSettingsAutoDecline,
					Enabled:         boolPtr(true),
					InactivityWeeks: intPtr(0),
				}},
			},
			expectError: "inactivityWeeks must be > 0",
		},
		{
			name: "invalid inactivityWeeks (negative) for auto-decline",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type:            OperationRepoSettingsAutoDecline,
					Enabled:         boolPtr(true),
					InactivityWeeks: intPtr(-5),
				}},
			},
			expectError: "inactivityWeeks must be > 0",
		},
		{
			name: "missing description for default-task",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type: OperationRepoDefaultTaskCreate,
				}},
			},
			expectError: "description is required",
		},
		{
			name: "valid default-task with refs",
			policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{
					Type:        OperationRepoDefaultTaskCreate,
					Description: stringPtr("task description"),
					SourceRef:   stringPtr("refs/heads/feature"),
					TargetRef:   stringPtr("refs/heads/main"),
				}},
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, errs := normalizePolicy(testCase.policy)
			if testCase.expectError == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
			} else {
				found := false
				for _, err := range errs {
					if strings.Contains(err, testCase.expectError) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("expected error containing %q, got: %v", testCase.expectError, errs)
				}
			}
		})
	}
}

func intPtr(v int) *int {
	return &v
}

func stringPtr(v string) *string {
	return &v
}

func TestResolveTargetsErrors(t *testing.T) {
	catalog := fakeCatalog{repos: map[string][]repository.Repository{
		"PRJ": {{ProjectKey: "PRJ", Slug: "repo-a"}},
	}}
	planner := NewPlanner(catalog)

	t.Run("missing repository", func(t *testing.T) {
		policy := Policy{
			APIVersion: APIVersion,
			Selector:   Selector{ProjectKey: "PRJ", Repositories: []string{"missing"}},
			Operations: []OperationSpec{{Type: OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPtr(true)}},
		}
		_, err := planner.Plan(context.Background(), policy)
		if err == nil || !strings.Contains(err.Error(), "was not found") {
			t.Fatalf("expected not found error, got: %v", err)
		}
	})

	t.Run("invalid pattern", func(t *testing.T) {
		policy := Policy{
			APIVersion: APIVersion,
			Selector:   Selector{ProjectKey: "PRJ", RepoPattern: "["},
			Operations: []OperationSpec{{Type: OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPtr(true)}},
		}
		_, err := planner.Plan(context.Background(), policy)
		if err == nil || !strings.Contains(err.Error(), "repoPattern is invalid") {
			t.Fatalf("expected pattern error, got: %v", err)
		}
	})

	t.Run("empty resolution", func(t *testing.T) {
		policy := Policy{
			APIVersion: APIVersion,
			Selector:   Selector{ProjectKey: "EMPTY"},
			Operations: []OperationSpec{{Type: OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPtr(true)}},
		}
		_, err := planner.Plan(context.Background(), policy)
		if err == nil || !strings.Contains(err.Error(), "resolved no repositories") {
			t.Fatalf("expected empty resolution error, got: %v", err)
		}
	})
}

func TestVerifyPlanValidatesContent(t *testing.T) {
	validPlan := Plan{
		APIVersion: APIVersion,
		Kind:       PlanKind,
		Validation: ValidationResult{Valid: true, Errors: []string{}},
		Targets:    []TargetPlan{{Repository: RepositoryTarget{ProjectKey: "P", Slug: "s"}, Validation: ValidationResult{Valid: true, Errors: []string{}}}},
	}
	hash, _ := computePlanHash(validPlan)
	validPlan.PlanHash = hash

	testCases := []struct {
		name        string
		modify      func(*Plan)
		expectError string
	}{
		{
			name:        "invalid version",
			modify:      func(p *Plan) { p.APIVersion = "v1" },
			expectError: "apiVersion must be",
		},
		{
			name:        "invalid kind",
			modify:      func(p *Plan) { p.Kind = "Plan" },
			expectError: "kind must be",
		},
		{
			name:        "missing hash",
			modify:      func(p *Plan) { p.PlanHash = "" },
			expectError: "missing planHash",
		},
		{
			name:        "validation failed",
			modify:      func(p *Plan) { p.Validation.Valid = false },
			expectError: "contains validation errors",
		},
		{
			name:        "no targets",
			modify:      func(p *Plan) { p.Targets = nil },
			expectError: "contains no targets",
		},
		{
			name:        "hash mismatch",
			modify:      func(p *Plan) { p.PlanHash = "sha256:abc" },
			expectError: "hash does not match",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			p := validPlan
			tc.modify(&p)
			err := VerifyPlan(p)
			if err == nil || !strings.Contains(err.Error(), tc.expectError) {
				t.Fatalf("expected error containing %q, got: %v", tc.expectError, err)
			}
		})
	}
}

func TestDescribeOperation(t *testing.T) {
	testCases := []struct {
		op     OperationSpec
		expect string
	}{
		{
			op:     OperationSpec{Type: OperationRepoPermissionUserGrant, Username: "u", Permission: "ADMIN"},
			expect: "grant user u ADMIN",
		},
		{
			op:     OperationSpec{Type: OperationRepoPermissionGroupGrant, Group: "g", Permission: "READ"},
			expect: "grant group g READ",
		},
		{
			op:     OperationSpec{Type: OperationRepoWebhookCreate, Name: "h"},
			expect: "create webhook h",
		},
		{
			op:     OperationSpec{Type: OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPtr(true)},
			expect: "set requiredAllTasksComplete=true",
		},
		{
			op:     OperationSpec{Type: OperationRepoPullRequestRequiredAllTasksComplete},
			expect: "set requiredAllTasksComplete",
		},
		{
			op:     OperationSpec{Type: OperationRepoPullRequestRequiredApproversCount, Count: intPtr(2)},
			expect: "set requiredApprovers.count=2",
		},
		{
			op:     OperationSpec{Type: OperationRepoPullRequestRequiredApproversCount},
			expect: "set requiredApprovers.count",
		},
		{
			op:     OperationSpec{Type: OperationBuildRequiredCreate},
			expect: "create required build check",
		},
		{
			op:     OperationSpec{Type: OperationRepoSettingsAutoMerge, Enabled: boolPtr(true)},
			expect: "set auto-merge enabled=true",
		},
		{
			op:     OperationSpec{Type: OperationRepoSettingsAutoMerge},
			expect: "set auto-merge settings",
		},
		{
			op:     OperationSpec{Type: OperationRepoSettingsAutoDecline, Enabled: boolPtr(true), InactivityWeeks: intPtr(4)},
			expect: "set auto-decline enabled=true inactivityWeeks=4",
		},
		{
			op:     OperationSpec{Type: OperationRepoSettingsAutoDecline, Enabled: boolPtr(false)},
			expect: "set auto-decline enabled=false",
		},
		{
			op:     OperationSpec{Type: OperationRepoSettingsAutoDecline},
			expect: "set auto-decline settings",
		},
		{
			op:     OperationSpec{Type: OperationRepoDefaultTaskCreate, Description: stringPtr("my task")},
			expect: "create default task: my task",
		},
		{
			op:     OperationSpec{Type: "unknown"},
			expect: "unknown",
		},
	}

	for _, tc := range testCases {
		desc := DescribeOperation(tc.op)
		if tc.op.Type == "unknown" {
			if desc != "unknown" {
				t.Fatalf("expected unknown, got %s", desc)
			}
			continue
		}
		if !strings.Contains(desc, tc.expect) {
			t.Fatalf("expected %q to contain %q", desc, tc.expect)
		}
	}
}

func TestParsePolicyYAMLAndLoadPlanJSON(t *testing.T) {
	t.Run("valid policy", func(t *testing.T) {
		raw := []byte(`
apiVersion: bb.io/v1alpha1
selector:
  projectKey: PRJ
operations:
  - type: repo.pull-request-settings.required-all-tasks-complete
    requiredAllTasksComplete: true
`)
		policy, err := ParsePolicyYAML(raw)
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		if policy.Selector.ProjectKey != "PRJ" {
			t.Fatalf("expected PRJ, got %s", policy.Selector.ProjectKey)
		}
	})

	t.Run("invalid schema policy", func(t *testing.T) {
		raw := []byte(`
apiVersion: bb.io/v1alpha1
selector:
  projectKey: PRJ
operations:
  - type: invalid
`)
		_, err := ParsePolicyYAML(raw)
		if err == nil || !strings.Contains(err.Error(), "does not match schema") {
			t.Fatalf("expected schema error, got: %v", err)
		}
	})

	t.Run("empty policy", func(t *testing.T) {
		_, err := ParsePolicyYAML([]byte(""))
		if err == nil || !strings.Contains(err.Error(), "is empty") {
			t.Fatalf("expected empty error, got: %v", err)
		}
	})

	t.Run("valid plan", func(t *testing.T) {
		plan := Plan{
			APIVersion: APIVersion,
			Kind:       PlanKind,
			Policy: Policy{
				APIVersion: APIVersion,
				Selector:   Selector{ProjectKey: "PRJ"},
				Operations: []OperationSpec{{Type: OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPtr(true)}},
			},
			Validation: ValidationResult{Valid: true, Errors: []string{}},
			Summary:    PlanSummary{TargetCount: 1, OperationCount: 1},
			Targets: []TargetPlan{{
				Repository: RepositoryTarget{ProjectKey: "P", Slug: "s"},
				Validation: ValidationResult{Valid: true, Errors: []string{}},
				Operations: []OperationSpec{{Type: OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPtr(true)}},
			}},
		}
		hash, _ := computePlanHash(plan)
		plan.PlanHash = hash

		encoded, _ := json.Marshal(plan)
		loaded, err := LoadPlanJSON(encoded)
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}
		if loaded.PlanHash != hash {
			t.Fatalf("expected hash %s, got %s", hash, loaded.PlanHash)
		}
	})
}

func TestNormalizePayload(t *testing.T) {
	t.Run("valid payload", func(t *testing.T) {
		op := OperationSpec{
			Type: OperationBuildRequiredCreate,
			Payload: map[string]any{
				"foo": "bar",
				"nested": map[string]any{
					"key": 123,
				},
			},
		}
		_, errs := normalizeOperation(op)
		if len(errs) > 0 {
			t.Fatalf("unexpected errors: %v", errs)
		}
	})

	t.Run("invalid map key", func(t *testing.T) {
		op := OperationSpec{
			Type: OperationBuildRequiredCreate,
			Payload: map[string]any{
				"foo": map[any]any{
					123: "bar",
				},
			},
		}
		_, errs := normalizeOperation(op)
		if len(errs) == 0 {
			t.Fatal("expected error")
		}
	})
}

func TestStatusStoreErrors(t *testing.T) {
	t.Run("load invalid operation id", func(t *testing.T) {
		store := NewStatusStore(t.TempDir())
		_, err := store.Load("../forbidden")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("load corrupt json", func(t *testing.T) {
		dir := t.TempDir()
		store := NewStatusStore(dir)
		_ = os.WriteFile(filepath.Join(dir, "corrupt.json"), []byte("invalid"), 0o600)
		_, err := store.Load("corrupt")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("load mismatched id", func(t *testing.T) {
		dir := t.TempDir()
		store := NewStatusStore(dir)
		status := ApplyStatus{APIVersion: APIVersion, Kind: ApplyStatusKind, OperationID: "other"}
		encoded, _ := json.Marshal(status)
		_ = os.WriteFile(filepath.Join(dir, "match.json"), encoded, 0o600)
		_, err := store.Load("match")
		if err == nil || !strings.Contains(err.Error(), "does not match") {
			t.Fatalf("expected mismatch error, got: %v", err)
		}
	})
}

func TestHasFailures(t *testing.T) {
	if (ApplyStatus{}).HasFailures() {
		t.Fatal("expected false")
	}
	if !(ApplyStatus{Summary: ApplySummary{FailedTargets: 1}}).HasFailures() {
		t.Fatal("expected true")
	}
	if !(ApplyStatus{Summary: ApplySummary{FailedOperations: 1}}).HasFailures() {
		t.Fatal("expected true")
	}
}

func TestCloneHelpers(t *testing.T) {
	m := map[string]any{
		"a": 1,
		"b": []any{"foo", map[string]any{"c": 3}},
		"d": []string{"bar"},
	}
	cloned := cloneMap(m)
	if !reflect.DeepEqual(m, cloned) {
		t.Fatalf("expected %#v, got %#v", m, cloned)
	}
}

func TestNormalizeValueComplex(t *testing.T) {
	input := map[string]any{
		"a": []any{1, "2", map[any]any{"b": 3}},
	}
	normalized, err := normalizeMap(input)
	if err != nil {
		t.Fatalf("normalize failed: %v", err)
	}
	if normalized["a"].([]any)[2].(map[string]any)["b"] != 3 {
		t.Fatalf("unexpected normalized output: %#v", normalized)
	}

	t.Run("invalid nested key", func(t *testing.T) {
		input := map[string]any{
			"a": map[any]any{123: "bar"},
		}
		_, err := normalizeMap(input)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSaveStoreError(t *testing.T) {
	// Use a path that is a file to trigger error in MkdirAll
	dir := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(dir, []byte("iamfile"), 0o600)
	store := NewStatusStore(dir)
	err := store.Save(ApplyStatus{OperationID: "op1"})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadPlanJSONErrors(t *testing.T) {
	t.Run("empty payload", func(t *testing.T) {
		_, err := LoadPlanJSON([]byte(""))
		if err == nil || !strings.Contains(err.Error(), "is empty") {
			t.Fatalf("expected empty error, got: %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		_, err := LoadPlanJSON([]byte("{"))
		if err == nil || !strings.Contains(err.Error(), "failed to parse") {
			t.Fatalf("expected parse error, got: %v", err)
		}
	})
}

func TestStatusStoreSaveValidation(t *testing.T) {
	t.Run("missing base dir", func(t *testing.T) {
		store := NewStatusStore("")
		err := store.Save(ApplyStatus{OperationID: "op-1"})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid operation id", func(t *testing.T) {
		store := NewStatusStore(t.TempDir())
		err := store.Save(ApplyStatus{OperationID: "../bad"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestResolveRepositoryEntry(t *testing.T) {
	t.Run("empty entry", func(t *testing.T) {
		_, err := resolveRepositoryEntry("PRJ", "")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing default project", func(t *testing.T) {
		_, err := resolveRepositoryEntry("", "repo-a")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("invalid project/slug format", func(t *testing.T) {
		_, err := resolveRepositoryEntry("PRJ", "PRJ/")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("uses default project", func(t *testing.T) {
		target, err := resolveRepositoryEntry("PRJ", "repo-a")
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if target.ProjectKey != "PRJ" || target.Slug != "repo-a" {
			t.Fatalf("unexpected target: %#v", target)
		}
	})
}

func TestCopyOperationsDeepClone(t *testing.T) {
	count := 2
	active := true
	required := true
	enabled := true
	inactivityWeeks := 4
	desc := "task description"
	sourceRef := "refs/heads/feature"
	targetRef := "refs/heads/main"

	input := []OperationSpec{ {
		Type:                     OperationRepoWebhookCreate,
		Events:                   []string{"repo:refs_changed"},
		Payload:                  map[string]any{"k": "v"},
		Active:                   &active,
		RequiredAllTasksComplete: &required,
		Count:                    &count,
		Enabled:                  &enabled,
		InactivityWeeks:          &inactivityWeeks,
		Description:              &desc,
		SourceRef:                &sourceRef,
		TargetRef:                &targetRef,
	}}

	cloned := copyOperations(input)
	if len(cloned) != 1 {
		t.Fatalf("expected one op, got %d", len(cloned))
	}
	if &cloned[0] == &input[0] {
		t.Fatal("expected a distinct operation copy")
	}

	input[0].Events[0] = "changed"
	input[0].Payload["k"] = "changed"
	*input[0].Active = false
	*input[0].RequiredAllTasksComplete = false
	*input[0].Count = 99
	*input[0].Enabled = false
	*input[0].InactivityWeeks = 10
	*input[0].Description = "changed"
	*input[0].SourceRef = "changed"
	*input[0].TargetRef = "changed"

	if cloned[0].Events[0] != "repo:refs_changed" {
		t.Fatalf("events not cloned deeply: %#v", cloned[0].Events)
	}
	if cloned[0].Payload["k"] != "v" {
		t.Fatalf("payload not cloned deeply: %#v", cloned[0].Payload)
	}
	if cloned[0].Active == nil || !*cloned[0].Active {
		t.Fatalf("active pointer not cloned correctly: %#v", cloned[0].Active)
	}
	if cloned[0].RequiredAllTasksComplete == nil || !*cloned[0].RequiredAllTasksComplete {
		t.Fatalf("requiredAllTasksComplete pointer not cloned correctly: %#v", cloned[0].RequiredAllTasksComplete)
	}
	if cloned[0].Count == nil || *cloned[0].Count != 2 {
		t.Fatalf("count pointer not cloned correctly: %#v", cloned[0].Count)
	}
	if cloned[0].Enabled == nil || !*cloned[0].Enabled {
		t.Fatalf("enabled pointer not cloned correctly: %#v", cloned[0].Enabled)
	}
	if cloned[0].InactivityWeeks == nil || *cloned[0].InactivityWeeks != 4 {
		t.Fatalf("inactivityWeeks pointer not cloned correctly: %#v", cloned[0].InactivityWeeks)
	}
	if cloned[0].Description == nil || *cloned[0].Description != "task description" {
		t.Fatalf("description pointer not cloned correctly: %#v", cloned[0].Description)
	}
	if cloned[0].SourceRef == nil || *cloned[0].SourceRef != "refs/heads/feature" {
		t.Fatalf("sourceRef pointer not cloned correctly: %#v", cloned[0].SourceRef)
	}
	if cloned[0].TargetRef == nil || *cloned[0].TargetRef != "refs/heads/main" {
		t.Fatalf("targetRef pointer not cloned correctly: %#v", cloned[0].TargetRef)
	}
}

func TestValidateIdentifier(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		if err := validateIdentifier("", "operation id"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("path separators", func(t *testing.T) {
		if err := validateIdentifier("bad/id", "operation id"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("unsupported characters", func(t *testing.T) {
		if err := validateIdentifier("bad id", "operation id"); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("valid", func(t *testing.T) {
		if err := validateIdentifier("op-1.ok", "operation id"); err != nil {
			t.Fatalf("expected valid id, got: %v", err)
		}
	})
}
