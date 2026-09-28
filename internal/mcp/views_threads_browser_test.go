//go:build views

// The threads view, drawn by a real browser, as views_browser_test.go draws
// the others.

package mcp

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

const (
	hostileComment = "Please fix.\n\n<script>window.parent.bbHost.pwned='comment'</script>\n<img src=x onerror=\"window.parent.bbHost.pwned='comment-image'\">"
	hostilePath    = `docs/<img src=x onerror="window.parent.bbHost.pwned='path'">.md`
	threadsURL     = "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview"
)

// The fixture frames, by what they show.
const (
	threadsInline = iota
	threadsFullscreen
	threadsStress
	threadsStressPhone
	threadsStressInPlace
	threadsStressFullscreen
)

func threadURL(id int64) string {
	return fmt.Sprintf("https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview?commentId=%d", id)
}

// fixtureThreads is a pull request's threads as reviewers leave them: a task
// on the pull request, a discussion on a changed line, a resolved thread, a
// done task, and text someone wrote to run a script.
func fixtureThreads() viewThreads {
	now := time.Now().UnixMilli()
	replies := make([]viewReply, 5)
	for i := range replies {
		replies[i] = viewReply{Author: "Bob Chen", AuthorUsername: "bob", Date: now - int64(5-i)*60000, Text: fmt.Sprintf("Reply %d.", i+1)}
	}
	return viewThreads{
		Summary: pullrequestactivityservice.Summary{TotalThreads: 6, Unresolved: 4, Resolved: 2, OpenTasks: 1, ResolvedTasks: 1},
		Threads: []viewThread{
			{ID: 1, Task: true, Author: "Carol Diaz", AuthorUsername: "carol", CreatedDate: now - 3600000, Text: "Cap the time a charge spends retrying.", URL: threadURL(1)},
			{ID: 2, Author: "Bob Chen", AuthorUsername: "bob", CreatedDate: now - 7200000, Text: "Should the cap be configurable?", URL: threadURL(2),
				Anchor:  &pullrequestactivityservice.Anchor{Path: "internal/ledger/ledger.go", Line: 9, LineType: "ADDED"},
				Context: []viewContextLine{{Type: "context", Old: 8, New: 8, Text: "// Refund returns the entry that reverses e."}, {Type: "add", New: 9, Text: "\treturn Entry{}", Anchor: true}},
				Replies: replies},
			{ID: 3, Resolved: true, Author: "Dave Okafor", AuthorUsername: "dave", CreatedDate: now - 9000000, Text: "Why drop the timeout?", URL: threadURL(3),
				Anchor: &pullrequestactivityservice.Anchor{Path: "internal/ledger/ledger.go", Line: 12, LineType: "REMOVED"}},
			{ID: 4, Task: true, Resolved: true, Author: "Erin Walsh", AuthorUsername: "erin", CreatedDate: now - 9500000, Text: "Typo in the README.", URL: threadURL(4),
				Anchor: &pullrequestactivityservice.Anchor{Path: "README.md", Line: 3, LineType: "CONTEXT"}},
			{ID: 5, Author: hostileName, AuthorUsername: "mallory", CreatedDate: now - 600000, Text: hostileComment, URL: "javascript:window.parent.bbHost.pwned='thread-link'",
				Anchor:  &pullrequestactivityservice.Anchor{Path: hostilePath, Line: 2, LineType: "ADDED"},
				Context: []viewContextLine{{Type: "add", New: 2, Text: hostileCode, Anchor: true}}},
			{ID: 6, Author: "Alice Smith", AuthorUsername: "alice", CreatedDate: now - 300000, Text: strings.Repeat("A long comment. ", 40), TextCut: true, URL: threadURL(6)},
		},
	}
}

