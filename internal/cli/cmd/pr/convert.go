package prcmd

import (
	"strings"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	jiraservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/jira"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequestactivity"
)

var commentStates = []string{"OPEN", "RESOLVED", "PENDING"}

func init() {
	repositoryEnums := func(prefix string) map[string][]string {
		return map[string][]string{
			prefix + "state":                result.PullRequestStates,
			prefix + "reviewers.role":       result.ReviewerRoles,
			prefix + "reviewers.status":     result.ReviewerStatuses,
			prefix + "mergeability.outcome": result.MergeOutcomes,
		}
	}

	result.Declare("pr list", result.For[PullRequests](repositoryEnums("pullRequests.")))
	result.Declare("pr get", result.For[SinglePullRequest](repositoryEnums("pullRequest.")))
	result.Declare("pr status", result.For[Status](nil))
	result.Declare("pr checkout", result.For[Checkout](nil))

	changeEnums := repositoryEnums("pullRequest.")
	for _, path := range []string{
		"pr create", "pr update", "pr merge", "pr decline", "pr reopen",
		"pr review approve", "pr review unapprove", "pr review reviewer remove",
	} {
		result.Declare(path, result.For[PullRequestChange](changeEnums))
	}
	result.Declare("pr review reviewer add", result.For[ReviewerAddition](changeEnums))

	result.Declare("pr commits", result.For[PullRequestCommits](nil))
	result.Declare("pr files", result.For[PullRequestChanges](nil))
	result.Declare("pr merge-base", result.For[MergeBase](nil))
	result.Declare("pr jira", result.For[LinkedIssues](nil))

	result.Declare("pr review complete", result.For[ReviewChange](map[string][]string{"review": {"completed"}}))
	result.Declare("pr review discard", result.For[ReviewChange](map[string][]string{"review": {"discarded"}}))
	result.Declare("pr review get", result.For[DraftReview](map[string][]string{"comments.state": commentStates}))

	result.Declare("pr comment list", result.For[CommentThreads](map[string][]string{
		"threads.state":  commentStates,
		"comments.state": commentStates,
	}))
	result.Declare("pr comment get", result.For[SingleComment](map[string][]string{"comment.state": commentStates}))
	result.Declare("pr comment resolve", result.For[SingleComment](map[string][]string{"comment.state": commentStates}))
	result.Declare("pr comment reopen", result.For[SingleComment](map[string][]string{"comment.state": commentStates}))
	result.Declare("pr comment add", result.For[AddedComment](map[string][]string{"comment.state": commentStates}))
	result.Declare("pr comment react", result.For[Reaction](map[string][]string{"action": {"added", "removed"}}))
	result.Declare("pr comment apply-suggestion", result.For[AppliedSuggestion](nil))

	result.Declare("pr activity list", result.For[Activities](nil))
	result.Declare("pr build status", result.For[BuildStatuses](map[string][]string{
		"statuses.state": {"SUCCESSFUL", "FAILED", "INPROGRESS", "CANCELLED", "UNKNOWN"},
	}))

	result.Declare("pr auto-merge get", result.For[AutoMergeState](nil))
	result.Declare("pr auto-merge enable", result.For[AutoMergeState](nil))
	result.Declare("pr auto-merge disable", result.For[AutoMergeCancellation](nil))

	result.Declare("pr watch", result.For[WatchState](nil))
	result.Declare("pr unwatch", result.For[WatchState](nil))
	result.Declare("pr rebase", result.For[RebaseResult](map[string][]string{"ref.type": result.RefTypes}))
	result.Declare("pr participants", result.For[Participants](nil))
	result.Declare("pr default-reviewers", result.For[DefaultReviewers](map[string][]string{
		"defaultReviewers.sourceRefMatcher.type": result.RefMatcherTypes,
		"defaultReviewers.targetRefMatcher.type": result.RefMatcherTypes,
		"defaultReviewers.scope":                 result.ConditionScopes,
	}))

	// pr diff is not declared here. It is an alias for bb diff pr and shares
	// that command's writer, so the two describe one contract declared with the
	// diff commands rather than two that can disagree.
}

// repositoryOf converts the service reference used throughout this package.
func repositoryOf(repo pullrequestservice.RepositoryRef) result.Repository {
	return result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}
}

// reviewSummaryFrom converts the service's review summary.
func reviewSummaryFrom(upstream pullrequestservice.ReviewSummary) ReviewSummary {
	return ReviewSummary{
		ActionRequired:    upstream.ActionRequired,
		UnresolvedThreads: upstream.UnresolvedThreads,
		OpenTasks:         upstream.OpenTasks,
		ResolvedThreads:   upstream.ResolvedThreads,
		ResolvedTasks:     upstream.ResolvedTasks,
		PendingComments:   upstream.PendingComments,
		NeedsWork:         upstream.NeedsWork,
		Approvals:         upstream.Approvals,
		Reviewers:         upstream.Reviewers,
		CommentCount:      upstream.CommentCount,
		CountsSource:      upstream.CountsSource,
	}
}

