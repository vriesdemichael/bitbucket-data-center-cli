package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	browseservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/browse"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commentanchor"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

// GetPullRequestInput is the argument set for get_pull_request.
type GetPullRequestInput struct {
	Project           string `json:"project" jsonschema:"Bitbucket project key (e.g. MYPROJECT)"`
	Repo              string `json:"repo" jsonschema:"Repository slug"`
	ID                string `json:"id" jsonschema:"Pull request ID"`
	SkipReviewSummary bool   `json:"skip_review_summary,omitempty" jsonschema:"Skip the activity timeline lookup used to count unresolved comment threads"`
}

// GetPullRequestOutput carries the pull request alongside the derived review
// state. Both names predate the envelope convention and are kept as they are.
type GetPullRequestOutput struct {
	PullRequest   pullrequestservice.PullRequest   `json:"pull_request"`
	ReviewSummary pullrequestservice.ReviewSummary `json:"review_summary"`
}

func specGetPullRequest() Spec {
	tool := &mcp.Tool{
		Name: "get_pull_request",
		Description: "Get pull request details including title, state, reviewer approvals, and merge status. " +
			"The review_summary field reports unresolved comment threads, open tasks and reviewers who requested changes; " +
			"action_required is true when the pull request is waiting on the author, and is absent when the counts it " +
			"rests on were not all measured -- read counts_source to see which were.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[GetPullRequestInput, GetPullRequestOutput] {
		svc := pullrequestservice.NewService(c.HTTP)
		activitySvc := pullrequestactivityservice.NewService(c.OpenAPI)
		commentSvc := commentservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in GetPullRequestInput) (*mcp.CallToolResult, GetPullRequestOutput, error) {
			pr, err := svc.Get(ctx, pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.ID)
			if err != nil {
				return nil, GetPullRequestOutput{}, fmt.Errorf("get_pull_request failed: %w", err)
			}

			counts := pullrequestservice.ReviewCounts{}
			if !in.SkipReviewSummary {
				threads, summaryErr := activitySvc.TrySummarize(ctx, pullrequestactivityservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.ID)
				if summaryErr != nil {
					return nil, GetPullRequestOutput{}, fmt.Errorf("get_pull_request failed: %w", summaryErr)
				}
				switch {
				case threads != nil:
					counts.Threads = threads
				default:
					// Bitbucket 10.x omits the task counters on this endpoint,
					// so fall back to the exact blocker-comment tally rather
					// than reporting nothing.
					if tasks, taskErr := commentSvc.CountTasks(ctx, commentservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.ID); taskErr == nil {
						counts.Tasks = &pullrequestservice.TaskCounts{Open: tasks.Open, Resolved: tasks.Resolved}
					}
				}
			}

			return nil, GetPullRequestOutput{
				PullRequest:   pr,
				ReviewSummary: pullrequestservice.BuildReviewSummary(pr, counts),
			}, nil
		}
	})
}

// ListPullRequestsInput is the argument set for list_pull_requests.
type ListPullRequestsInput struct {
	Project      string `json:"project,omitempty" jsonschema:"Bitbucket project key (omit for dashboard mode)"`
	Repo         string `json:"repo,omitempty" jsonschema:"Repository slug (omit for dashboard mode)"`
	State        string `json:"state,omitempty" jsonschema:"Filter by state: OPEN (default), MERGED, DECLINED, ALL"`
	Role         string `json:"role,omitempty" jsonschema:"Filter by role: REVIEWER, AUTHOR, or PARTICIPANT (works in both repo and dashboard mode)"`
	SourceBranch string `json:"source_branch,omitempty" jsonschema:"Filter by source branch name (repo mode only)"`
	TargetBranch string `json:"target_branch,omitempty" jsonschema:"Filter by target branch name (repo mode only)"`
	Limit        int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 25)"`
}

// ListPullRequestsOutput names the collection it holds.
type ListPullRequestsOutput struct {
	PullRequests []pullrequestservice.PullRequest `json:"pull_requests"`
}

