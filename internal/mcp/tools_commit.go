package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	commitservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/commit"
)

// ListCommitsInput is the argument set for list_commits.
type ListCommitsInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	Until   string `json:"until,omitempty" jsonschema:"Return commits reachable from this ref or commit (defaults to default branch)"`
	Since   string `json:"since,omitempty" jsonschema:"Exclude commits reachable from this ref or commit"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 25)"`
}

// ListCommitsOutput names the collection it holds.
type ListCommitsOutput struct {
	Commits []openapigenerated.RestCommit `json:"commits"`
}

func specListCommits() Spec {
	tool := &mcp.Tool{
		Name:        "list_commits",
		Description: "List commits in a repository branch. Use to walk history to find a good base or diagnose what changed.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[ListCommitsInput, ListCommitsOutput] {
		svc := commitservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in ListCommitsInput) (*mcp.CallToolResult, ListCommitsOutput, error) {
			commits, err := svc.List(ctx,
				commitservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				commitservice.ListOptions{
					Since:      in.Since,
					Until:      in.Until,
					MaxResults: limitOrDefault(in.Limit),
				},
			)
			if err != nil {
				return nil, ListCommitsOutput{}, fmt.Errorf("list_commits failed: %w", err)
			}
			return nil, ListCommitsOutput{Commits: commits}, nil
		}
	})
}

// GetCommitInput is the argument set for get_commit.
type GetCommitInput struct {
	Project  string `json:"project" jsonschema:"Bitbucket project key"`
	Repo     string `json:"repo" jsonschema:"Repository slug"`
	CommitID string `json:"commit_id" jsonschema:"Commit SHA"`
}

// GetCommitOutput names the commit it holds.
type GetCommitOutput struct {
	Commit openapigenerated.RestCommit `json:"commit"`
}

func specGetCommit() Spec {
	tool := &mcp.Tool{
		Name:        "get_commit",
		Description: "Get details of a specific commit including author, message, and timestamp.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[GetCommitInput, GetCommitOutput] {
		svc := commitservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in GetCommitInput) (*mcp.CallToolResult, GetCommitOutput, error) {
			commit, err := svc.Get(ctx,
				commitservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				in.CommitID,
			)
			if err != nil {
				return nil, GetCommitOutput{}, fmt.Errorf("get_commit failed: %w", err)
			}
			return nil, GetCommitOutput{Commit: commit}, nil
		}
	})
}

// CompareRefsInput is the argument set for compare_refs.
type CompareRefsInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	From    string `json:"from" jsonschema:"Base ref or commit (older side of comparison)"`
	To      string `json:"to" jsonschema:"Target ref or commit (newer side of comparison)"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of commits to return (default 25)"`
}

// CompareRefsOutput names the collection it holds.
type CompareRefsOutput struct {
	Commits []openapigenerated.RestCommit `json:"commits"`
}

func specCompareRefs() Spec {
	tool := &mcp.Tool{
		Name:        "compare_refs",
		Description: "List commits between two refs. Returns the commits reachable from 'to' but not from 'from'.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[CompareRefsInput, CompareRefsOutput] {
		svc := commitservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in CompareRefsInput) (*mcp.CallToolResult, CompareRefsOutput, error) {
			commits, err := svc.Compare(ctx,
				commitservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				commitservice.CompareOptions{
					From:       in.From,
					To:         in.To,
					MaxResults: limitOrDefault(in.Limit),
				},
			)
			if err != nil {
				return nil, CompareRefsOutput{}, fmt.Errorf("compare_refs failed: %w", err)
			}
			return nil, CompareRefsOutput{Commits: commits}, nil
		}
	})
}
