package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	branchservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/branch"
	commitservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/commit"
)

// ListBranchesInput is the argument set for list_branches.
type ListBranchesInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	Filter  string `json:"filter,omitempty" jsonschema:"Text filter applied to branch names"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 25)"`
}

// ListBranchesOutput names the collection so an agent reading the result knows
// what it is holding without inferring it from the tool it called.
type ListBranchesOutput struct {
	Branches []openapigenerated.RestBranch `json:"branches"`
}

func specListBranches() Spec {
	tool := &mcp.Tool{
		Name:        "list_branches",
		Description: "List branches in a repository. Use to discover existing branches before creating a new one or a pull request.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[ListBranchesInput, ListBranchesOutput] {
		svc := branchservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in ListBranchesInput) (*mcp.CallToolResult, ListBranchesOutput, error) {
			branches, err := svc.List(ctx,
				branchservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				branchservice.ListOptions{
					FilterText: in.Filter,
					MaxResults: limitOrDefault(in.Limit),
				},
			)
			if err != nil {
				return nil, ListBranchesOutput{}, fmt.Errorf("list_branches failed: %w", err)
			}
			return nil, ListBranchesOutput{Branches: branches}, nil
		}
	})
}

// ResolveRefInput is the argument set for resolve_ref.
type ResolveRefInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	Ref     string `json:"ref" jsonschema:"Branch or tag name (e.g. main, v1.2.3)"`
}

// ResolveRefOutput holds the branches and tags matching the queried ref.
type ResolveRefOutput struct {
	Refs []openapigenerated.RestMinimalRef `json:"refs"`
}

func specResolveRef() Spec {
	tool := &mcp.Tool{
		Name:        "resolve_ref",
		Description: "Resolve a branch or tag name to its tip commit SHA. Use as a cheap existence check before cloning or creating a pull request.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[ResolveRefInput, ResolveRefOutput] {
		svc := commitservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in ResolveRefInput) (*mcp.CallToolResult, ResolveRefOutput, error) {
			refs, err := svc.ListTagsAndBranches(ctx,
				commitservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				in.Ref,
			)
			if err != nil {
				return nil, ResolveRefOutput{}, fmt.Errorf("resolve_ref failed: %w", err)
			}
			return nil, ResolveRefOutput{Refs: refs}, nil
		}
	})
}
