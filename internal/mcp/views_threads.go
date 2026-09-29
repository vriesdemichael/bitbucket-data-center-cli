package mcp

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

// The threads a view carries. Its counts come from the whole timeline, so they
// hold for any number of threads; the threads past these are counted, and the
// view says it lists fewer.
const (
	maxViewThreads      = 200
	maxViewThreadsBytes = 256 << 10
	// maxViewCommentBytes is the most of one comment a view carries. A comment
	// cut there says so, and links to the rest in Bitbucket.
	maxViewCommentBytes = 16 << 10
	// threadContextLines is how many lines of the diff lead to an anchored
	// line, as a review comment in Bitbucket shows them.
	threadContextLines = 3
	// maxContextLineRunes is the most of one diff line a view carries, as much
	// as it draws of a line in the diff.
	maxContextLineRunes = 500
)

// viewThreads is a pull request's comment threads, as a view draws them.
type viewThreads struct {
	// Summary counts every thread on the pull request, however many the view
	// carries. A view counts from it, never from Threads.
	Summary pullrequestactivityservice.Summary `json:"summary"`
	Threads []viewThread                       `json:"threads"`
}

// viewThread is one thread: its opening comment, its replies, where it is
// anchored and the diff that leads to that line.
type viewThread struct {
	ID       int64  `json:"id"`
	Task     bool   `json:"task,omitempty"`
	Resolved bool   `json:"resolved,omitempty"`
	Pending  bool   `json:"pending,omitempty"`
	Author   string `json:"author,omitempty"`
	// AuthorUsername is the key of the author's avatar in the payload.
	AuthorUsername string                             `json:"author_username,omitempty"`
	CreatedDate    int64                              `json:"created_date,omitempty"`
	Anchor         *pullrequestactivityservice.Anchor `json:"anchor,omitempty"`
	Text           string                             `json:"text,omitempty"`
	// TextCut says the view carries only the start of the comment.
	TextCut bool        `json:"text_cut,omitempty"`
	Replies []viewReply `json:"replies,omitempty"`
	URL     string      `json:"url,omitempty"`
	// Context is the diff leading to the anchored line, the line itself last.
	// It is absent when the diff no longer has the line.
	Context []viewContextLine `json:"context,omitempty"`
}

type viewReply struct {
	Author         string `json:"author,omitempty"`
	AuthorUsername string `json:"author_username,omitempty"`
	Date           int64  `json:"date,omitempty"`
	Text           string `json:"text,omitempty"`
	TextCut        bool   `json:"text_cut,omitempty"`
}

// viewContextLine is a line of the diff around an anchored comment.
type viewContextLine struct {
	// Type is add, del or context, as the diff view names them.
	Type string `json:"type"`
	Old  int    `json:"old,omitempty"`
	New  int    `json:"new,omitempty"`
	Text string `json:"text"`
	// More is how many characters of a long line the view does not carry.
	More int `json:"more,omitempty"`
	// Anchor marks the line the comment is on.
	Anchor bool `json:"anchor,omitempty"`
}

// threadsForView reads a pull request's threads with everything the view
// draws, as list_pr_comments reads them, and the avatars of the people who
// wrote them. withContext adds the lines of the diff leading to each
// anchored line, which the diff view does without: it draws the diff.
func threadsForView(ctx context.Context, c Clients, in ShowInput, withContext bool) (viewThreads, map[string]string, error) {
	out, err := pullRequestThreads(ctx, c, ListPRCommentsInput{
		Project: in.Project, Repo: in.Repo, PRID: in.ID, State: "all", WithReplies: true, Limit: maxViewThreads,
	})
	if err != nil {
		return viewThreads{}, nil, err
	}

	threads := viewThreads{Summary: out.Summary, Threads: []viewThread{}}
	budget := maxViewThreadsBytes
	for _, thread := range out.Threads {
		view := viewThreadOf(thread)
		size := view.size()
		if size > budget {
			break
		}
		budget -= size
		threads.Threads = append(threads.Threads, view)
	}
	if withContext {
		withAnchorContext(ctx, c, in, threads.Threads)
	}

	people := map[string]string{}
	add := func(account pullrequestactivityservice.Account) {
		if account.Username != "" && len(people) < maxViewPeople {
			people[account.Username] = account.Slug
		}
	}
	for _, thread := range out.Threads[:len(threads.Threads)] {
		add(thread.AuthorAccount)
		for _, reply := range thread.Replies {
			add(reply.AuthorAccount)
		}
	}
	return threads, people, nil
}

func viewThreadOf(thread pullrequestactivityservice.Thread) viewThread {
	text, cut := cutComment(thread.Text)
	view := viewThread{
		ID:             thread.ID,
		Task:           thread.Kind == pullrequestactivityservice.ThreadKindTask,
		Resolved:       thread.Resolved,
		Pending:        strings.EqualFold(thread.State, "PENDING"),
		Author:         thread.Author,
		AuthorUsername: thread.AuthorAccount.Username,
		CreatedDate:    thread.CreatedDate,
		Anchor:         thread.Anchor,
		Text:           text,
		TextCut:        cut,
		URL:            thread.URL,
	}
	for _, reply := range thread.Replies {
		text, cut := cutComment(reply.Text)
		view.Replies = append(view.Replies, viewReply{
			Author: reply.Author, AuthorUsername: reply.AuthorAccount.Username, Date: reply.Date, Text: text, TextCut: cut,
		})
	}
	return view
}