// reviewSummariesFrom converts a list, preserving order and never returning nil.
func reviewSummariesFrom(upstream []pullrequestservice.ReviewSummary) []ReviewSummary {
	converted := make([]ReviewSummary, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, reviewSummaryFrom(one))
	}

	return converted
}

// commitFrom converts a pull request commit into the shared commit shape.
//
// The pull request endpoints return a flatter commit than the repository ones
// -- an author name and email rather than a person object, and no committer --
// so this fills what it has and leaves the rest empty rather than publishing a
// second commit shape for the same concept.
func commitFrom(upstream pullrequestservice.Commit) result.Commit {
	return result.Commit{
		ID:              upstream.ID,
		DisplayID:       upstream.DisplayID,
		Message:         upstream.Message,
		Author:          result.Person{Name: upstream.Author, EmailAddress: upstream.AuthorEmail},
		AuthorTimestamp: upstream.AuthorTimestamp,
	}
}

// commitsFrom converts a list, preserving order and never returning nil.
func commitsFrom(upstream []pullrequestservice.Commit) []result.Commit {
	converted := make([]result.Commit, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, commitFrom(one))
	}

	return converted
}

// changesFrom converts the changed-file list, never returning nil.
func changesFrom(upstream []pullrequestservice.Change) []Change {
	converted := make([]Change, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, Change{
			Path:       one.Path,
			SrcPath:    one.SrcPath,
			Type:       one.Type,
			NodeType:   one.NodeType,
			Executable: one.Executable,
		})
	}

	return converted
}

// buildStatusesFrom converts the build list, never returning nil.
func buildStatusesFrom(upstream []pullrequestservice.BuildStatus) []BuildStatus {
	converted := make([]BuildStatus, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, BuildStatus{Key: one.Key, State: one.State, URL: one.URL, Name: one.Name})
	}

	return converted
}

// autoMergeFrom converts the auto-merge state.
func autoMergeFrom(upstream pullrequestservice.AutoMerge) AutoMerge {
	return AutoMerge{
		Enabled:           upstream.Enabled,
		StrategyID:        upstream.StrategyID,
		MergedImmediately: upstream.MergedImmediately,
	}
}

// participantsFrom converts the participant list, never returning nil.
func participantsFrom(upstream []pullrequestservice.Participant) []Participant {
	converted := make([]Participant, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, Participant{
			Name:         one.Name,
			DisplayName:  one.DisplayName,
			EmailAddress: one.EmailAddress,
			Active:       one.Active,
		})
	}

	return converted
}

// issuesFrom converts the linked Jira issues, never returning nil.
func issuesFrom(upstream []jiraservice.JiraIssue) []JiraIssue {
	converted := make([]JiraIssue, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, JiraIssue{Key: one.Key, URL: one.URL})
	}

	return converted
}

// rebaseResultFrom pulls the branch and the two commit ids out of the upstream
// rebase result.
func rebaseResultFrom(repo result.Repository, upstream *openapigenerated.RestPullRequestRebaseResult) RebaseResult {
	converted := RebaseResult{Repository: repo}
	if upstream == nil || upstream.RefChange == nil {
		return converted
	}

	change := upstream.RefChange
	converted.FromHash = stringValue(change.FromHash)
	converted.ToHash = stringValue(change.ToHash)
	if change.Ref != nil {
		converted.Ref = result.Ref{
			ID:        change.Ref.Id,
			DisplayID: change.Ref.DisplayId,
			Type:      string(change.Ref.Type),
		}
	} else if change.RefId != nil {
		converted.Ref = result.Ref{ID: *change.RefId}
	}

	return converted
}

// threadFrom converts one activity thread.
func threadFrom(upstream pullrequestactivityservice.Thread) Thread {
	converted := Thread{
		ID:            upstream.ID,
		Kind:          upstream.Kind,
		State:         upstream.State,
		Resolved:      upstream.Resolved,
		Author:        upstream.Author,
		Version:       upstream.Version,
		CreatedDate:   upstream.CreatedDate,
		UpdatedDate:   upstream.UpdatedDate,
		Text:          upstream.Text,
		HasSuggestion: upstream.HasSuggestion,
		ReplyCount:    upstream.ReplyCount,
		URL:           upstream.URL,
	}
	if upstream.Anchor != nil {
		converted.Anchor = &ThreadAnchor{
			Path:     upstream.Anchor.Path,
			Line:     upstream.Anchor.Line,
			LineType: upstream.Anchor.LineType,
			Orphaned: upstream.Anchor.Orphaned,
		}
	}
	if upstream.LastReply != nil {
		reply := replyFrom(*upstream.LastReply)
		converted.LastReply = &reply
	}
	if upstream.Replies != nil {
		converted.Replies = make([]Reply, 0, len(upstream.Replies))
		for _, one := range upstream.Replies {
			converted.Replies = append(converted.Replies, replyFrom(one))
		}
	}

	return converted
}

func replyFrom(upstream pullrequestactivityservice.Reply) Reply {
	return Reply{ID: upstream.ID, Author: upstream.Author, Date: upstream.Date, Text: upstream.Text}
}

// threadsFrom converts a list, preserving order and never returning nil.
func threadsFrom(upstream []pullrequestactivityservice.Thread) []Thread {
	converted := make([]Thread, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, threadFrom(one))
	}

	return converted
}

