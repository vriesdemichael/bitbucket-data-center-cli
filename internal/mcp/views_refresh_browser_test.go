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

// The show call every card here answers, as the payload carries it.
var refreshedCardCall = &ShowInput{Kind: showKindPullRequest, Project: "PAY", Repo: "ledger", ID: "42"}

func refreshCardPayload(title, state string, readAt time.Time, fingerprint string) viewPayload {
	pr := fixturePullRequest()
	pr.Title, pr.State, pr.Open = title, state, state == "OPEN"
	summary := pullrequestservice.BuildReviewSummary(pr, pullrequestservice.ReviewCounts{})
	card := viewPullRequest{
		PullRequest:   pr,
		URL:           "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview",
		ReviewSummary: &summary,
		CheckCounts:   &viewCheckCounts{Successful: 3},
	}
	return viewPayload{
		Kind: showKindPullRequest, GeneratedAt: readAt.UTC().Format(time.RFC3339), PullRequest: &card,
		Show: refreshedCardCall, Fingerprint: fingerprint,
	}
}

func refreshDiffPayload(patch string, readAt time.Time, fingerprint string) viewPayload {
	pr := viewPullRequest{PullRequest: fixturePullRequest(), URL: "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview"}
	pr.Title = "Round refunds to the cent"
	return viewPayload{
		Kind: showKindDiff, GeneratedAt: readAt.UTC().Format(time.RFC3339), PullRequest: &pr, Diff: &viewDiff{Patch: patch},
		Show: &ShowInput{Kind: showKindDiff, Project: "PAY", Repo: "ledger", ID: "42"}, Fingerprint: fingerprint,
	}
}

// refreshAnswer is refresh_view's result: the data when there is a payload,
// and unchanged when there is none.
func refreshAnswer(t *testing.T, payload *viewPayload, text string) json.RawMessage {
	t.Helper()
	out := RefreshViewOutput{Changed: payload != nil, GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	result := &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
	if payload != nil {
		payload.Version = viewPayloadVersion
		out.Fingerprint = payload.Fingerprint
		result.Meta = mcp.Meta{viewPayloadKey: *payload}
	}
	result.StructuredContent = out
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode the answer: %v", err)
	}
	return encoded
}

func answers(results ...json.RawMessage) map[string][]json.RawMessage {
	return map[string][]json.RawMessage{"refresh_view": results}
}

// waitInFrame waits until condition, with d bound to the frame's document,
// holds.
func waitInFrame(t *testing.T, ctx context.Context, frame int, condition, what string) {
	t.Helper()
	script := fmt.Sprintf(`(() => { const d = window.bbHost.frames[%d].iframe.contentDocument; return Boolean(%s); })()`, frame, condition)
	if _, err := chromedp.Run(ctx, chromedp.Poll[chromedp.Void](script, chromedp.WithPollingTimeout(10*time.Second))); err != nil {
		t.Fatalf("frame %d: %s: %v", frame, what, err)
	}
}

func scrollToFrame(t *testing.T, ctx context.Context, frame int) {
	t.Helper()
	if err := chromedp.Do(ctx, chromedp.Evaluate[chromedp.Void](fmt.Sprintf(`window.bbHost.frames[%d].iframe.scrollIntoView({ block: "center" })`, frame))); err != nil {
		t.Fatalf("scroll to frame %d: %v", frame, err)
	}
}

// onScreen is whether a frame is in the host page's viewport, so a check that
// a view asked nothing is not passed by a view that could not have asked.
func onScreen(t *testing.T, ctx context.Context, frame int) bool {
	t.Helper()
	script := fmt.Sprintf(`(() => { const r = window.bbHost.frames[%d].iframe.getBoundingClientRect(); return r.bottom > 0 && r.top < window.innerHeight; })()`, frame)
	visible, err := chromedp.Run(ctx, chromedp.Evaluate[bool](script))
	if err != nil {
		t.Fatalf("frame %d: %v", frame, err)
	}
	return visible
}

