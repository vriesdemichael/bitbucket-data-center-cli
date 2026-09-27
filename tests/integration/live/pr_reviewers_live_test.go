//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestLivePRReviewersAreNotTheParticipants: Bitbucket lists a pull request's
// reviewers in "reviewers", and everyone else who took part in
// "participants". bb read "participants" whenever it was not empty, so a pull
// request someone else had commented on, or merged, reported that person as
// its only reviewer and the real reviewer's approval as missing.
func TestLivePRReviewersAreNotTheParticipants(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	people := make([]restrictedUser, 2)
	for i := range people {
		user, err := harness.createLicensedUser(ctx)
		if err != nil {
			t.Fatalf("create a second person failed: %v", err)
		}
		if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, user.Username,
			openapigenerated.SetPermissionForUserParamsPermissionREPOREAD); err != nil {
			t.Fatalf("grant read access failed: %v", err)
		}
		people[i] = user
	}
	reviewer, commenter := people[0], people[1]

	branch := testsupport.UniqueName("feature/reviewed-")
	if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, "reviewed.txt", "content\n"); err != nil {
		t.Fatalf("push failed: %v", err)
	}
	created := extractPRData(decodeJSONMap(t, mustLiveCLI(t, "pr", "create",
		"--from-ref", branch, "--to-ref", "refs/heads/master", "--title", "Reviewed by one, discussed by another",
		"--reviewers", reviewer.Username, "--no-default-reviewers", "--no-codeowners")))
	id := fmt.Sprint(created["id"])

	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s", seeded.Key, repo.Slug, id)
	if _, err := harness.liveJSONAs(ctx, reviewer, http.MethodPut, path+"/participants/"+reviewer.Username,
		map[string]any{"user": map[string]any{"name": reviewer.Username}, "status": "APPROVED"}); err != nil {
		t.Fatalf("the reviewer's approval failed: %v", err)
	}
	// A comment from someone who was not asked to review makes them a
	// participant, which is where the defect read reviewers from.
	if _, err := harness.liveJSONAs(ctx, commenter, http.MethodPost, path+"/comments",
		map[string]any{"text": "Drive-by: this looks fine to me."}); err != nil {
		t.Fatalf("the comment failed: %v", err)
	}
	participants, err := harness.liveJSON(ctx, http.MethodGet, path, nil)
	if err != nil {
		t.Fatalf("read the pull request from Bitbucket failed: %v", err)
	}
	if listed, _ := participants["participants"].([]any); len(listed) == 0 {
		t.Fatalf("the commenter is not among Bitbucket's participants, so this test proves nothing: %v", participants["participants"])
	}

	read := decodeJSONMap(t, mustLiveCLI(t, "pr", "get", id))
	reviewers, _ := extractPRData(read)["reviewers"].([]any)
	if len(reviewers) != 1 {
		t.Fatalf("bb reports %d reviewers, want the one asked: %v", len(reviewers), reviewers)
	}
	only, _ := reviewers[0].(map[string]any)
	if !strings.EqualFold(fmt.Sprint(only["name"]), reviewer.Username) || only["approved"] != true || only["status"] != "APPROVED" {
		t.Errorf("bb reports reviewer %v, want %s, approved", only, reviewer.Username)
	}
	summary, _ := read["reviewSummary"].(map[string]any)
	if summary["approvals"] != float64(1) || summary["reviewers"] != float64(1) {
		t.Errorf("the review summary counts %v of %v approvals, want 1 of 1", summary["approvals"], summary["reviewers"])
	}
}
