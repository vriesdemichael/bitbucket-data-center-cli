//go:build live

package live_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLiveGovernanceCLI(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// --- Issue 31: Group Permissions ---
	// Test listing groups (even if empty)
	output, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key)
	if err != nil {
		t.Fatalf("project group permissions list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"groups"`) {
		t.Fatalf("expected groups in output: %s", output)
	}

	output, err = executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo group permissions list failed: %v\noutput: %s", err, output)
	}

	// Try to grant to stash-users if it exists (usually does in local stack)
	_, _ = executeLiveCLI(t, "project", "permissions", "groups", "grant", seeded.Key, "stash-users", "PROJECT_READ")
	_, _ = executeLiveCLI(t, "repo", "settings", "security", "permissions", "groups", "grant", "stash-users", "REPO_READ", "--repo", seeded.Key+"/"+repo.Slug)

	// --- Issue 32: Reviewers & Hooks ---
	// Test listing hooks
	output, err = executeLiveCLI(t, "--json", "hook", "list", "--project", seeded.Key)
	if err != nil {
		t.Fatalf("project hook list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"hooks"`) {
		t.Fatalf("expected hooks in output: %s", output)
	}

	// Hook lifecycle on a built-in hook (com.atlassian.bitbucket.server.bitbucket-bundled-hooks:verify-committer-hook)
	hookKey := "com.atlassian.bitbucket.server.bitbucket-bundled-hooks:verify-committer-hook"
	_, _ = executeLiveCLI(t, "--json", "hook", "enable", hookKey, "--repo", seeded.Key+"/"+repo.Slug)

	// Test hook configuration (get)
	output, err = executeLiveCLI(t, "--json", "hook", "configure", hookKey, "--repo", seeded.Key+"/"+repo.Slug)
	if err == nil {
		// Try to update hook configuration (even with empty settings)
		_, _ = executeLiveCLI(t, "--json", "hook", "configure", hookKey, "{}", "--repo", seeded.Key+"/"+repo.Slug)
	}

	_, _ = executeLiveCLI(t, "--json", "hook", "disable", hookKey, "--repo", seeded.Key+"/"+repo.Slug)

	// Test listing reviewer conditions
	output, err = executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--project", seeded.Key)
	if err != nil {
		t.Fatalf("project reviewer list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"conditions"`) {
		t.Fatalf("expected conditions in output: %s", output)
	}

	// Reviewer condition lifecycle
	output, err = executeLiveCLI(t, "--json", "reviewer", "condition", "create", `{"requiredApprovals": 1}`, "--repo", seeded.Key+"/"+repo.Slug)
	if err == nil {
		// If successfully created (depends on default-reviewers plugin), we'll try to extract the ID and update/delete it
		var id string
		if strings.Contains(output, `"id":`) {
			// Basic extraction for JSON output
			parts := strings.Split(output, `"id":`)
			if len(parts) > 1 {
				idStr := strings.TrimSpace(strings.Split(parts[1], ",")[0])
				idStr = strings.TrimSpace(strings.Split(idStr, "}")[0])
				id = idStr
			}
		}

		if id != "" {
			_, _ = executeLiveCLI(t, "--json", "reviewer", "condition", "update", id, `{"requiredApprovals": 2}`, "--repo", seeded.Key+"/"+repo.Slug)
			_, _ = executeLiveCLI(t, "--json", "reviewer", "condition", "delete", id, "--repo", seeded.Key+"/"+repo.Slug)
		}
	}

	// --- Issue 33: PR Governance ---
	// Test getting PR settings
	output, err = executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo PR settings get failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"pull_request_settings"`) {
		t.Fatalf("expected pull_request_settings in output: %s", output)
	}

	// Test updating PR strategy (safe update)
	output, err = executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "set-strategy", "merge-base", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		// Some strategies might not be available depending on plugin config, but we try anyway
		t.Logf("repo set-strategy attempt output: %s", output)
	}

	// Test listing merge checks
	output, err = executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "merge-checks", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("repo merge-checks list failed: %v\noutput: %s", err, output)
	}
	if !strings.Contains(output, `"merge_checks"`) {
		t.Fatalf("expected merge_checks in output: %s", output)
	}
}

func TestLiveCLIProjectPermissionsUserGrantDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	username := strings.TrimSpace(harness.config.BitbucketUsername)
	if username == "" {
		t.Skip("no username configured for project permission dry-run live test")
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions users list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "grant", seeded.Key, username, "PROJECT_WRITE")
	if err != nil {
		t.Fatalf("project permissions users grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.permission.user.grant"`) {
		t.Fatalf("expected intent in dry-run output, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions users list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project permission side-effect from dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIProjectPermissionsGroupGrantDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	group := "stash-users"
	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions groups list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "groups", "grant", seeded.Key, group, "PROJECT_READ")
	if err != nil {
		t.Fatalf("project permissions groups grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.permission.group.grant"`) {
		t.Fatalf("expected project.permission.group.grant intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions groups list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project group permission side-effect from dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIProjectPermissionsUserRevokeDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions users list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "users", "revoke", seeded.Key, "dryrun-missing-user")
	if err != nil {
		t.Fatalf("project permissions users revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.permission.user.revoke"`) {
		t.Fatalf("expected project.permission.user.revoke intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "users", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions users list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project user permission side-effect from dry-run revoke\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIProjectPermissionsGroupRevokeDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions groups list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "permissions", "groups", "revoke", seeded.Key, "dryrun-missing-group")
	if err != nil {
		t.Fatalf("project permissions groups revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.permission.group.revoke"`) {
		t.Fatalf("expected project.permission.group.revoke intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "permissions", "groups", "list", seeded.Key, "--limit", "200")
	if err != nil {
		t.Fatalf("project permissions groups list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project group permission side-effect from dry-run revoke\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIHookEnableDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	hookKey := "com.atlassian.bitbucket.server.bitbucket-bundled-hooks:verify-committer-hook"

	listBeforeOutput, err := executeLiveCLI(t, "--json", "hook", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("hook list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "hook", "enable", hookKey, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("hook enable dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "hook.enable"`) {
		t.Fatalf("expected hook.enable intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "hook", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("hook list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no hook side-effect from dry-run enable\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIHookDisableDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	hookKey := "com.atlassian.bitbucket.server.bitbucket-bundled-hooks:verify-committer-hook"

	listBeforeOutput, err := executeLiveCLI(t, "--json", "hook", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("hook list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "hook", "disable", hookKey, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("hook disable dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "hook.disable"`) {
		t.Fatalf("expected hook.disable intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "hook", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("hook list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no hook side-effect from dry-run disable\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIHookConfigureDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	hookKey := "com.atlassian.bitbucket.server.bitbucket-bundled-hooks:verify-committer-hook"

	settingsBeforeOutput, err := executeLiveCLI(t, "--json", "hook", "configure", hookKey, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("hook configure get before failed: %v\noutput: %s", err, settingsBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "hook", "configure", hookKey, `{"required":true}`, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("hook configure dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "hook.configure"`) {
		t.Fatalf("expected hook.configure intent, got: %s", dryRunOutput)
	}

	settingsAfterOutput, err := executeLiveCLI(t, "--json", "hook", "configure", hookKey, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("hook configure get after failed: %v\noutput: %s", err, settingsAfterOutput)
	}

	if settingsBeforeOutput != settingsAfterOutput {
		t.Fatalf("expected no hook settings side-effect from dry-run configure\nbefore: %s\nafter: %s", settingsBeforeOutput, settingsAfterOutput)
	}

}

func TestLiveCLIReviewerConditionCreateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer", "condition", "create", `{"requiredApprovals":1}`, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "reviewer.condition.create"`) {
		t.Fatalf("expected reviewer.condition.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no reviewer condition side-effect from dry-run create\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIReviewerConditionUpdateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer", "condition", "update", "999999", `{"requiredApprovals":2}`, "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "reviewer.condition.update"`) {
		t.Fatalf("expected reviewer.condition.update intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no reviewer condition side-effect from dry-run update\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIReviewerConditionDeleteDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer", "condition", "delete", "999999", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "reviewer.condition.delete"`) {
		t.Fatalf("expected reviewer.condition.delete intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+seeded.Repos[0].Slug)
	if err != nil {
		t.Fatalf("reviewer condition list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no reviewer condition side-effect from dry-run delete\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIProjectCreateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "project", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("project list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	newKey := fmt.Sprintf("DRY%03d", time.Now().UnixNano()%1000)
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "project", "create", newKey, "--name", "Dry Run Project")
	if err != nil {
		t.Fatalf("project create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "project.create"`) {
		t.Fatalf("expected project.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "project", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("project list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no project side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveReviewerGroupsAndDefaultReviewersCLI(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// 1. List project-scoped reviewer groups (even if empty)
	output, err := executeLiveCLI(t, "--json", "reviewer-group", "list", "--project", seeded.Key)
	if err != nil {
		t.Logf("project reviewer-group list skipped/failed: %v", err)
	} else if !strings.Contains(output, `"reviewer_groups"`) {
		t.Fatalf("expected reviewer_groups in output: %s", output)
	}

	// 2. List repository-scoped reviewer groups
	output, err = executeLiveCLI(t, "--json", "reviewer-group", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Logf("repo reviewer-group list skipped/failed: %v", err)
	} else if !strings.Contains(output, `"reviewer_groups"`) {
		t.Fatalf("expected reviewer_groups in output: %s", output)
	}

	// 3. Dry-run create repository reviewer group
	dryRunOut, err := executeLiveCLI(t, "--json", "--dry-run", "reviewer-group", "create", "test-live-group", "--repo", seeded.Key+"/"+repo.Slug)
	if err == nil {
		if !strings.Contains(dryRunOut, `"intent": "reviewer-group.create"`) {
			t.Fatalf("expected intent in dry-run create, got: %s", dryRunOut)
		}
	}

	// 4. Create repository-scoped reviewer group
	createOut, err := executeLiveCLI(t, "--json", "reviewer-group", "create", "test-live-group", "--repo", seeded.Key+"/"+repo.Slug, "--description", "live desc")
	if err == nil {
		var id string
		if strings.Contains(createOut, `"id":`) {
			parts := strings.Split(createOut, `"id":`)
			if len(parts) > 1 {
				idStr := strings.TrimSpace(strings.Split(parts[1], ",")[0])
				idStr = strings.TrimSpace(strings.Split(idStr, "}")[0])
				id = idStr
			}
		}

		if id != "" {
			// Dry-run update
			_, _ = executeLiveCLI(t, "--json", "--dry-run", "reviewer-group", "update", id, "--repo", seeded.Key+"/"+repo.Slug, "--description", "new live desc")

			// Update
			_, _ = executeLiveCLI(t, "--json", "reviewer-group", "update", id, "--repo", seeded.Key+"/"+repo.Slug, "--description", "new live desc")

			// List users
			usersOut, err := executeLiveCLI(t, "--json", "reviewer-group", "users", id, "--repo", seeded.Key+"/"+repo.Slug)
			if err == nil && !strings.Contains(usersOut, `"users"`) {
				t.Fatalf("expected users in output: %s", usersOut)
			}

			// Dry-run delete
			_, _ = executeLiveCLI(t, "--json", "--dry-run", "reviewer-group", "delete", id, "--repo", seeded.Key+"/"+repo.Slug)

			// Delete
			_, _ = executeLiveCLI(t, "--json", "reviewer-group", "delete", id, "--repo", seeded.Key+"/"+repo.Slug)
		}
	}

	// 5. Default Reviewers
	repoResp, err := harness.client.GetRepositoryWithResponse(ctx, seeded.Key, repo.Slug)
	if err != nil {
		t.Fatalf("failed to get repository details: %v", err)
	}
	repoID := fmt.Sprintf("%d", *repoResp.ApplicationjsonCharsetUTF8200.Id)

	defOut, err := executeLiveCLI(t, "--json", "pr", "default-reviewers", "--repo", seeded.Key+"/"+repo.Slug, "--source-ref", "refs/heads/master", "--target-ref", "refs/heads/master", "--source-repo-id", repoID, "--target-repo-id", repoID)
	if err != nil {
		t.Logf("pr default-reviewers failed: %v", err)
	} else if !strings.Contains(defOut, `"default_reviewers"`) {
		t.Fatalf("expected default_reviewers in output: %s", defOut)
	}
}
