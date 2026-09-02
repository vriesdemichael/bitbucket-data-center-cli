package prcmd

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

const reviewSummaryPageSize = 25

func commentIDString(comment openapigenerated.RestComment) string {
	if comment.Id == nil {
		return "unknown"
	}
	return strconv.FormatInt(*comment.Id, 10)
}

func formatCommentSummary(comment openapigenerated.RestComment) string {
	text := ""
	if comment.Text != nil {
		text = strings.TrimSpace(*comment.Text)
	}
	if text == "" {
		text = "<empty>"
	}

	version := "?"
	if comment.Version != nil {
		version = strconv.Itoa(int(*comment.Version))
	}

	return fmt.Sprintf("[%s v%s] %s", commentIDString(comment), version, text)
}

func formatCommentDetail(comment openapigenerated.RestComment) string {
	lines := []string{formatCommentSummary(comment)}
	if anchorPath := commentAnchorPath(comment); anchorPath != "" {
		lines = append(lines, fmt.Sprintf("Path: %s", anchorPath))
	}
	if author := commentAuthorName(comment); author != "" {
		lines = append(lines, fmt.Sprintf("Author: %s", author))
	}
	if state := safeString(comment.State); state != "" {
		lines = append(lines, fmt.Sprintf("State: %s", state))
	}
	if text := strings.TrimSpace(safeString(comment.Text)); text != "" {
		lines = append(lines, "")
		lines = append(lines, text)
	}

	return strings.Join(lines, "\n")
}

// formatThreadCounts renders the one-line header that tells an agent whether a
// comment listing contains anything actionable.
func formatThreadCounts(summary pullrequestactivityservice.Summary) string {
	parts := make([]string, 0, 4)
	if summary.Unresolved > 0 {
		parts = append(parts, fmt.Sprintf("%d unresolved", summary.Unresolved))
	}
	if summary.OpenTasks > 0 {
		parts = append(parts, fmt.Sprintf("%d open %s", summary.OpenTasks, plural(summary.OpenTasks, "task", "tasks")))
	}
	if summary.Resolved > 0 {
		parts = append(parts, fmt.Sprintf("%d resolved", summary.Resolved))
	}
	if summary.Pending > 0 {
		parts = append(parts, fmt.Sprintf("%d pending", summary.Pending))
	}
	if len(parts) == 0 {
		return "No comments"
	}

	return strings.Join(parts, ", ")
}

// formatThread renders a single thread as a marker line plus its indented body.
// Unresolved threads are marked with "!" so they stand out in a long listing.
func formatThread(thread pullrequestactivityservice.Thread) string {
	marker := "! "
	switch {
	case thread.Resolved:
		marker = "  "
	case strings.EqualFold(thread.State, pullrequestactivityservice.ThreadStatePending):
		marker = "? "
	}

	header := fmt.Sprintf("%s[%d]", marker, thread.ID)
	if thread.Author != "" {
		header += " " + thread.Author
	}
	if location := formatThreadAnchor(thread.Anchor); location != "" {
		header += "  " + location
	}
	if attributes := formatThreadAttributes(thread); attributes != "" {
		header += "  " + attributes
	}

	lines := []string{header}
	for _, line := range strings.Split(strings.TrimSpace(thread.Text), "\n") {
		lines = append(lines, "    "+line)
	}
	for _, reply := range thread.Replies {
		lines = append(lines, fmt.Sprintf("    > %s: %s", reply.Author, singleLine(reply.Text)))
	}

	return strings.Join(lines, "\n")
}

func formatThreadAnchor(anchor *pullrequestactivityservice.Anchor) string {
	if anchor == nil {
		return ""
	}

	location := anchor.Path
	if anchor.Line > 0 {
		location = fmt.Sprintf("%s:%d", location, anchor.Line)
	}
	if anchor.Orphaned {
		location += " (outdated)"
	}

	return location
}

func formatThreadAttributes(thread pullrequestactivityservice.Thread) string {
	attributes := make([]string, 0, 4)
	if thread.Kind == pullrequestactivityservice.ThreadKindTask {
		attributes = append(attributes, "task")
	}
	if thread.Resolved {
		attributes = append(attributes, "resolved")
	}
	if strings.EqualFold(thread.State, pullrequestactivityservice.ThreadStatePending) {
		attributes = append(attributes, "pending")
	}
	if thread.HasSuggestion {
		attributes = append(attributes, "suggestion")
	}
	if thread.ReplyCount > 0 {
		attributes = append(attributes, pluralize(thread.ReplyCount, "reply"))
	}
	if len(attributes) == 0 {
		return ""
	}

	return "(" + strings.Join(attributes, ", ") + ")"
}

