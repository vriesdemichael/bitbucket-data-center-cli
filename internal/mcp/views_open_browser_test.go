//go:build views

package mcp

import (
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
)

// everyKind is what a server that shows every kind offers its views.
var everyKind = &viewOffers{Kinds: []string{showKindPullRequest, showKindPullRequests, showKindDiff}}

// A card opens its diff in the same view: the view asks bb for it, shows it,
// tells the model, and goes back to the card when the person asks.
func TestACardOpensItsDiffAndGoesBack(t *testing.T) {
	card := refreshCardPayload("Round refunds", "OPEN", time.Now(), "one")
	card.Offers = everyKind
	diff := refreshDiffPayload(strings.Join([]string{
		"diff --git src://ledger.go dst://ledger.go",
		"--- src://ledger.go",
		"+++ dst://ledger.go",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"",
	}, "\n"), time.Now(), "diff-one")
	diff.Offers = everyKind
	ctx := browser(t, []viewhost.Frame{
		{Title: "card", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, card),
			ToolResults: answers(refreshAnswer(t, &diff, "The person opened the diff of PAY/ledger#42 in a view: 1 files, +1 -1."))},
	})

	clickButton(t, ctx, 0, "Diff")
	waitInFrame(t, ctx, 0, `d.body.innerText.includes("ledger.go") && d.querySelector(".back-button")`, "the diff did not open in the view")
	calls := refreshCalls(t, ctx, 0)
	if len(calls) != 1 {
		t.Fatalf("the view asked %d times, want once: %v", len(calls), calls)
	}
	for key, want := range map[string]any{"kind": "diff", "project": "PAY", "repo": "ledger", "id": "42"} {
		if calls[0][key] != want {
			t.Errorf("opening the diff sent %s = %v, want %v (all: %v)", key, calls[0][key], want, calls[0])
		}
	}
	if since, ok := calls[0]["since"]; ok {
		t.Errorf("opening the diff sent since = %v; a view opening something holds nothing to compare", since)
	}
	if told := hostMessages(t, ctx, 0, "ui/update-model-context"); len(told) != 1 || !strings.Contains(messageText(told[0]), "opened the diff") {
		t.Errorf("the model was told %v, want what the person opened", told)
	}

	clickButton(t, ctx, 0, "Back to the pull request")
	waitInFrame(t, ctx, 0, `d.querySelector(".pr-card") && !d.querySelector(".back-button")`, "the card did not come back")
	if calls := refreshCalls(t, ctx, 0); len(calls) != 1 {
		t.Errorf("going back asked bb again: %v", calls)
	}
}

