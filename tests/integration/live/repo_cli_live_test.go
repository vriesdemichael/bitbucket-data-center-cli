//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLiveCLIRepoListAndComments(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	repoListOutput, err := executeLiveCLI(t, "--json", "repo", "list", "--limit", "50")
	if err != nil {
		t.Fatalf("repo list failed: %v\noutput: %s", err, repoListOutput)
	}
	if !jsonArrayContainsSlug(t, repoListOutput, repo.Slug) {
		t.Fatalf("expected repo slug %s in repo list output: %s", repo.Slug, repoListOutput)
	}

	projectRepoListOutput, err := executeLiveCLI(t, "--json", "repo", "list", "--project", seeded.Key, "--limit", "50")
	if err != nil {
		t.Fatalf("repo list with project filter failed: %v\noutput: %s", err, projectRepoListOutput)
	}
	if !jsonArrayContainsSlug(t, projectRepoListOutput, repo.Slug) {
		t.Fatalf("expected repo slug %s in project-filtered repo list output: %s", repo.Slug, projectRepoListOutput)
	}

	commitID := repo.CommitIDs[0]
	createCommitOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create", "--commit", commitID, "--text", "live cli commit comment")
	if err != nil {
		t.Fatalf("repo comment create (commit) failed: %v\noutput: %s", err, createCommitOutput)
	}
	commitCommentID, ok := commentIDFromCreateOutput(createCommitOutput)
	if !ok {
		t.Fatalf("expected comment id in commit create output: %s", createCommitOutput)
	}
	commitCommentVersion, _ := commentVersionFromCreateOutput(createCommitOutput)

	listCommitOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--commit", commitID, "--path", "seed.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("repo comment list (commit) failed: %v\noutput: %s", err, listCommitOutput)
	}
	if !jsonObjectHasCommentsArray(t, listCommitOutput) {
		t.Fatalf("expected comments array in commit list output: %s", listCommitOutput)
	}

	humanListCommitOutput, err := executeLiveCLI(t, "repo", "comment", "list", "--commit", commitID, "--path", "seed.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("repo comment list (commit human) failed: %v\noutput: %s", err, humanListCommitOutput)
	}
	if !strings.Contains(humanListCommitOutput, "No comments found") && !strings.Contains(humanListCommitOutput, "[") {
		t.Fatalf("expected human comment list output, got: %s", humanListCommitOutput)
	}

	updateCommitArgs := []string{"--json", "repo", "comment", "update", "--commit", commitID, "--id", commitCommentID, "--text", "live cli commit comment updated"}
	if commitCommentVersion != "" {
		updateCommitArgs = append(updateCommitArgs, "--version", commitCommentVersion)
	}
	updateCommitOutput, err := executeLiveCLI(t, updateCommitArgs...)
	if err != nil {
		t.Fatalf("repo comment update (commit) failed: %v\noutput: %s", err, updateCommitOutput)
	}
	updatedCommitVersion, ok := commentVersionFromCreateOutput(updateCommitOutput)
	if !ok {
		t.Fatalf("expected version in commit update output: %s", updateCommitOutput)
	}

	deleteCommitArgs := []string{"repo", "comment", "delete", "--commit", commitID, "--id", commitCommentID}
	if updatedCommitVersion != "" {
		deleteCommitArgs = append(deleteCommitArgs, "--version", updatedCommitVersion)
	}
	deleteCommitOutput, err := executeLiveCLI(t, deleteCommitArgs...)
	if err != nil {
		t.Fatalf("repo comment delete (commit) failed: %v\noutput: %s", err, deleteCommitOutput)
	}
	if !strings.Contains(deleteCommitOutput, "Deleted comment") {
		t.Fatalf("expected human delete output, got: %s", deleteCommitOutput)
	}

	branch := fmt.Sprintf("lt-repo-cli-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "repo-cli-feature.txt"); err != nil {
		t.Fatalf("push commit on branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request failed: %v", err)
	}

	createPROutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create", "--pr", pullRequestID, "--text", "live cli pr comment")
	if err != nil {
		t.Fatalf("repo comment create (pr) failed: %v\noutput: %s", err, createPROutput)
	}
	prCommentID, ok := commentIDFromCreateOutput(createPROutput)
	if !ok {
		t.Fatalf("expected comment id in pr create output: %s", createPROutput)
	}
	prCommentVersion, _ := commentVersionFromCreateOutput(createPROutput)

	listPROutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "repo-cli-feature.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("repo comment list (pr) failed: %v\noutput: %s", err, listPROutput)
	}
	if !jsonObjectHasCommentsArray(t, listPROutput) {
		t.Fatalf("expected comments array in pr list output: %s", listPROutput)
	}

	prCommentListOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--path", "repo-cli-feature.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, prCommentListOutput)
	}
	if !jsonObjectHasCommentsArray(t, prCommentListOutput) {
		t.Fatalf("expected comments array in pr comment list output: %s", prCommentListOutput)
	}

	aggregatePRCommentListOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "list", pullRequestID, "--limit", "25")
	if err != nil {
		t.Fatalf("aggregate pr comment list failed: %v\noutput: %s", err, aggregatePRCommentListOutput)
	}
	if !jsonObjectHasCommentsArray(t, aggregatePRCommentListOutput) {
		t.Fatalf("expected comments array in aggregate pr comment list output: %s", aggregatePRCommentListOutput)
	}
	if !strings.Contains(aggregatePRCommentListOutput, `"source": "activities"`) {
		t.Fatalf("expected activities source in aggregate pr comment list output: %s", aggregatePRCommentListOutput)
	}

	prCommentGetOutput, err := executeLiveCLI(t, "--json", "pr", "comment", "get", pullRequestID, prCommentID)
	if err != nil {
		t.Fatalf("pr comment get failed: %v\noutput: %s", err, prCommentGetOutput)
	}
	if !strings.Contains(prCommentGetOutput, `"comment"`) {
		t.Fatalf("expected comment payload in pr comment get output: %s", prCommentGetOutput)
	}

	prActivityListOutput, err := executeLiveCLI(t, "--json", "pr", "activity", "list", pullRequestID, "--limit", "25")
	if err != nil {
		t.Fatalf("pr activity list failed: %v\noutput: %s", err, prActivityListOutput)
	}
	if !strings.Contains(prActivityListOutput, `"activities"`) {
		t.Fatalf("expected activities payload in pr activity list output: %s", prActivityListOutput)
	}

	humanPRCommentListOutput, err := executeLiveCLI(t, "pr", "comment", "list", pullRequestID, "--path", "repo-cli-feature.txt", "--limit", "25")
	if err != nil {
		t.Fatalf("pr comment list human failed: %v\noutput: %s", err, humanPRCommentListOutput)
	}
	if !strings.Contains(humanPRCommentListOutput, "[") && !strings.Contains(humanPRCommentListOutput, "No comments found") {
		t.Fatalf("expected human pr comment list output, got: %s", humanPRCommentListOutput)
	}

	updatePRArgs := []string{"--json", "repo", "comment", "update", "--pr", pullRequestID, "--id", prCommentID, "--text", "live cli pr comment updated"}
	if prCommentVersion != "" {
		updatePRArgs = append(updatePRArgs, "--version", prCommentVersion)
	}
	updatePROutput, err := executeLiveCLI(t, updatePRArgs...)
	if err != nil {
		t.Fatalf("repo comment update (pr) failed: %v\noutput: %s", err, updatePROutput)
	}
	updatedPRVersion, ok := commentVersionFromCreateOutput(updatePROutput)
	if !ok {
		t.Fatalf("expected version in pr update output: %s", updatePROutput)
	}

	deletePRArgs := []string{"repo", "comment", "delete", "--pr", pullRequestID, "--id", prCommentID}
	if updatedPRVersion != "" {
		deletePRArgs = append(deletePRArgs, "--version", updatedPRVersion)
	}
	deletePROutput, err := executeLiveCLI(t, deletePRArgs...)
	if err != nil {
		t.Fatalf("repo comment delete (pr) failed: %v\noutput: %s", err, deletePROutput)
	}
	if !strings.Contains(deletePROutput, "Deleted comment") {
		t.Fatalf("expected human delete output, got: %s", deletePROutput)
	}
}