// stressThreads is more threads than a view carries: 200 of 240, the open
// ones on long paths with long first lines.
func stressThreads() viewThreads {
	now := time.Now().UnixMilli()
	threads := viewThreads{Summary: pullrequestactivityservice.Summary{TotalThreads: 240, Unresolved: 160, Resolved: 80, OpenTasks: 30, ResolvedTasks: 10}}
	for i := range 200 {
		thread := viewThread{
			ID:          int64(1000 + i),
			Task:        i < 30,
			Resolved:    i >= 150,
			Author:      fmt.Sprintf("Reviewer With A Rather Long Display Name %03d", i),
			CreatedDate: now - int64(i)*60000,
			Text:        fmt.Sprintf("Comment %03d: %s", i, strings.Repeat("a-very-long-token-without-spaces-", 12)),
			URL:         threadURL(int64(1000 + i)),
			Anchor: &pullrequestactivityservice.Anchor{
				Path: fmt.Sprintf("services/payments/internal/providers/generated/very/deeply/nested/directory/%02d/handler_%03d.go", i%20, i),
				Line: i + 1, LineType: "ADDED",
			},
		}
		threads.Threads = append(threads.Threads, thread)
	}
	return threads
}

func threadsFrames(t *testing.T) []viewhost.Frame {
	t.Helper()
	pr := viewPullRequest{PullRequest: fixturePullRequest(), URL: threadsURL}
	avatars := map[string]string{"bob": onePixelPNG, "carol": onePixelPNG}
	arguments := map[string]any{"kind": "threads", "project": "PAY", "repo": "ledger", "id": "42"}
	threads, stress := fixtureThreads(), stressThreads()
	payload := func(threads viewThreads) viewPayload {
		return viewPayload{Kind: showKindThreads, PullRequest: &pr, Threads: &threads, Avatars: avatars}
	}
	return []viewhost.Frame{
		{Title: "threads inline", Mode: "inline", Fullscreen: true, Arguments: arguments, Result: fixtureResult(t, payload(threads))},
		{Title: "threads fullscreen", Mode: "fullscreen", Fullscreen: true, Arguments: arguments, Result: fixtureResult(t, payload(threads))},
		{Title: "threads, 200 of 240", Mode: "inline", Fullscreen: true, Arguments: arguments, Result: fixtureResult(t, payload(stress))},
		{Title: "threads, 200 of 240, on a phone", Mode: "inline", Fullscreen: true, Width: 380, Arguments: arguments, Result: fixtureResult(t, payload(stress))},
		{Title: "threads, 200 of 240, in a host without fullscreen", Mode: "inline", Fullscreen: false, Arguments: arguments, Result: fixtureResult(t, payload(stress))},
		{Title: "threads, 200 of 240, fullscreen", Theme: "dark", Mode: "fullscreen", Fullscreen: true, Arguments: arguments, Result: fixtureResult(t, payload(stress))},
	}
}