func specListPullRequests() Spec {
	tool := &mcp.Tool{
		Name:        "list_pull_requests",
		Description: "List pull requests. Without project/repo, lists the current user's PRs across all repositories (dashboard).",
		// No enum on state or role: the service normalises case, so pinning the
		// upper-case spellings would reject "author", which works today. The
		// permitted values are in the field descriptions instead.
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[ListPullRequestsInput, ListPullRequestsOutput] {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in ListPullRequestsInput) (*mcp.CallToolResult, ListPullRequestsOutput, error) {
			state := in.State
			if state == "" {
				state = "OPEN"
			}
			limit := limitOrDefault(in.Limit)

			var prs []pullrequestservice.PullRequest
			var err error

			switch {
			case in.Project != "" && in.Repo != "":
				prs, err = svc.List(ctx,
					pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
					pullrequestservice.ListOptions{
						State:        state,
						Role:         in.Role,
						SourceBranch: in.SourceBranch,
						TargetBranch: in.TargetBranch,
						MaxResults:   limit,
					},
				)
			case in.Project != "" || in.Repo != "":
				return nil, ListPullRequestsOutput{}, fmt.Errorf("list_pull_requests requires both project and repo, or neither for dashboard mode")
			default:
				role := in.Role
				if role == "" {
					role = "REVIEWER"
				}
				prs, err = svc.ListDashboard(ctx, pullrequestservice.DashboardListOptions{
					State:      state,
					Role:       role,
					MaxResults: limit,
				})
			}
			if err != nil {
				return nil, ListPullRequestsOutput{}, fmt.Errorf("list_pull_requests failed: %w", err)
			}
			return nil, ListPullRequestsOutput{PullRequests: prs}, nil
		}
	})
}

// PullRequestOutput is the shared single-pull-request envelope used by every
// tool whose result is one pull request.
type PullRequestOutput struct {
	PullRequest pullrequestservice.PullRequest `json:"pull_request"`
}

// CreatePullRequestInput is the argument set for create_pull_request.
type CreatePullRequestInput struct {
	Project     string `json:"project" jsonschema:"Bitbucket project key"`
	Repo        string `json:"repo" jsonschema:"Repository slug"`
	FromRef     string `json:"from_ref" jsonschema:"Source branch name (e.g. feature/my-work)"`
	Title       string `json:"title" jsonschema:"Pull request title"`
	ToRef       string `json:"to_ref,omitempty" jsonschema:"Target branch name; defaults to repository default branch"`
	Description string `json:"description,omitempty" jsonschema:"Pull request description (optional)"`
	Reviewers   string `json:"reviewers,omitempty" jsonschema:"Comma-separated reviewer usernames to add (e.g. alice,bob)"`
	Draft       bool   `json:"draft,omitempty" jsonschema:"Create as a draft pull request (Bitbucket DC 8.0+; default false)"`
}

func specCreatePullRequest() Spec {
	tool := &mcp.Tool{
		Name:        "create_pull_request",
		Description: "Create a new pull request.",
		Annotations: mutating(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[CreatePullRequestInput, PullRequestOutput] {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in CreatePullRequestInput) (*mcp.CallToolResult, PullRequestOutput, error) {
			pr, err := svc.Create(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				pullrequestservice.CreateInput{
					FromRef:     in.FromRef,
					ToRef:       in.ToRef,
					Title:       in.Title,
					Description: in.Description,
					Reviewers:   parseCommaList(in.Reviewers),
					Draft:       in.Draft,
				},
			)
			if err != nil {
				return nil, PullRequestOutput{}, fmt.Errorf("create_pull_request failed: %w", err)
			}
			return nil, PullRequestOutput{PullRequest: pr}, nil
		}
	})
}

// ListPRCommentsInput is the argument set for list_pr_comments.
type ListPRCommentsInput struct {
	Project     string `json:"project" jsonschema:"Bitbucket project key"`
	Repo        string `json:"repo" jsonschema:"Repository slug"`
	PRID        string `json:"pr_id" jsonschema:"Pull request ID"`
	Path        string `json:"path,omitempty" jsonschema:"Optional file path to restrict comments to a single diff path"`
	State       string `json:"state,omitempty" jsonschema:"Filter threads by resolution state: open, resolved, pending, or all (default)"`
	TasksOnly   bool   `json:"tasks_only,omitempty" jsonschema:"Return only threads Bitbucket tracks as tasks (blocker comments)"`
	WithReplies bool   `json:"with_replies,omitempty" jsonschema:"Include the full text of every reply instead of only the most recent one"`
	Limit       int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 25)"`
}