// formatReviewSummaryLines renders the outstanding-feedback lines appended to
// `bb pr get` output. When no counts were measured it says so rather than
// claiming the pull request is clear.
func formatReviewSummaryLines(summary pullrequestservice.ReviewSummary) []string {
	lines := make([]string, 0, 2)

	if !summary.Measured() {
		lines = append(lines, "Open items: not checked")
	} else {
		parts := make([]string, 0, 2)
		if count := summary.UnresolvedThreads; count != nil && *count > 0 {
			parts = append(parts, fmt.Sprintf("%d unresolved %s", *count, plural(*count, "comment", "comments")))
		}
		if count := summary.OpenTasks; count != nil && *count > 0 {
			parts = append(parts, fmt.Sprintf("%d open %s", *count, plural(*count, "task", "tasks")))
		}
		if len(parts) == 0 {
			lines = append(lines, "Open items: none")
		} else {
			lines = append(lines, "Open items: "+strings.Join(parts, ", "))
		}
	}

	if len(summary.NeedsWork) > 0 {
		lines = append(lines, "Needs work: "+strings.Join(summary.NeedsWork, ", "))
	}

	return lines
}

func resolveReviewCounts(
	ctx context.Context,
	client *openapigenerated.ClientWithResponses,
	repo pullrequestservice.RepositoryRef,
	pullRequestID string,
) (pullrequestservice.ReviewCounts, error) {
	activityService := pullrequestactivityservice.NewService(client)
	threads, err := activityService.TrySummarize(
		ctx,
		pullrequestactivityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
		pullRequestID,
		reviewSummaryPageSize,
	)
	if err != nil {
		return pullrequestservice.ReviewCounts{}, err
	}
	if threads != nil {
		return pullrequestservice.ReviewCounts{Threads: threads}, nil
	}

	tasks, err := commentservice.NewService(client).CountTasks(ctx, commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, pullRequestID)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return pullrequestservice.ReviewCounts{}, err
		}
		return pullrequestservice.ReviewCounts{}, nil
	}

	return pullrequestservice.ReviewCounts{Tasks: &pullrequestservice.TaskCounts{Open: tasks.Open, Resolved: tasks.Resolved}}, nil
}

func collectReviewSummaries(
	ctx context.Context,
	client *openapigenerated.ClientWithResponses,
	repo pullrequestservice.RepositoryRef,
	pullRequests []pullrequestservice.PullRequest,
) ([]pullrequestservice.ReviewSummary, error) {
	summaries := make([]pullrequestservice.ReviewSummary, len(pullRequests))

	for index, pullRequest := range pullRequests {
		counts, err := resolveReviewCounts(ctx, client, repo, strconv.FormatInt(pullRequest.ID, 10))
		if err != nil {
			return nil, err
		}

		summaries[index] = pullrequestservice.BuildReviewSummary(pullRequest, counts)
	}

	return summaries, nil
}

func formatReviewStatusIndicator(summary pullrequestservice.ReviewSummary) string {
	parts := make([]string, 0, 3)
	if count := summary.UnresolvedThreads; count != nil && *count > 0 {
		parts = append(parts, fmt.Sprintf("unresolved:%d", *count))
	}
	if count := summary.OpenTasks; count != nil && *count > 0 {
		parts = append(parts, fmt.Sprintf("tasks:%d", *count))
	}
	if len(summary.NeedsWork) > 0 {
		parts = append(parts, "needs-work")
	}
	if len(parts) == 0 {
		return ""
	}

	return "[" + strings.Join(parts, " ") + "]"
}

func formatPullRequestCounts(pullRequest result.PullRequest) string {
	parts := make([]string, 0, 2)
	if pullRequest.OpenTaskCount != nil && *pullRequest.OpenTaskCount > 0 {
		parts = append(parts, fmt.Sprintf("tasks:%d", *pullRequest.OpenTaskCount))
	}
	if pullRequest.CommentCount != nil && *pullRequest.CommentCount > 0 {
		parts = append(parts, fmt.Sprintf("comments:%d", *pullRequest.CommentCount))
	}
	if len(parts) == 0 {
		return ""
	}

	return "[" + strings.Join(parts, " ") + "]"
}

func pluralize(count int, singular string) string {
	suffix := singular
	if count != 1 {
		if strings.HasSuffix(singular, "y") {
			suffix = strings.TrimSuffix(singular, "y") + "ies"
		} else {
			suffix = singular + "s"
		}
	}

	return fmt.Sprintf("%d %s", count, suffix)
}

func plural(count int, singular string, pluralForm string) string {
	if count == 1 {
		return singular
	}
	return pluralForm
}

func singleLine(text string) string {
	collapsed := strings.Join(strings.Fields(text), " ")
	if len(collapsed) > 120 {
		return collapsed[:117] + "..."
	}
	return collapsed
}

