//go:build views

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
)

// resultWaits are how long the page waits for its result before it says the
// result is slow, and then that it has not come, as the page states them.
func resultWaits(t *testing.T) (slow, none int) {
	t.Helper()
	wait := func(name string) int {
		match := regexp.MustCompile(`const ` + name + ` = (\d+);`).FindStringSubmatch(viewPage())
		if match == nil {
			t.Fatalf("the page states no %s", name)
		}
		ms, _ := strconv.Atoi(match[1])
		return ms
	}
	return wait("SLOW_RESULT_MS"), wait("NO_RESULT_MS")
}

// heldWaits keeps the page's timers of those lengths from running on their
// own: each is held in bbWaits, by its length, for the test to run.
func heldWaits(waits ...int) chromedp.Action[chromedp.Void] {
	lengths := make([]string, len(waits))
	for index, wait := range waits {
		lengths[index] = strconv.Itoa(wait)
	}
	script := fmt.Sprintf(`(() => {
		const held = new Set([%s]);
		const later = window.setTimeout;
		window.bbWaits = {};
		window.setTimeout = function (callback, ms, ...args) {
			if (held.has(ms)) {
				window.bbWaits[ms] = () => callback(...args);
				return 0;
			}
			return later.call(window, callback, ms, ...args);
		};
	})()`, strings.Join(lengths, ", "))
	return chromedp.Func(func(ctx context.Context, target *chromedp.Target) error {
		_, err := cdp.Call(ctx, target, cdppage.AddScriptToEvaluateOnNewDocument, cdppage.AddScriptToEvaluateOnNewDocumentParams{Source: script})
		return err
	})
}

// A view whose result does not come holds its place, then says it is still
// waiting, then that the answer has not come and how to go on, rather than
// wait for ever with no reason given. A result that comes after all is drawn,
// and the waits running out after that change nothing.
func TestAViewWhoseResultDoesNotComeSaysSo(t *testing.T) {
	slow, none := resultWaits(t)
	started := `window.bbHost && window.bbHost.frames.length === 1 && (() => {
		const d = window.bbHost.frames[0].iframe.contentDocument;
		return d && d.querySelector("#app [aria-busy]") && d.defaultView.bbWaits && Object.keys(d.defaultView.bbWaits).length === 2;
	})()`
	ctx := openHost(t, []viewhost.Frame{{Title: "no result", Mode: "inline"}}, []chromedp.Action[chromedp.Void]{heldWaits(slow, none)}, started)

	type state struct {
		Busy bool   `json:"busy"`
		Text string `json:"text"`
	}
	read := func() state {
		var got state
		inFrame(t, ctx, 0, `return { busy: Boolean(d.querySelector("[aria-busy]")), text: d.getElementById("app").textContent };`, &got)
		return got
	}
	run := func(wait int) {
		inFrame(t, ctx, 0, fmt.Sprintf(`w.bbWaits[%d](); return true;`, wait), new(bool))
	}

	if got := read(); !got.Busy || strings.Contains(got.Text, "waiting") || strings.Contains(got.Text, "has not reached") {
		t.Fatalf("before any wait the view reads %+v, want its place held and nothing said", got)
	}

	run(slow)
	var status string
	inFrame(t, ctx, 0, `const s = d.querySelector("[aria-busy] [role=status]"); return s ? s.textContent : "";`, &status)
	if got := read(); !got.Busy || status != "Still waiting for bb's answer." {
		t.Errorf("after %d ms the view reads %+v with status %q, want its place held and still waiting said", slow, got, status)
	}

	run(none)
	if got := read(); got.Busy || !strings.Contains(got.Text, "bb's answer has not reached this view") || !strings.Contains(got.Text, "Ask for it in the conversation instead.") {
		t.Errorf("after %d ms the view reads %+v, want that the answer has not come and how to go on", none, got)
	}

	late := fixtureResult(t, refreshCardPayload("Round refunds to the cent", "OPEN", time.Now(), "late"))
	inFrame(t, ctx, 0, fmt.Sprintf(`w.postMessage({ jsonrpc: "2.0", method: "ui/notifications/tool-result", params: %s }, "*"); return true;`, late), new(bool))
	waitInFrame(t, ctx, 0, `d.getElementById("app").textContent.includes("Round refunds to the cent")`, "the result that came late is not drawn")

	// Drawing the view again would drop what the person selected or hovers.
	inFrame(t, ctx, 0, `d.getElementById("app").firstElementChild.bbDrawn = true; return true;`, new(bool))
	run(slow)
	run(none)
	if got := read(); got.Busy || !strings.Contains(got.Text, "Round refunds to the cent") || strings.Contains(got.Text, "waiting") || strings.Contains(got.Text, "has not reached") {
		t.Errorf("the waits running out after the result leave the view reading %+v, want the card alone", got)
	}
	var same bool
	inFrame(t, ctx, 0, `return d.getElementById("app").firstElementChild.bbDrawn === true;`, &same)
	if !same {
		t.Error("the waits running out after the result drew the view again")
	}
}