// ListPRCommentsOutput carries the thread list and its summary. Both names
// predate the envelope convention and are kept as they are.
type ListPRCommentsOutput struct {
	Summary pullrequestactivityservice.Summary  `json:"summary"`
	Threads []pullrequestactivityservice.Thread `json:"threads"`
}

func specListPRComments() Spec {
	tool := &mcp.Tool{
		Name: "list_pr_comments",
		Description: "List review comment threads on a pull request, unresolved first. Bitbucket models a task " +
			"as a blocker comment, so this returns reviewer comments and tasks together, each with its resolution state, " +
			"file anchor and reply count. Use state=open to see only what is still waiting on the author. Without path " +
			"this returns the aggregate pull request comment view derived from activities.",
		// No enum on state: NormalizeThreadState accepts more spellings than a
		// fixed list would, and rejecting them at the schema is a new
		// constraint this migration has no reason to add.
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[ListPRCommentsInput, ListPRCommentsOutput] {
		commentSvc := commentservice.NewService(c.OpenAPI)
		activitySvc := pullrequestactivityservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in ListPRCommentsInput) (*mcp.CallToolResult, ListPRCommentsOutput, error) {
			limit := limitOrDefault(in.Limit)

			requestedState := in.State
			if requestedState == "" {
				requestedState = "all"
			}
			state, err := pullrequestactivityservice.NormalizeThreadState(requestedState)
			if err != nil {
				return nil, ListPRCommentsOutput{}, fmt.Errorf("list_pr_comments failed: %w", err)
			}

			threadOptions := pullrequestactivityservice.ThreadOptions{
				State:         state,
				TasksOnly:     in.TasksOnly,
				WithReplies:   in.WithReplies,
				BaseURL:       c.BaseURL,
				ProjectKey:    in.Project,
				Slug:          in.Repo,
				PullRequestID: in.PRID,
			}

			// The limit counts threads, and the grouping happens here, so the
			// whole timeline is read and the cap applied to what comes out of
			// it. Capping the fetch instead would cut a thread in half: a reply
			// arrives as its own activity, so the last thread in a truncated
			// window is missing the replies that did not fit, and the summary
			// counts fewer open threads than the pull request has.
			var threads []pullrequestactivityservice.Thread
			var summary pullrequestactivityservice.Summary
			if in.Path == "" {
				activities, listErr := activitySvc.List(ctx, pullrequestactivityservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.PRID,
					pullrequestactivityservice.ListOptions{MaxResults: pullrequestactivityservice.AllResults})
				if listErr != nil {
					return nil, ListPRCommentsOutput{}, fmt.Errorf("list_pr_comments failed: %w", listErr)
				}
				threads, summary = pullrequestactivityservice.ExtractThreads(activities, threadOptions)
			} else {
				target := commentservice.Target{
					Repository:    commentservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
					PullRequestID: in.PRID,
				}
				comments, listErr := commentSvc.List(ctx, target, in.Path, commentservice.AllResults)
				if listErr != nil {
					return nil, ListPRCommentsOutput{}, fmt.Errorf("list_pr_comments failed: %w", listErr)
				}
				threads, summary = pullrequestactivityservice.ThreadsFromComments(comments, threadOptions)
			}

			if len(threads) > limit {
				threads = threads[:limit]
			}

			return nil, ListPRCommentsOutput{Summary: summary, Threads: threads}, nil
		}
	})
}

// AddPRCommentInput is the argument set for add_pr_comment.
type AddPRCommentInput struct {
	Project  string `json:"project" jsonschema:"Bitbucket project key"`
	Repo     string `json:"repo" jsonschema:"Repository slug"`
	PRID     string `json:"pr_id" jsonschema:"Pull request ID"`
	Text     string `json:"text" jsonschema:"Comment text (Markdown supported)"`
	Path     string `json:"path,omitempty" jsonschema:"File path for inline comment (e.g. src/main.go)"`
	Line     int    `json:"line,omitempty" jsonschema:"Line number for inline comment"`
	LineType string `json:"line_type,omitempty" jsonschema:"Diff side for inline comment: ADDED (default), REMOVED, or CONTEXT"`
	ParentID int64  `json:"parent_id,omitempty" jsonschema:"Parent comment ID to reply to"`
}

