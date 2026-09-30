//go:build views

package mcp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

// commentedPatch changes two files: in ledger.go line 9 is removed and lines 9
// and 10 are added; old-notes.md is deleted.
var commentedPatch = strings.Join([]string{
	"diff --git src://internal/ledger/ledger.go dst://internal/ledger/ledger.go",
	"index 1111111..2222222 100644",
	"--- src://internal/ledger/ledger.go",
	"+++ dst://internal/ledger/ledger.go",
	"@@ -8,3 +8,4 @@ func Refund(e Entry, fraction float64) Entry {",
	" // Refund returns the entry that reverses e.",
	"-\treturn Entry{Amount: -int64(float64(e.Amount) * fraction)}",
	"+\treturn Entry{Amount: -int64(math.Round(float64(e.Amount) * fraction))}",
	"+\t// Rounded to the cent.",
	" }",
	"diff --git src://docs/old-notes.md dst://docs/old-notes.md",
	"deleted file mode 100644",
	"--- src://docs/old-notes.md",
	"+++ /dev/null",
	"@@ -1 +0,0 @@",
	"-Superseded by the README.",
	"",
}, "\n")

// commentedThreads are a diff's threads, each where Bitbucket's diff draws
// it: one on an added line, a resolved one on a removed line, one on a file,
// one Bitbucket's diff no longer draws, and one on the pull request.
func commentedThreads(extra ...viewThread) viewThreads {
	now := time.Now().UnixMilli()
	ledger := "internal/ledger/ledger.go"
	threads := []viewThread{
		{ID: 1, Task: true, Author: "Carol Diaz", CreatedDate: now - 3600000, Text: "Cap the time a charge spends retrying.", URL: threadURL(1)},
		{ID: 2, Author: "Bob Chen", CreatedDate: now - 7200000, Text: "Round half to even?", URL: threadURL(2), Place: "new:9",
			Anchor: &pullrequestactivityservice.Anchor{Path: ledger, Line: 9, LineType: "ADDED"}},
		{ID: 3, Resolved: true, Author: "Dave Okafor", CreatedDate: now - 9000000, Text: "This truncated.", URL: threadURL(3), Place: "old:9",
			Anchor:  &pullrequestactivityservice.Anchor{Path: ledger, Line: 9, LineType: "REMOVED"},
			Replies: []viewReply{{ID: 31, Author: "Alice Smith", Date: now - 8000000, Text: "It rounds now."}}},
		{ID: 4, Author: "Erin Walsh", CreatedDate: now - 9500000, Text: "Keep a copy somewhere?", URL: threadURL(4), Place: "file",
			Anchor: &pullrequestactivityservice.Anchor{Path: "docs/old-notes.md"}},
		{ID: 5, Author: "Alice Smith", CreatedDate: now - 600000, Text: "The caller rounds too.", URL: threadURL(5),
			Anchor: &pullrequestactivityservice.Anchor{Path: ledger, Line: 99, LineType: "CONTEXT", Orphaned: true}},
	}
	threads = append(threads, extra...)
	return viewThreads{Summary: pullrequestactivityservice.Summary{TotalThreads: len(threads)}, Threads: threads}
}

func commentedDiff(readAt time.Time, fingerprint string, threads viewThreads) viewPayload {
	payload := refreshDiffPayload(commentedPatch, readAt, fingerprint)
	payload.Threads = &threads
	payload.Offers = commenting
	return payload
}

// openTreeFile picks a file in a fullscreen diff's tree by its name, and
// waits for it to show beside the tree.
func openTreeFile(t *testing.T, ctx context.Context, frame int, name string, index int) {
	t.Helper()
	var clicked bool
	inFrame(t, ctx, frame, `const b = [...d.querySelectorAll(".diff-tree .file-link")].find((b) => b.querySelector(".file-name").textContent.startsWith(`+"`"+name+"`"+`)); if (b) b.click(); return Boolean(b);`, &clicked)
	if !clicked {
		t.Fatalf("the tree has no %s", name)
	}
	waitInFrame(t, ctx, frame, `d.getElementById("diff-file-`+strconv.Itoa(index)+`") !== null`, name+" did not show beside the tree")
}

