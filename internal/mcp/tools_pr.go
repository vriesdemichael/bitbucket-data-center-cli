package mcp

import (
	"context"
	"strconv"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	browseservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/browse"
	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
	diffservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/diff"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequestactivity"
)

func specGetPullRequest() Spec {
	tool := mcpgo.NewTool("get_pull_request",
		mcpgo.WithDescription("Get pull request details including title, state, reviewer approvals, and merge status. "+
			"The review_summary field reports unresolved comment threads, open tasks and reviewers who requested changes; "+
			"action_required is true when the pull request is waiting on the author."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key (e.g. MYPROJECT)")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithBoolean("skip_review_summary", mcpgo.Description("Skip the activity timeline lookup used to count unresolved comment threads")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		activitySvc := pullrequestactivityservice.NewService(c.OpenAPI)
		commentSvc := commentservice.NewService(c.OpenAPI)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			id, _ := req.RequireString("id")
			pr, err := svc.Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo}, id)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("get_pull_request failed", err), nil
			}

			counts := pullrequestservice.ReviewCounts{}
			if !req.GetBool("skip_review_summary", false) {
				threads, summaryErr := activitySvc.TrySummarize(ctx, pullrequestactivityservice.RepositoryRef{ProjectKey: project, Slug: repo}, id, 100)
				if summaryErr != nil {
					return mcpgo.NewToolResultErrorFromErr("get_pull_request failed", summaryErr), nil
				}
				switch {
				case threads != nil:
					counts.Threads = threads
				default:
					// Bitbucket 10.x omits the task counters on this endpoint,
					// so fall back to the exact blocker-comment tally rather
					// than reporting nothing.
					if tasks, taskErr := commentSvc.CountTasks(ctx, commentservice.RepositoryRef{ProjectKey: project, Slug: repo}, id); taskErr == nil {
						counts.Tasks = &pullrequestservice.TaskCounts{Open: tasks.Open, Resolved: tasks.Resolved}
					}
				}
			}

			return resultJSON(map[string]any{
				"pull_request":   pr,
				"review_summary": pullrequestservice.BuildReviewSummary(pr, counts),
			})
		}
	}}
}

func specListPullRequests() Spec {
	tool := mcpgo.NewTool("list_pull_requests",
		mcpgo.WithDescription("List pull requests. Without project/repo, lists the current user's PRs across all repositories (dashboard)."),
		mcpgo.WithString("project", mcpgo.Description("Bitbucket project key (omit for dashboard mode)")),
		mcpgo.WithString("repo", mcpgo.Description("Repository slug (omit for dashboard mode)")),
		mcpgo.WithString("state", mcpgo.Description("Filter by state: OPEN (default), MERGED, DECLINED, ALL")),
		mcpgo.WithString("role", mcpgo.Description("Filter by role: REVIEWER, AUTHOR, or PARTICIPANT (works in both repo and dashboard mode)")),
		mcpgo.WithString("source_branch", mcpgo.Description("Filter by source branch name (repo mode only)")),
		mcpgo.WithString("target_branch", mcpgo.Description("Filter by target branch name (repo mode only)")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum number of results (default 25)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project := req.GetString("project", "")
			repo := req.GetString("repo", "")
			state := req.GetString("state", "OPEN")
			limit := req.GetInt("limit", 25)

			var prs []pullrequestservice.PullRequest
			var err error

			if project != "" && repo != "" {
				opts := pullrequestservice.ListOptions{
					State:        state,
					Role:         req.GetString("role", ""),
					SourceBranch: req.GetString("source_branch", ""),
					TargetBranch: req.GetString("target_branch", ""),
					Limit:        limit,
				}
				prs, err = svc.List(ctx, pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo}, opts)
			} else if project != "" || repo != "" {
				return mcpgo.NewToolResultError("list_pull_requests requires both project and repo, or neither for dashboard mode"), nil
			} else {
				opts := pullrequestservice.DashboardListOptions{
					State: state,
					Role:  req.GetString("role", "REVIEWER"),
					Limit: limit,
				}
				prs, err = svc.ListDashboard(ctx, opts)
			}
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("list_pull_requests failed", err), nil
			}
			return resultJSON(map[string]any{"pull_requests": prs})
		}
	}}
}

