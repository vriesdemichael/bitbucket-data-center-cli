//go:build views

// A pull request's activity in its overview, drawn by a real browser, as
// views_browser_test.go draws the rest.

package mcp

import (
	"encoding/json"
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
	activityCard = iota
	activityFullscreen
	activityInPlace
	activityPhone
	activityStress
)

func threadURL(id int64) string {
	return fmt.Sprintf("https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview?commentId=%d", id)
}

// fixtureActivity is a pull request's activity as reviewers leave it, newest
// first: a long comment, text someone wrote to run a script, an approval, an
// open task, a request for changes, a discussion on a changed line, a
// resolved thread on a removed line, a done task on a file, a push and the
// opening. It carries ten of twelve items.
func fixtureActivity() viewActivity {
	now := time.Now().UnixMilli()
	ledger := "internal/ledger/ledger.go"
	replies := make([]viewReply, 5)
	for i := range replies {
		replies[i] = viewReply{ID: int64(20 + i), Author: "Bob Chen", AuthorUsername: "bob", Date: now - int64(5-i)*60000, Text: fmt.Sprintf("Reply %d.", i+1)}
	}
	thread := func(v viewThread) *viewThread { return &v }
	type anchor = pullrequestactivityservice.Anchor
	return viewActivity{Total: 12, Items: []viewActivityItem{
		{Action: "COMMENTED", Date: now - 300000, User: "Alice Smith", Username: "alice", Thread: thread(viewThread{
			ID: 6, Author: "Alice Smith", AuthorUsername: "alice", CreatedDate: now - 300000, Text: strings.Repeat("A long comment. ", 40), TextCut: true, URL: threadURL(6)})},
		{Action: "COMMENTED", Date: now - 600000, User: hostileName, Username: "mallory", Thread: thread(viewThread{
			ID: 5, Author: hostileName, AuthorUsername: "mallory", CreatedDate: now - 600000, Text: hostileComment, URL: "javascript:window.parent.bbHost.pwned='thread-link'",
			Anchor:  &anchor{Path: hostilePath, Line: 2, LineType: "ADDED"},
			Context: []viewContextLine{{Type: "add", New: 1, Text: "x"}, {Type: "add", New: 2, Text: hostileCode, Anchor: true}, {Type: "add", New: 3, Text: "y"}}})},
		{Action: "APPROVED", Date: now - 1800000, User: "Bob Chen", Username: "bob"},
		{Action: "COMMENTED", Date: now - 3600000, User: "Carol Diaz", Username: "carol", Thread: thread(viewThread{
			ID: 1, Task: true, Author: "Carol Diaz", AuthorUsername: "carol", CreatedDate: now - 3600000, Text: "Cap the time a charge spends retrying.", URL: threadURL(1)})},
		{Action: "REVIEWED", Date: now - 5000000, User: "Carol Diaz", Username: "carol"},
		{Action: "COMMENTED", Date: now - 7200000, User: "Bob Chen", Username: "bob", Thread: thread(viewThread{
			ID: 2, Author: "Bob Chen", AuthorUsername: "bob", CreatedDate: now - 7200000, Text: "Should the cap be configurable?", URL: threadURL(2),
			Anchor: &anchor{Path: ledger, Line: 9, LineType: "ADDED"},
			Context: []viewContextLine{
				{Type: "context", Old: 8, New: 8, Text: "// Refund returns the entry that reverses e."},
				{Type: "add", New: 9, Text: "\treturn Entry{}", Anchor: true},
				{Type: "context", Old: 9, New: 10, Text: "}"},
			},
			Replies: replies})},
		{Action: "COMMENTED", Date: now - 9000000, User: "Dave Okafor", Username: "dave", Thread: thread(viewThread{
			ID: 3, Resolved: true, Author: "Dave Okafor", AuthorUsername: "dave", CreatedDate: now - 9000000, Text: "Why drop the timeout?", URL: threadURL(3),
			Anchor:  &anchor{Path: ledger, Line: 12, LineType: "REMOVED"},
			Context: []viewContextLine{{Type: "del", Old: 12, Text: "\ttimeout := 10", Anchor: true}}})},
		{Action: "COMMENTED", Date: now - 9500000, User: "Erin Walsh", Username: "erin", Thread: thread(viewThread{
			ID: 4, Task: true, Resolved: true, Author: "Erin Walsh", AuthorUsername: "erin", CreatedDate: now - 9500000, Text: "Typo in the README.", URL: threadURL(4),
			Anchor: &anchor{Path: "README.md"}})},
		{Action: "RESCOPED", Date: now - 9800000, User: "Alice Smith", Username: "alice", Added: 2},
		{Action: "OPENED", Date: now - 10000000, User: "Alice Smith", Username: "alice"},
	}}
}