// AddPRCommentOutput names the created comment.
type AddPRCommentOutput struct {
	Comment pullrequestservice.Comment `json:"comment"`
}

func specAddPRComment() Spec {
	tool := &mcp.Tool{
		Name:        "add_pr_comment",
		Description: "Add a comment to a pull request. Provide path and line to create an inline comment on a specific file line. Provide parent_id to reply to an existing comment.",
		// No enum on line_type: commentanchor.Validate owns that vocabulary and
		// reports a better message than a schema violation would.
		Annotations: mutating(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[AddPRCommentInput, AddPRCommentOutput] {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in AddPRCommentInput) (*mcp.CallToolResult, AddPRCommentOutput, error) {
			filePath := strings.TrimSpace(in.Path)
			lineType := strings.TrimSpace(in.LineType)
			inline := filePath != "" || in.Line > 0

			if err := commentanchor.Validate(commentanchor.Options{
				Path:     filePath,
				Line:     in.Line,
				LineType: lineType,
				ParentID: in.ParentID,
			}, commentanchor.APINames); err != nil {
				return nil, AddPRCommentOutput{}, fmt.Errorf("add_pr_comment: %w", err)
			}

			ref := pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}

			var comment pullrequestservice.Comment
			var err error
			if inline {
				comment, err = svc.AddInlineComment(ctx, ref, in.PRID, in.Text,
					pullrequestservice.InlineCommentAnchor{
						Line:     in.Line,
						Path:     filePath,
						LineType: lineType,
					},
				)
			} else {
				comment, err = svc.AddComment(ctx, ref, in.PRID, in.Text, in.ParentID)
			}
			if err != nil {
				return nil, AddPRCommentOutput{}, fmt.Errorf("add_pr_comment failed: %w", err)
			}
			return nil, AddPRCommentOutput{Comment: comment}, nil
		}
	})
}

// SubmitPRReviewInput is the argument set for submit_pr_review.
type SubmitPRReviewInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	PRID    string `json:"pr_id" jsonschema:"Pull request ID"`
	Action  string `json:"action" jsonschema:"Action to take: approve, unapprove, or needs_work"`
}

func specSubmitPRReview() Spec {
	tool := &mcp.Tool{
		Name:        "submit_pr_review",
		Description: "Set review status on a pull request: approve, unapprove, or request changes (needs_work).",
		Annotations: mutating(),
		InputSchema: enumInputSchema[SubmitPRReviewInput](map[string][]string{
			"action": {"approve", "unapprove", "needs_work"},
		}),
	}
	return toolSpec(tool, false, func(c Clients) mcp.ToolHandlerFor[SubmitPRReviewInput, PullRequestOutput] {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in SubmitPRReviewInput) (*mcp.CallToolResult, PullRequestOutput, error) {
			ref := pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}
			var pr pullrequestservice.PullRequest
			var err error
			switch in.Action {
			case "approve":
				pr, err = svc.Approve(ctx, ref, in.PRID)
			case "unapprove":
				pr, err = svc.Unapprove(ctx, ref, in.PRID)
			case "needs_work":
				pr, err = svc.NeedsWork(ctx, ref, in.PRID)
			default:
				return nil, PullRequestOutput{}, fmt.Errorf("submit_pr_review: unknown action %q", in.Action)
			}
			if err != nil {
				return nil, PullRequestOutput{}, fmt.Errorf("submit_pr_review failed: %w", err)
			}
			return nil, PullRequestOutput{PullRequest: pr}, nil
		}
	})
}

// MergePullRequestInput is the argument set for merge_pull_request.
//
// Version is a pointer so that an omitted version means "skip the optimistic
// locking check" rather than "assert version 0", which is a real version.
type MergePullRequestInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	PRID    string `json:"pr_id" jsonschema:"Pull request ID"`
	Version *int   `json:"version,omitempty" jsonschema:"PR version for optimistic locking (omit to skip check)"`
}