func specCreatePullRequest() Spec {
	tool := mcpgo.NewTool("create_pull_request",
		mcpgo.WithDescription("Create a new pull request."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("from_ref", mcpgo.Required(), mcpgo.Description("Source branch name (e.g. feature/my-work)")),
		mcpgo.WithString("to_ref", mcpgo.Description("Target branch name; defaults to repository default branch")),
		mcpgo.WithString("title", mcpgo.Required(), mcpgo.Description("Pull request title")),
		mcpgo.WithString("description", mcpgo.Description("Pull request description (optional)")),
		mcpgo.WithString("reviewers", mcpgo.Description("Comma-separated reviewer usernames to add (e.g. alice,bob)")),
		mcpgo.WithBoolean("draft", mcpgo.Description("Create as a draft pull request (Bitbucket DC 8.0+; default false)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			fromRef, _ := req.RequireString("from_ref")
			title, _ := req.RequireString("title")
			pr, err := svc.Create(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo},
				pullrequestservice.CreateInput{
					FromRef:     fromRef,
					ToRef:       req.GetString("to_ref", ""),
					Title:       title,
					Description: req.GetString("description", ""),
					Reviewers:   parseCommaList(req.GetString("reviewers", "")),
					Draft:       req.GetBool("draft", false),
				},
			)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("create_pull_request failed", err), nil
			}
			return resultJSON(pr)
		}
	}}
}

func specListPRComments() Spec {
	tool := mcpgo.NewTool("list_pr_comments",
		mcpgo.WithDescription("List review comment threads on a pull request, unresolved first. Bitbucket models a task "+
			"as a blocker comment, so this returns reviewer comments and tasks together, each with its resolution state, "+
			"file anchor and reply count. Use state=open to see only what is still waiting on the author. Without path "+
			"this returns the aggregate pull request comment view derived from activities."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithString("path", mcpgo.Description("Optional file path to restrict comments to a single diff path")),
		mcpgo.WithString("state", mcpgo.Description("Filter threads by resolution state: open, resolved, pending, or all (default)")),
		mcpgo.WithBoolean("tasks_only", mcpgo.Description("Return only threads Bitbucket tracks as tasks (blocker comments)")),
		mcpgo.WithBoolean("with_replies", mcpgo.Description("Include the full text of every reply instead of only the most recent one")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum number of results (default 25)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		commentSvc := commentservice.NewService(c.OpenAPI)
		activitySvc := pullrequestactivityservice.NewService(c.OpenAPI)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")
			path := req.GetString("path", "")
			limit := req.GetInt("limit", 25)

			state, err := pullrequestactivityservice.NormalizeThreadState(req.GetString("state", "all"))
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("list_pr_comments failed", err), nil
			}

			threadOptions := pullrequestactivityservice.ThreadOptions{
				State:         state,
				TasksOnly:     req.GetBool("tasks_only", false),
				WithReplies:   req.GetBool("with_replies", false),
				BaseURL:       c.BaseURL,
				ProjectKey:    project,
				Slug:          repo,
				PullRequestID: prID,
			}

			var threads []pullrequestactivityservice.Thread
			var summary pullrequestactivityservice.Summary
			if path == "" {
				activities, listErr := activitySvc.List(ctx, pullrequestactivityservice.RepositoryRef{ProjectKey: project, Slug: repo}, prID, pullrequestactivityservice.ListOptions{Limit: limit})
				if listErr != nil {
					return mcpgo.NewToolResultErrorFromErr("list_pr_comments failed", listErr), nil
				}
				threads, summary = pullrequestactivityservice.ExtractThreads(activities, threadOptions)
			} else {
				target := commentservice.Target{
					Repository:    commentservice.RepositoryRef{ProjectKey: project, Slug: repo},
					PullRequestID: prID,
				}
				comments, listErr := commentSvc.List(ctx, target, path, limit)
				if listErr != nil {
					return mcpgo.NewToolResultErrorFromErr("list_pr_comments failed", listErr), nil
				}
				threads, summary = pullrequestactivityservice.ThreadsFromComments(comments, threadOptions)
			}

			return resultJSON(map[string]any{"summary": summary, "threads": threads})
		}
	}}
}

