package prcmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// ReviewSummary answers "is there anything for me to do on this pull request?"
//
// The count fields are pointers on purpose: absent means "not measured", which
// is a different claim from "measured and zero". Reporting an unmeasured count
// as zero would make a pull request with open feedback look clean.
//
// openTasks is a subset of unresolvedThreads, not a separate bucket, so the two
// must not be added together.
type ReviewSummary struct {
	ActionRequired    bool     `json:"actionRequired" jsonschema:"True when the pull request is waiting on someone: an unresolved thread, an open task, or a reviewer who requested changes."`
	UnresolvedThreads *int     `json:"unresolvedThreads,omitempty" jsonschema:"Threads still open, tasks included. Absent when nothing could be measured."`
	OpenTasks         *int     `json:"openTasks,omitempty" jsonschema:"The subset of unresolvedThreads that blocks the merge."`
	ResolvedThreads   *int     `json:"resolvedThreads,omitempty" jsonschema:"Threads that have been resolved."`
	ResolvedTasks     *int     `json:"resolvedTasks,omitempty" jsonschema:"Tasks that have been resolved."`
	PendingComments   *int     `json:"pendingComments,omitempty" jsonschema:"The author's own unpublished draft comments."`
	NeedsWork         []string `json:"needsWork,omitempty" jsonschema:"Reviewers who requested changes."`
	Approvals         int      `json:"approvals" jsonschema:"How many reviewers have approved."`
	Reviewers         int      `json:"reviewers" jsonschema:"How many reviewers are named."`
	CommentCount      *int     `json:"commentCount,omitempty" jsonschema:"Bitbucket's raw comment counter. It counts replies too, so it is a weaker signal than unresolvedThreads and is reported separately rather than folded into it."`
	CountsSource      string   `json:"countsSource" jsonschema:"Where the counts came from: activity for the timeline walk, tasks for the task tally, properties for the counters shipped with the pull request, or none."`
}

// ThreadAnchor locates an inline comment thread in the diff.
type ThreadAnchor struct {
	Path     string `json:"path" jsonschema:"File the thread is anchored to."`
	Line     int    `json:"line,omitempty" jsonschema:"Line within that file. Absent for a file-level comment."`
	LineType string `json:"lineType,omitempty" jsonschema:"ADDED, REMOVED or CONTEXT."`
	Orphaned bool   `json:"orphaned,omitempty" jsonschema:"Whether the anchored line no longer exists in the diff."`
}

// Reply is a single response inside a thread.
type Reply struct {
	ID     int64  `json:"id" jsonschema:"Comment identifier of the reply."`
	Author string `json:"author,omitempty" jsonschema:"Who wrote it."`
	Date   int64  `json:"date,omitempty" jsonschema:"When, in milliseconds since the epoch."`
	Text   string `json:"text,omitempty" jsonschema:"The reply text."`
}

// Thread is the agent-sized view of a comment thread.
//
// It carries what a caller acts on: identifiers to reply or resolve, the anchor
// to locate the feedback, the text, and enough reply context to tell whether it
// was already addressed.
type Thread struct {
	ID            int64         `json:"id" jsonschema:"Comment identifier of the thread root, which resolve, reopen and react address."`
	Kind          string        `json:"kind" jsonschema:"comment for an ordinary thread, task for a blocker comment."`
	State         string        `json:"state,omitempty" jsonschema:"OPEN, RESOLVED or PENDING."`
	Resolved      bool          `json:"resolved" jsonschema:"Whether the thread has been resolved."`
	Author        string        `json:"author,omitempty" jsonschema:"Who opened the thread."`
	Version       int           `json:"version,omitempty" jsonschema:"Optimistic-locking version of the root comment."`
	CreatedDate   int64         `json:"createdDate,omitempty" jsonschema:"When the thread was opened, in milliseconds since the epoch."`
	UpdatedDate   int64         `json:"updatedDate,omitempty" jsonschema:"When it last changed, in milliseconds since the epoch."`
	Anchor        *ThreadAnchor `json:"anchor,omitempty" jsonschema:"Where in the diff it is anchored. Absent for a pull-request-level comment."`
	Text          string        `json:"text,omitempty" jsonschema:"The thread's opening comment."`
	HasSuggestion bool          `json:"hasSuggestion,omitempty" jsonschema:"Whether the text carries a Bitbucket suggestion block, which bb pr comment apply-suggestion can apply."`
	ReplyCount    int           `json:"replyCount" jsonschema:"How many replies the thread has."`
	LastReply     *Reply        `json:"lastReply,omitempty" jsonschema:"The most recent reply, when there is one."`
	Replies       []Reply       `json:"replies,omitempty" jsonschema:"Every reply. Only populated with --with-replies."`
	URL           string        `json:"url,omitempty" jsonschema:"Link to the thread in the Bitbucket UI."`
}