// threadSummaryFrom converts the aggregate view.
func threadSummaryFrom(upstream pullrequestactivityservice.Summary) ThreadSummary {
	return ThreadSummary{
		TotalThreads:     upstream.TotalThreads,
		Unresolved:       upstream.Unresolved,
		Resolved:         upstream.Resolved,
		Pending:          upstream.Pending,
		OpenTasks:        upstream.OpenTasks,
		ResolvedTasks:    upstream.ResolvedTasks,
		UnresolvedInline: upstream.UnresolvedInline,
	}
}

// commentFrom converts one upstream comment.
//
// The upstream anchor nests the entire pull request, and the comment nests its
// replies, each of which nests the same again. Only what identifies and locates
// the comment survives; the reply bodies are a count, and bb pr comment list
// is the command that returns them properly.
func commentFrom(upstream openapigenerated.RestComment) Comment {
	converted := Comment{
		Text:     stringValue(upstream.Text),
		State:    stringValue(upstream.State),
		Severity: stringValue(upstream.Severity),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Version != nil {
		converted.Version = *upstream.Version
	}
	if upstream.Pending != nil {
		converted.Pending = *upstream.Pending
	}
	if upstream.ThreadResolved != nil {
		converted.Resolved = *upstream.ThreadResolved
	}
	if upstream.Anchored != nil {
		converted.Anchored = *upstream.Anchored
	}
	if upstream.CreatedDate != nil {
		converted.CreatedDate = *upstream.CreatedDate
	}
	if upstream.UpdatedDate != nil {
		converted.UpdatedDate = *upstream.UpdatedDate
	}
	if upstream.ResolvedDate != nil {
		converted.ResolvedDate = *upstream.ResolvedDate
	}
	if upstream.Comments != nil {
		converted.ReplyCount = len(*upstream.Comments)
	}
	if upstream.Author != nil {
		converted.Author = result.User{
			Name:         upstream.Author.Name,
			DisplayName:  upstream.Author.DisplayName,
			EmailAddress: stringValue(upstream.Author.EmailAddress),
			Slug:         upstream.Author.Slug,
			Type:         string(upstream.Author.Type),
		}
		if upstream.Author.Id != nil {
			converted.Author.ID = *upstream.Author.Id
		}
		if upstream.Author.Active != nil {
			converted.Author.Active = *upstream.Author.Active
		}
	}
	if upstream.Anchor != nil {
		anchor := CommentAnchor{
			Path:     joinCommentPath(pathComponents(upstream.Anchor.Path)),
			SrcPath:  joinCommentPath(pathComponents(upstream.Anchor.SrcPath)),
			FromHash: stringValue(upstream.Anchor.FromHash),
			ToHash:   stringValue(upstream.Anchor.ToHash),
		}
		if upstream.Anchor.Line != nil {
			anchor.Line = *upstream.Anchor.Line
		}
		if upstream.Anchor.LineType != nil {
			anchor.LineType = string(*upstream.Anchor.LineType)
		}
		if upstream.Anchor.FileType != nil {
			anchor.FileType = string(*upstream.Anchor.FileType)
		}
		if upstream.Anchor.DiffType != nil {
			anchor.DiffType = string(*upstream.Anchor.DiffType)
		}
		converted.Anchor = &anchor
	}

	return converted
}

// commentsFrom converts a list, preserving order and never returning nil.
func commentsFrom(upstream []openapigenerated.RestComment) []Comment {
	converted := make([]Comment, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, commentFrom(one))
	}

	return converted
}

// joinCommentPath renders Bitbucket's path object as a path.
//
// The upstream splits a path into components, a parent and a name, all of which
// say the same thing. A caller wants the path.
func joinCommentPath(components []string) string {
	return strings.Join(components, "/")
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

// pathComponents reads the components out of Bitbucket's path object.
func pathComponents(path *struct {
	Components *[]string `json:"components,omitempty"`
	Extension  *string   `json:"extension,omitempty"`
	Name       *string   `json:"name,omitempty"`
	Parent     *string   `json:"parent,omitempty"`
}) []string {
	if path == nil || path.Components == nil {
		return nil
	}

	return *path.Components
}

// activityFrom converts one timeline entry.
func activityFrom(upstream pullrequestactivityservice.Activity) Activity {
	converted := Activity{
		ID:          upstream.ID,
		Action:      upstream.Action,
		CreatedDate: upstream.CreatedDate,
		Raw:         upstream.Raw,
	}
	if converted.Raw == nil {
		converted.Raw = map[string]any{}
	}
	if upstream.Comment != nil {
		comment := commentFrom(*upstream.Comment)
		converted.Comment = &comment
	}

	return converted
}

// activitiesFrom converts a list, preserving order and never returning nil.
func activitiesFrom(upstream []pullrequestactivityservice.Activity) []Activity {
	converted := make([]Activity, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, activityFrom(one))
	}

	return converted
}

// checkoutFrom converts the internal checkout outcome.
func checkoutFrom(outcome checkoutResult) Checkout {
	return Checkout(outcome)
}