func refreshCalls(t *testing.T, ctx context.Context, frame int) []map[string]any {
	t.Helper()
	var calls []map[string]any
	for _, message := range hostMessages(t, ctx, frame, "tools/call") {
		params, _ := message["params"].(map[string]any)
		if params["name"] == "refresh_view" {
			arguments, _ := params["arguments"].(map[string]any)
			calls = append(calls, arguments)
		}
	}
	return calls
}

// A view whose data was read a while ago asks bb for it again once it is on
// screen, with the show call it answers and its fingerprint, draws what comes
// back and tells the model. One read just now waits.
func TestAViewReadAWhileAgoRefreshesOnScreen(t *testing.T) {
	stale, fresh := time.Now().Add(-2*time.Hour), time.Now()
	merged := refreshCardPayload("Refunds round to the cent", "MERGED", time.Now(), "two")
	ctx := browser(t, []viewhost.Frame{
		{Title: "read two hours ago", Mode: "inline", Fullscreen: true,
			Result:      fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", stale, "one")),
			ToolResults: answers(refreshAnswer(t, &merged, "The view of pull request PAY/ledger#42 you showed the person has changed: merged."))},
		{Title: "read just now", Mode: "inline", Fullscreen: true,
			Result:      fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", fresh, "one")),
			ToolResults: answers(refreshAnswer(t, &merged, "changed"))},
	})

	waitInFrame(t, ctx, 0, `d.body.innerText.includes("Refunds round to the cent")`, "the refreshed card is not drawn")
	calls := refreshCalls(t, ctx, 0)
	if len(calls) != 1 {
		t.Fatalf("the view asked %d times, want once", len(calls))
	}
	for key, want := range map[string]any{"kind": "pull_request", "project": "PAY", "repo": "ledger", "id": "42", "since": "one"} {
		if calls[0][key] != want {
			t.Errorf("the refresh sent %s = %v, want %v (all: %v)", key, calls[0][key], want, calls[0])
		}
	}
	told := hostMessages(t, ctx, 0, "ui/update-model-context")
	if len(told) != 1 || !strings.Contains(messageText(told[0]), "has changed") {
		t.Errorf("the model was told %v, want the change", told)
	}
	if text := frameText(t, ctx, 0); !strings.Contains(strings.ToLower(text), "merged") {
		t.Errorf("the refreshed card reads %q, want it merged", text)
	}

	if !onScreen(t, ctx, 1) {
		t.Fatal("the fresh card is off screen, so it could not have asked anyway")
	}
	if err := chromedp.Do(ctx, chromedp.Sleep(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if calls := refreshCalls(t, ctx, 1); len(calls) != 0 {
		t.Errorf("a card read just now asked again at once: %v", calls)
	}
}

// A changed diff is offered, not swapped in under the person's reading: the
// old one stays until they ask for the new one.
func TestAChangedDiffIsOffered(t *testing.T) {
	stale := time.Now().Add(-2 * time.Hour)
	oldPatch := strings.Join([]string{
		"diff --git src://ledger.go dst://ledger.go",
		"--- src://ledger.go",
		"+++ dst://ledger.go",
		"@@ -1 +1 @@",
		"-old",
		"+new",
		"",
	}, "\n")
	newPatch := oldPatch + strings.Join([]string{
		"diff --git src://refunds.md dst://refunds.md",
		"new file mode 100644",
		"--- /dev/null",
		"+++ dst://refunds.md",
		"@@ -0,0 +1 @@",
		"+Refunds round to the cent.",
		"",
	}, "\n")
	changed := refreshDiffPayload(newPatch, time.Now(), "two")
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff read two hours ago", Mode: "inline", Fullscreen: false,
			Result:      fixtureResult(t, refreshDiffPayload(oldPatch, stale, "one")),
			ToolResults: answers(refreshAnswer(t, &changed, "The view of the diff you showed the person has changed: 2 files."))},
	})

	waitInFrame(t, ctx, 0, `d.querySelector(".refresh-notice")`, "the changed diff is not offered")
	if text := frameText(t, ctx, 0); strings.Contains(text, "refunds.md") || !strings.Contains(text, "ledger.go") {
		t.Errorf("before the person asked, the diff reads %q; want the old one", text)
	}
	if told := hostMessages(t, ctx, 0, "ui/update-model-context"); len(told) != 0 {
		t.Errorf("the model was told of a diff the person has not seen: %v", told)
	}

	clickButton(t, ctx, 0, "Show the new diff")
	waitInFrame(t, ctx, 0, `d.body.innerText.includes("refunds.md") && !d.querySelector(".refresh-notice")`, "the new diff is not drawn")
	if told := hostMessages(t, ctx, 0, "ui/update-model-context"); len(told) != 1 || !strings.Contains(messageText(told[0]), "has changed") {
		t.Errorf("the model was told %v once the person saw the new diff, want the change", told)
	}
}