// ThreadSummary is the aggregate view of a set of threads.
//
// It covers everything the source returned, before any filter. What that spans
// depends on the source: the activity timeline is the whole pull request, the
// path-scoped endpoint is one file, and the blocker endpoint is tasks only.
//
// Tasks are counted twice on purpose: a task is a kind of thread, so an open
// task raises both unresolved and openTasks. Do not add the two together.
type ThreadSummary struct {
	TotalThreads     int `json:"totalThreads" jsonschema:"unresolved + resolved + pending."`
	Unresolved       int `json:"unresolved" jsonschema:"Threads still waiting on someone, tasks included."`
	Resolved         int `json:"resolved" jsonschema:"Threads that have been resolved."`
	Pending          int `json:"pending" jsonschema:"The author's own unpublished draft comments."`
	OpenTasks        int `json:"openTasks" jsonschema:"The subset of unresolved that Bitbucket tracks as tasks."`
	ResolvedTasks    int `json:"resolvedTasks" jsonschema:"The subset of resolved that Bitbucket tracks as tasks."`
	UnresolvedInline int `json:"unresolvedInline,omitempty" jsonschema:"The subset of unresolved anchored to a file."`
}

// CommentAnchor locates a comment in the diff.
//
// Only the fields that say where the comment is. The upstream anchor nests the
// entire pull request -- its author, both refs, and both refs' repositories and
// projects -- underneath, which is how a single comment used to arrive carrying
// tens of kilobytes of context the caller already had.
type CommentAnchor struct {
	Path     string `json:"path,omitempty" jsonschema:"File the comment is anchored to."`
	SrcPath  string `json:"srcPath,omitempty" jsonschema:"Path before a rename, when the file was renamed."`
	Line     int32  `json:"line,omitempty" jsonschema:"Line within that file."`
	LineType string `json:"lineType,omitempty" jsonschema:"ADDED, REMOVED or CONTEXT."`
	FileType string `json:"fileType,omitempty" jsonschema:"FROM or TO, which side of the diff the line is on."`
	DiffType string `json:"diffType,omitempty" jsonschema:"COMMIT, EFFECTIVE or RANGE."`
	FromHash string `json:"fromHash,omitempty" jsonschema:"Commit the diff was taken from."`
	ToHash   string `json:"toHash,omitempty" jsonschema:"Commit the diff was taken to."`
}

// Comment is one pull request comment.
type Comment struct {
	ID           int64          `json:"id,omitempty" jsonschema:"Comment identifier."`
	Version      int32          `json:"version,omitempty" jsonschema:"Optimistic-locking version. Pass it back when updating, or the update is refused."`
	Text         string         `json:"text,omitempty" jsonschema:"The comment text."`
	State        string         `json:"state,omitempty" jsonschema:"OPEN, RESOLVED or PENDING."`
	Severity     string         `json:"severity,omitempty" jsonschema:"NORMAL for an ordinary comment, BLOCKER for a task."`
	Pending      bool           `json:"pending" jsonschema:"Whether this is an unpublished draft comment."`
	Resolved     bool           `json:"resolved" jsonschema:"Whether the thread this comment belongs to is resolved."`
	Anchored     bool           `json:"anchored" jsonschema:"Whether the comment is attached to a line rather than to the pull request."`
	Anchor       *CommentAnchor `json:"anchor,omitempty" jsonschema:"Where in the diff it sits. Absent for a pull-request-level comment."`
	Author       result.User    `json:"author,omitzero" jsonschema:"Who wrote it."`
	ReplyCount   int            `json:"replyCount" jsonschema:"Direct replies to this comment."`
	CreatedDate  int64          `json:"createdDate,omitempty" jsonschema:"When it was written, in milliseconds since the epoch."`
	UpdatedDate  int64          `json:"updatedDate,omitempty" jsonschema:"When it last changed, in milliseconds since the epoch."`
	ResolvedDate int64          `json:"resolvedDate,omitempty" jsonschema:"When it was resolved, in milliseconds since the epoch."`
}

