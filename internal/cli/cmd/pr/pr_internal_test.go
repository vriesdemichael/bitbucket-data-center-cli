package prcmd

import (
	"context"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

func TestPRDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	cmd := New(Dependencies{})
	if cmd == nil {
		t.Fatal("expected New(Dependencies{}) to return non-nil command")
	}
}

func TestPRFormatSafeHelpers(t *testing.T) {
	// The pointer helpers moved to internal/safederef and are tested
	// there. safeUsers is this package's own and stays.
	s := "test"
	if safeUsers(nil) != nil {
		t.Fatal("expected nil for safeUsers(nil)")
	}
	users := []openapigenerated.RestApplicationUser{{Name: &s}}
	if len(safeUsers(&users)) != 1 {
		t.Fatal("expected 1 element for safeUsers(&users)")
	}
}

func TestFormatPullRequestCountsAndActivity(t *testing.T) {
	tasks := 3
	comments := 5
	pr := result.PullRequest{
		OpenTaskCount: &tasks,
		CommentCount:  &comments,
	}
	counts := formatPullRequestCounts(pr)
	if !strings.Contains(counts, "tasks:3") || !strings.Contains(counts, "comments:5") {
		t.Fatalf("unexpected formatPullRequestCounts result: %s", counts)
	}

	// Empty counts
	if formatPullRequestCounts(result.PullRequest{}) != "" {
		t.Fatal("expected empty string for empty counts")
	}

	// Activity summary
	activitySummary := formatPullRequestActivitySummary(pullrequestactivityservice.Activity{
		ID:     12,
		Action: "OPENED",
	})
	if !strings.Contains(activitySummary, "[12 OPENED]") {
		t.Fatalf("unexpected activitySummary: %s", activitySummary)
	}

	// Activity summary with comment
	commentText := "Looks good"
	activitySummary = formatPullRequestActivitySummary(pullrequestactivityservice.Activity{
		ID:     13,
		Action: "COMMENTED",
		Comment: &openapigenerated.RestComment{
			Text: &commentText,
		},
	})
	if !strings.Contains(activitySummary, "Looks good") {
		t.Fatalf("unexpected activitySummary with comment: %s", activitySummary)
	}
}

func TestNormalizeEmoticon(t *testing.T) {
	if normalizeEmoticon(":smile:") != "smile" {
		t.Fatalf("unexpected normalizeEmoticon: %s", normalizeEmoticon(":smile:"))
	}
	if normalizeEmoticon("smile") != "smile" {
		t.Fatalf("unexpected normalizeEmoticon plain: %s", normalizeEmoticon("smile"))
	}
}

func TestCheckRepoPermission(t *testing.T) {
	checker := nopPermissionChecker{}
	if err := checker.CheckRepoPermission(context.Background(), "PRJ", "repo1", openapi.RepoRead); err != nil {
		t.Fatalf("unexpected error from nopPermissionChecker: %v", err)
	}
}