func specMergePullRequest() Spec {
	tool := &mcp.Tool{
		Name:        "merge_pull_request",
		Description: "Merge a pull request. All required build checks must pass and all reviewers must have approved.",
		Annotations: mutating(),
	}
	return toolSpec(tool, false, func(c Clients) mcp.ToolHandlerFor[MergePullRequestInput, PullRequestOutput] {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in MergePullRequestInput) (*mcp.CallToolResult, PullRequestOutput, error) {
			pr, err := svc.Merge(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				in.PRID,
				in.Version,
			)
			if err != nil {
				return nil, PullRequestOutput{}, fmt.Errorf("merge_pull_request failed: %w", err)
			}
			return nil, PullRequestOutput{PullRequest: pr}, nil
		}
	})
}

// EnableAutoMergeInput is the argument set for enable_auto_merge.
type EnableAutoMergeInput struct {
	Project  string `json:"project" jsonschema:"Bitbucket project key"`
	Repo     string `json:"repo" jsonschema:"Repository slug"`
	PRID     string `json:"pr_id" jsonschema:"Pull request ID"`
	Strategy string `json:"strategy,omitempty" jsonschema:"Merge strategy: no-ff (default), ff, ff-only, rebase-no-ff, rebase-ff-only, squash, squash-ff-only"`
}

// AutoMergeOutput names the auto-merge state it holds.
type AutoMergeOutput struct {
	AutoMerge pullrequestservice.AutoMerge `json:"auto_merge"`
}

func specEnableAutoMerge() Spec {
	tool := &mcp.Tool{
		Name:        "enable_auto_merge",
		Description: "Enable auto-merge on a pull request. The PR will be merged automatically once all required checks pass and reviewers have approved. Requires Bitbucket DC 8.0+.",
		Annotations: mutating(),
		InputSchema: enumInputSchema[EnableAutoMergeInput](map[string][]string{
			"strategy": openapi.MergeStrategies,
		}),
	}
	return toolSpec(tool, false, func(c Clients) mcp.ToolHandlerFor[EnableAutoMergeInput, AutoMergeOutput] {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in EnableAutoMergeInput) (*mcp.CallToolResult, AutoMergeOutput, error) {
			strategy := in.Strategy
			if strategy == "" {
				strategy = "no-ff"
			}
			autoMerge, err := svc.EnableAutoMerge(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				in.PRID,
				strategy,
			)
			if err != nil {
				return nil, AutoMergeOutput{}, fmt.Errorf("enable_auto_merge failed: %w", err)
			}
			return nil, AutoMergeOutput{AutoMerge: autoMerge}, nil
		}
	})
}

// DisableAutoMergeInput is the argument set for disable_auto_merge.
type DisableAutoMergeInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	PRID    string `json:"pr_id" jsonschema:"Pull request ID"`
}

func specDisableAutoMerge() Spec {
	tool := &mcp.Tool{
		Name:        "disable_auto_merge",
		Description: "Disable auto-merge on a pull request. The PR will no longer be merged automatically.",
		Annotations: mutating(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[DisableAutoMergeInput, AutoMergeOutput] {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in DisableAutoMergeInput) (*mcp.CallToolResult, AutoMergeOutput, error) {
			if err := svc.DisableAutoMerge(ctx, pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.PRID); err != nil {
				return nil, AutoMergeOutput{}, fmt.Errorf("disable_auto_merge failed: %w", err)
			}
			return nil, AutoMergeOutput{AutoMerge: pullrequestservice.AutoMerge{Enabled: false}}, nil
		}
	})
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

// GetPRDiffInput is the argument set for get_pr_diff.
type GetPRDiffInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	PRID    string `json:"pr_id" jsonschema:"Pull request ID"`
}

// GetPRDiffOutput names the diff it holds.
type GetPRDiffOutput struct {
	Diff diffservice.Result `json:"diff"`
}

func specGetPRDiff() Spec {
	tool := &mcp.Tool{
		Name:        "get_pr_diff",
		Description: "Get the diff of a pull request as unified diff text.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[GetPRDiffInput, GetPRDiffOutput] {
		svc := diffservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in GetPRDiffInput) (*mcp.CallToolResult, GetPRDiffOutput, error) {
			result, err := svc.DiffPR(ctx, diffservice.DiffPRInput{
				Repository:    diffservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				PullRequestID: in.PRID,
				Output:        diffservice.OutputKindRaw,
			})
			if err != nil {
				return nil, GetPRDiffOutput{}, fmt.Errorf("get_pr_diff failed: %w", err)
			}
			// The unified diff is what a model actually reads, so it is the text
			// content verbatim rather than the JSON encoding of the envelope the
			// SDK would supply by default. structuredContent still carries the
			// full result for a client that parses it.
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: result.Patch}},
			}, GetPRDiffOutput{Diff: result}, nil
		}
	})
}

