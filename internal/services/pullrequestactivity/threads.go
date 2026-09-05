package pullrequestactivity

import (
	"context"
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"net/url"
	"regexp"
	"sort"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Thread states as reported by Bitbucket on the root comment of a thread.
const (
	ThreadStateOpen     = "OPEN"
	ThreadStateResolved = "RESOLVED"
	ThreadStatePending  = "PENDING"
)

// Thread kinds. Bitbucket Data Center models a pull request task as a comment
// with severity BLOCKER, so both arrive through the same activity feed.
const (
	ThreadKindComment = "comment"
	ThreadKindTask    = "task"
)

// Anchor locates a thread in the diff. It is absent for pull-request-level
// comments, which are not attached to a file.
type Anchor struct {
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	LineType string `json:"line_type,omitempty"`
	Orphaned bool   `json:"orphaned,omitempty"`
}

// Reply is a single response inside a thread.
type Reply struct {
	ID     int64  `json:"id"`
	Author string `json:"author,omitempty"`
	Date   int64  `json:"date,omitempty"`
	Text   string `json:"text,omitempty"`
}

// Thread is the agent-sized view of a pull request comment thread. It carries
// only fields an agent acts on: identifiers to reply or resolve, the anchor to
// locate the feedback, the text itself, and enough reply context to tell
// whether the thread was already addressed. Everything the raw
// openapigenerated.RestComment nests underneath (most notably the entire pull
// request under anchor.pullRequest) is deliberately dropped.
type Thread struct {
	ID            int64   `json:"id"`
	Kind          string  `json:"kind"`
	State         string  `json:"state,omitempty"`
	Resolved      bool    `json:"resolved"`
	Author        string  `json:"author,omitempty"`
	Version       int     `json:"version,omitempty"`
	CreatedDate   int64   `json:"created_date,omitempty"`
	UpdatedDate   int64   `json:"updated_date,omitempty"`
	Anchor        *Anchor `json:"anchor,omitempty"`
	Text          string  `json:"text,omitempty"`
	HasSuggestion bool    `json:"has_suggestion,omitempty"`
	ReplyCount    int     `json:"reply_count"`
	LastReply     *Reply  `json:"last_reply,omitempty"`
	Replies       []Reply `json:"replies,omitempty"`
	URL           string  `json:"url,omitempty"`
}

// Summary is the aggregate view of a set of threads. It answers "is there
// anything for me to do?" without the caller having to walk the threads.
//
// It covers everything the source returned, before any state or kind filter is
// applied. What that spans depends on the source: the activity timeline is the
// whole pull request, the path-scoped comment endpoint is one file, and the
// blocker-comment endpoint is tasks only. It is not a pull-request-wide count
// in the latter two cases.
//
// Tasks are counted twice on purpose: a task is a kind of thread, so an open
// task raises both Unresolved and OpenTasks. Unresolved is therefore the total
// still waiting on someone, and OpenTasks is the subset of those that block the
// merge. Do not add the two together.
type Summary struct {
	// TotalThreads is Unresolved + Resolved + Pending.
	TotalThreads int `json:"total_threads"`
	// Unresolved counts every thread still open, tasks included.
	Unresolved int `json:"unresolved"`
	Resolved   int `json:"resolved"`
	// Pending counts the author's own unpublished draft comments.
	Pending int `json:"pending"`
	// OpenTasks is the subset of Unresolved that Bitbucket tracks as tasks.
	OpenTasks int `json:"open_tasks"`
	// ResolvedTasks is the subset of Resolved that Bitbucket tracks as tasks.
	ResolvedTasks int `json:"resolved_tasks"`
	// UnresolvedInline is the subset of Unresolved anchored to a file.
	UnresolvedInline int `json:"unresolved_inline,omitempty"`
}

// ThreadOptions controls how comments are projected onto threads.
type ThreadOptions struct {
	// State filters threads by resolution state: open, resolved, pending or
	// all. An empty value means all.
	State string
	// TasksOnly keeps only threads that Bitbucket marks as blocker comments.
	TasksOnly bool
	// WithReplies populates Thread.Replies with the full reply text rather than
	// collapsing replies to a count and the most recent one.
	WithReplies bool
	// BaseURL, ProjectKey, Slug and PullRequestID are used to build a browser
	// link per thread. The link is omitted when any of them is empty.
	BaseURL       string
	ProjectKey    string
	Slug          string
	PullRequestID string
}

// suggestionPattern matches the fenced block Bitbucket uses for code
// suggestions, which `bb pr comment apply-suggestion` can apply directly.
var suggestionPattern = regexp.MustCompile("(?m)^\\s*```\\s*suggestion\\b")

// ExtractThreads projects the comments carried by an activity feed onto the
// slim Thread view, applying the filters in options. Threads are ordered
// unresolved first, then by creation date.
func ExtractThreads(activities []Activity, options ThreadOptions) ([]Thread, Summary) {
	return buildThreads(ExtractComments(activities), extractOrphanedAnchors(activities), options)
}

// ThreadsFromComments projects an already-fetched comment page onto the Thread
// view. It is used by the path-scoped and blocker-comment listings, which do
// not go through the activity feed.
func ThreadsFromComments(comments []openapigenerated.RestComment, options ThreadOptions) ([]Thread, Summary) {
	return buildThreads(comments, nil, options)
}

// buildThreads maps every comment, then filters. The returned summary is
// computed over the unfiltered set, so it reflects everything the caller passed
// in rather than whatever filter was applied on top.
func buildThreads(comments []openapigenerated.RestComment, orphaned map[int64]bool, options ThreadOptions) ([]Thread, Summary) {
	all := make([]Thread, 0, len(comments))
	for _, comment := range comments {
		thread := mapThread(comment, options)
		if thread.Anchor != nil && orphaned[thread.ID] {
			thread.Anchor.Orphaned = true
		}
		all = append(all, thread)
	}

	summary := SummarizeThreads(all)

	threads := make([]Thread, 0, len(all))
	for _, thread := range all {
		if threadMatches(thread, options) {
			threads = append(threads, thread)
		}
	}

	sortThreads(threads)

	return threads, summary
}

// TrySummarize fetches the activity timeline and summarises its comment
// threads. The timeline is an enrichment rather than the primary payload, so
// when it is genuinely unavailable — the server does not expose the endpoint,
// or the token may not read it — the summary comes back nil and the caller
// falls back to whatever counters it already has.
//
// Every other failure is reported. Swallowing them indiscriminately would turn
// a broken token or a failing server into a silent "nothing outstanding", which
// is the exact failure mode the review summary exists to prevent.
// It takes no limit. A count of the open threads is either of all of them or it
// is a different number wearing the same name, and both callers were passing a
// value that happened to be a page size and so happened to be right.
func (service *Service) TrySummarize(ctx context.Context, repository RepositoryRef, pullRequestID string) (*Summary, error) {
	activities, err := service.List(ctx, repository, pullRequestID, ListOptions{MaxResults: AllResults})
	if err != nil {
		if ctx.Err() != nil {
			return nil, err
		}
		if timelineUnavailable(err) {
			return nil, nil
		}
		return nil, err
	}

	_, summary := ExtractThreads(activities, ThreadOptions{})

	return &summary, nil
}

// timelineUnavailable reports whether err means the activity timeline cannot be
// read here, as opposed to something being wrong that the caller should hear
// about.
// The route-missing check is kept for intent rather than for effect: a
// route-missing error is also KindNotFound, so the next clause already covers
// it. Removing it changes no outcome, which is why no test can pin it here --
// TestLiveRouteMissingClassification pins the classification itself instead.
func timelineUnavailable(err error) bool {
	return openapi.IsRouteMissing(err) ||
		apperrors.IsKind(err, apperrors.KindNotFound) ||
		apperrors.IsKind(err, apperrors.KindAuthorization)
}

// SummarizeThreads aggregates threads into the counts an agent uses to decide
// whether a pull request needs attention. It is intentionally computed over the
// unfiltered thread set so that the counts describe the pull request rather
// than the current filter.
func SummarizeThreads(threads []Thread) Summary {
	summary := Summary{TotalThreads: len(threads)}

	for _, thread := range threads {
		isTask := thread.Kind == ThreadKindTask

		switch {
		case strings.EqualFold(thread.State, ThreadStatePending):
			summary.Pending++
		case thread.Resolved:
			summary.Resolved++
			if isTask {
				summary.ResolvedTasks++
			}
		default:
			summary.Unresolved++
			if isTask {
				summary.OpenTasks++
			}
			if thread.Anchor != nil {
				summary.UnresolvedInline++
			}
		}
	}

	return summary
}

// NormalizeThreadState validates a user supplied state filter.
func NormalizeThreadState(state string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(state))
	switch normalized {
	case "", "all":
		return "all", nil
	case "open", "unresolved":
		return "open", nil
	case "resolved":
		return "resolved", nil
	case "pending":
		return "pending", nil
	default:
		return "", fmt.Errorf("state must be one of open, resolved, pending, all")
	}
}