func specAddPRComment() Spec {
	tool := mcpgo.NewTool("add_pr_comment",
		mcpgo.WithDescription("Add a comment to a pull request. Provide path and line to create an inline comment on a specific file line. Provide parent_id to reply to an existing comment."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithString("text", mcpgo.Required(), mcpgo.Description("Comment text (Markdown supported)")),
		mcpgo.WithString("path", mcpgo.Description("File path for inline comment (e.g. src/main.go)")),
		mcpgo.WithNumber("line", mcpgo.Description("Line number for inline comment")),
		mcpgo.WithString("line_type", mcpgo.Description("Diff side for inline comment: ADDED (default), REMOVED, or CONTEXT")),
		mcpgo.WithNumber("parent_id", mcpgo.Description("Parent comment ID to reply to")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")
			text, _ := req.RequireString("text")
			filePath := strings.TrimSpace(req.GetString("path", ""))
			line := req.GetInt("line", 0)
			lineType := strings.TrimSpace(req.GetString("line_type", ""))
			parentID := int64(req.GetInt("parent_id", 0))
			inline := filePath != "" || line > 0

			// Reject partial or conflicting anchors rather than silently
			// downgrading to a general comment, which would leave the comment
			// attached to the wrong place with no indication anything was off.
			if inline && filePath == "" {
				return mcpgo.NewToolResultError("add_pr_comment: line requires path for an inline comment"), nil
			}
			if inline && line <= 0 {
				return mcpgo.NewToolResultError("add_pr_comment: path requires a positive line for an inline comment"), nil
			}
			if inline && parentID > 0 {
				return mcpgo.NewToolResultError("add_pr_comment: parent_id cannot be combined with path/line; reply to a comment or anchor a new one, not both"), nil
			}
			if !inline && lineType != "" {
				return mcpgo.NewToolResultError("add_pr_comment: line_type only applies to inline comments; provide path and line too"), nil
			}

			ref := pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo}

			var comment pullrequestservice.Comment
			var err error
			if inline {
				comment, err = svc.AddInlineComment(ctx, ref, prID, text,
					pullrequestservice.InlineCommentAnchor{
						Line:     line,
						Path:     filePath,
						LineType: lineType,
					},
				)
			} else {
				comment, err = svc.AddComment(ctx, ref, prID, text, parentID)
			}
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("add_pr_comment failed", err), nil
			}
			return resultJSON(comment)
		}
	}}
}

func specSubmitPRReview() Spec {
	tool := mcpgo.NewTool("submit_pr_review",
		mcpgo.WithDescription("Set review status on a pull request: approve, unapprove, or request changes (needs_work)."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithString("action", mcpgo.Required(), mcpgo.Description("Action to take: approve, unapprove, or needs_work"),
			mcpgo.Enum("approve", "unapprove", "needs_work")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")
			action, _ := req.RequireString("action")
			ref := pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo}
			var pr pullrequestservice.PullRequest
			var err error
			switch action {
			case "approve":
				pr, err = svc.Approve(ctx, ref, prID)
			case "unapprove":
				pr, err = svc.Unapprove(ctx, ref, prID)
			case "needs_work":
				pr, err = svc.NeedsWork(ctx, ref, prID)
			default:
				return mcpgo.NewToolResultErrorFromErr("submit_pr_review: unknown action "+strconv.Quote(action), nil), nil
			}
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("submit_pr_review failed", err), nil
			}
			return resultJSON(pr)
		}
	}}
}

func specMergePullRequest() Spec {
	tool := mcpgo.NewTool("merge_pull_request",
		mcpgo.WithDescription("Merge a pull request. All required build checks must pass and all reviewers must have approved."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithNumber("version", mcpgo.Description("PR version for optimistic locking (omit to skip check)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")
			var version *int
			if v := req.GetInt("version", -1); v >= 0 {
				version = &v
			}
			pr, err := svc.Merge(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo},
				prID,
				version,
			)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("merge_pull_request failed", err), nil
			}
			return resultJSON(pr)
		}
	}}
}

func specEnableAutoMerge() Spec {
	tool := mcpgo.NewTool("enable_auto_merge",
		mcpgo.WithDescription("Enable auto-merge on a pull request. The PR will be merged automatically once all required checks pass and reviewers have approved. Requires Bitbucket DC 8.0+."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithString("strategy", mcpgo.Description("Merge strategy: no-ff (default), ff-only, rebase-no-ff, rebase-ff-only, squash, squash-ff-only"),
			mcpgo.Enum("no-ff", "ff-only", "rebase-no-ff", "rebase-ff-only", "squash", "squash-ff-only")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")
			strategy := req.GetString("strategy", "no-ff")
			autoMerge, err := svc.EnableAutoMerge(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo},
				prID,
				strategy,
			)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("enable_auto_merge failed", err), nil
			}
			return resultJSON(autoMerge)
		}
	}}
}

