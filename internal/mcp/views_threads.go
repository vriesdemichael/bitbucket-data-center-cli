package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

// The threads a diff carries. Its counts come from the whole timeline, so they
// hold for any number of threads; the threads past these are counted, and the
// view says it lists fewer.
const (
	maxViewThreads      = 200
	maxViewThreadsBytes = 256 << 10
	// maxViewCommentBytes is the most of one comment a view carries. A comment
	// cut there says so, and links to the rest in Bitbucket.
	maxViewCommentBytes = 16 << 10
	// maxContextLineRunes is the most of one diff line a view carries, as much
	// as it draws of a line in the diff.
	maxContextLineRunes = 500
	// maxPlacedFiles is how many files a diff asks Bitbucket where it draws
	// their comments, one request each. The threads on files past these are
	// placed by their anchors.
	maxPlacedFiles = 30
)

// viewThreads is a pull request's comment threads, as a view draws them.
type viewThreads struct {
	// Summary counts every thread on the pull request, however many the view
	// carries. A view counts from it, never from Threads.
	Summary pullrequestactivityservice.Summary `json:"summary"`
	Threads []viewThread                       `json:"threads"`
}

// viewThread is one thread: its opening comment, its replies, and where it is.
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
	// Place is where a diff draws the thread, as Bitbucket's own diff does:
	// old:N under line N of the file as it was, new:N under line N as it is,
	// file at the top of its file. It is empty for a thread on the pull
	// request, and for one Bitbucket's diff no longer draws.
	Place string `json:"place,omitempty"`
	// Context is the lines of the diff an activity shows a comment on a line
	// among, as Bitbucket's overview does, with that line marked.
	Context []viewContextLine `json:"context,omitempty"`
}

type viewReply struct {
	// ID is the reply's own, which a reply to it names as its parent.
	ID             int64  `json:"id"`
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

// threadsForDiff reads a pull request's threads for its diff, as
// list_pr_comments reads them, and the people whose avatars it draws. Each
// thread gets the place Bitbucket's own diff draws it at (see placeThreads);
// patch is the diff, whose files say which to ask about.
func threadsForDiff(ctx context.Context, c Clients, in ShowInput, patch string) (viewThreads, map[string]string, error) {
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
	placeThreads(ctx, c, in, patch, threads.Threads)

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
			ID: reply.ID, Author: reply.Author, AuthorUsername: reply.AuthorAccount.Username, Date: reply.Date, Text: text, TextCut: cut,
		})
	}
	return view
}

// size is about how many bytes a thread adds to the payload: its text, the
// lines of the diff it carries, and a little for each of its parts.
func (thread viewThread) size() int {
	size := 200 + len(thread.Text)
	if thread.Anchor != nil {
		size += len(thread.Anchor.Path)
	}
	for _, reply := range thread.Replies {
		size += 100 + len(reply.Text)
	}
	for _, line := range thread.Context {
		size += 40 + len(line.Text)
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

// placeThreads gives each thread the place Bitbucket's own diff draws it at,
// as its diff with comments says. A thread's anchor names its line on the side
// it was written on, so a comment on an unchanged line written on the left of
// a side-by-side diff names the line as the file was, and lands elsewhere when
// read as the file is; Bitbucket's diff says the line it draws it under.
// Bitbucket's diff draws no comment on a line outside its hunks. The threads
// on a file Bitbucket cannot answer for are placed by their anchors.
func placeThreads(ctx context.Context, c Clients, in ShowInput, patch string, threads []viewThread) {
	chunks := patchFiles(patch)
	chunkOf := map[string]int{}
	for index, chunk := range chunks {
		chunkOf[describePatchFile(chunk).Path] = index
	}

	places := map[int64]string{}
	asked := map[string]bool{}
	placed := map[string]bool{}
	for _, thread := range threads {
		anchor := thread.Anchor
		if anchor == nil || anchor.Path == "" || asked[anchor.Path] || len(asked) >= maxPlacedFiles {
			continue
		}
		// A file too large for the view is not drawn, so it is not asked
		// for either: its diff with comments weighs more than its patch.
		index, inPatch := chunkOf[anchor.Path]
		if !inPatch || len(chunks[index]) > maxViewFileBytes {
			continue
		}
		asked[anchor.Path] = true
		file := describePatchFile(chunks[index])
		answer, err := fileDiffWithComments(ctx, c, in, file)
		if err != nil {
			continue
		}
		placed[anchor.Path] = true
		for id, place := range answer {
			places[id] = place
		}
	}

	for i := range threads {
		anchor := threads[i].Anchor
		switch {
		case anchor == nil || anchor.Path == "":
		case placed[anchor.Path]:
			threads[i].Place = places[threads[i].ID]
		default:
			threads[i].Place = placeByAnchor(*anchor)
		}
	}
}

// placeByAnchor is where a thread goes by where it was written, for a file
// Bitbucket did not say where it draws its comments.
func placeByAnchor(anchor pullrequestactivityservice.Anchor) string {
	switch {
	case anchor.Orphaned:
		return ""
	case anchor.Line <= 0:
		return "file"
	case strings.EqualFold(anchor.LineType, "REMOVED"):
		return "old:" + strconv.Itoa(anchor.Line)
	default:
		return "new:" + strconv.Itoa(anchor.Line)
	}
}

// fileDiff is one file of Bitbucket's diff with comments, as far as placing
// its comments needs it.
type fileDiff struct {
	Diffs []struct {
		Hunks []struct {
			Segments []struct {
				Type  string `json:"type"`
				Lines []struct {
					Source      int     `json:"source"`
					Destination int     `json:"destination"`
					CommentIDs  []int64 `json:"commentIds"`
				} `json:"lines"`
			} `json:"segments"`
		} `json:"hunks"`
		FileComments []struct {
			ID int64 `json:"id"`
		} `json:"fileComments"`
	} `json:"diffs"`
}

// fileDiffWithComments asks Bitbucket where its diff of one file of the pull
// request draws each comment on it, as its diff page does.
func fileDiffWithComments(ctx context.Context, c Clients, in ShowInput, file viewDiffFile) (map[int64]string, error) {
	if c.HTTP == nil {
		return nil, fmt.Errorf("no Bitbucket client")
	}
	segments := strings.Split(file.Path, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests/%s/diff/%s",
		url.PathEscape(in.Project), url.PathEscape(in.Repo), url.PathEscape(in.ID), strings.Join(segments, "/"))
	query := map[string]string{"withComments": "true"}
	if file.OldPath != "" && file.OldPath != file.Path {
		query["srcPath"] = file.OldPath
	}
	var answer fileDiff
	if err := c.HTTP.GetJSON(ctx, path, query, &answer); err != nil {
		return nil, err
	}
	return placesOf(answer), nil
}

// placesOf reads where a file's diff with comments draws each comment, by the
// comment's ID. A comment on a removed line is drawn under that line of the
// file as it was; on any other line, under that line as the file is.
func placesOf(answer fileDiff) map[int64]string {
	places := map[int64]string{}
	for _, diff := range answer.Diffs {
		for _, comment := range diff.FileComments {
			places[comment.ID] = "file"
		}
		for _, hunk := range diff.Hunks {
			for _, segment := range hunk.Segments {
				removed := strings.EqualFold(segment.Type, "REMOVED")
				for _, line := range segment.Lines {
					place := "new:" + strconv.Itoa(line.Destination)
					if removed {
						place = "old:" + strconv.Itoa(line.Source)
					}
					for _, id := range line.CommentIDs {
						places[id] = place
					}
				}
			}
		}
	}
	return places
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