func threadMatches(thread Thread, options ThreadOptions) bool {
	if options.TasksOnly && thread.Kind != ThreadKindTask {
		return false
	}

	switch strings.ToLower(strings.TrimSpace(options.State)) {
	case "", "all":
		return true
	case "open", "unresolved":
		return !thread.Resolved && !strings.EqualFold(thread.State, ThreadStatePending)
	case "resolved":
		return thread.Resolved
	case "pending":
		return strings.EqualFold(thread.State, ThreadStatePending)
	default:
		return true
	}
}

// sortThreads orders unresolved threads first so the actionable items are at
// the top of both human and JSON output, then falls back to creation order for
// a stable result.
func sortThreads(threads []Thread) {
	sort.SliceStable(threads, func(first, second int) bool {
		left, right := threads[first], threads[second]
		if left.Resolved != right.Resolved {
			return !left.Resolved
		}
		if left.CreatedDate != right.CreatedDate {
			return left.CreatedDate < right.CreatedDate
		}
		return left.ID < right.ID
	})
}

func mapThread(comment openapigenerated.RestComment, options ThreadOptions) Thread {
	thread := Thread{
		Kind:        ThreadKindComment,
		State:       strings.ToUpper(strings.TrimSpace(safederef.String(comment.State))),
		Author:      commentAuthor(comment),
		CreatedDate: safederef.Int64(comment.CreatedDate),
		UpdatedDate: safederef.Int64(comment.UpdatedDate),
		Text:        strings.TrimSpace(safederef.String(comment.Text)),
	}

	if comment.Id != nil {
		thread.ID = *comment.Id
	}
	if comment.Version != nil {
		thread.Version = int(*comment.Version)
	}
	if severity := strings.ToUpper(strings.TrimSpace(safederef.String(comment.Severity))); severity == "BLOCKER" {
		thread.Kind = ThreadKindTask
	}

	thread.Resolved = commentResolved(comment, thread.State)
	thread.HasSuggestion = suggestionPattern.MatchString(thread.Text)
	thread.Anchor = mapAnchor(comment)
	thread.URL = threadURL(options, thread.ID)

	replies := collectReplies(comment.Comments)
	thread.ReplyCount = len(replies)
	if len(replies) > 0 {
		last := replies[len(replies)-1]
		thread.LastReply = &last
		if options.WithReplies {
			thread.Replies = replies
		}
	}

	return thread
}