func formatPullRequestActivitySummary(activity pullrequestactivityservice.Activity) string {
	action := strings.TrimSpace(activity.Action)
	if action == "" {
		action = "UNKNOWN"
	}
	if activity.Comment != nil {
		return fmt.Sprintf("[%d %s] %s", activity.ID, action, formatCommentSummary(*activity.Comment))
	}

	return fmt.Sprintf("[%d %s]", activity.ID, action)
}

func commentAnchorPath(comment openapigenerated.RestComment) string {
	if comment.Anchor == nil || comment.Anchor.Path == nil {
		return ""
	}
	if comment.Anchor.Path.Parent != nil && comment.Anchor.Path.Name != nil {
		parent := strings.TrimSpace(*comment.Anchor.Path.Parent)
		name := strings.TrimSpace(*comment.Anchor.Path.Name)
		if parent == "" {
			return name
		}
		if name == "" {
			return parent
		}
		return parent + "/" + name
	}
	if comment.Anchor.Path.Name != nil {
		return strings.TrimSpace(*comment.Anchor.Path.Name)
	}

	return ""
}

func commentAuthorName(comment openapigenerated.RestComment) string {
	if comment.Author == nil {
		return ""
	}
	if displayName := strings.TrimSpace(comment.Author.DisplayName); displayName != "" {
		return displayName
	}
	return strings.TrimSpace(comment.Author.Name)
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func safeInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func safeInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func safeStringSlice(values *[]string) []string {
	if values == nil {
		return nil
	}
	return *values
}

func safeUsers(values *[]openapigenerated.RestApplicationUser) []openapigenerated.RestApplicationUser {
	if values == nil {
		return nil
	}
	return *values
}

func printDefaultReviewers(cmd *cobra.Command, conditions []openapigenerated.RestPullRequestCondition) {
	if len(conditions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No default reviewers or conditions found"))
		return
	}

	rows := make([][]string, len(conditions))
	for i, c := range conditions {
		idStr := ""
		if c.Id != nil {
			idStr = fmt.Sprintf("%d", *c.Id)
		}

		sourceRef := "ANY"
		if c.SourceRefMatcher != nil && c.SourceRefMatcher.DisplayId != nil {
			sourceRef = *c.SourceRefMatcher.DisplayId
		}

		targetRef := "ANY"
		if c.TargetRefMatcher != nil && c.TargetRefMatcher.DisplayId != nil {
			targetRef = *c.TargetRefMatcher.DisplayId
		}

		reqApprovals := "0"
		if c.RequiredApprovals != nil {
			reqApprovals = fmt.Sprintf("%d", *c.RequiredApprovals)
		}

		var reviewers []string
		if c.Reviewers != nil {
			for _, r := range *c.Reviewers {
				name := ""
				if r.Name != nil {
					name = *r.Name
				}
				if name != "" {
					reviewers = append(reviewers, name)
				}
			}
		}
		reviewersStr := strings.Join(reviewers, ", ")

		rows[i] = []string{
			style.Secondary.Render(idStr),
			style.Resource.Render(sourceRef),
			style.Resource.Render(targetRef),
			reqApprovals,
			reviewersStr,
		}
	}

	style.WriteTable(cmd.OutOrStdout(), rows)
}

func hasApprovedReviewer(reviewers []pullrequestservice.Reviewer) bool {
	for _, reviewer := range reviewers {
		if reviewer.Approved || strings.EqualFold(strings.TrimSpace(reviewer.Status), "APPROVED") {
			return true
		}
	}

	return false
}

func reviewerApprovedByUser(reviewers []pullrequestservice.Reviewer, username string) bool {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return false
	}
	for _, reviewer := range reviewers {
		if strings.EqualFold(strings.TrimSpace(reviewer.Name), trimmed) && (reviewer.Approved || strings.EqualFold(strings.TrimSpace(reviewer.Status), "APPROVED")) {
			return true
		}
	}
	return false
}

func hasReviewer(reviewers []pullrequestservice.Reviewer, username string) bool {
	trimmed := strings.TrimSpace(username)
	for _, reviewer := range reviewers {
		if strings.EqualFold(strings.TrimSpace(reviewer.Name), trimmed) {
			return true
		}
	}
	return false
}

func shortCommitID(commit pullrequestservice.Commit) string {
	if strings.TrimSpace(commit.DisplayID) != "" {
		return commit.DisplayID
	}
	id := strings.TrimSpace(commit.ID)
	if len(id) > 11 {
		return id[:11]
	}
	return id
}

func firstMessageLine(message string) string {
	trimmed := strings.TrimSpace(message)
	if index := strings.IndexByte(trimmed, '\n'); index >= 0 {
		return strings.TrimSpace(trimmed[:index])
	}
	return trimmed
}

func normalizeEmoticon(e string) string {
	return strings.Trim(e, ":")
}