// Change is one file changed in a pull request.
type Change struct {
	Path       string `json:"path" jsonschema:"Path after the change."`
	SrcPath    string `json:"srcPath,omitempty" jsonschema:"Path before a rename or copy."`
	Type       string `json:"type,omitempty" jsonschema:"ADD, MODIFY, DELETE, COPY, MOVE or UNKNOWN."`
	NodeType   string `json:"nodeType,omitempty" jsonschema:"FILE, DIRECTORY or SUBMODULE."`
	Executable bool   `json:"executable,omitempty" jsonschema:"Whether the file is executable after the change."`
}

// BuildStatus is one build reported against the pull request head.
type BuildStatus struct {
	Key   string `json:"key,omitempty" jsonschema:"Build key, unique per commit."`
	State string `json:"state,omitempty" jsonschema:"SUCCESSFUL, FAILED, INPROGRESS, CANCELLED or UNKNOWN."`
	URL   string `json:"url,omitempty" jsonschema:"Link to the build in the reporting system."`
	Name  string `json:"name,omitempty" jsonschema:"Display name of the build."`
}

// AutoMerge is whether a pull request is armed to merge itself.
type AutoMerge struct {
	Enabled    bool   `json:"enabled" jsonschema:"Whether an auto-merge is pending."`
	StrategyID string `json:"strategyId,omitempty" jsonschema:"Merge strategy the auto-merge will use."`
	// MergedImmediately reports that arming merged the pull request rather than
	// queueing it. Enabled is false then: there is no pending auto-merge, and
	// saying otherwise would describe a state that will never fire.
	MergedImmediately bool `json:"mergedImmediately,omitempty" jsonschema:"True when arming merged the pull request outright because its checks already passed. enabled is false in that case: there is nothing left pending."`
}

// JiraIssue is one Jira issue linked to a pull request.
type JiraIssue struct {
	Key string `json:"key" jsonschema:"Issue key, for example PROJ-123."`
	URL string `json:"url,omitempty" jsonschema:"Link to the issue, when the instance reports one."`
}

// Activity is one entry in a pull request's timeline.
//
// raw carries the upstream entry unchanged. Bitbucket's activity types differ
// by action -- an approval, a rescope and a comment share almost no fields --
// so bb names the three every entry has and leaves the rest as it arrived,
// rather than claiming a shape that only holds for some actions.
type Activity struct {
	ID          int64          `json:"id,omitempty" jsonschema:"Activity identifier."`
	Action      string         `json:"action,omitempty" jsonschema:"What happened: OPENED, COMMENTED, APPROVED, RESCOPED, MERGED, DECLINED and so on."`
	CreatedDate int64          `json:"createdDate,omitempty" jsonschema:"When, in milliseconds since the epoch."`
	Comment     *Comment       `json:"comment,omitempty" jsonschema:"The comment, when the action was COMMENTED."`
	Raw         map[string]any `json:"raw" jsonschema:"The upstream activity entry, unchanged. Which fields it carries depends on action."`
}

// Participant is someone who has taken part in a pull request.
type Participant struct {
	Name         string `json:"name" jsonschema:"Username."`
	DisplayName  string `json:"displayName,omitempty" jsonschema:"Human-readable name."`
	EmailAddress string `json:"emailAddress,omitempty" jsonschema:"Email address, when the instance exposes it."`
	Active       bool   `json:"active" jsonschema:"Whether the account is enabled."`
}