// commentResolved treats an explicit RESOLVED state as authoritative and falls
// back to the thread-level flags for servers that omit it.
func commentResolved(comment openapigenerated.RestComment, state string) bool {
	if state == ThreadStateResolved {
		return true
	}
	if state == ThreadStateOpen || state == ThreadStatePending {
		return false
	}
	if comment.ThreadResolved != nil && *comment.ThreadResolved {
		return true
	}

	return comment.ResolvedDate != nil && *comment.ResolvedDate > 0
}

func mapAnchor(comment openapigenerated.RestComment) *Anchor {
	if comment.Anchor == nil {
		return nil
	}

	anchor := Anchor{
		Path:     anchorPath(comment),
		LineType: strings.TrimSpace(string(safeLineType(comment))),
	}
	if comment.Anchor.Line != nil {
		anchor.Line = int(*comment.Anchor.Line)
	}

	if anchor.Path == "" && anchor.Line == 0 {
		return nil
	}

	return &anchor
}

func safeLineType(comment openapigenerated.RestComment) openapigenerated.RestCommentAnchorLineType {
	if comment.Anchor == nil || comment.Anchor.LineType == nil {
		return ""
	}

	return *comment.Anchor.LineType
}

// anchorPath rebuilds the file path from the parent/name pair Bitbucket returns.
func anchorPath(comment openapigenerated.RestComment) string {
	if comment.Anchor == nil || comment.Anchor.Path == nil {
		return ""
	}

	path := comment.Anchor.Path
	name := ""
	parent := ""
	if path.Name != nil {
		name = strings.TrimSpace(*path.Name)
	}
	if path.Parent != nil {
		parent = strings.TrimSpace(*path.Parent)
	}

	switch {
	case parent == "":
		return name
	case name == "":
		return parent
	default:
		return parent + "/" + name
	}
}

