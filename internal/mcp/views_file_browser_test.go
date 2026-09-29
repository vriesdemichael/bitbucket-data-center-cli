//go:build views

package mcp

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
)

// viewing is what a server that shows files offers its views.
var viewing = &viewOffers{Kinds: []string{showKindPullRequest, showKindDiff, showKindFile}}

func filePayload(file viewFile) viewPayload {
	return viewPayload{
		Kind: showKindFile, GeneratedAt: time.Now().UTC().Format(time.RFC3339), File: &file,
		Show: &ShowInput{Kind: showKindFile, Project: "PAY", Repo: "ledger", Path: file.Path, At: file.At}, Fingerprint: "file", Offers: viewing,
	}
}

// A text file is drawn as numbered lines coloured as bb highlighted them, and
// the next window is read and drawn after them when the person asks.
func TestAFileIsDrawnAsHighlightedLinesAWindowAtATime(t *testing.T) {
	first := viewFile{
		Path: "internal/ledger/ledger.go", At: "master", URL: "https://bitbucket.example.com/projects/PAY/repos/ledger/browse/internal/ledger/ledger.go",
		Kind: "text", MIMEType: "text/plain; charset=utf-8", Size: 4096,
		Lines: "package ledger\n\n// Refund reverses é.\n", StartLine: 1, EndLine: 3, TotalLines: 5, NextLine: 4,
		Highlight: []string{"k7 t7", "", "c21"},
	}
	next := first
	next.Lines, next.StartLine, next.EndLine, next.NextLine, next.Highlight = "func Refund() {}\n}\n", 4, 5, 0, []string{"k4 t1 f6 t5", "t1"}
	nextPayload := filePayload(next)
	ctx := browser(t, []viewhost.Frame{
		{Title: "file fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, filePayload(first)),
			ToolResults: answers(refreshAnswer(t, &nextPayload, "opened"))},
	})

	var drawn map[string]any
	inFrame(t, ctx, 0, `const rows = [...d.querySelectorAll(".file-table tr")];
		return { numbers: rows.map((r) => r.querySelector(".line-number").textContent).join(","),
			keyword: (d.querySelector(".hl-k") || {}).textContent || "", comment: (d.querySelector(".hl-c") || {}).textContent || "" };`, &drawn)
	if drawn["numbers"] != "1,2,3" || drawn["keyword"] != "package" || drawn["comment"] != "// Refund reverses é." {
		t.Errorf("the file draws %v, want lines 1 to 3 with package as a keyword and the comment coloured whole", drawn)
	}

	clickButton(t, ctx, 0, "Show lines 4 on")
	waitInFrame(t, ctx, 0, `[...d.querySelectorAll(".file-table .line-number")].map((n) => n.textContent).join(",") === "1,2,3,4,5"`, "the next window is not drawn after the first")
	calls := refreshCalls(t, ctx, 0)
	if len(calls) != 1 || calls[0]["kind"] != "file" || calls[0]["start_line"] != float64(4) || calls[0]["path"] != first.Path {
		t.Errorf("the view read the next window with %v, want the file from line 4", calls)
	}
	var function string
	inFrame(t, ctx, 0, `return (d.querySelector(".hl-f") || {}).textContent || "";`, &function)
	if function != "Refund" {
		t.Errorf("the next window's function reads %q, want Refund coloured", function)
	}
}

// A picture is drawn from the data bb embedded, fitted to the view, and at
// its size on a click; audio plays from its data; a file that cannot be
// shown says what it is. Data that is not a picture or audio is never a
// source.
func TestAFileIsDrawnAsWhatItIs(t *testing.T) {
	ctx := browser(t, []viewhost.Frame{
		{Title: "picture", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, filePayload(viewFile{Path: "docs/logo.png", Kind: "image", MIMEType: "image/png", Size: 70, Data: onePixelPNG, Width: 1, Height: 1}))},
		{Title: "audio", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, filePayload(viewFile{Path: "sounds/ding.mp3", Kind: "audio", MIMEType: "audio/mpeg", Size: 4, Data: "data:audio/mpeg;base64,SUQzBA=="}))},
		{Title: "audio that is a page", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, filePayload(viewFile{Path: "sounds/trap.mp3", Kind: "audio", Size: 4, Data: "data:text/html;base64,PHNjcmlwdD4="}))},
		{Title: "a PDF", Mode: "inline", Fullscreen: true, Result: fixtureResult(t, filePayload(viewFile{Path: "docs/spec.pdf", Kind: "binary", Size: 90000, Description: "docs/spec.pdf is a PDF document of 88 KB, which is not shown here."}))},
	})

	var picture map[string]any
	inFrame(t, ctx, 0, `const i = d.querySelector(".file-figure img"); return { src: i ? i.getAttribute("src") : "", fitted: !d.querySelector(".file-figure.actual") };`, &picture)
	if picture["src"] != onePixelPNG || picture["fitted"] != true {
		t.Errorf("the picture is drawn as %v, want its data, fitted", picture)
	}
	var clicked bool
	inFrame(t, ctx, 0, `d.querySelector(".file-figure img").click(); return true;`, &clicked)
	waitInFrame(t, ctx, 0, `d.querySelector(".file-figure.actual")`, "a click does not show the picture at its size")

	var sources []string
	for frame := 1; frame <= 2; frame++ {
		var source string
		inFrame(t, ctx, frame, `const a = d.querySelector("audio"); return a ? (a.getAttribute("src") || "") : "no player";`, &source)
		sources = append(sources, source)
	}
	if sources[0] != "data:audio/mpeg;base64,SUQzBA==" || sources[1] != "" {
		t.Errorf("the players' sources are %q, want the audio's data, and none for data that is a page", sources)
	}
	if text := frameText(t, ctx, 3); !strings.Contains(text, "is a PDF document") {
		t.Errorf("a file that cannot be shown reads %q, want what it is", text)
	}
}

// A diff views a changed file as the pull request has it, in the same view.
func TestADiffViewsAFileAsThePullRequestHasIt(t *testing.T) {
	diff := refreshDiffPayload(commentedPatch, time.Now(), "diff")
	diff.Offers = viewing
	file := filePayload(viewFile{Path: "internal/ledger/ledger.go", Kind: "text", Lines: "package ledger\n", StartLine: 1, EndLine: 1, TotalLines: 1})
	ctx := browser(t, []viewhost.Frame{
		{Title: "diff fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, diff),
			ToolResults: map[string][]json.RawMessage{"refresh_view": {refreshAnswer(t, &file, "opened")}}},
	})

	var buttons int
	inFrame(t, ctx, 0, `return d.querySelectorAll(".view-file").length;`, &buttons)
	if buttons != 1 {
		t.Errorf("the diff offers %d files to view, want one: the deleted file has nothing to view", buttons)
	}
	var clicked bool
	inFrame(t, ctx, 0, `d.querySelector("#diff-file-0 .view-file").click(); return true;`, &clicked)
	waitInFrame(t, ctx, 0, `d.querySelector(".file-table") && d.body.innerText.includes("Back to the diff")`, "the file did not open in the view")
	calls := refreshCalls(t, ctx, 0)
	if len(calls) != 1 || calls[0]["kind"] != "file" || calls[0]["path"] != "internal/ledger/ledger.go" || calls[0]["at"] != fixturePullRequest().SourceCommit {
		t.Errorf("viewing the file asked %v, want the file at the pull request's source commit", calls)
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
	withHighlights(&diff)
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