func TestLiveCLIRepoSettingsSurface(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	permissionListOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "100")
	if err != nil {
		t.Fatalf("repo settings permissions users list failed: %v\noutput: %s", err, permissionListOutput)
	}
	permissionListPayload := decodeJSONMap(t, permissionListOutput)
	if _, ok := permissionListPayload["users"]; !ok {
		t.Fatalf("expected users field in permissions list output: %s", permissionListOutput)
	}

	username := harness.config.BitbucketUsername
	if strings.TrimSpace(username) != "" {
		grantOutput, grantErr := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "grant", username, "repo_write")
		if grantErr != nil {
			t.Fatalf("repo settings permissions users grant failed: %v\noutput: %s", grantErr, grantOutput)
		}
		if asString(decodeJSONMap(t, grantOutput)["status"]) != "ok" {
			t.Fatalf("expected grant status ok, got: %s", grantOutput)
		}
	}

	webhooksListOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("repo settings workflow webhooks list failed: %v\noutput: %s", err, webhooksListOutput)
	}
	webhooksListPayload := decodeJSONMap(t, webhooksListOutput)
	if _, ok := webhooksListPayload["webhooks"]; !ok {
		t.Fatalf("expected webhooks field in webhooks list output: %s", webhooksListOutput)
	}

	webhookName := fmt.Sprintf("lt-cli-webhook-%d", time.Now().UnixNano()%100000)
	createWebhookOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "create", webhookName, "http://localhost:65535/hook", "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("repo settings workflow webhooks create failed: %v\noutput: %s", err, createWebhookOutput)
	}
	webhookID, ok := webhookIDFromCreateOutput(createWebhookOutput)
	if ok {
		deleteWebhookOutput, deleteErr := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "delete", webhookID)
		if deleteErr != nil {
			t.Fatalf("repo settings workflow webhooks delete failed: %v\noutput: %s", deleteErr, deleteWebhookOutput)
		}
		if asString(decodeJSONMap(t, deleteWebhookOutput)["status"]) != "ok" {
			t.Fatalf("expected webhook delete status ok, got: %s", deleteWebhookOutput)
		}
	}

	pullRequestsGetOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("repo settings pull-requests get failed: %v\noutput: %s", err, pullRequestsGetOutput)
	}
	getPayload := decodeJSONMap(t, pullRequestsGetOutput)
	if _, ok := getPayload["pull_request_settings"]; !ok {
		t.Fatalf("expected pull_request_settings field in get output: %s", pullRequestsGetOutput)
	}

	pullRequestsUpdateOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true")
	if err != nil {
		t.Fatalf("repo settings pull-requests update failed: %v\noutput: %s", err, pullRequestsUpdateOutput)
	}
	if asString(decodeJSONMap(t, pullRequestsUpdateOutput)["status"]) != "ok" {
		t.Fatalf("expected pull-requests update status ok, got: %s", pullRequestsUpdateOutput)
	}

	pullRequestsApproversOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "update-approvers", "--count", "2")
	if err != nil {
		t.Fatalf("repo settings pull-requests update-approvers failed: %v\noutput: %s", err, pullRequestsApproversOutput)
	}
	if asString(decodeJSONMap(t, pullRequestsApproversOutput)["status"]) != "ok" {
		t.Fatalf("expected pull-requests update-approvers status ok, got: %s", pullRequestsApproversOutput)
	}

	humanPermissionListOutput, err := executeLiveCLI(t, "repo", "settings", "security", "permissions", "users", "list", "--limit", "10")
	if err != nil {
		t.Fatalf("repo settings permissions users list (human) failed: %v\noutput: %s", err, humanPermissionListOutput)
	}
	if strings.TrimSpace(humanPermissionListOutput) == "" {
		t.Fatalf("expected non-empty human permissions output")
	}

	humanWebhooksListOutput, err := executeLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("repo settings webhooks list (human) failed: %v\noutput: %s", err, humanWebhooksListOutput)
	}
	if !strings.Contains(humanWebhooksListOutput, "Webhooks configured:") {
		t.Fatalf("expected human webhooks output, got: %s", humanWebhooksListOutput)
	}

	humanPullRequestsGetOutput, err := executeLiveCLI(t, "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("repo settings pull-requests get (human) failed: %v\noutput: %s", err, humanPullRequestsGetOutput)
	}
	if !strings.Contains(humanPullRequestsGetOutput, "Required tasks complete:") {
		t.Fatalf("expected human pull-request settings output, got: %s", humanPullRequestsGetOutput)
	}

	humanPullRequestsUpdateOutput, err := executeLiveCLI(t, "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=false")
	if err != nil {
		t.Fatalf("repo settings pull-requests update (human) failed: %v\noutput: %s", err, humanPullRequestsUpdateOutput)
	}
	if !strings.Contains(humanPullRequestsUpdateOutput, "Updated pull-request settings") {
		t.Fatalf("expected human pull-requests update output, got: %s", humanPullRequestsUpdateOutput)
	}

	humanPullRequestsApproversOutput, err := executeLiveCLI(t, "repo", "settings", "pull-requests", "update-approvers", "--count", "1")
	if err != nil {
		t.Fatalf("repo settings pull-requests update-approvers (human) failed: %v\noutput: %s", err, humanPullRequestsApproversOutput)
	}
	if !strings.Contains(humanPullRequestsApproversOutput, "Updated pull-request settings") {
		t.Fatalf("expected human pull-requests update-approvers output, got: %s", humanPullRequestsApproversOutput)
	}
}

