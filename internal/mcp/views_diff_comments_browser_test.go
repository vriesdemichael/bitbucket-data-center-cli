//go:build views

package mcp

import (
	"encoding/json"
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

func commentedThreads(extra ...viewThread) viewThreads {
	now := time.Now().UnixMilli()
	ledger := "internal/ledger/ledger.go"
	threads := []viewThread{
		{ID: 1, Task: true, Author: "Carol Diaz", CreatedDate: now - 3600000, Text: "Cap the time a charge spends retrying.", URL: threadURL(1)},
		{ID: 2, Author: "Bob Chen", CreatedDate: now - 7200000, Text: "Round half to even?", URL: threadURL(2),
			Anchor: &pullrequestactivityservice.Anchor{Path: ledger, Line: 9, LineType: "ADDED"}},
		{ID: 3, Resolved: true, Author: "Dave Okafor", CreatedDate: now - 9000000, Text: "This truncated.", URL: threadURL(3),
			Anchor: &pullrequestactivityservice.Anchor{Path: ledger, Line: 9, LineType: "REMOVED"}},
		{ID: 4, Author: "Erin Walsh", CreatedDate: now - 9500000, Text: "Keep a copy somewhere?", URL: threadURL(4),
			Anchor: &pullrequestactivityservice.Anchor{Path: "docs/old-notes.md"}},
		{ID: 5, Author: "Alice Smith", CreatedDate: now - 600000, Text: "The caller rounds too.", URL: threadURL(5),
			Anchor: &pullrequestactivityservice.Anchor{Path: ledger, Line: 99, LineType: "CONTEXT"}},
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

// A diff draws each comment where it is: under its line, on the side it was
// written on; at the top of its file when it is on the file or on a line the
// diff does not draw; and the pull request's own over the files, folded.
func TestADiffDrawsItsCommentsWhereTheyAre(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, commentedDiff(time.Now(), "one", commentedThreads())),
			ToolResults: answers(refreshAnswer(t, nil, "unchanged"))},
	})

	var under map[string]string
	inFrame(t, ctx, 0, `const rows = [...d.querySelectorAll("#diff-file-0 tr")];
		const after = (type, n) => { const i = rows.findIndex((r) => r.classList.contains(type) && [...r.querySelectorAll(".line-number")].some((b) => b.textContent === String(n)));
			return i >= 0 && rows[i + 1] && rows[i + 1].classList.contains("diff-thread") ? rows[i + 1].innerText : ""; };
		return { added: after("add", 9), removed: after("del", 9) };`, &under)
	if !strings.Contains(under["added"], "Round half to even?") {
		t.Errorf("under added line 9 is %q, want the comment on it", under["added"])
	}
	if !strings.Contains(under["removed"], "This truncated.") {
		t.Errorf("under removed line 9 is %q, want the comment on the old side", under["removed"])
	}

	var tops map[string]string
	inFrame(t, ctx, 0, `const top = (i) => { const f = d.querySelector("#diff-file-" + i + " .file-threads"); return f ? f.innerText : ""; };
		return { ledger: top(0), notes: top(1) };`, &tops)
	if !strings.Contains(tops["ledger"], "The caller rounds too.") || strings.Contains(tops["ledger"], "Round half to even?") {
		t.Errorf("the top of ledger.go reads %q, want the comment on a line the diff does not draw, and only that", tops["ledger"])
	}
	if !strings.Contains(tops["notes"], "Keep a copy somewhere?") {
		t.Errorf("the top of old-notes.md reads %q, want the comment on the file", tops["notes"])
	}

	var tree string
	inFrame(t, ctx, 0, `return d.querySelector(".diff-tree").innerText;`, &tree)
	if !strings.Contains(tree, "2") {
		t.Errorf("the file tree reads %q, want ledger.go's two open comments counted", tree)
	}

	if strings.Contains(frameText(t, ctx, 0), "Cap the time a charge spends retrying.") {
		t.Error("the pull request's own comment shows before its section is opened")
	}
	clickButton(t, ctx, 0, "1 comment on the pull request")
	waitInFrame(t, ctx, 0, `d.querySelector(".diff-pr-threads") && d.querySelector(".diff-pr-threads").innerText.includes("Cap the time a charge spends retrying.")`, "the pull request's comments do not open")
}

// A selected line is commented on through add_pr_comment, anchored to the
// last line selected on its side, and the view shows the comment under it.
func TestASelectedLineIsCommentedOn(t *testing.T) {
	now := time.Now().UnixMilli()
	added := viewThread{ID: 9, Author: "Alice Smith", CreatedDate: now, Text: "Say which rounding.", URL: threadURL(9),
		Anchor: &pullrequestactivityservice.Anchor{Path: "internal/ledger/ledger.go", Line: 10, LineType: "ADDED"}}
	after := commentedDiff(time.Now(), "two", commentedThreads(added))
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, commentedDiff(time.Now(), "one", commentedThreads())),
			ToolResults: map[string][]json.RawMessage{
				"add_pr_comment": {toolAnswer(t, `{"comment":{"id":9}}`, false)},
				"refresh_view":   {refreshAnswer(t, &after, "changed")},
			}},
	})

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

// A diff read a while ago whose lines did not change, only its comments,
// changes in place: nothing is offered, and the new comment shows.
func TestADiffWhoseCommentsChangedChangesInPlace(t *testing.T) {
	now := time.Now().UnixMilli()
	added := viewThread{ID: 10, Author: "Bob Chen", CreatedDate: now, Text: "Another thought.", URL: threadURL(10),
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
