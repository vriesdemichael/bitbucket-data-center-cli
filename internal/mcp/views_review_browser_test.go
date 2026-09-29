//go:build views

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
)

// reviewing is what a server that lets views review offers them.
var reviewing = &viewOffers{Kinds: []string{showKindPullRequest, showKindDiff, showKindThreads}, Tools: []string{"add_pr_comment", "submit_pr_review"}}

func reviewedCard(me *viewMe, state, fingerprint string) viewPayload {
	card := refreshCardPayload("Round refunds", state, time.Now(), fingerprint)
	card.Offers = reviewing
	card.Me = me
	return card
}

// A reviewer approves from the overview through submit_pr_review, and the
// view shows the approval once Bitbucket has it; pressed again it takes the
// approval back. A review that fails says why.
func TestAReviewerApprovesFromTheOverview(t *testing.T) {
	approved := reviewedCard(&viewMe{Username: "dave", Status: "APPROVED"}, "OPEN", "two")
	ctx := browser(t, []viewhost.Frame{
		{Title: "card fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, reviewedCard(&viewMe{Username: "dave", Status: "UNAPPROVED"}, "OPEN", "one")),
			ToolResults: map[string][]json.RawMessage{
				"submit_pr_review": {toolAnswer(t, `{"pull_request":{"id":42}}`, false), toolAnswer(t, "The person declined the confirmation.", true)},
				"refresh_view":     {refreshAnswer(t, &approved, "The view of pull request PAY/ledger#42 you showed the person has changed: 2 approved.")},
			}},
	})

	clickButton(t, ctx, 0, "Approve")
	waitInFrame(t, ctx, 0, `[...d.querySelectorAll(".review-actions button")].some((b) => b.textContent.includes("Approved") && b.getAttribute("aria-pressed") === "true")`, "the approval does not show once given")
	calls := toolCalls(t, ctx, 0, "submit_pr_review")
	if len(calls) != 1 {
		t.Fatalf("the view called submit_pr_review %d times, want once: %v", len(calls), calls)
	}
	for key, want := range map[string]any{"project": "PAY", "repo": "ledger", "pr_id": "42", "action": "approve"} {
		if calls[0][key] != want {
			t.Errorf("the approval sent %s = %v, want %v (all: %v)", key, calls[0][key], want, calls[0])
		}
	}

	clickButton(t, ctx, 0, "Approved")
	waitInFrame(t, ctx, 0, `d.querySelector(".review-actions .form-error") && d.querySelector(".review-actions .form-error").textContent.includes("declined")`, "a review that failed does not say why")
	if calls := toolCalls(t, ctx, 0, "submit_pr_review"); len(calls) != 2 || calls[1]["action"] != "unapprove" {
		t.Errorf("pressing Approved again sent %v, want unapprove", calls)
	}
}

// A review is offered only to someone who can give one: not the author, not
// on a closed pull request, and not where the server keeps submit_pr_review
// from views.
func TestAReviewIsOfferedOnlyToSomeoneWhoCanGiveOne(t *testing.T) {
	withoutReview := reviewedCard(&viewMe{Username: "dave"}, "OPEN", "one")
	withoutReview.Offers = commenting
	ctx := browser(t, []viewhost.Frame{
		{Title: "the author", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, reviewedCard(&viewMe{Username: "alice", Author: true}, "OPEN", "one")),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
		{Title: "a merged pull request", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, reviewedCard(&viewMe{Username: "dave"}, "MERGED", "one")),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
		{Title: "a server that keeps submit_pr_review from views", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, withoutReview),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
		{Title: "a reviewer who can", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, reviewedCard(&viewMe{Username: "dave"}, "OPEN", "one")),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
	})
	for frame := range 3 {
		var offered bool
		inFrame(t, ctx, frame, `return Boolean(d.querySelector(".review-actions"));`, &offered)
		if offered {
			t.Errorf("frame %d offers a review to someone who cannot give one", frame)
		}
	}
	var offered bool
	inFrame(t, ctx, 3, `return Boolean(d.querySelector(".review-actions")) && d.querySelector(".review-actions").innerText.includes("Request changes");`, &offered)
	if !offered {
		t.Errorf("a reviewer who can review is offered nothing: %s", strings.TrimSpace(frameText(t, ctx, 3)))
	}
}