// A diff draws each comment where Bitbucket's diff draws it: under its line
// in a card that names the line, on the side it was written on; a resolved
// one folded to a line; a file's own at the top of the file. A comment
// Bitbucket's diff no longer draws, and one on the pull request, are in the
// overview and not here.
func TestADiffDrawsItsCommentsWhereBitbucketDoes(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, commentedDiff(time.Now(), "one", commentedThreads())),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
	})

	// The first file in the tree shows first: docs comes before internal.
	var tops string
	inFrame(t, ctx, 0, `const top = d.querySelector("#diff-file-1 .file-threads"); return top ? top.innerText : "";`, &tops)
	if !strings.Contains(tops, "Keep a copy somewhere?") {
		t.Errorf("the top of old-notes.md reads %q, want the comment on the file", tops)
	}
	var header string
	inFrame(t, ctx, 0, `return d.querySelector("#diff-file-1 .diff-file-head").textContent;`, &header)
	if strings.Contains(header, "null") || !strings.Contains(header, "Deleted") {
		t.Errorf("the deleted file's header reads %q, want its lozenge and no stray text", header)
	}

	openTreeFile(t, ctx, 0, "ledger.go", 0)
	var under map[string]string
	inFrame(t, ctx, 0, `const rows = [...d.querySelectorAll("#diff-file-0 tr")];
		const after = (type, n) => { const i = rows.findIndex((r) => r.classList.contains(type) && !r.classList.contains("diff-thread") && [...r.querySelectorAll(".line-number")].some((b) => b.textContent === String(n)));
			return i >= 0 && rows[i + 1] && rows[i + 1].classList.contains("diff-thread") ? rows[i + 1].innerText : ""; };
		return { added: after("add", 9), removed: after("del", 9) };`, &under)
	if !strings.Contains(under["added"], "Round half to even?") || !strings.Contains(under["added"], "Line +9") {
		t.Errorf("under added line 9 is %q, want the comment on it, naming Line +9", under["added"])
	}
	// innerText is as drawn, and a lozenge is drawn in capitals.
	if !strings.Contains(under["removed"], "Dave Okafor") || !strings.Contains(strings.ToLower(under["removed"]), "resolved") || !strings.Contains(under["removed"], "Line -9") ||
		strings.Contains(under["removed"], "This truncated.") {
		t.Errorf("under removed line 9 is %q, want the resolved thread folded to one line naming Line -9", under["removed"])
	}

	// A resolved thread opens on a click, with every reply.
	var opened bool
	inFrame(t, ctx, 0, `const b = d.querySelector("#thread-3 .comment-folded"); if (b) b.click(); return Boolean(b);`, &opened)
	if !opened {
		t.Fatal("the resolved thread has nothing to open it by")
	}
	waitInFrame(t, ctx, 0, `d.getElementById("thread-3") && d.getElementById("thread-3").innerText.includes("This truncated.") && d.getElementById("thread-3").innerText.includes("It rounds now.")`,
		"the resolved thread does not open with its reply")

	// The line's colour carries on beside the card.
	var colours []string
	inFrame(t, ctx, 0, `const t = d.querySelector("#thread-2").closest("tr"); return [getComputedStyle(t.querySelector(".line-number")).backgroundColor, getComputedStyle(t.previousElementSibling.querySelector("td")).backgroundColor];`, &colours)
	if len(colours) != 2 || colours[0] != colours[1] {
		t.Errorf("beside the card the line's colour is %v, want the added line's", colours)
	}

	page := frameText(t, ctx, 0)
	for _, absent := range []string{"Cap the time a charge spends retrying.", "The caller rounds too."} {
		if strings.Contains(page, absent) {
			t.Errorf("the diff draws %q, which Bitbucket's diff does not", absent)
		}
	}
	var tree string
	inFrame(t, ctx, 0, `return d.querySelector(".diff-tree").innerText;`, &tree)
	if !strings.Contains(tree, "1") {
		t.Errorf("the file tree reads %q, want ledger.go's open comment counted", tree)
	}
}

