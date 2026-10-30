//go:build views

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// What a view does where a host does less than the views can use. Each host here
// is a stand-in for what a real one was measured to do: declaring no serverTools,
// listing fullscreen and answering inline, taking a context and leaving the model
// to read it, keeping a page for a whole session.

// fallbackPatch is a one-file diff, to select lines in.
var fallbackPatch = strings.Join([]string{
	"diff --git src://internal/ledger/ledger.go dst://internal/ledger/ledger.go",
	"--- src://internal/ledger/ledger.go",
	"+++ dst://internal/ledger/ledger.go",
	"@@ -8,2 +8,2 @@ func Refund(e Entry) Entry {",
	" // Refund returns the entry that reverses e.",
	"-\treturn Entry{}",
	"+\treturn Entry{Amount: -e.Amount}",
	"",
}, "\n")

func fallbackDiff(offers *viewOffers) viewPayload {
	payload := refreshDiffPayload(fallbackPatch, time.Now(), "diff-one")
	payload.Offers = offers
	return payload
}

// selectLine selects the first changed line of a diff, as a click does.
func selectLine(t *testing.T, ctx context.Context, frame int) {
	t.Helper()
	var clicked bool
	inFrame(t, ctx, frame, `const n = [...d.querySelectorAll("tr.del .line-number, tr.add .line-number")].find((b) => b.textContent !== ""); if (n) n.click(); return Boolean(n);`, &clicked)
	if !clicked {
		t.Fatalf("frame %d has no line to select", frame)
	}
}

func buttonsOf(t *testing.T, ctx context.Context, frame int) string {
	t.Helper()
	var buttons string
	inFrame(t, ctx, frame, `return [...d.querySelectorAll("button")].map((b) => b.textContent.trim()).join("|");`, &buttons)
	return buttons
}