// A refresh the person asks for keeps what they opened: here, a filter.
func TestARefreshKeepsWhatThePersonOpened(t *testing.T) {
	prs := func(extra ...pullrequestservice.PullRequest) []viewPullRequest {
		open, draft, merged := fixturePullRequest(), fixturePullRequest(), fixturePullRequest()
		open.Title = "Round refunds"
		draft.ID, draft.Title, draft.Draft = 43, "Send an idempotency key", true
		merged.ID, merged.Title, merged.State, merged.Open = 44, "Bump golang.org/x/net", "MERGED", false
		list := []viewPullRequest{{PullRequest: open}, {PullRequest: draft}, {PullRequest: merged}}
		for _, pr := range extra {
			list = append(list, viewPullRequest{PullRequest: pr})
		}
		return list
	}
	another := fixturePullRequest()
	another.ID, another.Title, another.Draft = 45, "Retry the ledger client", true
	call := &ShowInput{Kind: showKindPullRequests}
	before := viewPayload{Kind: showKindPullRequests, GeneratedAt: time.Now().UTC().Format(time.RFC3339), PullRequests: prs(), Show: call, Fingerprint: "one"}
	after := viewPayload{Kind: showKindPullRequests, GeneratedAt: time.Now().UTC().Format(time.RFC3339), PullRequests: prs(another), Show: call, Fingerprint: "two"}
	ctx := browser(t, []viewhost.Frame{
		{Title: "list in fullscreen", Mode: "fullscreen", Fullscreen: true,
			Result: fixtureResult(t, before), ToolResults: answers(refreshAnswer(t, &after, "changed"))},
	})

	clickButton(t, ctx, 0, "Draft")
	waitInFrame(t, ctx, 0, `!d.body.innerText.includes("Round refunds")`, "the filter did not apply")
	var clicked bool
	inFrame(t, ctx, 0, `const b = d.querySelector(".refresh-button"); if (b) b.click(); return Boolean(b);`, &clicked)
	if !clicked {
		t.Fatal("the fullscreen list has no refresh button")
	}
	waitInFrame(t, ctx, 0, `d.body.innerText.includes("Retry the ledger client")`, "the refreshed list is not drawn")
	text := frameText(t, ctx, 0)
	if strings.Contains(text, "Round refunds") || strings.Contains(text, "Bump golang.org") || !strings.Contains(text, "Send an idempotency key") {
		t.Errorf("after the refresh the list reads %q; want the drafts alone, as filtered", text)
	}
}