// Fullscreen, a diff is Bitbucket's diff page: its tree lists folders first,
// then files, each by name, a folder that holds only a folder joined to it,
// and shows one file at a time, the one picked.
func TestADiffShowsOneFileAtATimeFromItsTree(t *testing.T) {
	patch := strings.Join([]string{
		"diff --git a/README.md b/README.md", "--- a/README.md", "+++ b/README.md", "@@ -1 +1 @@", "-a", "+b",
		"diff --git a/internal/payments/retry.go b/internal/payments/retry.go", "--- a/internal/payments/retry.go", "+++ b/internal/payments/retry.go", "@@ -1 +1 @@", "-a", "+b",
		"diff --git a/internal/payments/retry_test.go b/internal/payments/retry_test.go", "--- a/internal/payments/retry_test.go", "+++ b/internal/payments/retry_test.go", "@@ -1 +1 @@", "-a", "+b",
		"diff --git a/config/payments.yaml b/config/payments.yaml", "--- a/config/payments.yaml", "+++ b/config/payments.yaml", "@@ -1 +1 @@", "-a", "+b",
		"",
	}, "\n")
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, refreshDiffPayload(patch, time.Now(), "one"))},
	})

	var rows []string
	inFrame(t, ctx, 0, `return [...d.querySelectorAll(".diff-tree li")].map((li) => (li.classList.contains("tree-dir") ? "/" : "") + li.textContent.replace(/[+−]\d+/g, "").trim());`, &rows)
	want := []string{"/config", "payments.yaml", "/internal/payments", "retry_test.go", "retry.go", "README.md"}
	if strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Errorf("the tree lists %v, want folders first, joined, then files, by name: %v", rows, want)
	}

	var shown []string
	inFrame(t, ctx, 0, `return [...d.querySelectorAll(".diff-file")].map((s) => s.id);`, &shown)
	if len(shown) != 1 || shown[0] != "diff-file-3" {
		t.Errorf("the diff shows %v, want the first file in the tree alone", shown)
	}
	openTreeFile(t, ctx, 0, "README.md", 0)
	inFrame(t, ctx, 0, `return [...d.querySelectorAll(".diff-file")].map((s) => s.id);`, &shown)
	var current string
	inFrame(t, ctx, 0, `const c = d.querySelector(".diff-tree [aria-current='true']"); return c ? c.textContent : "";`, &current)
	if len(shown) != 1 || shown[0] != "diff-file-0" || !strings.Contains(current, "README.md") {
		t.Errorf("after picking README.md the diff shows %v and the tree marks %q, want README.md alone, marked", shown, current)
	}
}

// A selected line is commented on through add_pr_comment, anchored to the
// last line selected on its side, and the view shows the comment under it.
func TestASelectedLineIsCommentedOn(t *testing.T) {
	now := time.Now().UnixMilli()
	added := viewThread{ID: 9, Author: "Alice Smith", CreatedDate: now, Text: "Say which rounding.", URL: threadURL(9), Place: "new:10",
		Anchor: &pullrequestactivityservice.Anchor{Path: "internal/ledger/ledger.go", Line: 10, LineType: "ADDED"}}
	after := commentedDiff(time.Now(), "two", commentedThreads(added))
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, commentedDiff(time.Now(), "one", commentedThreads())),
			ToolResults: map[string][]json.RawMessage{
				"add_pr_comment": {toolAnswer(t, `{"comment":{"id":9}}`, false)},
				"refresh_view":   {refreshAnswer(t, &after, "changed")},
			}},
	})
	openTreeFile(t, ctx, 0, "ledger.go", 0)

	var clicked bool
	inFrame(t, ctx, 0, `const b = [...d.querySelectorAll("#diff-file-0 tr.add .line-number")].find((b) => b.textContent === "10"); if (b) b.click(); return Boolean(b);`, &clicked)
	if !clicked {
		t.Fatal("added line 10 has no line number to select")
	}
	inFrame(t, ctx, 0, `const b = [...d.querySelectorAll("#selection-bar button")].find((b) => b.textContent.trim() === "Comment"); if (b) b.click(); return Boolean(b);`, &clicked)
	if !clicked {
		t.Fatal("the selection bar has no Comment button")
	}
	waitInFrame(t, ctx, 0, `d.activeElement && (d.activeElement.dataset.draft || "").startsWith("line|internal/ledger/ledger.go|new:10")`, "the box to comment on line 10 did not open with the caret in it")
	typeInto(t, ctx, 0, `[data-draft="line|internal/ledger/ledger.go|new:10"]`, "Say which rounding.")
	inFrame(t, ctx, 0, `const b = d.querySelector("tr.diff-draft .send-button"); b.click(); return true;`, &clicked)
	waitInFrame(t, ctx, 0, `!d.querySelector("tr.diff-draft") && [...d.querySelectorAll("tr.diff-thread")].some((r) => r.innerText.includes("Say which rounding."))`, "the comment is not shown under its line once sent")

	calls := toolCalls(t, ctx, 0, "add_pr_comment")
	if len(calls) != 1 {
		t.Fatalf("the view called add_pr_comment %d times, want once: %v", len(calls), calls)
	}
	for key, want := range map[string]any{"project": "PAY", "repo": "ledger", "pr_id": "42", "path": "internal/ledger/ledger.go", "line": float64(10), "line_type": "ADDED", "text": "Say which rounding."} {
		if calls[0][key] != want {
			t.Errorf("the comment sent %s = %v, want %v (all: %v)", key, calls[0][key], want, calls[0])
		}
	}
	var offered bool
	inFrame(t, ctx, 0, `return Boolean(d.querySelector(".refresh-notice"));`, &offered)
	if offered {
		t.Error("a diff whose lines did not change offers itself anew instead of showing the comment in place")
	}
}

