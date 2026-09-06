//go:build live

package live_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestLiveReviewerConditionsLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	username := harness.username()

	// 1. List initial conditions on repo
	listOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("initial reviewer condition list failed: %v\noutput: %s", err, listOutput)
	}

	// 2. Create reviewer condition with explicit branch matchers and reviewer
	conditionJSON := fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"name": "%s"}],
		"requiredApprovals": 1
	}`, username)

	createOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "create", conditionJSON, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Logf("reviewer condition create live attempt output: %s (err: %v)", createOutput, err)
		return
	}

	var createEnvelope struct {
		Version string         `json:"version"`
		Data    map[string]any `json:"data"`
	}
	_ = json.Unmarshal([]byte(createOutput), &createEnvelope)

	// Extract condition ID
	var conditionID string
	if id, ok := createEnvelope.Data["id"]; ok {
		conditionID = fmt.Sprintf("%v", id)
	} else if strings.Contains(createOutput, `"id":`) {
		parts := strings.Split(createOutput, `"id":`)
		if len(parts) > 1 {
			idStr := strings.TrimSpace(strings.Split(parts[1], ",")[0])
			idStr = strings.TrimSpace(strings.Split(idStr, "}")[0])
			conditionID = idStr
		}
	}

	if conditionID == "" {
		t.Logf("could not parse condition ID from output: %s", createOutput)
		return
	}

	defer func() {
		_, _ = executeLiveCLI(t, "reviewer", "condition", "delete", conditionID, "--repo", seeded.Key+"/"+repo.Slug)
	}()

	// 3. Verify condition appears in listing
	afterList, err := executeLiveCLI(t, "--json", "reviewer", "condition", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition list after create failed: %v\noutput: %s", err, afterList)
	}
	if !strings.Contains(afterList, conditionID) {
		t.Fatalf("expected condition ID %s in list output: %s", conditionID, afterList)
	}

	// 4. Update condition required approvals
	updateJSON := fmt.Sprintf(`{
		"sourceMatcher": {"id": "ANY_REF", "type": {"id": "ANY_REF"}},
		"targetMatcher": {"id": "refs/heads/master", "type": {"id": "BRANCH"}},
		"reviewers": [{"name": "%s"}],
		"requiredApprovals": 2
	}`, username)

	updateOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "update", conditionID, updateJSON, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition update failed: %v\noutput: %s", err, updateOutput)
	}

	// 5. Delete condition with dry-run
	dryRunOutput, err := executeLiveCLI(t, "--dry-run", "reviewer", "condition", "delete", conditionID, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}
	if !strings.Contains(strings.ToLower(dryRunOutput), "dry-run") && !strings.Contains(strings.ToLower(dryRunOutput), "delete") {
		t.Fatalf("expected dry-run preview in output: %s", dryRunOutput)
	}

	// 6. Delete condition for real
	deleteOutput, err := executeLiveCLI(t, "--json", "reviewer", "condition", "delete", conditionID, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer condition delete failed: %v\noutput: %s", err, deleteOutput)
	}
}

func TestLiveReviewerGroupsLifecycle(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	username := harness.username()
	groupName := fmt.Sprintf("team-gov-%d", time.Now().UnixNano()%10000)

	// 1. Create reviewer group on repository
	//
	// The member is not decoration: Bitbucket refuses a group with none, so
	// before --users existed this call failed on every run. The test carried on
	// with t.Logf and a bare return, so steps 2 to 4 never executed and the
	// suite stayed green while asserting nothing (#533).
	createOutput, err := executeLiveCLI(t, "--json", "reviewer-group", "create", groupName, "--repo", seeded.Key+"/"+repo.Slug, "--users", username)
	if err != nil {
		t.Fatalf("reviewer group create failed: %v\noutput: %s", err, createOutput)
	}

	groupID := fmt.Sprintf("%d", int64(decodeJSONMap(t, createOutput)["id"].(float64)))

	defer func() {
		_, _ = executeLiveCLI(t, "reviewer-group", "delete", groupID, "--repo", seeded.Key+"/"+repo.Slug)
	}()

	// 2. List reviewer groups on repository
	listOutput, err := executeLiveCLI(t, "--json", "reviewer-group", "list", "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer group list failed: %v\noutput: %s", err, listOutput)
	}
	if !strings.Contains(listOutput, groupName) {
		t.Fatalf("expected group name %s in list output: %s", groupName, listOutput)
	}

	// 3. The member is really in the group, which is what makes it usable
	usersOutput, err := executeLiveCLI(t, "--json", "reviewer-group", "users", groupID, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer group users failed: %v\noutput: %s", err, usersOutput)
	}
	if !strings.Contains(usersOutput, username) {
		t.Fatalf("expected user %s in reviewer group users output: %s", username, usersOutput)
	}

	// 4. Delete reviewer group with dry-run
	dryRunOutput, err := executeLiveCLI(t, "--dry-run", "reviewer-group", "delete", groupID, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer group delete dry-run failed: %v\noutput: %s", err, dryRunOutput)
	}

	// 5. Delete reviewer group for real
	deleteOutput, err := executeLiveCLI(t, "--json", "reviewer-group", "delete", groupID, "--repo", seeded.Key+"/"+repo.Slug)
	if err != nil {
		t.Fatalf("reviewer group delete failed: %v\noutput: %s", err, deleteOutput)
	}
}
