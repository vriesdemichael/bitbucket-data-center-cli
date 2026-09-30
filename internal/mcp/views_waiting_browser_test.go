//go:build views

package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
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
func heldWaits(waits ...int) chromedp.Action {
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
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, err := cdppage.AddScriptToEvaluateOnNewDocument(script).Do(ctx)
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
	ctx := openHost(t, []viewhost.Frame{{Title: "no result", Mode: "inline"}}, []chromedp.Action{heldWaits(slow, none)}, started)

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
