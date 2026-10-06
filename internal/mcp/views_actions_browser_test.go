//go:build views

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
)

// commenting is what a server that lets views comment offers them.
var commenting = &viewOffers{Kinds: []string{showKindPullRequest, showKindDiff}, Tools: []string{"add_pr_comment"}}

// toolAnswer is a tool's result as the host passes it back: text, and an
// error when failed is set.
func toolAnswer(t *testing.T, text string, failed bool) json.RawMessage {
	t.Helper()
	encoded, err := json.Marshal(&mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}, IsError: failed})
	if err != nil {
		t.Fatalf("encode the answer: %v", err)
	}
	return encoded
}

// toolCalls are the arguments of the view's calls of one tool.
func toolCalls(t *testing.T, ctx context.Context, frame int, tool string) []map[string]any {
	t.Helper()
	var calls []map[string]any
	for _, message := range hostMessages(t, ctx, frame, "tools/call") {
		params, _ := message["params"].(map[string]any)
		if params["name"] == tool {
			arguments, _ := params["arguments"].(map[string]any)
			calls = append(calls, arguments)
		}
	}
	return calls
}

// typeInto writes text into a draft, as typing does.
func typeInto(t *testing.T, ctx context.Context, frame int, selector, text string) {
	t.Helper()
	encoded, _ := json.Marshal(text)
	var typed bool
	inFrame(t, ctx, frame, `const i = d.querySelector(`+"`"+selector+"`"+`); if (!i) return false; i.value = `+string(encoded)+`; i.dispatchEvent(new Event("input")); return true;`, &typed)
	if !typed {
		t.Fatalf("frame %d has no %s to write in", frame, selector)
	}
}

// A reply goes to Bitbucket through add_pr_comment, with the comment it
// answers, and the view reads itself again to show it. What the person wrote
// survives a redraw, and stays where sending failed.
func TestAReplyGoesThroughTheModelsToolAndShows(t *testing.T) {
	replied := fixtureActivity()
	replied.Items[3].Thread.Replies = append(replied.Items[3].Thread.Replies, viewReply{ID: 99, Author: "Alice Smith", AuthorUsername: "alice", Date: time.Now().UnixMilli(), Text: "Capped at thirty seconds."})
	after := overviewPayload(replied, commenting, "two")
	ctx := browser(t, []viewhost.Frame{
		{Title: "overview fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, overviewPayload(fixtureActivity(), commenting, "one")),
			ToolResults: map[string][]json.RawMessage{
				"add_pr_comment": {toolAnswer(t, "Bitbucket refused the comment.", true), toolAnswer(t, `{"comment":{"id":99}}`, false)},
				"refresh_view":   {refreshAnswer(t, &after, "The view of the pull request you showed the person has changed.")},
			}},
	})

	var clicked bool
	inFrame(t, ctx, 0, `const b = d.querySelector("#thread-1 .text-action"); if (b) b.click(); return Boolean(b);`, &clicked)
	if !clicked {
		t.Fatal("the open task has no Reply")
	}
	waitInFrame(t, ctx, 0, `d.activeElement && d.activeElement.dataset.draft === "reply-1"`, "the reply box did not open with the caret in it")
	typeInto(t, ctx, 0, `[data-draft="reply-1"]`, "Capped at thirty seconds.")

	// A redraw, here a resolved thread opening, keeps the draft.
	inFrame(t, ctx, 0, `d.querySelector("#thread-3 .comment-folded").click(); return true;`, &clicked)
	var kept string
	inFrame(t, ctx, 0, `const i = d.querySelector('[data-draft="reply-1"]'); return i ? i.value : "";`, &kept)
	if kept != "Capped at thirty seconds." {
		t.Fatalf("after a redraw the reply reads %q, want what was written", kept)
	}

	// The first answer fails: the reason shows and the text stays.
	inFrame(t, ctx, 0, `const b = d.querySelector("#thread-1 .send-button"); b.click(); return true;`, &clicked)
	waitInFrame(t, ctx, 0, `d.querySelector("#thread-1 .form-error") && d.querySelector("#thread-1 .form-error").textContent.includes("refused")`, "a failed reply does not say why")
	inFrame(t, ctx, 0, `const i = d.querySelector('[data-draft="reply-1"]'); return i ? i.value : "";`, &kept)
	if kept != "Capped at thirty seconds." {
		t.Fatalf("after a failed reply the box reads %q, want what was written", kept)
	}

	// The second goes through, and the view reads itself again.
	inFrame(t, ctx, 0, `const b = d.querySelector("#thread-1 .send-button"); b.click(); return true;`, &clicked)
	waitInFrame(t, ctx, 0, `!d.querySelector('[data-draft="reply-1"]') && d.querySelector("#thread-1").innerText.includes("Capped at thirty seconds.")`, "the reply is not shown once sent")
	calls := toolCalls(t, ctx, 0, "add_pr_comment")
	if len(calls) != 2 {
		t.Fatalf("the view called add_pr_comment %d times, want twice: %v", len(calls), calls)
	}
	for key, want := range map[string]any{"project": "PAY", "repo": "ledger", "pr_id": "42", "text": "Capped at thirty seconds.", "parent_id": float64(1)} {
		if calls[1][key] != want {
			t.Errorf("the reply sent %s = %v, want %v (all: %v)", key, calls[1][key], want, calls[1])
		}
	}
	if refreshes := refreshCalls(t, ctx, 0); len(refreshes) != 1 || refreshes[0]["since"] != "one" {
		t.Errorf("after the reply the view asked %v, want one refresh from what it held", refreshes)
	}
}