// ListFilters echoes back what the listing was narrowed by.
//
// Reported so a caller reading a saved document knows what it is looking at: an
// empty list means something different under state=open than under state=all.
type ListFilters struct {
	State        string `json:"state,omitempty" jsonschema:"State filter: open, merged, declined or all."`
	Start        int    `json:"start" jsonschema:"Offset the page started at."`
	Limit        int    `json:"limit" jsonschema:"Maximum entries requested."`
	SourceBranch string `json:"sourceBranch,omitempty" jsonschema:"Source branch filter, when one was given."`
	TargetBranch string `json:"targetBranch,omitempty" jsonschema:"Target branch filter, when one was given."`
}

// PullRequests is what `bb pr list` returns.
type PullRequests struct {
	Repository      result.Repository    `json:"repository"`
	Filters         ListFilters          `json:"filters"`
	PullRequests    []result.PullRequest `json:"pullRequests" jsonschema:"Matching pull requests. Empty rather than absent when nothing matched."`
	ReviewSummaries []ReviewSummary      `json:"reviewSummaries,omitempty" jsonschema:"One summary per pull request, in the same order. Only present with --with-review-status."`
}

// SinglePullRequest is what `bb pr get` returns.
type SinglePullRequest struct {
	Repository    result.Repository  `json:"repository"`
	PullRequest   result.PullRequest `json:"pullRequest"`
	ReviewSummary ReviewSummary      `json:"reviewSummary" jsonschema:"Outstanding review feedback, so it is visible without a second lookup."`
}

// PullRequestChange is what the commands that act on a pull request return:
// create, update, merge, decline, reopen, approve, unapprove and reviewer
// remove.
type PullRequestChange struct {
	Repository  result.Repository  `json:"repository"`
	PullRequest result.PullRequest `json:"pullRequest" jsonschema:"The pull request as it stands after the change."`
}

// ReviewerAddition is what `bb pr review reviewer add` returns.
//
// The three lists are separate because they are three different outcomes and a
// caller acts on them differently: added changed something, alreadyPresent did
// not, and skippedAuthor is a request that could never have worked.
type ReviewerAddition struct {
	Repository     result.Repository  `json:"repository"`
	PullRequest    result.PullRequest `json:"pullRequest"`
	Added          []string           `json:"added" jsonschema:"Usernames added as reviewers by this call."`
	SkippedAuthor  []string           `json:"skippedAuthor" jsonschema:"Usernames skipped because they opened the pull request. Bitbucket refuses an author as their own reviewer."`
	AlreadyPresent []string           `json:"alreadyPresent" jsonschema:"Usernames that were already reviewers, so nothing changed for them."`
}

// PullRequestCommits is what `bb pr commits` returns.
type PullRequestCommits struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the commits belong to."`
	Commits       []result.Commit   `json:"commits" jsonschema:"Commits that make up the pull request, newest first. Empty rather than absent when there are none."`
}

// PullRequestChanges is what `bb pr files` returns.
type PullRequestChanges struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the changes belong to."`
	Changes       []Change          `json:"changes" jsonschema:"Files changed. Empty rather than absent when there are none."`
}

// MergeBase is what `bb pr merge-base` returns.
type MergeBase struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the merge base was computed for."`
	MergeBase     result.Commit     `json:"mergeBase" jsonschema:"The commit the source and target branches last shared."`
}

// LinkedIssues is what `bb pr jira` returns.
type LinkedIssues struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the issues are linked to."`
	Issues        []JiraIssue       `json:"issues" jsonschema:"Linked Jira issues. Empty rather than absent when there are none."`
}

// ReviewChange is what `bb pr review complete` and `discard` report.
type ReviewChange struct {
	result.Status
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request whose draft review was acted on."`
	Review        string            `json:"review" jsonschema:"completed when the draft was published, discarded when it was thrown away."`
}