// Reply under a comment in the diff answers that comment.
func TestAReplyInTheDiffAnswersItsComment(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, commentedDiff(time.Now(), "one", commentedThreads())),
			ToolResults: map[string][]json.RawMessage{
				"add_pr_comment": {toolAnswer(t, `{"comment":{"id":40}}`, false)},
				"refresh_view":   {refreshAnswer(t, nil, "unchanged")},
			}},
	})
	openTreeFile(t, ctx, 0, "ledger.go", 0)
	var clicked bool
	inFrame(t, ctx, 0, `d.querySelector("#thread-3 .comment-folded").click(); return true;`, &clicked)
	waitInFrame(t, ctx, 0, `d.querySelectorAll("#thread-3 .text-action").length === 2`, "the opened thread has no Reply under each of its comments")
	inFrame(t, ctx, 0, `d.querySelectorAll("#thread-3 .text-action")[1].click(); return true;`, &clicked)
	typeInto(t, ctx, 0, `[data-draft="reply-31"]`, "Thanks.")
	inFrame(t, ctx, 0, `d.querySelector("#thread-3 .send-button").click(); return true;`, &clicked)
	waitInFrame(t, ctx, 0, `!d.querySelector('[data-draft="reply-31"]')`, "the reply box did not close once sent")
	calls := toolCalls(t, ctx, 0, "add_pr_comment")
	if len(calls) != 1 || calls[0]["parent_id"] != float64(31) || calls[0]["text"] != "Thanks." {
		t.Errorf("the reply sent %v, want Thanks. answering comment 31", calls)
	}
}

// A diff read a while ago whose lines did not change, only its comments,
// changes in place: nothing is offered, and the new comment shows.
func TestADiffWhoseCommentsChangedChangesInPlace(t *testing.T) {
	now := time.Now().UnixMilli()
	added := viewThread{ID: 10, Author: "Bob Chen", CreatedDate: now, Text: "Another thought.", URL: threadURL(10), Place: "new:9",
		Anchor: &pullrequestactivityservice.Anchor{Path: "internal/ledger/ledger.go", Line: 9, LineType: "ADDED"}}
	after := commentedDiff(time.Now(), "two", commentedThreads(added))
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff read two hours ago", Mode: "inline", Fullscreen: false, Result: fixtureResult(t, commentedDiff(time.Now().Add(-2*time.Hour), "one", commentedThreads())),
			ToolResults: answers(refreshAnswer(t, &after, "changed"))},
	})

	clickButton(t, ctx, 0, "ledger.go")
	waitInFrame(t, ctx, 0, `[...d.querySelectorAll("tr.diff-thread")].some((r) => r.innerText.includes("Another thought."))`, "the new comment did not appear in place")
	var offered, open bool
	inFrame(t, ctx, 0, `return Boolean(d.querySelector(".refresh-notice"));`, &offered)
	inFrame(t, ctx, 0, `return Boolean(d.querySelector(".inline-file"));`, &open)
	if offered || !open {
		t.Errorf("after a refresh that changed only comments: offered anew = %v, the file the person opened still open = %v; want false and true", offered, open)
	}
}