// stressActivity is more than a view carries: 200 of 240 items, long names
// and long comments on long paths.
func stressActivity() viewActivity {
	now := time.Now().UnixMilli()
	activity := viewActivity{Total: 240}
	for i := range 200 {
		v := viewThread{
			ID: int64(1000 + i), Task: i%7 == 0, Resolved: i%3 == 0,
			Author:      fmt.Sprintf("Reviewer With A Rather Long Display Name %03d", i),
			CreatedDate: now - int64(i)*60000,
			Text:        fmt.Sprintf("Comment %03d: %s", i, strings.Repeat("a-very-long-token-without-spaces-", 12)),
			URL:         threadURL(int64(1000 + i)),
			Anchor: &pullrequestactivityservice.Anchor{
				Path: fmt.Sprintf("services/payments/internal/providers/generated/very/deeply/nested/directory/%02d/handler_%03d.go", i%20, i),
				Line: i + 1, LineType: "ADDED",
			},
			Context: []viewContextLine{{Type: "add", New: i + 1, Text: strings.Repeat("x := y + z; ", 60), More: 40, Anchor: true}},
		}
		activity.Items = append(activity.Items, viewActivityItem{Action: "COMMENTED", Date: v.CreatedDate, User: v.Author, Thread: &v})
	}
	return activity
}

func overviewPayload(activity viewActivity, offers *viewOffers, fingerprint string) viewPayload {
	count := func(n int) *int { return &n }
	pr := viewPullRequest{PullRequest: fixturePullRequest(), URL: threadsURL,
		ReviewSummary: &pullrequestservice.ReviewSummary{UnresolvedThreads: count(4), OpenTasks: count(1), ResolvedThreads: count(2), ResolvedTasks: count(1)}}
	return viewPayload{
		Kind: showKindPullRequest, GeneratedAt: time.Now().UTC().Format(time.RFC3339), PullRequest: &pr, Activity: &activity,
		Avatars: map[string]string{"bob": onePixelPNG, "carol": onePixelPNG},
		Show:    &ShowInput{Kind: showKindPullRequest, Project: "PAY", Repo: "ledger", ID: "42"}, Fingerprint: fingerprint, Offers: offers,
	}
}

func activityFrames(t *testing.T) []viewhost.Frame {
	t.Helper()
	arguments := map[string]any{"kind": "pull_request", "project": "PAY", "repo": "ledger", "id": "42"}
	payload := fixtureResult(t, overviewPayload(fixtureActivity(), everyKind, "one"))
	return []viewhost.Frame{
		{Title: "card", Mode: "inline", Fullscreen: true, Arguments: arguments, Result: payload},
		{Title: "overview fullscreen", Mode: "fullscreen", Fullscreen: true, Arguments: arguments, Result: payload},
		{Title: "overview in a host without fullscreen", Mode: "inline", Fullscreen: false, Arguments: arguments, Result: payload},
		{Title: "overview fullscreen, on a phone", Mode: "fullscreen", Fullscreen: true, Width: 380, Arguments: arguments, Result: payload},
		{Title: "overview, 200 of 240 items", Theme: "dark", Mode: "fullscreen", Fullscreen: true, Arguments: arguments,
			Result: fixtureResult(t, overviewPayload(stressActivity(), everyKind, "one"))},
	}
}