// A view off screen asks nothing until it comes on screen, and a view the
// host took down asks nothing at all.
func TestAViewAsksNothingOffScreenOrAfterTeardown(t *testing.T) {
	stale := time.Now().Add(-2 * time.Hour)
	merged := refreshCardPayload("Refunds round to the cent", "MERGED", time.Now(), "two")
	ctx := browser(t, []viewhost.Frame{
		// Tall enough that the cards below start off screen.
		{Title: "a tall view above", Mode: "fullscreen", Fullscreen: true, Height: 1600,
			Result: fixtureResult(t, refreshCardPayload("Something else", "OPEN", time.Now(), "x"))},
		{Title: "below, read two hours ago", Mode: "inline", Fullscreen: true,
			Result: fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", stale, "one")), ToolResults: answers(refreshAnswer(t, &merged, "changed"))},
		{Title: "below, read two hours ago, taken down", Mode: "inline", Fullscreen: true,
			Result: fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", stale, "one")), ToolResults: answers(refreshAnswer(t, &merged, "changed"))},
	})

	if onScreen(t, ctx, 1) || onScreen(t, ctx, 2) {
		t.Fatal("the cards start on screen; the test needs them below it")
	}
	if err := chromedp.Do(ctx,
		chromedp.Evaluate[chromedp.Void](`window.bbHost.frames[2].teardown()`),
		chromedp.Sleep(1500*time.Millisecond),
	); err != nil {
		t.Fatal(err)
	}
	if calls := append(refreshCalls(t, ctx, 1), refreshCalls(t, ctx, 2)...); len(calls) != 0 {
		t.Fatalf("views off screen asked: %v", calls)
	}
	answered, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`window.bbHost.frames[2].messages.some((m) => m.id === "teardown-2" && m.result)`))
	if err != nil || !answered {
		t.Fatalf("the view did not answer its teardown: %v", err)
	}

	scrollToFrame(t, ctx, 1)
	waitInFrame(t, ctx, 1, `d.body.innerText.includes("Refunds round to the cent")`, "the card that came on screen did not refresh")
	if !onScreen(t, ctx, 2) {
		t.Fatal("the torn-down card is not on screen next to the other")
	}
	if err := chromedp.Do(ctx, chromedp.Sleep(1500*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if calls := refreshCalls(t, ctx, 2); len(calls) != 0 {
		t.Errorf("a view the host took down asked: %v", calls)
	}
}

// A refresh that fails leaves the data as it was, and the card says so; a
// host that passes no tool calls gets no refresh and no button for one.
func TestAViewSaysWhenItCouldNotRefresh(t *testing.T) {
	stale := time.Now().Add(-2 * time.Hour)
	ctx := browser(t, []viewhost.Frame{
		{Title: "refresh fails", Mode: "inline", Fullscreen: true,
			Result:      fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", stale, "one")),
			ToolResults: answers(json.RawMessage(`{"error":{"code":-32000,"message":"bb is not running"}}`))},
		{Title: "host passes no tool calls", Mode: "inline", Fullscreen: true,
			Result: fixtureResult(t, refreshCardPayload("Round refunds", "OPEN", stale, "one"))},
	})

	waitInFrame(t, ctx, 0, `d.body.innerText.includes("could not refresh")`, "the card does not say it could not refresh")
	var title string
	inFrame(t, ctx, 0, `const s = d.querySelector(".stamp"); return s ? s.title : "";`, &title)
	if !strings.Contains(title, "bb is not running") {
		t.Errorf("the stamp's title is %q, want the reason", title)
	}
	if text := frameText(t, ctx, 0); !strings.Contains(text, "Round refunds") {
		t.Errorf("after a failed refresh the card reads %q, want the data it had", text)
	}

	if !onScreen(t, ctx, 1) {
		t.Fatal("the second card is off screen, so it could not have asked anyway")
	}
	if calls := refreshCalls(t, ctx, 1); len(calls) != 0 {
		t.Errorf("a host that passes no tool calls was asked: %v", calls)
	}
	var button, stale2 bool
	inFrame(t, ctx, 1, `return Boolean(d.querySelector(".refresh-button"));`, &button)
	inFrame(t, ctx, 1, `return d.body.innerText.includes("may be out of date");`, &stale2)
	if button || !stale2 {
		t.Errorf("without tool calls the card has a refresh button: %v, says it may be out of date: %v; want no button, and the warning", button, stale2)
	}
}