// letItLand gives a message the view sent time to reach the stand-in host.
func letItLand(t *testing.T, ctx context.Context) {
	t.Helper()
	if err := chromedp.Do(ctx, chromedp.Sleep(200*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
}

// A page a client kept can be older than the data, and asking again reaches the
// same page, so the words hold in any client.
func TestAViewOfAnotherVersionSaysWhatWorksInAnyClient(t *testing.T) {
	result := func(version int, kind string) json.RawMessage {
		encoded, err := json.Marshal(&mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "Showed the person something."}},
			Meta:    mcp.Meta{viewPayloadKey: map[string]any{"version": version, "kind": kind}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return encoded
	}
	ctx := browser(t, []viewhost.Frame{
		{Title: "a payload from another version", Mode: "inline", Result: result(viewPayloadVersion+1, showKindPullRequest)},
		{Title: "a kind this page does not know", Mode: "inline", Result: result(viewPayloadVersion, "a_kind_from_later")},
	})
	for frame := range 2 {
		text := frameText(t, ctx, frame)
		for _, want := range []string{"different version of bb", "Ask for it again", "if this message comes back, start a new conversation"} {
			if !strings.Contains(text, want) {
				t.Errorf("frame %d reads %q, want %q", frame, text, want)
			}
		}
	}
}

// Add to chat context promises only that the host took the text, and each button
// shows only where the host declares what it needs.
func TestSelectedLinesOfferOnlyWhatTheHostDeclares(t *testing.T) {
	frame := func(title string, without ...string) viewhost.Frame {
		return viewhost.Frame{Title: title, Mode: "inline", Result: fixtureResult(t, fallbackDiff(nil)), Without: without}
	}
	ctx := browser(t, []viewhost.Frame{
		frame("every capability"),
		frame("no context", "updateModelContext"),
		frame("no message", "message"),
		frame("neither", "updateModelContext", "message"),
	})
	for index, want := range []struct{ context, ask bool }{{true, true}, {false, true}, {true, false}, {false, false}} {
		inFrame(t, ctx, index, `d.querySelector(".file-link").click(); return null;`, nil)
		waitInFrame(t, ctx, index, `d.querySelector(".diff-table")`, "the file did not open in place")
		selectLine(t, ctx, index)
		var bar string
		inFrame(t, ctx, index, `return d.getElementById("selection-bar").textContent;`, &bar)
		if !strings.Contains(bar, "1 line") {
			t.Fatalf("frame %d: no selection to act on, so the check below proves nothing: %q", index, bar)
		}
		if got := strings.Contains(bar, "Add to chat context"); got != want.context {
			t.Errorf("frame %d: Add to chat context shows = %v, want %v: %q", index, got, want.context, bar)
		}
		if got := strings.Contains(bar, "Ask about this"); got != want.ask {
			t.Errorf("frame %d: Ask about this shows = %v, want %v: %q", index, got, want.ask, bar)
		}
	}

	clickButton(t, ctx, 0, "Add to chat context")
	waitInFrame(t, ctx, 0, `d.getElementById("selection-bar").textContent.includes("Added to this view's context")`, "the host's answer was not said")
	var said string
	inFrame(t, ctx, 0, `return d.getElementById("selection-bar").textContent;`, &said)
	if !strings.Contains(said, "Name these lines when you ask about them.") || strings.Contains(said, "the model reads") {
		t.Errorf("after Add to chat context the bar reads %q, want it to promise only what is known", said)
	}
	if sent := hostMessages(t, ctx, 0, "ui/update-model-context"); len(sent) != 1 || !strings.Contains(messageText(sent[0]), "Entry{}") {
		t.Errorf("the host was handed %v, want the selected line", sent)
	}
	clickButton(t, ctx, 0, "Ask about this")
	waitInFrame(t, ctx, 0, `d.getElementById("selection-bar").textContent.includes("Sent.")`, "the message was not said to be sent")
	if sent := hostMessages(t, ctx, 0, "ui/message"); len(sent) != 1 || !strings.Contains(messageText(sent[0]), "Entry{}") {
		t.Errorf("the host was sent %v, want the selected line in the message", sent)
	}
}

// A host may list fullscreen and answer inline. The button is named for what it
// may do, a refusal opens what was asked for in place and stands, and the host's
// own fullscreen is the host's to close.
func TestAHostThatListsFullscreenAndRefusesIt(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff", Mode: "inline", RefusesFullscreen: true, Result: fixtureResult(t, fallbackDiff(nil))},
		{Title: "card", Mode: "inline", RefusesFullscreen: true, Result: fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", time.Now(), "one"))},
	})

	before := buttonsOf(t, ctx, 0)
	if !strings.Contains(before, "Expand") || strings.Contains(strings.ToLower(before), "full screen") {
		t.Errorf("before the host granted fullscreen the buttons read %q, want Expand and no promise of full screen", before)
	}

	clickButton(t, ctx, 0, "Expand")
	waitInFrame(t, ctx, 0, `d.querySelector(".inline-file .diff-table")`, "a refused Expand did not open the file in place")
	after := buttonsOf(t, ctx, 0)
	if strings.Contains(after, "Expand") || strings.Contains(after, "Exit full screen") {
		t.Errorf("after the refusal the buttons read %q, want neither Expand nor Exit", after)
	}
	if requested := hostMessages(t, ctx, 0, "ui/request-display-mode"); len(requested) != 1 {
		t.Errorf("the view asked %d times, want once", len(requested))
	}

	// A later change that lists fullscreen again does not bring the offer back.
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`window.bbHost.frames[0].setMode("inline", { availableDisplayModes: ["inline", "fullscreen"] })`)); err != nil {
		t.Fatal(err)
	}
	letItLand(t, ctx)
	if buttons := buttonsOf(t, ctx, 0); strings.Contains(buttons, "Expand") {
		t.Errorf("a refusal did not stand: after the host listed fullscreen again the buttons read %q", buttons)
	}

	// The host's own fullscreen draws the page, with no way out of the view's.
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`window.bbHost.frames[0].setMode("fullscreen")`)); err != nil {
		t.Fatal(err)
	}
	waitInFrame(t, ctx, 0, `d.querySelector(".fullscreen-header")`, "the host's own fullscreen did not draw the page")
	if buttons := buttonsOf(t, ctx, 0); strings.Contains(buttons, "Exit full screen") {
		t.Errorf("in the host's own fullscreen the view offers to exit: %q", buttons)
	}
	if requested := hostMessages(t, ctx, 0, "ui/request-display-mode"); len(requested) != 1 {
		t.Errorf("the host's own fullscreen made the view ask again: %d requests", len(requested))
	}
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](`window.bbHost.frames[0].setMode("inline")`)); err != nil {
		t.Fatal(err)
	}
	waitInFrame(t, ctx, 0, `!d.querySelector(".fullscreen-header")`, "closing the host's own fullscreen did not draw the view inline")

	// A card opens its overview in place, as it did.
	clickButton(t, ctx, 1, "Overview")
	waitInFrame(t, ctx, 1, `d.querySelector(".inline-details")`, "a refused Overview did not open in place")
}