// collectReplies flattens the recursive reply tree into a chronological list.
func collectReplies(comments *[]openapigenerated.RestComment) []Reply {
	if comments == nil {
		return nil
	}

	replies := make([]Reply, 0, len(*comments))
	for _, comment := range *comments {
		reply := Reply{
			Author: commentAuthor(comment),
			Date:   safederef.Int64(comment.CreatedDate),
			Text:   strings.TrimSpace(safederef.String(comment.Text)),
		}
		if comment.Id != nil {
			reply.ID = *comment.Id
		}
		replies = append(replies, reply)
		replies = append(replies, collectReplies(comment.Comments)...)
	}

	sort.SliceStable(replies, func(first, second int) bool {
		if replies[first].Date != replies[second].Date {
			return replies[first].Date < replies[second].Date
		}
		return replies[first].ID < replies[second].ID
	})

	return replies
}

// extractOrphanedAnchors reads anchor.orphaned out of the raw activity payload.
// Bitbucket returns the flag but the published OpenAPI spec omits it, so the
// generated RestComment model has nowhere to put it. An orphaned anchor points
// at a line that no longer exists in the diff, which an agent needs to know
// before it tries to act on the referenced location.
func extractOrphanedAnchors(activities []Activity) map[int64]bool {
	orphaned := map[int64]bool{}

	for _, activity := range activities {
		comment, ok := activity.Raw["comment"].(map[string]any)
		if !ok {
			continue
		}
		markOrphanedComment(comment, orphaned)
	}

	return orphaned
}

func markOrphanedComment(comment map[string]any, orphaned map[int64]bool) {
	id, ok := comment["id"].(float64)
	if ok {
		if anchor, found := comment["anchor"].(map[string]any); found {
			if flag, isBool := anchor["orphaned"].(bool); isBool && flag {
				orphaned[int64(id)] = true
			}
		}
	}

	replies, ok := comment["comments"].([]any)
	if !ok {
		return
	}
	for _, reply := range replies {
		if nested, isMap := reply.(map[string]any); isMap {
			markOrphanedComment(nested, orphaned)
		}
	}
}

func commentAuthor(comment openapigenerated.RestComment) string {
	if comment.Author == nil {
		return ""
	}
	if displayName := strings.TrimSpace(comment.Author.DisplayName); displayName != "" {
		return displayName
	}

	return strings.TrimSpace(comment.Author.Name)
}

func threadURL(options ThreadOptions, commentID int64) string {
	base := strings.TrimRight(strings.TrimSpace(options.BaseURL), "/")
	project := strings.TrimSpace(options.ProjectKey)
	slug := strings.TrimSpace(options.Slug)
	pullRequestID := strings.TrimSpace(options.PullRequestID)
	if base == "" || project == "" || slug == "" || pullRequestID == "" || commentID == 0 {
		return ""
	}

	return fmt.Sprintf(
		"%s/projects/%s/repos/%s/pull-requests/%s/overview?commentId=%d",
		base,
		url.PathEscape(project),
		url.PathEscape(slug),
		url.PathEscape(pullRequestID),
		commentID,
	)
}