// droppedResult is show's answer as a client passes it on that keeps its text
// and structuredContent and drops its _meta, as VS Code's agent host does.
func droppedResult(t *testing.T, text string, out ShowOutput) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(&mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, StructuredContent: out})
	if err != nil {
		t.Fatalf("encode the result: %v", err)
	}
	return encoded
}

// A view whose answer reached it without what it draws reads that through
// refresh_view, with the show call the host gave it, and draws it; show's text
// told the model, so the model is not told again. A host that passes no tool
// calls, or a read that fails, gets a view that says what happened rather
// than show's text for the model. A client that said it draws no views was
// sent nothing, and nothing is read for it.
func TestAViewWhoseAnswerLostItsDataReadsIt(t *testing.T) {
	card := refreshCardPayload("Round refunds to the cent", "OPEN", time.Now(), "one")
	call := map[string]any{"kind": "pull_request", "project": "PAY", "repo": "ledger", "id": 42}
	shown := droppedResult(t, "Showed the person pull request PAY/ledger#42 as an interactive card: open.\n\n"+notDrawn,
		ShowOutput{Shown: true, Kind: showKindPullRequest, Target: "PAY/ledger#42"})
	notShown := droppedResult(t, "This client displays no views, so nothing was shown.",
		ShowOutput{Shown: false, Kind: showKindPullRequest, Target: "PAY/ledger#42"})
	const (
		readsIt = iota
		passesNoCalls
		readFails
		drawsNoViews
	)
	ctx := browser(t, []viewhost.Frame{
		readsIt: {Title: "a host that passes tool calls", Arguments: call, Result: shown,
			ToolResults: answers(refreshAnswer(t, &card, "The person opened pull request PAY/ledger#42 in a view: open."))},
		passesNoCalls: {Title: "a host that passes none", Arguments: call, Result: shown},
		readFails: {Title: "a read that fails", Arguments: call, Result: shown,
			ToolResults: answers(toolAnswer(t, "Bitbucket did not answer.", true))},
		drawsNoViews: {Title: "a client that said it draws no views", Arguments: call, Result: notShown,
			ToolResults: answers(refreshAnswer(t, &card, "The person opened pull request PAY/ledger#42 in a view: open."))},
	})

	if text := frameText(t, ctx, readsIt); !strings.Contains(text, "Round refunds to the cent") {
		t.Fatalf("the view did not draw what it read:\n%s", text)
	}
	calls := toolCalls(t, ctx, readsIt, "refresh_view")
	if len(calls) != 1 {
		t.Fatalf("the view read it %d times, want once", len(calls))
	}
	for key, want := range map[string]any{"kind": "pull_request", "project": "PAY", "repo": "ledger", "id": float64(42)} {
		if calls[0][key] != want {
			t.Errorf("the read sent %s = %v, want %v (all: %v)", key, calls[0][key], want, calls[0])
		}
	}
	if since, ok := calls[0]["since"]; ok {
		t.Errorf("the read sent since %v, so bb could answer that nothing changed", since)
	}
	if told := hostMessages(t, ctx, readsIt, "ui/update-model-context"); len(told) != 0 {
		t.Errorf("the model was told %v, which show's text already told it", told)
	}

	text := frameText(t, ctx, passesNoCalls)
	if !strings.Contains(text, "without what the view draws") || strings.Contains(text, "If the person says") {
		t.Errorf("a host that passes no tool calls shows:\n%s\nwant what happened, not show's text for the model", text)
	}
	if asked := hostMessages(t, ctx, passesNoCalls, "tools/call"); len(asked) != 0 {
		t.Errorf("the view asked a host that passes no tool calls: %v", asked)
	}

	if text := frameText(t, ctx, readFails); !strings.Contains(text, "without what the view draws") || !strings.Contains(text, "Bitbucket did not answer.") {
		t.Errorf("a read that fails shows:\n%s\nwant what happened and why the read failed", text)
	}

	if text := frameText(t, ctx, drawsNoViews); !strings.Contains(text, "This client displays no views") {
		t.Errorf("a client that said it draws no views shows:\n%s\nwant show's answer", text)
	}
	if asked := toolCalls(t, ctx, drawsNoViews, "refresh_view"); len(asked) != 0 {
		t.Errorf("the view read what bb did not send a client that said it draws no views: %v", asked)
	}
}
