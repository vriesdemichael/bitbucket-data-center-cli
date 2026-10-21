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
var reviewing = &viewOffers{Kinds: []string{showKindPullRequest, showKindDiff}, Tools: []string{"add_pr_comment", "prepare_pr_review"}}

func reviewedCard(me *viewMe, state, fingerprint string) viewPayload {
	card := refreshCardPayload("Round refunds", state, time.Now(), fingerprint)
	card.Offers = reviewing
	card.Me = me
	return card
}

// A reviewer's Approve prepares the review through prepare_pr_review and shows
// where the draft waits, with what it holds and what to choose there, and a
// link to it; nothing is approved from the view (#796). A review that cannot
// be prepared says why.
func TestAReviewerApprovesFromTheOverview(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "card fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, reviewedCard(&viewMe{Username: "dave", Status: "UNAPPROVED"}, "OPEN", "one")),
			ToolResults: map[string][]json.RawMessage{
				"prepare_pr_review": {
					structuredAnswer(t, "Prepared.", map[string]any{"review": map[string]any{
						"url": "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42", "pending_comments": 2, "suggested_status": "approve",
					}}),
					toolAnswer(t, "Bitbucket did not answer.", true),
				},
			}},
	})

	clickButton(t, ctx, 0, "Approve")
	waitInFrame(t, ctx, 0, `d.querySelector(".review-prepared") && d.querySelector(".review-prepared").textContent.includes("2 comments") && d.querySelector(".review-prepared").textContent.includes("Finish review, and choose Approve")`, "the prepared review does not say where it waits and what it holds")
	calls := toolCalls(t, ctx, 0, "prepare_pr_review")
	if len(calls) != 1 {
		t.Fatalf("the view called prepare_pr_review %d times, want once: %v", len(calls), calls)
	}
	for key, want := range map[string]any{"project": "PAY", "repo": "ledger", "pr_id": float64(42), "suggested_status": "approve"} {
		if calls[0][key] != want {
			t.Errorf("the review sent %s = %v, want %v (all: %v)", key, calls[0][key], want, calls[0])
		}
	}
	var linked bool
	inFrame(t, ctx, 0, `return [...d.querySelectorAll(".review-prepared button")].some((b) => b.textContent.includes("Open in Bitbucket"));`, &linked)
	if !linked {
		t.Error("the prepared review has no link to where it waits")
	}

	clickButton(t, ctx, 0, "Request changes")
	waitInFrame(t, ctx, 0, `d.querySelector(".review-actions .form-error") && d.querySelector(".review-actions .form-error").textContent.includes("did not answer")`, "a review that could not be prepared does not say why")
	if calls := toolCalls(t, ctx, 0, "prepare_pr_review"); len(calls) != 2 || calls[1]["suggested_status"] != "needs_work" {
		t.Errorf("Request changes sent %v, want needs_work suggested", calls)
	}
}

// A review is offered only to someone who can give one: not the author, not
// on a closed pull request, and not where the server keeps prepare_pr_review
// from views.
func TestAReviewIsOfferedOnlyToSomeoneWhoCanGiveOne(t *testing.T) {
	withoutReview := reviewedCard(&viewMe{Username: "dave"}, "OPEN", "one")
	withoutReview.Offers = commenting
	ctx := browser(t, []viewhost.Frame{
		{Title: "the author", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, reviewedCard(&viewMe{Username: "alice", Author: true}, "OPEN", "one")),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
		{Title: "a merged pull request", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, reviewedCard(&viewMe{Username: "dave"}, "MERGED", "one")),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
		{Title: "a server that keeps prepare_pr_review from views", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, withoutReview),
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