// A comment on a line is numbered on the side Bitbucket numbers it on: a
// removed line as the file was, and a line both sides have as it is now,
// whatever it was numbered before.
func TestACommentOnALineIsNumberedOnItsSide(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, commentedDiff(time.Now(), "one", commentedThreads())),
			ToolResults: map[string][]json.RawMessage{
				"add_pr_comment": {toolAnswer(t, `{"comment":{"id":20}}`, false)},
				"refresh_view":   {refreshAnswer(t, nil, "unchanged")},
			}},
	})
	openTreeFile(t, ctx, 0, "ledger.go", 0)

	comment := func(rowClass, number, text string) {
		t.Helper()
		var clicked bool
		inFrame(t, ctx, 0, `const b = [...d.querySelectorAll("#diff-file-0 tr.`+rowClass+`:not(.diff-thread) .line-number")].find((b) => b.textContent === "`+number+`"); if (b) b.click(); return Boolean(b);`, &clicked)
		if !clicked {
			t.Fatalf("no %s line numbered %s to select", rowClass, number)
		}
		inFrame(t, ctx, 0, `const b = [...d.querySelectorAll("#selection-bar button")].find((b) => b.textContent.trim() === "Comment"); if (b) b.click(); return Boolean(b);`, &clicked)
		if !clicked {
			t.Fatal("the selection bar has no Comment button")
		}
		typeInto(t, ctx, 0, `tr.diff-draft textarea`, text)
		inFrame(t, ctx, 0, `d.querySelector("tr.diff-draft .send-button").click(); return true;`, &clicked)
		waitInFrame(t, ctx, 0, `!d.querySelector("tr.diff-draft")`, "the comment box did not close once sent")
	}

	// The removed line 9, and the closing brace: line 10 before, 11 now.
	comment("del", "9", "Why was this truncating?")
	comment("context", "11", "Close enough.")
	calls := toolCalls(t, ctx, 0, "add_pr_comment")
	if len(calls) != 2 {
		t.Fatalf("the view called add_pr_comment %d times, want twice: %v", len(calls), calls)
	}
	if calls[0]["line"] != float64(9) || calls[0]["line_type"] != "REMOVED" {
		t.Errorf("the comment on the removed line sent line %v %v, want 9 REMOVED", calls[0]["line"], calls[0]["line_type"])
	}
	if calls[1]["line"] != float64(11) || calls[1]["line_type"] != "CONTEXT" {
		t.Errorf("the comment on the unchanged line sent line %v %v, want 11 CONTEXT", calls[1]["line"], calls[1]["line_type"])
	}
}

// A diff draws each code line in the colours bb highlighted it with, the
// spans matched to its lines in the order the patch has them, a line that
// is not code skipped.
func TestADiffDrawsItsCodeHighlighted(t *testing.T) {
	patch := strings.Join([]string{
		"diff --git a/ledger.go b/ledger.go",
		"--- a/ledger.go",
		"+++ b/ledger.go",
		"@@ -1,2 +1,2 @@",
		" package ledger",
		"-var x = 1",
		`\ No newline at end of file`,
		"+var x = 2",
		"",
	}, "\n")
	diff := refreshDiffPayload(patch, time.Now(), "diff")
	withHighlights(&diff, viewOffers{})
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, diff)},
	})

	var rows []string
	inFrame(t, ctx, 0, `return [...d.querySelectorAll("#diff-file-0 tr.context, #diff-file-0 tr.add, #diff-file-0 tr.del")].map((r) =>
		[...r.querySelectorAll(".code [class^='hl-']")].map((s) => s.className + ":" + s.textContent).join(" "));`, &rows)
	want := []string{"hl-k:package", "hl-k:var hl-n:1", "hl-k:var hl-n:2"}
	if len(rows) != len(want) {
		t.Fatalf("the diff draws %d code lines, want %d: %q", len(rows), len(want), rows)
	}
	for i := range want {
		if rows[i] != want[i] {
			t.Errorf("code line %d is coloured %q, want %q", i+1, rows[i], want[i])
		}
	}
}