func specDisableAutoMerge() Spec {
	tool := mcpgo.NewTool("disable_auto_merge",
		mcpgo.WithDescription("Disable auto-merge on a pull request. The PR will no longer be merged automatically."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")
			if err := svc.DisableAutoMerge(ctx, pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo}, prID); err != nil {
				return mcpgo.NewToolResultErrorFromErr("disable_auto_merge failed", err), nil
			}
			return resultJSON(map[string]any{"status": "ok", "auto_merge": map[string]any{"enabled": false}})
		}
	}}
}

// parseCommaList splits a comma-separated string into trimmed, non-empty
// values, returning nil when the input yields no usable entries. MCP tool
// arguments arrive as single strings, so this adapts them to the []string
// inputs the service layer expects (e.g. "alice, bob" -> ["alice", "bob"]).
func parseCommaList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func specGetPRDiff() Spec {
	tool := mcpgo.NewTool("get_pr_diff",
		mcpgo.WithDescription("Get the diff of a pull request as unified diff text."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := diffservice.NewService(c.OpenAPI)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")
			result, err := svc.DiffPR(ctx, diffservice.DiffPRInput{
				Repository:    diffservice.RepositoryRef{ProjectKey: project, Slug: repo},
				PullRequestID: prID,
				Output:        diffservice.OutputKindRaw,
			})
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("get_pr_diff failed", err), nil
			}
			return resultJSON(result)
		}
	}}
}

func specGetFileContent() Spec {
	tool := mcpgo.NewTool("get_file_content",
		mcpgo.WithDescription("Get the raw content of a file in a repository."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("path", mcpgo.Required(), mcpgo.Description("File path in the repository")),
		mcpgo.WithString("at", mcpgo.Description("Git ref or branch to read from (e.g. refs/heads/main)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := browseservice.NewService(c.OpenAPI, c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			path, _ := req.RequireString("path")
			at := req.GetString("at", "")
			content, err := svc.Raw(ctx, browseservice.RepositoryRef{ProjectKey: project, Slug: repo}, path, at)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("get_file_content failed", err), nil
			}
			return mcpgo.NewToolResultText(string(content)), nil
		}
	}}
}

// specUpdatePullRequest completes the draft workflow the agent skill documents:
// open a pull request as a draft, then mark it ready for review. Without this
// the second half of that workflow was reachable only from the CLI.
//
// Bitbucket requires the current version for optimistic locking, which is why
// version is required rather than looked up here: fetching it inside the tool
// would reintroduce the lost-update the version exists to prevent.
func specUpdatePullRequest() Spec {
	tool := mcpgo.NewTool("update_pull_request",
		mcpgo.WithDescription("Update a pull request's title, description, or draft state. Use draft=false to mark a "+
			"draft pull request ready for review. Requires the current version from get_pull_request for optimistic "+
			"locking; a stale version is rejected rather than overwriting someone else's edit."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithNumber("version", mcpgo.Required(), mcpgo.Description("Current pull request version, from get_pull_request")),
		mcpgo.WithString("title", mcpgo.Description("New title; omit to leave unchanged")),
		mcpgo.WithString("description", mcpgo.Description("New description; omit to leave unchanged")),
		mcpgo.WithBoolean("draft", mcpgo.Description("Set or clear the draft flag; omit to leave unchanged (Bitbucket DC 8.0+)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")

			input := pullrequestservice.UpdateInput{
				Title:       req.GetString("title", ""),
				Description: req.GetString("description", ""),
				Version:     int(req.GetFloat("version", 0)),
			}

			// Draft is a pointer so that "leave unchanged" is distinguishable
			// from "set to false", which is the difference between not touching
			// a draft and publishing it.
			if _, ok := req.GetArguments()["draft"]; ok {
				draft := req.GetBool("draft", false)
				input.Draft = &draft
			}

			pr, err := svc.Update(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo},
				prID,
				input,
			)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("update_pull_request failed", err), nil
			}

			return resultJSON(pr)
		}
	}}
}