// GetFileContentInput is the argument set for get_file_content.
type GetFileContentInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	Path    string `json:"path" jsonschema:"File path in the repository"`
	At      string `json:"at,omitempty" jsonschema:"Git ref or branch to read from (e.g. refs/heads/main)"`
}

// GetFileContentOutput names the file payload it holds. The path and ref travel
// with the content because a model that fetched several files needs to tell
// them apart in its own context.
type GetFileContentOutput struct {
	Path    string `json:"path"`
	At      string `json:"at,omitempty"`
	Content string `json:"content"`
}

func specGetFileContent() Spec {
	tool := &mcp.Tool{
		Name:        "get_file_content",
		Description: "Get the raw content of a file in a repository.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[GetFileContentInput, GetFileContentOutput] {
		svc := browseservice.NewService(c.OpenAPI, c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in GetFileContentInput) (*mcp.CallToolResult, GetFileContentOutput, error) {
			content, err := svc.Raw(ctx, browseservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.Path, in.At)
			if err != nil {
				return nil, GetFileContentOutput{}, fmt.Errorf("get_file_content failed: %w", err)
			}
			// As with get_pr_diff: the file itself is the text content, not the
			// JSON encoding of the envelope around it.
			return &mcp.CallToolResult{
					Content: []mcp.Content{&mcp.TextContent{Text: string(content)}},
				}, GetFileContentOutput{
					Path:    in.Path,
					At:      in.At,
					Content: string(content),
				}, nil
		}
	})
}

// UpdatePullRequestInput is the argument set for update_pull_request.
//
// Draft is a pointer so that "leave unchanged" is distinguishable from "set to
// false", which is the difference between not touching a draft and publishing
// it.
//
// Bitbucket requires the current version for optimistic locking, which is why
// version is required rather than looked up here: fetching it inside the tool
// would reintroduce the lost update the version exists to prevent.
type UpdatePullRequestInput struct {
	Project     string `json:"project" jsonschema:"Bitbucket project key"`
	Repo        string `json:"repo" jsonschema:"Repository slug"`
	PRID        string `json:"pr_id" jsonschema:"Pull request ID"`
	Version     int    `json:"version" jsonschema:"Current pull request version, from get_pull_request"`
	Title       string `json:"title,omitempty" jsonschema:"New title; omit to leave unchanged"`
	Description string `json:"description,omitempty" jsonschema:"New description; omit to leave unchanged"`
	Draft       *bool  `json:"draft,omitempty" jsonschema:"Set or clear the draft flag; omit to leave unchanged (Bitbucket DC 8.0+)"`
}

// specUpdatePullRequest completes the draft workflow the agent skill documents:
// open a pull request as a draft, then mark it ready for review. Without this
// the second half of that workflow was reachable only from the CLI.
func specUpdatePullRequest() Spec {
	tool := &mcp.Tool{
		Name: "update_pull_request",
		Description: "Update a pull request's title, description, or draft state. Use draft=false to mark a " +
			"draft pull request ready for review. Requires the current version from get_pull_request for optimistic " +
			"locking; a stale version is rejected rather than overwriting someone else's edit.",
		Annotations: mutating(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[UpdatePullRequestInput, PullRequestOutput] {
		svc := pullrequestservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in UpdatePullRequestInput) (*mcp.CallToolResult, PullRequestOutput, error) {
			pr, err := svc.Update(ctx,
				pullrequestservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				in.PRID,
				pullrequestservice.UpdateInput{
					Title:       in.Title,
					Description: in.Description,
					Version:     in.Version,
					Draft:       in.Draft,
				},
			)
			if err != nil {
				return nil, PullRequestOutput{}, fmt.Errorf("update_pull_request failed: %w", err)
			}
			return nil, PullRequestOutput{PullRequest: pr}, nil
		}
	})
}
