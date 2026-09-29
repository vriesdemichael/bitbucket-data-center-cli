package mcp

import (
	"strings"
	"testing"

	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

// One file's diff, with two hunks: an added, a removed and an unchanged line
// each to anchor a comment to. Its lines are quoted, because git writes a
// blank unchanged line as a single space, which an editor strips from the end
// of a line.
var threadContextPatch = strings.Join([]string{
	"diff --git a/client.go b/client.go",
	"--- a/client.go",
	"+++ b/client.go",
	"@@ -1,5 +1,6 @@",
	" package payments",
	" ",
	"-// Charge charges.",
	"+// Charge charges an amount.",
	"+// A transient failure is retried.",
	" func Charge() {",
	" \tcall()",
	"@@ -20,4 +21,4 @@ func Refund() {",
	" \tone()",
	" \ttwo()",
	"-\tthree()",
	"+\tfour()",
	"",
}, "\n")

func TestAThreadCarriesTheDiffLeadingToItsLine(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		line     int
		lineType string
		want     []string
	}{
		// An added line is numbered in the new file.
		{"added", 3, "ADDED", []string{"context:package payments", "context:", "del:// Charge charges.", "add:// Charge charges an amount."}},
		// A removed line is numbered in the old file.
		{"removed", 22, "REMOVED", []string{"context:one()", "context:two()", "del:three()"}},
		// An unchanged line is numbered in the new file, and what leads to it
		// stays within its hunk.
		{"context in the second hunk", 22, "CONTEXT", []string{"context:one()", "context:two()"}},
		{"unchanged, typed as nothing", 5, "", []string{"del:// Charge charges.", "add:// Charge charges an amount.", "add:// A transient failure is retried.", "context:func Charge() {"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			context := contextAt(threadContextPatch, tc.line, tc.lineType)
			got := make([]string, len(context))
			for i, line := range context {
				got[i] = line.Type + ":" + strings.TrimSpace(line.Text)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("context = %v, want %v", got, tc.want)
			}
			if len(context) > 0 && !context[len(context)-1].Anchor {
				t.Error("the anchored line is not marked")
			}
			for _, line := range context[:max(0, len(context)-1)] {
				if line.Anchor {
					t.Errorf("a line leading to the anchor is marked: %+v", line)
				}
			}
		})
	}
}

func TestAThreadOnALineTheDiffLacksHasNoContext(t *testing.T) {
	t.Parallel()

	if context := contextAt(threadContextPatch, 99, "ADDED"); context != nil {
		t.Errorf("a line the diff does not have got context %v", context)
	}
	// Line 4 of the old file is unchanged, so no removed line has that number.
	if context := contextAt(threadContextPatch, 4, "REMOVED"); context != nil {
		t.Errorf("line 4 of the old file is not a removed line, got %v", context)
	}
}

func TestAViewSaysWhatItCutOfALongComment(t *testing.T) {
	t.Parallel()

	short := "fine as it is"
	if text, cut := cutComment(short); text != short || cut {
		t.Errorf("a short comment came back %q, cut %v", text, cut)
	}
	// A multi-byte character straddles the cut, which falls before it.
	long := strings.Repeat("a", maxViewCommentBytes-1) + "é" + strings.Repeat("b", 10)
	text, cut := cutComment(long)
	if !cut || len(text) != maxViewCommentBytes-1 || !strings.HasSuffix(text, "a") {
		t.Errorf("a long comment came back %d bytes ending %q, cut %v; want it cut before the é", len(text), text[len(text)-3:], cut)
	}

	line := strings.Repeat("x", maxContextLineRunes) + strings.Repeat("é", 12)
	if kept, more := cutLine(line); kept != strings.Repeat("x", maxContextLineRunes) || more != 12 {
		t.Errorf("a long diff line kept %d bytes and counted %d more, want %d and 12", len(kept), more, maxContextLineRunes)
	}
}

func TestTheThreadsSummaryCountsTheWhole(t *testing.T) {
	t.Parallel()

	in := ShowInput{Kind: showKindThreads, Project: "PAY", Repo: "ledger", ID: "7"}
	threads := viewThreads{
		Summary: pullrequestactivityservice.Summary{TotalThreads: 240, Unresolved: 12, Resolved: 226, Pending: 2, OpenTasks: 3},
		Threads: make([]viewThread, 200),
	}
	got := summarizeThreads(in, "Retry payments", threads).shown()
	for _, want := range []string{"PAY/ledger#7", `"Retry payments"`, "12 unresolved", "3 of them open tasks", "226 resolved", "2 pending", "the first 200 of the 240 threads"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q lacks %q", got, want)
		}
	}
}
