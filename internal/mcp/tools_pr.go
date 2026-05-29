package mcp

import (
	"context"
	"strconv"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequestactivity"
)

func specGetPullRequest() Spec {
	tool := mcpgo.NewTool("get_pull_request",
		mcpgo.WithDescription("Get pull request details including title, state, reviewer approvals, and merge status."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key (e.g. MYPROJECT)")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			id, _ := req.RequireString("id")
			pr, err := svc.Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo}, id)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("get_pull_request failed", err), nil
			}
			return resultJSON(pr)
		}
	}}
}

func specListPullRequests() Spec {
	tool := mcpgo.NewTool("list_pull_requests",
		mcpgo.WithDescription("List pull requests for a repository. Defaults to OPEN state."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("state", mcpgo.Description("Filter by state: OPEN (default), MERGED, DECLINED, ALL")),
		mcpgo.WithString("source_branch", mcpgo.Description("Filter by source branch name")),
		mcpgo.WithString("target_branch", mcpgo.Description("Filter by target branch name")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum number of results (default 25)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			opts := pullrequestservice.ListOptions{
				State:        req.GetString("state", "OPEN"),
				SourceBranch: req.GetString("source_branch", ""),
				TargetBranch: req.GetString("target_branch", ""),
				Limit:        req.GetInt("limit", 25),
			}
			prs, err := svc.List(ctx, pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo}, opts)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("list_pull_requests failed", err), nil
			}
			return resultJSON(prs)
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
		mcpgo.WithDescription("List comments on a pull request. Without path this returns the aggregate pull request comment view derived from activities."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithString("path", mcpgo.Description("Optional file path to restrict comments to a single diff path")),
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
			var comments []openapigenerated.RestComment
			var err error
			if path == "" {
				activities, listErr := activitySvc.List(ctx, pullrequestactivityservice.RepositoryRef{ProjectKey: project, Slug: repo}, prID, pullrequestactivityservice.ListOptions{Limit: limit})
				if listErr != nil {
					return mcpgo.NewToolResultErrorFromErr("list_pr_comments failed", listErr), nil
				}
				comments = pullrequestactivityservice.ExtractComments(activities)
			} else {
				target := commentservice.Target{
					Repository:    commentservice.RepositoryRef{ProjectKey: project, Slug: repo},
					PullRequestID: prID,
				}
				comments, err = commentSvc.List(ctx, target, path, limit)
			}
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("list_pr_comments failed", err), nil
			}
			return resultJSON(comments)
		}
	}}
}

func specAddPRComment() Spec {
	tool := mcpgo.NewTool("add_pr_comment",
		mcpgo.WithDescription("Add a comment to a pull request."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithString("text", mcpgo.Required(), mcpgo.Description("Comment text (Markdown supported)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := commentservice.NewService(c.OpenAPI)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")
			text, _ := req.RequireString("text")
			target := commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: project, Slug: repo},
				PullRequestID: prID,
			}
			comment, err := svc.Create(ctx, target, text)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("add_pr_comment failed", err), nil
			}
			return resultJSON(comment)
		}
	}}
}

func specListPRTasks() Spec {
	tool := mcpgo.NewTool("list_pr_tasks",
		mcpgo.WithDescription("List tasks on a pull request. Open tasks block merging."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithString("state", mcpgo.Description("Filter by state: OPEN (default) or RESOLVED")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum number of results (default 25)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")
			prID, _ := req.RequireString("pr_id")
			tasks, err := svc.ListTasks(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo},
				prID,
				pullrequestservice.TaskListOptions{
					State: req.GetString("state", "OPEN"),
					Limit: req.GetInt("limit", 25),
				},
			)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("list_pr_tasks failed", err), nil
			}
			return resultJSON(tasks)
		}
	}}
}

func specSubmitPRReview() Spec {
	tool := mcpgo.NewTool("submit_pr_review",
		mcpgo.WithDescription("Approve or unapprove a pull request."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
		mcpgo.WithString("pr_id", mcpgo.Required(), mcpgo.Description("Pull request ID")),
		mcpgo.WithString("action", mcpgo.Required(), mcpgo.Description("Action to take: approve or unapprove"),
			mcpgo.Enum("approve", "unapprove")),
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