// A list opens a pull request's card in the same view, and asks Bitbucket
// again for another state, in place of the list it was.
func TestAListOpensACardAndAsksAgain(t *testing.T) {
	list := viewPayload{
		Kind: showKindPullRequests, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		PullRequests: []viewPullRequest{{PullRequest: fixturePullRequest()}},
		Show:         &ShowInput{Kind: showKindPullRequests}, Fingerprint: "list-one", Offers: everyKind,
	}
	list.PullRequests[0].Title = "Round refunds"
	card := refreshCardPayload("Round refunds", "OPEN", time.Now(), "card-one")
	card.Offers = everyKind
	closed := list
	closed.Show = &ShowInput{Kind: showKindPullRequests, State: "closed"}
	closed.PullRequests = []viewPullRequest{{PullRequest: fixturePullRequest()}}
	closed.PullRequests[0].Title, closed.PullRequests[0].State = "Bump golang.org/x/net", "MERGED"
	ctx := browser(t, []viewhost.Frame{
		{Title: "list in fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, list),
			ToolResults: answers(refreshAnswer(t, &card, "opened the card"), refreshAnswer(t, &closed, "opened the closed list"))},
	})

	var clicked bool
	inFrame(t, ctx, 0, `const b = d.querySelector(".pr-list .row-button"); if (b) b.click(); return Boolean(b);`, &clicked)
	if !clicked {
		t.Fatal("the list has no row to open")
	}
	waitInFrame(t, ctx, 0, `d.body.innerText.includes("Back to the list")`, "the row did not open its card")
	calls := refreshCalls(t, ctx, 0)
	if len(calls) != 1 || calls[0]["kind"] != "pull_request" || calls[0]["project"] != "PAY" || calls[0]["repo"] != "ledger" || calls[0]["id"] != "42" {
		t.Fatalf("opening the row sent %v, want the pull request PAY/ledger#42", calls)
	}

	clickButton(t, ctx, 0, "Back to the list")
	waitInFrame(t, ctx, 0, `d.querySelector(".list-scope select")`, "the list did not come back with its state to choose")
	inFrame(t, ctx, 0, `const s = d.querySelector(".list-scope select"); s.value = "closed"; s.dispatchEvent(new Event("change")); return true;`, &clicked)
	waitInFrame(t, ctx, 0, `d.body.innerText.includes("Bump golang.org/x/net")`, "the list in another state did not come")
	calls = refreshCalls(t, ctx, 0)
	if len(calls) != 2 || calls[1]["kind"] != "pull_requests" || calls[1]["state"] != "closed" {
		t.Fatalf("asking for another state sent %v, want the list in state closed", calls)
	}
	var back bool
	inFrame(t, ctx, 0, `return Boolean(d.querySelector(".back-button"));`, &back)
	if back {
		t.Error("a list asked again in another state offers to go back to the one it replaced")
	}
}

// A view offers to open only what works where it is: nothing the server does
// not show, and nothing at all in a host that passes no tool calls. A row
// then opens in Bitbucket.
func TestAViewOffersToOpenOnlyWhatWorksHere(t *testing.T) {
	withoutOffers := refreshCardPayload("Round refunds", "OPEN", time.Now(), "one")
	offered := refreshCardPayload("Round refunds", "OPEN", time.Now(), "one")
	offered.Offers = everyKind
	list := viewPayload{
		Kind: showKindPullRequests, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		PullRequests: []viewPullRequest{{PullRequest: fixturePullRequest(), URL: "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview"}},
		Show:         &ShowInput{Kind: showKindPullRequests}, Fingerprint: "list-one", Offers: everyKind,
	}
	ctx := browser(t, []viewhost.Frame{
		{Title: "a server that shows no other kind", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, withoutOffers),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
		{Title: "a host that passes no tool calls", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, offered)},
		{Title: "a list in a host that passes no tool calls", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, list)},
	})

	for frame := range 2 {
		var tabs bool
		inFrame(t, ctx, frame, `return [...d.querySelectorAll("button")].some((b) => b.textContent.trim() === "Diff");`, &tabs)
		if tabs {
			t.Errorf("frame %d offers to open the diff where it cannot", frame)
		}
		var card bool
		inFrame(t, ctx, frame, `return Boolean(d.querySelector(".pr-card"));`, &card)
		if !card {
			t.Fatalf("frame %d drew no card, so the check above proves nothing", frame)
		}
	}

	var clicked bool
	inFrame(t, ctx, 2, `const b = d.querySelector(".pr-list .row-button"); if (b) b.click(); return Boolean(b);`, &clicked)
	if !clicked {
		t.Fatal("the list has no row")
	}
	if err := chromedp.Do(ctx, chromedp.Sleep(300*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if opened := hostMessages(t, ctx, 2, "ui/open-link"); len(opened) != 1 || !strings.Contains(messageText(opened[0]), "/pull-requests/42/") {
		t.Errorf("in a host without tool calls a row opened %v, want the pull request in Bitbucket", opened)
	}
	if calls := refreshCalls(t, ctx, 2); len(calls) != 0 {
		t.Errorf("a host without tool calls was asked: %v", calls)
	}
}