// DraftReview is what `bb pr review get` returns.
type DraftReview struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the draft belongs to."`
	Comments      []Comment         `json:"comments" jsonschema:"Unpublished draft comments. Empty rather than absent when there are none."`
}

// CommentThreads is what `bb pr comment list` returns.
//
// comments carries the flat comment list and is present only with --full;
// threads and summary are the grouped view and are always present. Both are
// named fields rather than the payload changing shape with the flag, so one
// command has one contract.
type CommentThreads struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the comments belong to."`
	Source        string            `json:"source" jsonschema:"Which endpoint answered: activity for the timeline, path for the path-scoped endpoint, blocker for the task endpoint. It decides what summary spans."`
	Path          string            `json:"path,omitempty" jsonschema:"File the listing was scoped to, when --path was given."`
	State         string            `json:"state,omitempty" jsonschema:"State filter that was applied: open, resolved, pending or all."`
	Summary       ThreadSummary     `json:"summary"`
	Threads       []Thread          `json:"threads" jsonschema:"Comment threads, unresolved first. Empty rather than absent when there are none."`
	Comments      []Comment         `json:"comments,omitempty" jsonschema:"Every comment, ungrouped. Only present with --full."`
}

// SingleComment is what `bb pr comment get`, `resolve` and `reopen` return.
type SingleComment struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the comment belongs to."`
	Comment       Comment           `json:"comment"`
}

// AddedComment is what `bb pr comment add` returns.
//
// The anchor fields are echoed back because the command accepts them and
// Bitbucket may not honour them: a comment asked for at a line that is not in
// the diff is accepted as a pull-request-level comment instead, and comparing
// what was asked for against comment.anchor is how a caller notices.
type AddedComment struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the comment was added to."`
	Comment       Comment           `json:"comment" jsonschema:"The comment as Bitbucket stored it."`
	Blocker       bool              `json:"blocker" jsonschema:"Whether it was posted as a task rather than an ordinary comment."`
	Pending       bool              `json:"pending" jsonschema:"Whether it was posted as an unpublished draft."`
	Path          string            `json:"path,omitempty" jsonschema:"File it was asked to be anchored to, when --path was given."`
	Line          int               `json:"line,omitempty" jsonschema:"Line it was asked to be anchored to, when --line was given."`
	LineType      string            `json:"lineType,omitempty" jsonschema:"Side of the diff that line is on, when --line-type was given."`
	ParentID      int64             `json:"parentId,omitempty" jsonschema:"Comment it was posted as a reply to, when --parent was given."`
}

// Reaction is what `bb pr comment react` reports.
//
// One shape for adding and removing. Bitbucket answers an add with a reaction
// object that nests the whole comment, which nests the whole pull request; a
// remove has no body at all. Neither was worth publishing, so both report what
// happened instead.
type Reaction struct {
	result.Status
	Action        string            `json:"action" jsonschema:"added or removed."`
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the comment belongs to."`
	CommentID     string            `json:"commentId" jsonschema:"Comment that was reacted to."`
	Emoticon      string            `json:"emoticon" jsonschema:"The reaction, by name, for example thumbsup."`
}

// AppliedSuggestion is what `bb pr comment apply-suggestion` reports.
type AppliedSuggestion struct {
	result.Status
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the suggestion was applied to."`
	CommentID     string            `json:"commentId" jsonschema:"Comment carrying the suggestion that was applied."`
}

// Activities is what `bb pr activity list` returns.
type Activities struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the timeline belongs to."`
	Activities    []Activity        `json:"activities" jsonschema:"Timeline entries, newest first. Empty rather than absent when there are none."`
}

// BuildStatuses is what `bb pr build status` returns.
type BuildStatuses struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request whose head commit was checked."`
	Statuses      []BuildStatus     `json:"statuses" jsonschema:"Builds reported against that commit. Empty rather than absent when there are none."`
}

// AutoMergeState is what `bb pr auto-merge get` and `enable` return.
type AutoMergeState struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request the auto-merge belongs to."`
	AutoMerge     AutoMerge         `json:"autoMerge"`
}

