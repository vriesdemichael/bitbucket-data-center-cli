package prcmd

import (
	"context"
	"strings"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequestactivity"
)

func TestPRDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	cmd := New(Dependencies{})
	if cmd == nil {
		t.Fatal("expected New(Dependencies{}) to return non-nil command")
	}
}

func TestPRFormatSafeHelpers(t *testing.T) {
	if safeString(nil) != "" {
		t.Fatal("expected empty string for safeString(nil)")
	}
	s := "test"
	if safeString(&s) != "test" {
		t.Fatal("expected test for safeString(&s)")
	}

	if safeInt32(nil) != 0 {
		t.Fatal("expected 0 for safeInt32(nil)")
	}
	i32 := int32(42)
	if safeInt32(&i32) != 42 {
		t.Fatal("expected 42 for safeInt32(&i32)")
	}

	if safeInt64(nil) != 0 {
		t.Fatal("expected 0 for safeInt64(nil)")
	}
	i64 := int64(420)
	if safeInt64(&i64) != 420 {
		t.Fatal("expected 420 for safeInt64(&i64)")
	}

	if safeStringSlice(nil) != nil {
		t.Fatal("expected nil for safeStringSlice(nil)")
	}
	sl := []string{"a", "b"}
	if len(safeStringSlice(&sl)) != 2 {
		t.Fatal("expected 2 elements for safeStringSlice(&sl)")
	}

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
	pr := pullrequestservice.PullRequest{
		OpenTaskCount: &tasks,
		CommentCount:  &comments,
	}
	counts := formatPullRequestCounts(pr)
	if !strings.Contains(counts, "tasks:3") || !strings.Contains(counts, "comments:5") {
		t.Fatalf("unexpected formatPullRequestCounts result: %s", counts)
	}

	// Empty counts
	if formatPullRequestCounts(pullrequestservice.PullRequest{}) != "" {
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
	if err := checker.CheckRepoPermission(context.Background(), "PRJ", "repo1", openapigenerated.REPOREAD); err != nil {
		t.Fatalf("unexpected error from nopPermissionChecker: %v", err)
	}
}
