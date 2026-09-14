package pullrequest

import (
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestDraftChangeRefusalAllowsOnlyAnOpenPullRequest(t *testing.T) {
	t.Parallel()

	if err := DraftChangeRefusal(PullRequest{ID: 42, State: "OPEN", Open: true}); err != nil {
		t.Fatalf("an open pull request was refused: %v", err)
	}
	if err := DraftChangeRefusal(PullRequest{ID: 42, State: "OPEN", Open: true, Draft: true}); err != nil {
		t.Fatalf("an open draft was refused: %v", err)
	}

	for _, state := range []string{"DECLINED", "MERGED"} {
		err := DraftChangeRefusal(PullRequest{ID: 42, State: state, Closed: true})
		if !apperrors.IsKind(err, apperrors.KindConflict) {
			t.Errorf("a %s pull request: got %v, want a conflict", state, err)
			continue
		}
		if !strings.Contains(err.Error(), "#42") || !strings.Contains(err.Error(), state) {
			t.Errorf("a %s pull request: %q does not say which pull request or why", state, err)
		}
	}
}

func TestReviewerEchoKeepsNamedReviewersOnly(t *testing.T) {
	t.Parallel()

	echo := reviewerEcho(PullRequest{Reviewers: []Reviewer{{Name: "alice"}, {Name: "  "}, {Name: " bob "}}})
	if len(echo) != 2 {
		t.Fatalf("echo = %v, want alice and bob", echo)
	}
	for index, want := range []string{"alice", "bob"} {
		user, _ := echo[index]["user"].(map[string]any)
		if user["name"] != want {
			t.Errorf("echo[%d] = %v, want %s", index, echo[index], want)
		}
	}

	if empty := reviewerEcho(PullRequest{}); empty == nil || len(empty) != 0 {
		t.Errorf("no reviewers must echo an empty list rather than nil, got %#v", empty)
	}
}
