//go:build views

package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
)

func checkedCard(required []viewRequiredCheck, known bool) viewPayload {
	card := refreshCardPayload("Round refunds", "OPEN", time.Now(), "one")
	card.PullRequest.RequiredChecks = required
	card.PullRequest.RequiredKnown = known
	return card
}

// The card flags required builds that have not reported, and its overview
// lists every required build where it stands, missing first, saying why a
// build that reported under a key without naming it as its parent does not
// count. Where bb could not tell, the card says nothing about requirements.
func TestTheCardShowsTheBuildsItMustPass(t *testing.T) {
	required := []viewRequiredCheck{
		{Key: "integration"},
		{Key: "lint", Unparented: true},
		{Key: "unit", Name: "Unit tests", State: "SUCCESSFUL", URL: "https://ci.example.com/unit"},
	}
	ctx := browser(t, []viewhost.Frame{
		{Title: "card", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, checkedCard(required, true))},
		{Title: "overview", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, checkedCard(required, true))},
		{Title: "bb could not tell", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, checkedCard(required, false))},
	})

	if card := frameText(t, ctx, 0); !strings.Contains(card, "2 required builds missing") {
		t.Errorf("the card reads %q, want the required builds still missing flagged", card)
	}

	var board string
	inFrame(t, ctx, 1, `const s = [...d.querySelectorAll(".side-section")].find((s) => (s.querySelector(".section-title") || {}).textContent === "Required builds"); return s ? s.innerText : "";`, &board)
	for _, want := range []string{"integration", "Not reported on this commit.", "lint", "without naming it as its parent", "unit", "Unit tests"} {
		if !strings.Contains(board, want) {
			t.Errorf("the checks board reads %q, want %q", board, want)
		}
	}
	if strings.Index(board, "integration") > strings.Index(board, "unit") {
		t.Errorf("the checks board reads %q, want what is missing before what passed", board)
	}

	var unknown string
	inFrame(t, ctx, 2, `return d.body.innerText;`, &unknown)
	if strings.Contains(strings.ToLower(unknown), "required build") {
		t.Errorf("where bb could not tell, the card says %q about requirements, want nothing", unknown)
	}
	if !strings.Contains(unknown, "Round refunds") {
		t.Fatalf("the third frame drew no card, so the check above proves nothing: %q", unknown)
	}
}