// A comment on the pull request itself carries no comment to answer.
func TestACommentOnThePullRequestGoesThroughTheModelsTool(t *testing.T) {
	after := overviewPayload(fixtureActivity(), commenting, "two")
	ctx := browser(t, []viewhost.Frame{
		{Title: "overview fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, overviewPayload(fixtureActivity(), commenting, "one")),
			ToolResults: map[string][]json.RawMessage{
				"add_pr_comment": {toolAnswer(t, `{"comment":{"id":100}}`, false)},
				"refresh_view":   {refreshAnswer(t, &after, "changed")},
			}},
	})

	clickButton(t, ctx, 0, "Add a comment")
	typeInto(t, ctx, 0, `[data-draft="comment-pr"]`, "Looks good to me.")
	var sent bool
	inFrame(t, ctx, 0, `const i = d.querySelector('[data-draft="comment-pr"]'); i.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, bubbles: true })); return true;`, &sent)
	waitInFrame(t, ctx, 0, `!d.querySelector('[data-draft="comment-pr"]')`, "the comment box did not close once sent")
	calls := toolCalls(t, ctx, 0, "add_pr_comment")
	if len(calls) != 1 || calls[0]["text"] != "Looks good to me." || calls[0]["parent_id"] != nil {
		t.Errorf("the comment sent %v, want its text and no comment it answers", calls)
	}
}

// A view offers to write only where the server lets views comment, and the
// host passes tool calls.
func TestAViewOffersToCommentOnlyWhereItCan(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "a server that keeps add_pr_comment from views", Mode: "fullscreen", Fullscreen: true,
			Result: fixtureResult(t, overviewPayload(fixtureActivity(), everyKind, "one")), ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
		{Title: "a host that passes no tool calls", Mode: "fullscreen", Fullscreen: true,
			Result: fixtureResult(t, overviewPayload(fixtureActivity(), commenting, "one"))},
	})
	if err := chromedp.Do(ctx, chromedp.Sleep(200*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	for frame := range 2 {
		var buttons string
		inFrame(t, ctx, frame, `return [...d.querySelectorAll("button")].map((b) => b.textContent.trim()).join("|");`, &buttons)
		if strings.Contains(buttons, "Reply") || strings.Contains(buttons, "Add a comment") {
			t.Errorf("frame %d offers to write where it cannot: %s", frame, buttons)
		}
		if !strings.Contains(frameText(t, ctx, frame), "Cap the time a charge spends retrying.") {
			t.Fatalf("frame %d drew no activity, so the check above proves nothing", frame)
		}
	}
}