// AutoMergeCancellation is what `bb pr auto-merge disable` reports.
type AutoMergeCancellation struct {
	result.Status
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request whose pending auto-merge was cancelled."`
}

// WatchState is what `bb pr watch` and `bb pr unwatch` report.
type WatchState struct {
	Repository    result.Repository `json:"repository"`
	PullRequestID string            `json:"pullRequestId" jsonschema:"Pull request that was watched or unwatched."`
	Watched       bool              `json:"watched" jsonschema:"Whether you are now watching it."`
}

// RebaseResult is what `bb pr rebase` returns.
//
// The upstream result nests a ref change carrying the branch and the commits it
// moved between; the rest of that object is the same ref spelled three ways.
type RebaseResult struct {
	Repository result.Repository `json:"repository"`
	Ref        result.Ref        `json:"ref,omitzero" jsonschema:"The source branch that was rebased."`
	FromHash   string            `json:"fromHash,omitempty" jsonschema:"Commit the branch pointed at before the rebase."`
	ToHash     string            `json:"toHash,omitempty" jsonschema:"Commit it points at now."`
}

// Participants is what `bb pr participants` returns.
type Participants struct {
	Repository   result.Repository `json:"repository"`
	Participants []Participant     `json:"participants" jsonschema:"People who have taken part in a pull request in this repository. Empty rather than absent when there are none."`
}

// DefaultReviewers is what `bb pr default-reviewers` returns.
type DefaultReviewers struct {
	DefaultReviewers []result.Condition `json:"defaultReviewers" jsonschema:"Conditions that would apply to a pull request between the given refs. Empty rather than absent when none do."`
}

// Checkout is what `bb pr checkout` reports.
type Checkout struct {
	PullRequest      int64  `json:"pullRequest" jsonschema:"Pull request number that was checked out."`
	Branch           string `json:"branch,omitempty" jsonschema:"Local branch the checkout landed on. Absent when --detach was used."`
	Detached         bool   `json:"detached" jsonschema:"Whether the working tree is on a detached HEAD rather than a branch."`
	Remote           string `json:"remote" jsonschema:"Git remote the source branch was fetched from."`
	RemoteURL        string `json:"remoteUrl" jsonschema:"URL of that remote."`
	RemoteAdded      bool   `json:"remoteAdded" jsonschema:"Whether bb added that remote, which happens for a fork it had not seen before."`
	SourceBranch     string `json:"sourceBranch" jsonschema:"Branch the pull request merges from."`
	SourceRepository string `json:"sourceRepository" jsonschema:"Repository that branch lives in, as PROJECT/slug."`
	Fork             bool   `json:"fork" jsonschema:"Whether the pull request comes from a fork."`
	FastForwarded    bool   `json:"fastForwarded" jsonschema:"Whether an existing local branch was fast-forwarded rather than created."`
}

// StatusSection is one group of pull requests in `bb pr status`.
type StatusSection struct {
	PullRequests []result.PullRequest `json:"pullRequests" jsonschema:"Pull requests in this section. Empty rather than absent when there are none."`
	Note         string               `json:"note,omitempty" jsonschema:"Why the section is empty or unavailable, when it is not simply that there are none."`
}

// CurrentBranchSection is the section scoped to the checkout you are in.
type CurrentBranchSection struct {
	StatusSection
	Branch     string `json:"branch,omitempty" jsonschema:"Branch the working tree is on, when there is one."`
	Repository string `json:"repository,omitempty" jsonschema:"Repository that branch belongs to, as PROJECT/slug."`
}

// Status is what `bb pr status` returns.
type Status struct {
	CurrentBranch        CurrentBranchSection `json:"currentBranch" jsonschema:"Pull requests for the branch you are on. Needs a git checkout with a Bitbucket remote; reported as unavailable rather than as an error when there is not one."`
	CreatedByYou         StatusSection        `json:"createdByYou" jsonschema:"Open pull requests you opened, across every repository."`
	RequestingYourReview StatusSection        `json:"requestingYourReview" jsonschema:"Open pull requests waiting on your review, across every repository."`
}
