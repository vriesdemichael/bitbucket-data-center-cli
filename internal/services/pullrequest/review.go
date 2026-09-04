package pullrequest

import (
	"strings"

	pullrequestactivity "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

// Where a ReviewSummary's counts came from. This matters because the sources
// measure different things and are not equally available across Bitbucket
// versions, and because a summary with no counts at all must not be mistaken
// for a pull request with nothing outstanding.
const (
	// CountsSourceActivities means the counts were derived from the activity
	// timeline and describe unresolved threads exactly.
	CountsSourceActivities = "activities"
	// CountsSourceBlockerComments means only the task tally was fetched, via
	// the blocker-comment count endpoint. Comment threads are not counted.
	CountsSourceBlockerComments = "blocker_comments"
	// CountsSourceProperties means the counters Bitbucket attaches to pull
	// request list payloads were used. Bitbucket 10.x omits these on the
	// single pull request endpoint, so this mostly applies to listings.
	CountsSourceProperties = "properties"
	// CountsSourceNone means no counts were measured. Reviewer status is still
	// reported; the count fields are absent rather than zero.
	CountsSourceNone = "none"
)

// TaskCounts carries an exact open/resolved task tally.
type TaskCounts struct {
	Open     int
	Resolved int
}

// ReviewSummary answers "is there anything for me to do on this pull request?"
// It is attached to pull request output so an agent sees outstanding review
// feedback without being told to go looking for it.
//
// The count fields are pointers on purpose: a nil count means "not measured",
// which is a different claim from "measured and zero". Reporting an unmeasured
// count as zero would make a pull request with open feedback look clean.
//
// OpenTasks is a subset of UnresolvedThreads, not a separate bucket, so the two
// must not be added together.
type ReviewSummary struct {
	// ActionRequired is true when the pull request is waiting on its author:
	// an unresolved thread, an open task, or a reviewer who requested changes.
	//
	// It is absent when the answer is not known, for the same reason the counts
	// below are. A positive signal is certain whatever else went unmeasured, so
	// true is always safe to report; false means every source that could have
	// raised it was actually consulted. Reporting false from a partial
	// measurement is the failure this whole type exists to prevent, and it is
	// the worst place to do it -- a caller branches on this one field.
	ActionRequired *bool `json:"action_required,omitempty"`

	// UnresolvedThreads counts every thread still open, tasks included.
	UnresolvedThreads *int `json:"unresolved_threads,omitempty"`
	// OpenTasks is the subset of UnresolvedThreads that blocks the merge.
	OpenTasks       *int `json:"open_tasks,omitempty"`
	ResolvedThreads *int `json:"resolved_threads,omitempty"`
	ResolvedTasks   *int `json:"resolved_tasks,omitempty"`
	// PendingComments counts the author's own unpublished draft comments.
	PendingComments *int `json:"pending_comments,omitempty"`

	// NeedsWork lists reviewers who requested changes.
	NeedsWork []string `json:"needs_work,omitempty"`
	Approvals int      `json:"approvals"`
	Reviewers int      `json:"reviewers"`

	// CommentCount is the raw comment counter from the pull request payload. It
	// counts every comment, replies included, so it is a weaker signal than
	// UnresolvedThreads and is reported separately rather than merged into it.
	CommentCount *int `json:"comment_count,omitempty"`

	CountsSource string `json:"counts_source"`
}

// ReviewCounts carries whichever measurement the caller managed to obtain.
// Leave both fields nil when nothing could be measured.
type ReviewCounts struct {
	// Threads is the full picture, derived from the activity timeline.
	Threads *pullrequestactivity.Summary
	// Tasks is the cheap fallback: an exact task tally with no thread counts.
	Tasks *TaskCounts
}

// BuildReviewSummary combines reviewer status, which is free because it already
// travels with the pull request, with whichever counts the caller obtained.
// Sources are preferred most-informative first: activity threads, then the task
// tally, then the counters on the pull request payload.
func BuildReviewSummary(pullRequest PullRequest, counts ReviewCounts) ReviewSummary {
	summary := ReviewSummary{
		Reviewers:    len(pullRequest.Reviewers),
		CommentCount: pullRequest.CommentCount,
		CountsSource: CountsSourceNone,
	}

	for _, reviewer := range pullRequest.Reviewers {
		if reviewer.Approved {
			summary.Approvals++
		}
		if strings.EqualFold(strings.TrimSpace(reviewer.Status), "NEEDS_WORK") {
			name := reviewer.Name
			if name == "" {
				name = reviewer.DisplayName
			}
			if name != "" {
				summary.NeedsWork = append(summary.NeedsWork, name)
			}
		}
	}

	switch {
	case counts.Threads != nil:
		threads := counts.Threads
		summary.CountsSource = CountsSourceActivities
		summary.UnresolvedThreads = countPtr(threads.Unresolved)
		summary.OpenTasks = countPtr(threads.OpenTasks)
		summary.ResolvedThreads = countPtr(threads.Resolved)
		summary.ResolvedTasks = countPtr(threads.ResolvedTasks)
		summary.PendingComments = countPtr(threads.Pending)
	case counts.Tasks != nil:
		summary.CountsSource = CountsSourceBlockerComments
		summary.OpenTasks = countPtr(counts.Tasks.Open)
		summary.ResolvedTasks = countPtr(counts.Tasks.Resolved)
	case pullRequest.OpenTaskCount != nil || pullRequest.ResolvedTaskCount != nil:
		summary.CountsSource = CountsSourceProperties
		summary.OpenTasks = pullRequest.OpenTaskCount
		summary.ResolvedTasks = pullRequest.ResolvedTaskCount
	}

	summary.ActionRequired = actionRequired(summary)

	return summary
}

// actionRequired answers only when it can.
//
// Any positive signal settles it: an open task is an open task whether or not
// the threads were counted. Nothing found settles it only if everything that
// could have raised it was looked at -- reviewer status always is, since it
// travels with the pull request, but the two counts depend on which source the
// caller managed to reach.
func actionRequired(summary ReviewSummary) *bool {
	if positive(summary.UnresolvedThreads) || positive(summary.OpenTasks) || len(summary.NeedsWork) > 0 {
		return boolPtr(true)
	}

	if summary.UnresolvedThreads == nil || summary.OpenTasks == nil {
		return nil
	}

	return boolPtr(false)
}

func boolPtr(value bool) *bool {
	return &value
}

// Measured reports whether any count was obtained. When false the summary can
// only speak to reviewer status, so callers must not present it as "nothing
// outstanding".
func (summary ReviewSummary) Measured() bool {
	return summary.CountsSource != CountsSourceNone
}

func countPtr(value int) *int {
	return &value
}

func positive(value *int) bool {
	return value != nil && *value > 0
}