// size is about how many bytes a thread adds to the payload: its text, and a
// little for each of its parts.
func (thread viewThread) size() int {
	size := 200 + len(thread.Text)
	if thread.Anchor != nil {
		size += len(thread.Anchor.Path)
	}
	for _, reply := range thread.Replies {
		size += 100 + len(reply.Text)
	}
	return size
}

// cutComment is a comment's text as a view carries it: whole, or its first
// maxViewCommentBytes, cut at a character.
func cutComment(text string) (string, bool) {
	if len(text) <= maxViewCommentBytes {
		return text, false
	}
	end := maxViewCommentBytes
	for end > 0 && !utf8.RuneStart(text[end]) {
		end--
	}
	return text[:end], true
}

// withAnchorContext gives each thread anchored to a line the lines of the diff
// that lead to it. The diff is an extra: a Bitbucket that cannot answer for it
// leaves the threads without their context rather than failing the view.
func withAnchorContext(ctx context.Context, c Clients, in ShowInput, threads []viewThread) {
	anchored := false
	for _, thread := range threads {
		if thread.Anchor != nil && thread.Anchor.Line > 0 && !thread.Anchor.Orphaned {
			anchored = true
			break
		}
	}
	if !anchored {
		return
	}
	result, err := diffservice.NewService(c.OpenAPI).DiffPR(ctx, diffservice.DiffPRInput{
		Repository:    diffservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
		PullRequestID: in.ID,
		Output:        diffservice.OutputKindRaw,
	})
	if err != nil {
		return
	}
	files := map[string]string{}
	for _, chunk := range patchFiles(result.Patch) {
		files[describePatchFile(chunk).Path] = chunk
	}
	for i := range threads {
		anchor := threads[i].Anchor
		if anchor == nil || anchor.Line <= 0 || anchor.Orphaned {
			continue
		}
		if chunk, ok := files[anchor.Path]; ok {
			threads[i].Context = contextAt(chunk, anchor.Line, anchor.LineType)
		}
	}
}

var hunkStart = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@`)

// contextAt is the line of one file's diff a comment is anchored to, with up
// to threadContextLines lines leading to it from the same hunk. Bitbucket numbers a
// removed line in the old file and any other in the new one. It is nil when
// the diff does not have the line.
func contextAt(chunk string, line int, lineType string) []viewContextLine {
	var hunk []viewContextLine
	oldNo, newNo, inHunk := 0, 0, false
	for _, text := range strings.Split(chunk, "\n") {
		if start := hunkStart.FindStringSubmatch(text); start != nil {
			oldNo, _ = strconv.Atoi(start[1])
			newNo, _ = strconv.Atoi(start[2])
			hunk, inHunk = hunk[:0], true
			continue
		}
		if !inHunk || text == "" {
			continue
		}
		var entry viewContextLine
		switch text[0] {
		case '+':
			entry = viewContextLine{Type: "add", New: newNo}
			newNo++
		case '-':
			entry = viewContextLine{Type: "del", Old: oldNo}
			oldNo++
		case ' ':
			entry = viewContextLine{Type: "context", Old: oldNo, New: newNo}
			oldNo++
			newNo++
		default:
			continue
		}
		entry.Text, entry.More = cutLine(text[1:])
		hunk = append(hunk, entry)

		var here bool
		switch strings.ToUpper(lineType) {
		case "REMOVED":
			here = entry.Type == "del" && entry.Old == line
		case "ADDED":
			here = entry.Type == "add" && entry.New == line
		default:
			here = entry.Type != "del" && entry.New == line
		}
		if here {
			from := max(0, len(hunk)-1-threadContextLines)
			context := append([]viewContextLine(nil), hunk[from:]...)
			context[len(context)-1].Anchor = true
			return context
		}
	}
	return nil
}

// cutLine is a diff line as a view carries it: whole, or its first
// maxContextLineRunes characters and how many more it has.
func cutLine(text string) (string, int) {
	count := utf8.RuneCountInString(text)
	if count <= maxContextLineRunes {
		return text, 0
	}
	end, runes := 0, 0
	for runes < maxContextLineRunes {
		_, width := utf8.DecodeRuneInString(text[end:])
		end += width
		runes++
	}
	return text[:end], count - maxContextLineRunes
}

// summarizeThreads is what the model reads beside the threads view.
func summarizeThreads(in ShowInput, title string, threads viewThreads) viewSummary {
	summary := threads.Summary
	var b strings.Builder
	fmt.Fprintf(&b, "%d unresolved", summary.Unresolved)
	if summary.OpenTasks > 0 {
		fmt.Fprintf(&b, " (%d of them open tasks)", summary.OpenTasks)
	}
	fmt.Fprintf(&b, ", %d resolved", summary.Resolved)
	if summary.Pending > 0 {
		fmt.Fprintf(&b, ", %d pending", summary.Pending)
	}
	b.WriteString(".")
	if len(threads.Threads) < summary.TotalThreads {
		fmt.Fprintf(&b, " The view carries the first %d of the %d threads.", len(threads.Threads), summary.TotalThreads)
	}
	return viewSummary{
		subject: fmt.Sprintf("the comment threads of %s/%s#%s %q", in.Project, in.Repo, in.ID, title),
		form:    "an interactive view",
		state:   b.String(),
	}
}
