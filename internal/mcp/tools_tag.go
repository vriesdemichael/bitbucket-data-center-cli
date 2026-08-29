package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	tagservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/tag"
)

// ListTagsInput is the argument set for list_tags.
type ListTagsInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	Filter  string `json:"filter,omitempty" jsonschema:"Text filter applied to tag names"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 25)"`
}

// ListTagsOutput names the collection it holds.
type ListTagsOutput struct {
	Tags []openapigenerated.RestTag `json:"tags"`
}

func specListTags() Spec {
	tool := &mcp.Tool{
		Name:        "list_tags",
		Description: "List tags in a repository. Use to find the latest release baseline or versioning information.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[ListTagsInput, ListTagsOutput] {
		svc := tagservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in ListTagsInput) (*mcp.CallToolResult, ListTagsOutput, error) {
			tags, err := svc.List(ctx,
				tagservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				tagservice.ListOptions{
					FilterText: in.Filter,
					Limit:      limitOrDefault(in.Limit),
				},
			)
			if err != nil {
				return nil, ListTagsOutput{}, fmt.Errorf("list_tags failed: %w", err)
			}
			return nil, ListTagsOutput{Tags: tags}, nil
		}
	})
}

// CreateTagInput is the argument set for create_tag.
type CreateTagInput struct {
	Project    string `json:"project" jsonschema:"Bitbucket project key"`
	Repo       string `json:"repo" jsonschema:"Repository slug"`
	Name       string `json:"name" jsonschema:"Tag name (e.g. v1.2.3)"`
	StartPoint string `json:"start_point" jsonschema:"Branch name or commit SHA to tag"`
	Message    string `json:"message,omitempty" jsonschema:"Optional annotated tag message; omit for a lightweight tag"`
}

// CreateTagOutput names the created tag.
type CreateTagOutput struct {
	Tag openapigenerated.RestTag `json:"tag"`
}

func specCreateTag() Spec {
	tool := &mcp.Tool{
		Name:        "create_tag",
		Description: "Create a tag on a specific commit or ref. Use for release tagging after a PR is merged.",
		Annotations: mutating(false),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[CreateTagInput, CreateTagOutput] {
		svc := tagservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in CreateTagInput) (*mcp.CallToolResult, CreateTagOutput, error) {
			tag, err := svc.Create(ctx,
				tagservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				in.Name,
				in.StartPoint,
				in.Message,
			)
			if err != nil {
				return nil, CreateTagOutput{}, fmt.Errorf("create_tag failed: %w", err)
			}
			return nil, CreateTagOutput{Tag: tag}, nil
		}
	})
}