func TestTheActivityDrawsTextOthersWroteAsText(t *testing.T) {
	ctx := browser(t, activityFrames(t))

	for _, frame := range []int{activityFullscreen, activityStress} {
		var escaped bool
		inFrame(t, ctx, frame, `return !d.querySelector("img[src='x'], script:not(:first-of-type), b[onmouseover]");`, &escaped)
		if !escaped {
			t.Errorf("frame %d built an element from text someone wrote", frame)
		}
	}
	// textContent, as written: innerText breaks a path's parts onto lines.
	var page string
	inFrame(t, ctx, activityFullscreen, `return d.body.textContent;`, &page)
	for _, want := range []string{hostileName, hostilePath, hostileCode, "<script>window.parent.bbHost.pwned='comment'</script>"} {
		if !strings.Contains(page, want) {
			t.Errorf("the activity does not show %q as written:\n%s", want, page)
		}
	}

	// A thread whose link is not a web address gets no button to open it.
	var buttons int
	inFrame(t, ctx, activityFullscreen, `return [...d.querySelectorAll("#thread-5 .icon-button")].filter((b) => b.title.includes("Bitbucket")).length;`, &buttons)
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

// The overview lists what happened as Bitbucket's does: newest first, in its
// words; a comment on the pull request as its thread; a comment on a line with
// the lines of the diff it is among and the thread under its line, which the
// card names; every reply out; a resolved thread folded to a line until it is
// opened; a task with its checkbox. What the view does not carry is counted.
func TestTheActivityListsWhatHappenedAsBitbucketDoes(t *testing.T) {
	ctx := browser(t, activityFrames(t))

	var entries []string
	inFrame(t, ctx, activityFullscreen, `return [...d.querySelectorAll(".activity-list > li")].map((li) => { const c = li.cloneNode(true); c.querySelectorAll(".avatar").forEach((a) => a.remove()); return c.textContent.replace(/\s+/g, " ").trim().slice(0, 60); });`, &entries)
	for i, want := range []string{"Alice Smith", "Alice <b", "Bob Chen marked the pull request as Approved", "Carol Diaz", "Carol Diaz marked the pull request as Changes requested",
		"Bob Chen commented on a file", "Dave Okafor commented on a file", "Erin Walsh commented on a file", "Alice Smith updated the pull request · 2 commits added", "Alice Smith opened the pull request"} {
		if i >= len(entries) || !strings.HasPrefix(entries[i], want) {
			t.Errorf("entry %d reads %q, want it to start %q (all: %q)", i, valueAt(entries, i), want, entries)
		}
	}

	// A comment on a line: the file, its lines, and the thread under its line.
	var rows []string
	inFrame(t, ctx, activityFullscreen, `return [...d.querySelector("#thread-2").closest(".activity-file").querySelectorAll("tr")].map((r) => r.classList.contains("diff-thread") ? "thread:" + r.querySelector(".comment-line").textContent : r.textContent.trim());`, &rows)
	if want := []string{"88// Refund returns the entry that reverses e.", "9\treturn Entry{}", "thread:Line +9", "910}"}; strings.Join(rows, "|") != strings.Join(want, "|") {
		t.Errorf("the comment on line 9 is drawn among %q, want %q", rows, want)
	}
	var crumb string
	inFrame(t, ctx, activityFullscreen, `return d.querySelector("#thread-2").closest(".activity-file").querySelector(".file-crumb").textContent;`, &crumb)
	if crumb != "internal/ledger/ledger.go" {
		t.Errorf("the comment on line 9 is headed %q, want its file", crumb)
	}
	var replies []string
	inFrame(t, ctx, activityFullscreen, `return [...d.querySelectorAll("#thread-2 .comment.reply .comment-text")].map((r) => r.textContent);`, &replies)
	if len(replies) != 5 || replies[0] != "Reply 1." || replies[4] != "Reply 5." {
		t.Errorf("thread 2's replies are %q, want all five out", replies)
	}

	// A resolved thread folds to a line, and opens on a click.
	folded := frameText(t, ctx, activityFullscreen)
	if strings.Contains(folded, "Why drop the timeout?") || strings.Contains(folded, "Typo in the README.") {
		t.Error("a resolved thread shows its text before it is opened")
	}
	var marks string
	inFrame(t, ctx, activityFullscreen, `return d.querySelector("#thread-3").textContent + "|" + d.querySelector("#thread-4").textContent;`, &marks)
	if !strings.Contains(marks, "Resolved") || !strings.Contains(marks, "Done") {
		t.Errorf("the folded threads read %q, want one resolved and one done", marks)
	}
	inFrame(t, ctx, activityFullscreen, `d.querySelector("#thread-3 .comment-folded").click(); return true;`, nil)
	waitInFrame(t, ctx, activityFullscreen, `d.querySelector("#thread-3").innerText.includes("Why drop the timeout?")`, "a resolved thread does not open")

	// A task has its checkbox, ticked once it is done.
	var task string
	inFrame(t, ctx, activityFullscreen, `const c = d.querySelector("#thread-1 .task-check"); return c ? c.className : "";`, &task)
	if !strings.Contains(task, "task-check") || strings.Contains(task, "done") {
		t.Errorf("the open task's checkbox is %q, want one not ticked", task)
	}

	if !strings.Contains(frameText(t, ctx, activityFullscreen), "This view carries the latest 10 of 12 items.") {
		t.Error("the activity carries fewer items than there are without saying so")
	}
	if page := frameText(t, ctx, activityFullscreen); !strings.Contains(page, "This comment is longer than a view carries.") {
		t.Errorf("a cut comment does not say so:\n%s", page)
	}
}

func valueAt(values []string, i int) string {
	if i < len(values) {
		return values[i]
	}
	return ""
}

func TestTheActivityStaysInBounds(t *testing.T) {
	ctx := browser(t, activityFrames(t))
	chromedp.Run(ctx, chromedp.Sleep(300*time.Millisecond))

	for frame := range len(activityFrames(t)) {
		var wide bool
		inFrame(t, ctx, frame, `return d.documentElement.scrollWidth > d.documentElement.clientWidth + 1;`, &wide)
		if wide {
			t.Errorf("frame %d scrolls sideways: something long broke its layout", frame)
		}
	}

	// Opened out in place, the overview shows the latest few, and more a step
	// at a time.
	clickButton(t, ctx, activityInPlace, "Overview")
	waitInFrame(t, ctx, activityInPlace, `d.querySelectorAll(".activity-list > li").length === 6`, "the overview opened in place does not show the latest six items")
	clickButton(t, ctx, activityInPlace, "Show 4 more items")
	waitInFrame(t, ctx, activityInPlace, `d.querySelectorAll(".activity-list > li").length === 10`, "a step does not show the rest of what the view carries")
}

// The card's count of open tasks opens the overview at the newest of them.
func TestTheCardOpensItsOpenTasks(t *testing.T) {
	ctx := browser(t, activityFrames(t))

	clickButton(t, ctx, activityCard, "1 open task")
	waitInFrame(t, ctx, activityCard, `d.querySelector(".fullscreen-header") && d.getElementById("thread-1")`, "the open task did not open the overview at it")
	var top float64
	inFrame(t, ctx, activityCard, `const r = d.getElementById("thread-1").getBoundingClientRect(); return r.top;`, &top)
	if top < 0 || top > 900 {
		t.Errorf("the open task is %.0fpx down the overview, want it scrolled to", top)
	}
}

// A comment on a file opens the diff at that file, and at the comment.
func TestAFileInTheActivityOpensTheDiffAtIt(t *testing.T) {
	diff := commentedDiff(time.Now(), "diff", commentedThreads())
	ctx := browser(t, []viewhost.Frame{
		{Title: "overview fullscreen", Mode: "fullscreen", Fullscreen: true, Result: fixtureResult(t, overviewPayload(fixtureActivity(), everyKind, "one")),
			ToolResults: answers(refreshAnswer(t, &diff, "The person opened the diff."))},
	})

	var clicked bool
	inFrame(t, ctx, 0, `const b = d.querySelector("#thread-2").closest(".activity-file").querySelector("button.file-crumb"); if (b) b.click(); return Boolean(b);`, &clicked)
	if !clicked {
		t.Fatal("the comment on a file has no file to open")
	}
	waitInFrame(t, ctx, 0, `d.getElementById("diff-file-0") && d.getElementById("thread-2")`, "the diff did not open at ledger.go and its comment")
	calls := refreshCalls(t, ctx, 0)
	if len(calls) == 0 || calls[len(calls)-1]["kind"] != "diff" || calls[len(calls)-1]["id"] != "42" {
		t.Errorf("opening the file asked %v, want the pull request's diff", calls)
	}
}

// Bitbucket's thread counts include the tasks. A pull request's card and
// overview count a task as a task and not as a comment too: 7 unresolved
// threads, 2 of them tasks, are 2 open tasks and 5 unresolved comments.
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

// The payload's activity item encodes as the view reads it.
func TestAnActivityItemEncodesAsTheViewReadsIt(t *testing.T) {
	encoded, err := json.Marshal(viewActivityItem{Action: "RESCOPED", User: "Alice", Username: "alice", Added: 2, AddedReviewers: []string{"Bob"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"action":"RESCOPED"`, `"user":"Alice"`, `"username":"alice"`, `"added":2`, `"added_reviewers":["Bob"]`} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("%s lacks %s", encoded, want)
		}
	}
}