func TestLiveCLIRepoPermissionsUserGrantDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	username := strings.TrimSpace(harness.config.BitbucketUsername)
	if username == "" {
		t.Skip("no username configured for permission dry-run live test")
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions users list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "grant", username, "REPO_WRITE")
	if err != nil {
		t.Fatalf("permissions users grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.permission.user.grant"`) {
		t.Fatalf("expected intent in dry-run output, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions users list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no permission side-effect from dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIRepoPermissionsGroupGrantDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	group := "stash-users"
	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions groups list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "groups", "grant", group, "REPO_READ")
	if err != nil {
		t.Fatalf("permissions groups grant dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.permission.group.grant"`) {
		t.Fatalf("expected repo.permission.group.grant intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions groups list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no group permission side-effect from dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIRepoPermissionsUserRevokeDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions users list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "revoke", "dryrun-missing-user")
	if err != nil {
		t.Fatalf("permissions users revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.permission.user.revoke"`) {
		t.Fatalf("expected repo.permission.user.revoke intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions users list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no user permission side-effect from revoke dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIRepoPermissionsGroupRevokeDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions groups list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "groups", "revoke", "dryrun-missing-group")
	if err != nil {
		t.Fatalf("permissions groups revoke dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.permission.group.revoke"`) {
		t.Fatalf("expected repo.permission.group.revoke intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "security", "permissions", "groups", "list", "--limit", "200")
	if err != nil {
		t.Fatalf("permissions groups list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no group permission side-effect from revoke dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIRepoWebhookCreateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("webhooks list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	name := fmt.Sprintf("lt-dryrun-webhook-%d", time.Now().UnixNano()%100000)
	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "workflow", "webhooks", "create", name, "http://localhost:65535/hook", "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("webhook create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.webhook.create"`) {
		t.Fatalf("expected repo.webhook.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("webhooks list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no webhook side-effect from dry-run create\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIRepoPullRequestSettingsUpdateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	settingsBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get before failed: %v\noutput: %s", err, settingsBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "pull-requests", "update", "--required-all-tasks-complete=true")
	if err != nil {
		t.Fatalf("pull-request settings update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.pull-request-settings.update"`) {
		t.Fatalf("expected repo.pull-request-settings.update intent, got: %s", dryRunOutput)
	}

	settingsAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get after failed: %v\noutput: %s", err, settingsAfterOutput)
	}

	if settingsBeforeOutput != settingsAfterOutput {
		t.Fatalf("expected no pull-request settings side-effect from dry-run update\nbefore: %s\nafter: %s", settingsBeforeOutput, settingsAfterOutput)
	}
}

func TestLiveCLIRepoPullRequestSettingsUpdateApproversDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	settingsBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get before failed: %v\noutput: %s", err, settingsBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "pull-requests", "update-approvers", "--count", "2")
	if err != nil {
		t.Fatalf("pull-request settings update-approvers dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.pull-request-settings.update-approvers"`) {
		t.Fatalf("expected update-approvers intent, got: %s", dryRunOutput)
	}

	settingsAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get after failed: %v\noutput: %s", err, settingsAfterOutput)
	}

	if settingsBeforeOutput != settingsAfterOutput {
		t.Fatalf("expected no pull-request settings side-effect from update-approvers dry-run\nbefore: %s\nafter: %s", settingsBeforeOutput, settingsAfterOutput)
	}
}

func TestLiveCLIRepoPullRequestSettingsSetStrategyDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	settingsBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get before failed: %v\noutput: %s", err, settingsBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "pull-requests", "set-strategy", "merge-base")
	if err != nil {
		t.Fatalf("pull-request settings set-strategy dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.pull-request-settings.set-strategy"`) {
		t.Fatalf("expected set-strategy intent, got: %s", dryRunOutput)
	}

	settingsAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "pull-requests", "get")
	if err != nil {
		t.Fatalf("pull-request settings get after failed: %v\noutput: %s", err, settingsAfterOutput)
	}

	if settingsBeforeOutput != settingsAfterOutput {
		t.Fatalf("expected no pull-request settings side-effect from set-strategy dry-run\nbefore: %s\nafter: %s", settingsBeforeOutput, settingsAfterOutput)
	}
}

func TestLiveCLIRepoWebhookDeleteDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	createName := fmt.Sprintf("lt-dryrun-webhook-del-%d", time.Now().UnixNano()%100000)
	createOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "create", createName, "http://localhost:65535/hook", "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("webhook create fixture failed: %v\noutput: %s", err, createOutput)
	}
	webhookID, ok := webhookIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected webhook id in create output, got: %s", createOutput)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("webhooks list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "workflow", "webhooks", "delete", webhookID)
	if err != nil {
		t.Fatalf("webhook delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.webhook.delete"`) {
		t.Fatalf("expected repo.webhook.delete intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "list")
	if err != nil {
		t.Fatalf("webhooks list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no webhook side-effect from delete dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}

	_, _ = executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "delete", webhookID)
}

func TestLiveCLIPRCreateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := "feature/live-pr-dryrun-create"
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-pr-create.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "pr", "list", "--state", "all", "--source-branch", branch, "--target-branch", "master")
	if err != nil {
		t.Fatalf("pr list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "create", "--from-ref", branch, "--to-ref", "master", "--title", "Dry run PR")
	if err != nil {
		t.Fatalf("pr create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"planning_mode": "stateful"`) {
		t.Fatalf("expected stateful planning mode, got: %s", dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.create"`) {
		t.Fatalf("expected pr.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "pr", "list", "--state", "all", "--source-branch", branch, "--target-branch", "master")
	if err != nil {
		t.Fatalf("pr list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no pull request side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIPRUpdateDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeTitle := prFieldAsString(t, beforeOutput, "title")

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "update", pullRequestID, "--title", beforeTitle+" dry-run", "--version", "0")
	if err != nil {
		t.Fatalf("pr update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.update"`) {
		t.Fatalf("expected pr.update intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterTitle := prFieldAsString(t, afterOutput, "title")
	if beforeTitle != afterTitle {
		t.Fatalf("expected no title side-effect from update dry-run\nbefore: %s\nafter: %s", beforeTitle, afterTitle)
	}
}

func TestLiveCLIPRGetIncludesMergeability(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	output, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	payload := decodeJSONMap(t, output)
	pullRequest, ok := payload["pull_request"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request field missing from output: %s", output)
	}
	mergeability, ok := pullRequest["mergeability"].(map[string]any)
	if !ok {
		t.Fatalf("mergeability field missing from pr get output: %s", output)
	}
	if _, ok := mergeability["mergeable"].(bool); !ok {
		t.Fatalf("mergeability.mergeable missing from pr get output: %s", output)
	}
}

func TestLiveCLIPRMergeDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeState := prFieldAsString(t, beforeOutput, "state")

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "merge", pullRequestID, "--version", "0")
	if err != nil {
		t.Fatalf("pr merge dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.merge"`) {
		t.Fatalf("expected pr.merge intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterState := prFieldAsString(t, afterOutput, "state")
	if beforeState != afterState {
		t.Fatalf("expected no state side-effect from merge dry-run\nbefore: %s\nafter: %s", beforeState, afterState)
	}
}

func TestLiveCLIPRReviewerAddDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	username := strings.TrimSpace(harness.config.BitbucketUsername)
	if username == "" {
		t.Skip("no username configured for reviewer dry-run live test")
	}

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeReviewers := prReviewersSnapshot(t, beforeOutput)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "reviewer", "add", pullRequestID, "--user", username)
	if err != nil {
		t.Fatalf("pr reviewer add dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.review.reviewer.add"`) {
		t.Fatalf("expected pr.review.reviewer.add intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterReviewers := prReviewersSnapshot(t, afterOutput)
	if beforeReviewers != afterReviewers {
		t.Fatalf("expected no reviewer side-effect from add dry-run\nbefore: %s\nafter: %s", beforeReviewers, afterReviewers)
	}
}

func TestLiveCLIPRReviewerRemoveDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	username := strings.TrimSpace(harness.config.BitbucketUsername)
	if username == "" {
		t.Skip("no username configured for reviewer dry-run live test")
	}

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeReviewers := prReviewersSnapshot(t, beforeOutput)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "reviewer", "remove", pullRequestID, "--user", username)
	if err != nil {
		t.Fatalf("pr reviewer remove dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.review.reviewer.remove"`) {
		t.Fatalf("expected pr.review.reviewer.remove intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterReviewers := prReviewersSnapshot(t, afterOutput)
	if beforeReviewers != afterReviewers {
		t.Fatalf("expected no reviewer side-effect from remove dry-run\nbefore: %s\nafter: %s", beforeReviewers, afterReviewers)
	}
}

func TestLiveCLIPRTaskCreateDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "task", "list", pullRequestID, "--state", "all", "--limit", "200")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not_found") {
			t.Skipf("pull request task endpoint unavailable in live environment: %v", err)
		}
		t.Fatalf("pr task list before failed: %v\noutput: %s", err, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "task", "create", pullRequestID, "--text", "dry-run task")
	if err != nil {
		t.Fatalf("pr task create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.task.create"`) {
		t.Fatalf("expected pr.task.create intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "task", "list", pullRequestID, "--state", "all", "--limit", "200")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "not_found") {
			t.Skipf("pull request task endpoint unavailable in live environment: %v", err)
		}
		t.Fatalf("pr task list after failed: %v\noutput: %s", err, afterOutput)
	}

	if beforeOutput != afterOutput {
		t.Fatalf("expected no task side-effect from create dry-run\nbefore: %s\nafter: %s", beforeOutput, afterOutput)
	}
}

func TestLiveCLIPRTaskDeleteDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	createTaskOutput, err := executeLiveCLI(t, "--json", "pr", "task", "create", pullRequestID, "--text", "fixture task for dry-run delete")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()+" "+createTaskOutput), "not_found") {
			t.Skipf("pull request task endpoint unavailable in live environment: %v", err)
		}
		t.Fatalf("pr task create fixture failed: %v\noutput: %s", err, createTaskOutput)
	}

	taskID := taskIDFromPRTaskOutput(t, createTaskOutput)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "task", "list", pullRequestID, "--state", "all", "--limit", "200")
	if err != nil {
		t.Fatalf("pr task list before failed: %v\noutput: %s", err, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "task", "delete", pullRequestID, "--task", taskID)
	if err != nil {
		t.Fatalf("pr task delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.task.delete"`) {
		t.Fatalf("expected pr.task.delete intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "task", "list", pullRequestID, "--state", "all", "--limit", "200")
	if err != nil {
		t.Fatalf("pr task list after failed: %v\noutput: %s", err, afterOutput)
	}

	if beforeOutput != afterOutput {
		t.Fatalf("expected no task side-effect from delete dry-run\nbefore: %s\nafter: %s", beforeOutput, afterOutput)
	}

	_, _ = executeLiveCLI(t, "pr", "task", "delete", pullRequestID, "--task", taskID)
}

func TestLiveCLIPRTaskUpdateDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	createTaskOutput, err := executeLiveCLI(t, "--json", "pr", "task", "create", pullRequestID, "--text", "fixture task for dry-run update")
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()+" "+createTaskOutput), "not_found") {
			t.Skipf("pull request task endpoint unavailable in live environment: %v", err)
		}
		t.Fatalf("pr task create fixture failed: %v\noutput: %s", err, createTaskOutput)
	}

	taskID := taskIDFromPRTaskOutput(t, createTaskOutput)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "task", "list", pullRequestID, "--state", "all", "--limit", "200")
	if err != nil {
		t.Fatalf("pr task list before failed: %v\noutput: %s", err, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "task", "update", pullRequestID, "--task", taskID, "--text", "dry-run updated text")
	if err != nil {
		t.Fatalf("pr task update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.task.update"`) {
		t.Fatalf("expected pr.task.update intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "task", "list", pullRequestID, "--state", "all", "--limit", "200")
	if err != nil {
		t.Fatalf("pr task list after failed: %v\noutput: %s", err, afterOutput)
	}

	if beforeOutput != afterOutput {
		t.Fatalf("expected no task side-effect from update dry-run\nbefore: %s\nafter: %s", beforeOutput, afterOutput)
	}

	_, _ = executeLiveCLI(t, "pr", "task", "delete", pullRequestID, "--task", taskID)
}

func TestLiveCLIPRDeclineDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeState := prFieldAsString(t, beforeOutput, "state")

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "decline", pullRequestID, "--version", "0")
	if err != nil {
		t.Fatalf("pr decline dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.decline"`) {
		t.Fatalf("expected pr.decline intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterState := prFieldAsString(t, afterOutput, "state")
	if beforeState != afterState {
		t.Fatalf("expected no state side-effect from decline dry-run\nbefore: %s\nafter: %s", beforeState, afterState)
	}
}

func TestLiveCLIPRReopenDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	declineOutput, err := executeLiveCLI(t, "--json", "pr", "decline", pullRequestID, "--version", "0")
	if err != nil {
		t.Fatalf("pr decline fixture failed: %v\noutput: %s", err, declineOutput)
	}

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeState := prFieldAsString(t, beforeOutput, "state")

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "reopen", pullRequestID, "--version", "1")
	if err != nil {
		t.Fatalf("pr reopen dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.reopen"`) {
		t.Fatalf("expected pr.reopen intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterState := prFieldAsString(t, afterOutput, "state")
	if beforeState != afterState {
		t.Fatalf("expected no state side-effect from reopen dry-run\nbefore: %s\nafter: %s", beforeState, afterState)
	}
}

func TestLiveCLIPRApproveDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeReviewers := prReviewersSnapshot(t, beforeOutput)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "approve", pullRequestID)
	if err != nil {
		t.Fatalf("pr approve dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.review.approve"`) {
		t.Fatalf("expected pr.review.approve intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterReviewers := prReviewersSnapshot(t, afterOutput)
	if beforeReviewers != afterReviewers {
		t.Fatalf("expected no reviewer side-effect from approve dry-run\nbefore: %s\nafter: %s", beforeReviewers, afterReviewers)
	}
}

func TestLiveCLIPRUnapproveDryRunNoSideEffect(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	beforeOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get before failed: %v\noutput: %s", err, beforeOutput)
	}
	beforeReviewers := prReviewersSnapshot(t, beforeOutput)

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "unapprove", pullRequestID)
	if err != nil {
		t.Fatalf("pr unapprove dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "pr.review.unapprove"`) {
		t.Fatalf("expected pr.review.unapprove intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get after failed: %v\noutput: %s", err, afterOutput)
	}
	afterReviewers := prReviewersSnapshot(t, afterOutput)
	if beforeReviewers != afterReviewers {
		t.Fatalf("expected no reviewer side-effect from unapprove dry-run\nbefore: %s\nafter: %s", beforeReviewers, afterReviewers)
	}
}

func TestLiveCLIPRWatchUnwatchRebase(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Watch dry-run
	watchDryRun, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "watch", pullRequestID)
	if err != nil {
		t.Fatalf("pr watch dry-run failed: %v\noutput: %s", err, watchDryRun)
	}
	if !strings.Contains(watchDryRun, `"intent": "pr.watch"`) {
		t.Fatalf("expected pr.watch intent, got: %s", watchDryRun)
	}

	// Watch live
	watchLive, err := executeLiveCLI(t, "--json", "pr", "watch", pullRequestID)
	if err != nil {
		t.Fatalf("pr watch live failed: %v\noutput: %s", err, watchLive)
	}

	// Unwatch dry-run
	unwatchDryRun, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "unwatch", pullRequestID)
	if err != nil {
		t.Fatalf("pr unwatch dry-run failed: %v\noutput: %s", err, unwatchDryRun)
	}
	if !strings.Contains(unwatchDryRun, `"intent": "pr.unwatch"`) {
		t.Fatalf("expected pr.unwatch intent, got: %s", unwatchDryRun)
	}

	// Unwatch live
	unwatchLive, err := executeLiveCLI(t, "--json", "pr", "unwatch", pullRequestID)
	if err != nil {
		t.Fatalf("pr unwatch live failed: %v\noutput: %s", err, unwatchLive)
	}

	// Rebase dry-run (checking rebaseability)
	rebaseDryRun, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "rebase", pullRequestID)
	if err != nil {
		t.Fatalf("pr rebase dry-run failed: %v\noutput: %s", err, rebaseDryRun)
	}
	if !strings.Contains(rebaseDryRun, `"intent": "pr.rebase"`) {
		t.Fatalf("expected pr.rebase intent, got: %s", rebaseDryRun)
	}
}

func TestLiveCommitPRsAndParticipants(t *testing.T) {
	harness, seeded, repo, pullRequestID := prepareOpenPRDryRunFixture(t)
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Get the PR to find its source commit
	prGetJSON, err := executeLiveCLI(t, "--json", "pr", "get", pullRequestID)
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, prGetJSON)
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(prGetJSON), &envelope); err != nil {
		t.Fatalf("failed to parse pr get JSON: %v", err)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("data field not found or not a map in envelope: %s", prGetJSON)
	}
	prData, ok := data["pull_request"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request field not found or not a map in data: %s", prGetJSON)
	}
	sourceCommit, ok := prData["source_commit"].(string)
	if !ok || sourceCommit == "" {
		t.Fatalf("source_commit not found or empty in get output: %s", prGetJSON)
	}

	// 1. Test List pull requests containing commit
	commitPRsJSON, err := executeLiveCLI(t, "--json", "commit", "prs", sourceCommit)
	if err != nil {
		t.Fatalf("commit prs failed: %v\noutput: %s", err, commitPRsJSON)
	}
	if !strings.Contains(commitPRsJSON, fmt.Sprintf(`"id": %s`, pullRequestID)) {
		t.Fatalf("expected PR ID %s in commit prs output, got: %s", pullRequestID, commitPRsJSON)
	}

	// 2. Test Search participants
	participantsJSON, err := executeLiveCLI(t, "--json", "pr", "participants", "--search", "admin")
	if err != nil {
		t.Fatalf("pr participants failed: %v\noutput: %s", err, participantsJSON)
	}
	if !strings.Contains(participantsJSON, `"name": "admin"`) {
		t.Fatalf("expected participant admin in output, got: %s", participantsJSON)
	}
}

func TestLiveCLIRepoCommentCreateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := fmt.Sprintf("feature/live-comment-dryrun-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-comment-fixture.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request fixture failed: %v", err)
	}

	listBeforeOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("repo comment list before failed: %v\noutput: %s", err, listBeforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "comment", "create", "--pr", pullRequestID, "--text", "dry-run comment")
	if err != nil {
		t.Fatalf("repo comment create dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.comment.create"`) {
		t.Fatalf("expected repo.comment.create intent, got: %s", dryRunOutput)
	}

	listAfterOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("repo comment list after failed: %v\noutput: %s", err, listAfterOutput)
	}

	if listBeforeOutput != listAfterOutput {
		t.Fatalf("expected no comment side-effect from create dry-run\nbefore: %s\nafter: %s", listBeforeOutput, listAfterOutput)
	}
}

func TestLiveCLIRepoCommentUpdateDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := fmt.Sprintf("feature/live-comment-update-dryrun-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-comment-update-fixture.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request fixture failed: %v", err)
	}

	createOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create", "--pr", pullRequestID, "--text", "fixture comment")
	if err != nil {
		t.Fatalf("create fixture comment failed: %v\noutput: %s", err, createOutput)
	}
	commentID, ok := commentIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected comment id in fixture output: %s", createOutput)
	}

	beforeOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-update-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("comment list before failed: %v\noutput: %s", err, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "comment", "update", "--pr", pullRequestID, "--id", commentID, "--text", "dry-run updated comment")
	if err != nil {
		t.Fatalf("repo comment update dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.comment.update"`) {
		t.Fatalf("expected repo.comment.update intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-update-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("comment list after failed: %v\noutput: %s", err, afterOutput)
	}

	if beforeOutput != afterOutput {
		t.Fatalf("expected no comment side-effect from update dry-run\nbefore: %s\nafter: %s", beforeOutput, afterOutput)
	}

	_, _ = executeLiveCLI(t, "repo", "comment", "delete", "--pr", pullRequestID, "--id", commentID)
}

func TestLiveCLIRepoCommentDeleteDryRunNoSideEffect(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	branch := fmt.Sprintf("feature/live-comment-delete-dryrun-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-comment-delete-fixture.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request fixture failed: %v", err)
	}

	createOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "create", "--pr", pullRequestID, "--text", "fixture comment delete")
	if err != nil {
		t.Fatalf("create fixture comment failed: %v\noutput: %s", err, createOutput)
	}
	commentID, ok := commentIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected comment id in fixture output: %s", createOutput)
	}

	beforeOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-delete-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("comment list before failed: %v\noutput: %s", err, beforeOutput)
	}

	dryRunOutput, err := executeLiveCLI(t, "--json", "--dry-run", "repo", "comment", "delete", "--pr", pullRequestID, "--id", commentID)
	if err != nil {
		t.Fatalf("repo comment delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(dryRunOutput, `"intent": "repo.comment.delete"`) {
		t.Fatalf("expected repo.comment.delete intent, got: %s", dryRunOutput)
	}

	afterOutput, err := executeLiveCLI(t, "--json", "repo", "comment", "list", "--pr", pullRequestID, "--path", "dryrun-comment-delete-fixture.txt", "--limit", "200")
	if err != nil {
		t.Fatalf("comment list after failed: %v\noutput: %s", err, afterOutput)
	}

	if beforeOutput != afterOutput {
		t.Fatalf("expected no comment side-effect from delete dry-run\nbefore: %s\nafter: %s", beforeOutput, afterOutput)
	}

	_, _ = executeLiveCLI(t, "repo", "comment", "delete", "--pr", pullRequestID, "--id", commentID)
}

func prepareOpenPRDryRunFixture(t *testing.T) (*liveHarness, seededProject, seededRepository, string) {
	t.Helper()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 2)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	branch := fmt.Sprintf("feature/live-pr-dryrun-%d", time.Now().UnixNano()%100000)
	if err := harness.pushCommitOnBranch(seeded.Key, repo.Slug, branch, "dryrun-pr-fixture.txt"); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}

	pullRequestID, err := harness.createPullRequest(ctx, seeded.Key, repo.Slug, branch, "master")
	if err != nil {
		t.Fatalf("create pull request fixture failed: %v", err)
	}

	return harness, seeded, repo, pullRequestID
}

func prFieldAsString(t *testing.T, output, field string) string {
	t.Helper()
	payload := decodeJSONMap(t, output)
	pullRequest, ok := payload["pull_request"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request field missing from output: %s", output)
	}
	return asString(pullRequest[field])
}

func prReviewersSnapshot(t *testing.T, output string) string {
	t.Helper()
	payload := decodeJSONMap(t, output)
	pullRequest, ok := payload["pull_request"].(map[string]any)
	if !ok {
		t.Fatalf("pull_request field missing from output: %s", output)
	}
	reviewers := pullRequest["reviewers"]
	raw, err := json.Marshal(reviewers)
	if err != nil {
		t.Fatalf("failed to marshal reviewers snapshot: %v", err)
	}
	return string(raw)
}

func taskIDFromPRTaskOutput(t *testing.T, output string) string {
	t.Helper()
	payload := decodeJSONMap(t, output)
	task, ok := payload["task"].(map[string]any)
	if !ok {
		t.Fatalf("task field missing from output: %s", output)
	}
	id, ok := numericOrStringID(task["id"])
	if !ok {
		t.Fatalf("task id missing from output: %s", output)
	}
	return id
}

func jsonArrayContainsSlug(t *testing.T, output string, slug string) bool {
	t.Helper()

	items := make([]map[string]any, 0)
	if err := unmarshalJSONArray(output, &items); err != nil {
		t.Fatalf("expected json array output, got parse error %v for: %s", err, output)
	}

	for _, item := range items {
		if asString(item["slug"]) == slug {
			return true
		}
	}

	return false
}

func jsonObjectHasCommentsArray(t *testing.T, output string) bool {
	t.Helper()

	payload := decodeJSONMap(t, output)
	_, ok := payload["comments"].([]any)
	return ok
}

func commentIDFromCreateOutput(output string) (string, bool) {
	payload := map[string]any{}
	if err := unmarshalJSONObject(output, &payload); err != nil {
		return "", false
	}

	comment, ok := payload["comment"].(map[string]any)
	if !ok {
		return "", false
	}

	return numericOrStringID(comment["id"])
}

func commentVersionFromCreateOutput(output string) (string, bool) {
	payload := map[string]any{}
	if err := unmarshalJSONObject(output, &payload); err != nil {
		return "", false
	}

	comment, ok := payload["comment"].(map[string]any)
	if !ok {
		return "", false
	}

	return numericOrStringID(comment["version"])
}

func webhookIDFromCreateOutput(output string) (string, bool) {
	payload := map[string]any{}
	if err := unmarshalJSONObject(output, &payload); err != nil {
		return "", false
	}

	webhook, ok := payload["webhook"].(map[string]any)
	if !ok {
		return "", false
	}

	return numericOrStringID(webhook["id"])
}

func numericOrStringID(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return "", false
		}
		return trimmed, true
	case float64:
		return strconv.FormatInt(int64(typed), 10), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	case int:
		return strconv.Itoa(typed), true
	default:
		return "", false
	}
}

func unmarshalJSONObject(value string, target *map[string]any) error {
	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		return err
	}

	rawData, ok := envelope["data"]
	if !ok {
		return fmt.Errorf("missing data field")
	}

	encodedData, err := json.Marshal(rawData)
	if err != nil {
		return err
	}

	return json.Unmarshal(encodedData, target)
}

func unmarshalJSONArray(value string, target *[]map[string]any) error {
	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(value), &envelope); err != nil {
		return err
	}

	rawData, ok := envelope["data"]
	if !ok {
		return fmt.Errorf("missing data field")
	}

	encodedData, err := json.Marshal(rawData)
	if err != nil {
		return err
	}

	return json.Unmarshal(encodedData, target)
}
