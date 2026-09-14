package pullrequest

import (
	"context"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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

// SetDraft refuses a target it can judge on its own before it reads anything.
// The service has no client at all, so a request would panic rather than fail:
// a validation error here can only have come before one.
func TestSetDraftRefusesAnIncompleteTargetBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	service := NewService(nil)

	for _, repository := range []RepositoryRef{{}, {ProjectKey: "PRJ"}, {Slug: "demo"}} {
		if _, _, err := service.SetDraft(context.Background(), repository, "42", false); !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Errorf("repository %+v: got %v, want a validation error", repository, err)
		}
	}

	for _, id := range []string{"", "not-a-number"} {
		_, changed, err := service.SetDraft(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, id, true)
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Errorf("pull request id %q: got %v, want a validation error", id, err)
		}
		if changed {
			t.Errorf("pull request id %q: a refused change reported itself changed", id)
		}
	}
}

// A rebase that has to read the version reads it over the REST client. Without
// one it has to say so, not dereference nil -- and only when it needs it.
func TestRebaseWithoutAVersionSaysItCannotReadOne(t *testing.T) {
	t.Parallel()

	service := NewService(nil).WithAPIClient(&openapigenerated.ClientWithResponses{})

	_, err := service.Rebase(context.Background(), RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}, "42", nil)
	if !apperrors.IsKind(err, apperrors.KindInternal) {
		t.Fatalf("got %v, want an internal error", err)
	}
	if !strings.Contains(err.Error(), "http client is not configured") {
		t.Errorf("%q does not name the missing client", err)
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