// A view asks for fullscreen and gets it: from then on the button says so, and
// the view can leave what it asked for. A view the host opened in fullscreen
// offers no way out of it.
func TestFullscreenTheViewAskedForIsTheViewsToLeave(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "granted", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, fallbackDiff(nil))},
		{Title: "the host's", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, fallbackDiff(nil))},
		{Title: "a list, the host's", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, viewPayload{
			Kind: showKindPullRequests, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			PullRequests: []viewPullRequest{{PullRequest: fixturePullRequest(), URL: threadsURL}},
		})},
	})

	clickButton(t, ctx, 0, "Expand")
	waitInFrame(t, ctx, 0, `d.querySelector(".fullscreen-header")`, "a granted Expand did not go fullscreen")
	clickButton(t, ctx, 0, "Exit full screen")
	waitInFrame(t, ctx, 0, `!d.querySelector(".fullscreen-header")`, "Exit full screen did not go back")
	if buttons := buttonsOf(t, ctx, 0); !strings.Contains(buttons, "Full screen") || strings.Contains(buttons, "Expand") {
		t.Errorf("once the host granted fullscreen the buttons read %q, want Full screen", buttons)
	}

	for frame := 1; frame <= 2; frame++ {
		if buttons := buttonsOf(t, ctx, frame); strings.Contains(buttons, "Exit full screen") {
			t.Errorf("frame %d, in the host's fullscreen, offers to exit it: %q", frame, buttons)
		}
	}
}