func TestThreadsDrawTextOthersWroteAsText(t *testing.T) {
	ctx := browser(t, threadsFrames(t))

	for _, frame := range []int{threadsInline, threadsFullscreen} {
		var escaped bool
		inFrame(t, ctx, frame, `return !d.querySelector("img[src='x'], script:not(:first-of-type), b[onmouseover]");`, &escaped)
		if !escaped {
			t.Errorf("frame %d built an element from text someone wrote", frame)
		}
	}
	// textContent, as written: innerText breaks a path's parts onto lines.
	var page string
	inFrame(t, ctx, threadsFullscreen, `return d.body.textContent;`, &page)
	for _, want := range []string{hostileName, hostilePath, hostileCode, "<script>window.parent.bbHost.pwned='comment'</script>"} {
		if !strings.Contains(page, want) {
			t.Errorf("the threads do not show %q as written:\n%s", want, page)
		}
	}

	// A thread whose link is not a web address gets no button to open it.
	var buttons int
	inFrame(t, ctx, threadsFullscreen, `return d.querySelectorAll("#thread-5 .icon-button").length;`, &buttons)
	if buttons != 0 {
		t.Errorf("a thread with a javascript: link has %d buttons to open it, want none", buttons)
	}

	var pwned any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.bbHost.pwned || null`, &pwned)); err != nil {
		t.Fatal(err)
	}
	if pwned != nil {
		t.Errorf("text someone wrote ran as a script: %v", pwned)
	}
}

func TestThreadsCountTheWhole(t *testing.T) {
	ctx := browser(t, threadsFrames(t))

	card := frameText(t, ctx, threadsStress)
	for _, want := range []string{"30 open tasks", "130 unresolved comments", "80 resolved comments", "10 tasks done", "and 156 more open comments, in full screen"} {
		if !strings.Contains(card, want) {
			t.Errorf("the threads card does not say %q:\n%s", want, card)
		}
	}
	page := frameText(t, ctx, threadsStressFullscreen)
	if !strings.Contains(page, "This view carries 200 of the 240 comment threads.") {
		t.Errorf("the threads page carries fewer threads than there are without saying so:\n%s", page)
	}
}

func TestThreadsNeverHideWhatNeedsAttention(t *testing.T) {
	ctx := browser(t, threadsFrames(t))

	// The card lists what is open where it is, as the full view does: the
	// pull request's own threads first, then each file's, by path, under a
	// heading each.
	var card []string
	inFrame(t, ctx, threadsInline, `return [...d.querySelectorAll(".thread-list > li")].map((li) => li.classList.contains("thread-place") ? "@" + li.textContent : li.querySelector(".thread-excerpt").textContent.slice(0, 14));`, &card)
	want := []string{"@On the pull request", "Cap the time a", "A long comment", "@" + hostilePath, "Please fix.", "@internal/ledger/ledger.go", "Should the cap"}
	if strings.Join(card, "|") != strings.Join(want, "|") {
		t.Errorf("the card lists\n  %v\nwant\n  %v", card, want)
	}

	// In fullscreen every open thread is out, and the resolved ones fold to
	// their count.
	var open, resolved []string
	inFrame(t, ctx, threadsFullscreen, `return [...d.querySelectorAll(".thread-group > article.thread")].map((a) => a.id);`, &open)
	inFrame(t, ctx, threadsFullscreen, `return [...d.querySelectorAll(".thread-group > .group > .fold")].map((b) => b.textContent);`, &resolved)
	for _, id := range []string{"thread-1", "thread-2", "thread-5", "thread-6"} {
		if !containsString(open, id) {
			t.Errorf("open thread %s is not out in fullscreen: %v", id, open)
		}
	}
	for _, id := range []string{"thread-3", "thread-4"} {
		if containsString(open, id) {
			t.Errorf("resolved thread %s is out, want it folded", id)
		}
	}
	if len(resolved) != 2 || !containsString(resolved, "1 resolved comment") {
		t.Errorf("the resolved threads fold as %v, want one fold of 1 in each of their groups", resolved)
	}

	// A long discussion folds its earlier replies and shows the latest.
	var replies string
	inFrame(t, ctx, threadsFullscreen, `const r = d.querySelector("#thread-2 .replies"); return r ? r.textContent : "";`, &replies)
	if !strings.Contains(replies, "2 earlier replies") || strings.Contains(replies, "Reply 2.") || !strings.Contains(replies, "Reply 5.") {
		t.Errorf("thread 2's replies read %q, want the two earlier folded and the last three out", replies)
	}

	// A comment cut short says so, and links to the rest.
	if page := frameText(t, ctx, threadsFullscreen); !strings.Contains(page, "This comment is longer than a view carries.") {
		t.Errorf("a cut comment does not say so:\n%s", page)
	}
}

func TestThreadsStayInBounds(t *testing.T) {
	ctx := browser(t, threadsFrames(t))
	chromedp.Run(ctx, chromedp.Sleep(300*time.Millisecond))

	for frame := range len(threadsFrames(t)) {
		var wide bool
		inFrame(t, ctx, frame, `return d.documentElement.scrollWidth > d.documentElement.clientWidth + 1;`, &wide)
		if wide {
			t.Errorf("frame %d scrolls sideways: something long broke its layout", frame)
		}
	}
	for _, tc := range []struct {
		frame, rows int
	}{{threadsStress, 4}, {threadsStressPhone, 3}, {threadsStressInPlace, 4}} {
		var rows int
		var height float64
		inFrame(t, ctx, tc.frame, `return d.querySelectorAll(".thread-row").length;`, &rows)
		inFrame(t, ctx, tc.frame, `return f.iframe.getBoundingClientRect().height;`, &height)
		if rows != tc.rows {
			t.Errorf("frame %d lists %d threads inline, want %d", tc.frame, rows, tc.rows)
		}
		if height > 900 || height < 100 {
			t.Errorf("inline frame %d is %.0fpx tall, want it drawn and within 900px", tc.frame, height)
		}
	}

	// In a host without fullscreen the list grows by a step.
	clickButton(t, ctx, threadsStressInPlace, "Show 25 more open comments")
	var rows int
	inFrame(t, ctx, threadsStressInPlace, `return d.querySelectorAll(".thread-row").length;`, &rows)
	if rows != 29 {
		t.Errorf("a step shows %d threads, want 29", rows)
	}
}

func TestThreadsOpenThroughTheHost(t *testing.T) {
	ctx := browser(t, threadsFrames(t))

	// A thread's own link opens through the host.
	inFrame(t, ctx, threadsFullscreen, `const b = d.querySelector("#thread-2 .icon-button"); if (b) b.click(); return true;`, nil)
	opened := hostMessages(t, ctx, threadsFullscreen, "ui/open-link")
	found := false
	for _, message := range opened {
		if params, _ := message["params"].(map[string]any); params["url"] == threadURL(2) {
			found = true
		}
	}
	if !found {
		t.Errorf("opening thread 2 asked the host for %v, want %s", opened, threadURL(2))
	}

	// A thread in the card opens the threads in fullscreen, at that thread.
	inFrame(t, ctx, threadsInline, `const r = d.querySelector(".thread-row"); if (r) r.click(); return true;`, nil)
	var at bool
	if err := chromedp.Run(ctx, chromedp.Poll(fmt.Sprintf(`(() => { const d = window.bbHost.frames[%d].iframe.contentDocument; return d.querySelector(".threads-main") !== null && d.getElementById("thread-1") !== null; })()`, threadsInline), &at,
		chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Errorf("a thread picked in the card did not open the threads in fullscreen: %v", err)
	}
}

// Bitbucket's thread counts include the tasks. A pull request's card and
// overview count a task as a task and not as a comment too, as the threads
// view does: 7 unresolved threads, 2 of them tasks, are 2 open tasks and 5
// unresolved comments.
func TestTheCardCountsATaskOnce(t *testing.T) {
	count := func(n int) *int { return &n }
	pr := viewPullRequest{
		PullRequest: fixturePullRequest(),
		URL:         threadsURL,
		ReviewSummary: &pullrequestservice.ReviewSummary{
			UnresolvedThreads: count(7), OpenTasks: count(2), ResolvedThreads: count(2), ResolvedTasks: count(1),
		},
	}
	arguments := map[string]any{"kind": "pull_request", "project": "PAY", "repo": "ledger", "id": "42"}
	payload := fixtureResult(t, viewPayload{Kind: showKindPullRequest, PullRequest: &pr})
	ctx := browser(t, []viewhost.Frame{
		{Title: "card", Mode: "inline", Fullscreen: true, Arguments: arguments, Result: payload},
		{Title: "overview", Mode: "fullscreen", Fullscreen: true, Arguments: arguments, Result: payload},
	})

	card := frameText(t, ctx, 0)
	if !strings.Contains(card, "2 open tasks") || !strings.Contains(card, "5 unresolved comments") || strings.Contains(card, "7 unresolved") {
		t.Errorf("the card counts %q, want 2 open tasks and 5 unresolved comments", card)
	}
	var rows []string
	inFrame(t, ctx, 1, `return [...d.querySelectorAll(".details-list li")].map((li) => li.textContent);`, &rows)
	for _, want := range []string{"Tasks2 open · 1 resolved", "Comments5 unresolved · 1 resolved"} {
		if !containsString(rows, want) {
			t.Errorf("the overview's details are %v, want %q", rows, want)
		}
	}
}