// An inline view shows no scrollbar while the host catches up with it, since a
// scrollbar narrows it and a narrower view is taller. A host that caps the view
// gets it back, or a person could not see there is more.
func TestAScrollbarShowsOnlyWhereTheHostCapsTheView(t *testing.T) {
	card := fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", time.Now(), "one"))
	ctx := browser(t, []viewhost.Frame{
		{Title: "follows the view", Mode: "inline", Result: card},
		{Title: "caps it", Mode: "inline", CapsHeight: 60, Result: card},
		{Title: "fullscreen", Mode: "fullscreen", Fullscreen: true, Result: card},
	})
	if err := chromedp.Do(ctx, chromedp.Sleep(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	for frame, want := range []string{"none", "auto", "auto"} {
		var got string
		inFrame(t, ctx, frame, `return w.getComputedStyle(d.documentElement).scrollbarWidth;`, &got)
		if got != want {
			t.Errorf("frame %d has scrollbar-width %q, want %q", frame, got, want)
		}
	}
}

// inMotionCard is a card with a build running, or auto-merge on.
func inMotionCard(running int, autoMerge bool, offers *viewOffers) viewPayload {
	card := refreshCardPayload("Round refunds", "OPEN", time.Now(), "one")
	card.PullRequest.CheckCounts = &viewCheckCounts{Successful: 3, InProgress: running}
	if autoMerge {
		card.PullRequest.AutoMerge = &pullrequestservice.AutoMerge{Enabled: true}
	}
	card.Offers = offers
	return card
}

// A view that cannot read itself again always says when it was read and that it
// does not update, and one with something in motion says what may have moved on,
// beside the way to look. Where the host passes tool calls, nothing changes.
func TestAViewThatCannotRefreshSaysSo(t *testing.T) {
	unchanged := answers(refreshAnswer(t, nil, "unchanged"))
	list := viewPayload{
		Kind: showKindPullRequests, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		PullRequests: []viewPullRequest{{PullRequest: fixturePullRequest(), URL: threadsURL, CheckCounts: &viewCheckCounts{InProgress: 1}}},
		Show:         &ShowInput{Kind: showKindPullRequests}, Fingerprint: "list",
	}
	ctx := browser(t, []viewhost.Frame{
		{Title: "still, no calls", Mode: "inline", Result: fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", time.Now(), "one"))},
		{Title: "a running build, no calls", Mode: "inline", Result: fixtureResult(t, inMotionCard(1, false, nil))},
		{Title: "two running builds, no calls", Mode: "inline", Result: fixtureResult(t, inMotionCard(2, false, nil))},
		{Title: "auto-merge, no calls", Mode: "inline", Result: fixtureResult(t, inMotionCard(0, true, nil))},
		{Title: "a list with a running build, no calls", Mode: "inline", Result: fixtureResult(t, list)},
		{Title: "diff, no calls", Mode: "inline", Result: fixtureResult(t, fallbackDiff(nil))},
		{Title: "a running build, on a phone, no calls", Mode: "inline", Width: 360, Result: fixtureResult(t, inMotionCard(1, false, nil))},
		{Title: "a running build, with calls", Mode: "inline", Result: fixtureResult(t, inMotionCard(1, false, nil)), ToolResults: unchanged},
		{Title: "a list with a running build, with calls", Mode: "inline", Result: fixtureResult(t, list), ToolResults: unchanged},
		{Title: "diff, with calls", Mode: "inline", Result: fixtureResult(t, fallbackDiff(nil)), ToolResults: unchanged},
	})

	const stays = "does not update in this client"
	for frame, wants := range map[int][]string{
		0: {"As of", stays},
		1: {"As of", stays, "The running build may have finished.", "Open it in Bitbucket to look, or ask for it again in the conversation."},
		2: {"As of", stays, "The running builds may have finished."},
		3: {"As of", stays, "It may have merged by itself."},
		4: {"As of", stays, "Builds that were running may have finished.", "ask for the list again in the conversation"},
		5: {"As of", stays},
		6: {"As of", stays, "The running build may have finished."},
	} {
		text := frameText(t, ctx, frame)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("frame %d does not say %q:\n%s", frame, want, text)
			}
		}
		if frame == 0 || frame == 5 {
			if strings.Contains(text, "may have") {
				t.Errorf("frame %d has nothing in motion and says it may have moved on:\n%s", frame, text)
			}
		}
		var button bool
		inFrame(t, ctx, frame, `return Boolean(d.querySelector(".refresh-button"));`, &button)
		if button {
			t.Errorf("frame %d offers to refresh where it cannot", frame)
		}
	}

	// On a phone none of it is cut off.
	var cut bool
	inFrame(t, ctx, 6, `const s = d.querySelector(".stamp"); return s.scrollWidth > s.clientWidth + 1;`, &cut)
	if cut {
		t.Error("on a phone the stamp of a view that does not update is cut off")
	}

	for frame := 7; frame <= 9; frame++ {
		text := frameText(t, ctx, frame)
		if strings.Contains(text, stays) || strings.Contains(text, "may have") {
			t.Errorf("frame %d, whose host passes tool calls, reads as if it could not refresh:\n%s", frame, text)
		}
		if !strings.Contains(text, "Pull requests") && !strings.Contains(text, "Round refunds") {
			t.Fatalf("frame %d drew nothing, so the check above proves nothing:\n%s", frame, text)
		}
	}
}

// reviewOffers is what a server with every action offers its views.
var reviewOffers = &viewOffers{
	Kinds: []string{showKindPullRequest, showKindPullRequests, showKindDiff},
	Tools: []string{"add_pr_comment", "prepare_pr_review"},
}

// readOnlyOffers is what a read-only server offers: it writes nothing, and
// prepare_pr_review only reads.
var readOnlyOffers = &viewOffers{
	Kinds: []string{showKindPullRequest, showKindPullRequests, showKindDiff},
	Tools: []string{"prepare_pr_review", "suggest_form_values"},
}

// openedBy clicks a button in a frame and returns the one link the view asked the
// host to open for it.
func openedBy(t *testing.T, ctx context.Context, frame int, script string) string {
	t.Helper()
	known := len(hostMessages(t, ctx, frame, "ui/open-link"))
	var clicked bool
	inFrame(t, ctx, frame, script, &clicked)
	if !clicked {
		t.Fatalf("frame %d: nothing to click: %s", frame, script)
	}
	letItLand(t, ctx)
	opened := hostMessages(t, ctx, frame, "ui/open-link")
	if len(opened) != known+1 {
		t.Fatalf("frame %d asked the host to open %d links, want one more than %d: %s", frame, len(opened), known, script)
	}
	return messageText(opened[len(opened)-1])
}

func clickNamed(name string) string {
	return fmt.Sprintf(`const b = [...d.querySelectorAll("button")].find((b) => b.textContent.trim() === %q); if (b) b.click(); return Boolean(b);`, name)
}

// As a list's row does, what a view cannot do here leads to the same place in
// Bitbucket, one line says why, and calling the tool is never tried. What the
// server does not offer stays hidden, whatever the host.
func TestWithoutToolCallsWhatTheServerOffersOpensBitbucket(t *testing.T) {
	overview := overviewPayload(fixtureActivity(), reviewOffers, "one")
	overview.Me = &viewMe{Username: "dave", Status: "UNAPPROVED"}
	readOnly := overviewPayload(fixtureActivity(), readOnlyOffers, "one")
	readOnly.Me = &viewMe{Username: "dave", Status: "UNAPPROVED"}
	list := func(show *ShowInput) viewPayload {
		return viewPayload{
			Kind: showKindPullRequests, GeneratedAt: time.Now().UTC().Format(time.RFC3339),
			PullRequests: []viewPullRequest{{PullRequest: fixturePullRequest(), URL: threadsURL}},
			Show:         show, Fingerprint: "list", Offers: reviewOffers,
		}
	}
	const (
		overviewFrame = iota
		cardFrame
		readOnlyFrame
		diffFrame
		listFrame
		repoListFrame
		roleListFrame
		noOffersFrame
		withCallsFrame
	)
	ctx := browser(t, []viewhost.Frame{
		overviewFrame: {Title: "overview", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, overview)},
		cardFrame:     {Title: "card", Mode: "inline", Result: fixtureResult(t, overview)},
		readOnlyFrame: {Title: "read-only overview", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, readOnly)},
		diffFrame:     {Title: "diff", Mode: "inline", Result: fixtureResult(t, fallbackDiff(reviewOffers))},
		listFrame:     {Title: "list", Mode: "inline", Result: fixtureResult(t, list(&ShowInput{Kind: showKindPullRequests}))},
		repoListFrame: {Title: "list of a repository", Mode: "inline", Result: fixtureResult(t, list(&ShowInput{Kind: showKindPullRequests, Project: "PAY", Repo: "ledger", State: "closed"}))},
		roleListFrame: {Title: "list, by role", Mode: "inline", Result: fixtureResult(t, list(&ShowInput{Kind: showKindPullRequests, Role: "REVIEWER"}))},
		noOffersFrame: {Title: "a payload that offers nothing", Mode: "inline", Result: fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", time.Now(), "one"))},
		withCallsFrame: {Title: "with tool calls", Mode: "inline", Result: fixtureResult(t, overview),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
	})
	const overviewURL = threadsURL
	diffURL := strings.TrimSuffix(threadsURL, "/overview") + "/diff"

	for name, got := range map[string]string{
		"Approve":         openedBy(t, ctx, overviewFrame, clickNamed("Approve")),
		"Request changes": openedBy(t, ctx, overviewFrame, clickNamed("Request changes")),
		"Add a comment":   openedBy(t, ctx, overviewFrame, clickNamed("Add a comment")),
	} {
		if got != overviewURL {
			t.Errorf("%s opened %q, want the pull request in Bitbucket", name, got)
		}
	}
	if got := openedBy(t, ctx, overviewFrame, `const b = [...d.querySelectorAll(".comment-actions .text-action")].find((b) => b.textContent.trim() === "Reply"); if (b) b.click(); return Boolean(b);`); !strings.HasPrefix(got, "https://") {
		t.Errorf("Reply opened %q, want its thread in Bitbucket", got)
	}
	if got := openedBy(t, ctx, overviewFrame, `const b = d.querySelector("button.file-crumb"); if (b) b.click(); return Boolean(b);`); !strings.HasPrefix(got, diffURL+"#") {
		t.Errorf("a comment's file opened %q, want that file's diff in Bitbucket", got)
	}
	if calls := hostMessages(t, ctx, overviewFrame, "tools/call"); len(calls) != 0 {
		t.Errorf("the view called tools the host does not pass: %v", calls)
	}

	if got := openedBy(t, ctx, cardFrame, clickNamed("Diff")); got != diffURL {
		t.Errorf("the card's Diff opened %q, want %s", got, diffURL)
	}
	if got := openedBy(t, ctx, diffFrame, clickNamed("Overview")); got != overviewURL {
		t.Errorf("the diff's Overview opened %q, want %s", got, overviewURL)
	}

	// A comment on selected lines opens the file there too.
	inFrame(t, ctx, diffFrame, `d.querySelector(".file-link").click(); return null;`, nil)
	waitInFrame(t, ctx, diffFrame, `d.querySelector(".diff-table")`, "the file did not open in place")
	selectLine(t, ctx, diffFrame)
	if got := openedBy(t, ctx, diffFrame, clickNamed("Comment")); !strings.HasPrefix(got, diffURL+"#internal/ledger/ledger.go") {
		t.Errorf("Comment on selected lines opened %q, want that file's diff in Bitbucket", got)
	}

	// One line says why, once per view.
	const why = "This client does not let a view act, so these open Bitbucket."
	for _, frame := range []int{overviewFrame, cardFrame, readOnlyFrame, diffFrame, listFrame} {
		if got := strings.Count(frameText(t, ctx, frame), why); got != 1 {
			t.Errorf("frame %d says why %d times, want once:\n%s", frame, got, frameText(t, ctx, frame))
		}
	}

	// A list says what it shows, and that the conversation can ask for others.
	for frame, wants := range map[int][]string{
		listFrame:     {"This list shows open pull requests in any role.", "Ask in the conversation for another state or role."},
		repoListFrame: {"This list shows merged and declined pull requests.", "Ask in the conversation for another state."},
		roleListFrame: {"This list shows open pull requests you are reviewing."},
	} {
		text := frameText(t, ctx, frame)
		for _, want := range wants {
			if !strings.Contains(text, want) {
				t.Errorf("frame %d does not say %q:\n%s", frame, want, text)
			}
		}
	}

	// A read-only server keeps hiding what it does not offer.
	buttons := buttonsOf(t, ctx, readOnlyFrame)
	if strings.Contains(buttons, "Add a comment") || strings.Contains(buttons, "Reply") {
		t.Errorf("a read-only server's view offers to write: %q", buttons)
	}
	if !strings.Contains(buttons, "Approve") {
		t.Errorf("a read-only server offers prepare_pr_review, so Approve should lead to Bitbucket: %q", buttons)
	}
	if !strings.Contains(frameText(t, ctx, readOnlyFrame), "Cap the time a charge spends retrying.") {
		t.Fatal("the read-only frame drew no activity, so the check above proves nothing")
	}

	// A payload that offers nothing, as a client's stored one may be, is as it was.
	if text := frameText(t, ctx, noOffersFrame); strings.Contains(text, why) || strings.Contains(buttonsOf(t, ctx, noOffersFrame), "Diff") {
		t.Errorf("a payload that offers nothing reads:\n%s", text)
	}

	// Nothing changes where the host passes tool calls.
	if text := frameText(t, ctx, withCallsFrame); strings.Contains(text, why) {
		t.Errorf("a host that passes tool calls is told it does not:\n%s", text)
	}
}

// The person is told before they write, and what they wrote is not lost: it goes
// by the surest way the host has, falling through to text to select, and
// Bitbucket's create page opens for the same branches.
func TestAFormThatCannotSubmitHandsOverWhatThePersonWrote(t *testing.T) {
	form := fixtureResult(t, formPayload(newForm(), creating))
	frame := func(title string, change func(*viewhost.Frame)) viewhost.Frame {
		built := viewhost.Frame{Title: title, Mode: "inline", Result: form}
		change(&built)
		return built
	}
	const (
		messageFrame = iota
		contextFrame
		textFrame
		messageRefused
		allRefused
	)
	ctx := browser(t, []viewhost.Frame{
		messageFrame:   frame("message", func(*viewhost.Frame) {}),
		contextFrame:   frame("only context", func(f *viewhost.Frame) { f.Without = []string{"message"} }),
		textFrame:      frame("neither", func(f *viewhost.Frame) { f.Without = []string{"message", "updateModelContext"} }),
		messageRefused: frame("declares message, refuses it", func(f *viewhost.Frame) { f.Refuses = []string{"ui/message"} }),
		allRefused:     frame("declares both, refuses both", func(f *viewhost.Frame) { f.Refuses = []string{"ui/message", "ui/update-model-context"} }),
	})
	const asWritten = "Retry payments with a cap"
	for index := range 5 {
		typeInto(t, ctx, index, `[data-draft="form.title"]`, asWritten)
	}

	// The notice names the route this host has.
	for index, want := range map[int]string{
		messageFrame: "send it to the conversation",
		contextFrame: "add it to this view's context",
		textFrame:    "copy it into the conversation",
	} {
		if text := frameText(t, ctx, index); !strings.Contains(text, "cannot submit") || !strings.Contains(text, want) {
			t.Errorf("frame %d does not say %q:\n%s", index, want, text)
		}
	}

	// A message in the person's name.
	clickButton(t, ctx, messageFrame, "Send to the conversation")
	waitInFrame(t, ctx, messageFrame, `d.body.innerText.includes("Sent to the conversation.")`, "the form did not say it was sent")
	sent := hostMessages(t, ctx, messageFrame, "ui/message")
	if len(sent) != 1 {
		t.Fatalf("the form sent %d messages, want one", len(sent))
	}
	for _, want := range []string{"PAY/ledger", "From: feature/retries", "To: master", "Title: " + asWritten, "Reviewers: bob, carol, erin", "Draft: no", "Retries **twice**."} {
		if !strings.Contains(messageText(sent[0]), want) {
			t.Errorf("the message does not carry %q:\n%s", want, messageText(sent[0]))
		}
	}
	if told := hostMessages(t, ctx, messageFrame, "ui/update-model-context"); len(told) != 0 {
		t.Errorf("a host that took the message was also given context: %v", told)
	}

	// The context, promising only that it was taken.
	clickButton(t, ctx, contextFrame, "Add to chat context")
	waitInFrame(t, ctx, contextFrame, `d.body.innerText.includes("Added to this view's context")`, "the form did not say where it went")
	if told := hostMessages(t, ctx, contextFrame, "ui/update-model-context"); len(told) != 1 || !strings.Contains(messageText(told[0]), asWritten) {
		t.Errorf("the context holds %v, want what the person wrote", told)
	}

	// Text to select, where nothing else is declared.
	clickButton(t, ctx, textFrame, "Show as text")
	waitInFrame(t, ctx, textFrame, `d.querySelector(".form-text")`, "the form did not show the text")
	var text string
	inFrame(t, ctx, textFrame, `return d.querySelector(".form-text").value;`, &text)
	if !strings.Contains(text, asWritten) || !strings.Contains(text, "Retries **twice**.") {
		t.Errorf("the text to select reads %q", text)
	}

	// A way that does not take it falls through to the next.
	clickButton(t, ctx, messageRefused, "Send to the conversation")
	waitInFrame(t, ctx, messageRefused, `d.body.innerText.includes("Added to this view's context")`, "a refused message did not fall through to the context")
	if told := hostMessages(t, ctx, messageRefused, "ui/update-model-context"); len(told) != 1 || !strings.Contains(messageText(told[0]), asWritten) {
		t.Errorf("after a refused message the context holds %v", told)
	}
	clickButton(t, ctx, allRefused, "Send to the conversation")
	waitInFrame(t, ctx, allRefused, `d.querySelector(".form-text")`, "with every way refused the form did not show the text")
	inFrame(t, ctx, allRefused, `return d.querySelector(".form-text").value;`, &text)
	if !strings.Contains(text, asWritten) {
		t.Errorf("with every way refused the text reads %q", text)
	}

	// Bitbucket's create page, for the same branches. Its address carries nothing
	// else, and the form says so.
	want := "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests?create&sourceBranch=refs%2Fheads%2Ffeature%2Fretries&targetBranch=refs%2Fheads%2Fmaster"
	if got := openedBy(t, ctx, messageFrame, clickNamed("Open Bitbucket's create page")); got != want {
		t.Errorf("the create page opened %q, want %q", got, want)
	}
	if text := frameText(t, ctx, messageFrame); !strings.Contains(text, "What you wrote here does not go with it.") {
		t.Errorf("the form does not say what the create page leaves out:\n%s", text)
	}
}

// A form with no title goes nowhere, and says why; an edit says so in its own
// words, and its way out is the pull request itself.
func TestAFormThatCannotSaveSaysSoInItsOwnWords(t *testing.T) {
	edit := newForm()
	edit.Mode = "edit"
	empty := newForm()
	empty.Title = ""
	ctx := browser(t, []viewhost.Frame{
		{Title: "edit", Mode: "inline", Result: fixtureResult(t, formPayload(edit, creating))},
		{Title: "no title", Mode: "inline", Result: fixtureResult(t, formPayload(empty, creating))},
	})

	if text := frameText(t, ctx, 0); !strings.Contains(text, "cannot save this form") {
		t.Errorf("an edit form reads:\n%s", text)
	}
	if got := openedBy(t, ctx, 0, clickNamed("Open in Bitbucket")); !strings.HasSuffix(got, "/pull-requests/42/overview") {
		t.Errorf("Open in Bitbucket opened %q, want the pull request", got)
	}
	clickButton(t, ctx, 0, "Send to the conversation")
	waitInFrame(t, ctx, 0, `d.body.innerText.includes("Sent to the conversation.")`, "the edit was not said to be sent")
	sent := hostMessages(t, ctx, 0, "ui/message")
	if len(sent) != 1 || !strings.Contains(messageText(sent[0]), "Update pull request PAY/ledger#42") {
		t.Errorf("the edit sent %v", sent)
	}

	clickButton(t, ctx, 1, "Send to the conversation")
	waitInFrame(t, ctx, 1, `d.querySelector(".form-error")`, "a form with no title said nothing")
	if sent := hostMessages(t, ctx, 1, "ui/message"); len(sent) != 0 {
		t.Errorf("a form with no title was sent: %v", sent)
	}
}
