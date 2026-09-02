package mcp

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
)

// GetBuildStatusInput is the argument set for get_build_status.
type GetBuildStatusInput struct {
	CommitID string `json:"commit_id" jsonschema:"Full commit SHA"`
	Limit    int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 25)"`
}

// GetBuildStatusOutput names the collection it holds.
type GetBuildStatusOutput struct {
	BuildStatuses []openapigenerated.RestBuildStatus `json:"build_statuses"`
}

func specGetBuildStatus() Spec {
	tool := &mcp.Tool{
		Name:        "get_build_status",
		Description: "Get build/CI statuses for a specific commit. Use this to check whether CI passed before declaring a PR ready to merge.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[GetBuildStatusInput, GetBuildStatusOutput] {
		svc := qualityservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in GetBuildStatusInput) (*mcp.CallToolResult, GetBuildStatusOutput, error) {
			statuses, err := svc.GetBuildStatuses(ctx, in.CommitID, limitOrDefault(in.Limit), "")
			if err != nil {
				return nil, GetBuildStatusOutput{}, fmt.Errorf("get_build_status failed: %w", err)
			}
			return nil, GetBuildStatusOutput{BuildStatuses: statuses}, nil
		}
	})
}

// SetBuildStatusInput is the argument set for set_build_status.
type SetBuildStatusInput struct {
	CommitID    string `json:"commit_id" jsonschema:"Full commit SHA"`
	Key         string `json:"key" jsonschema:"Unique build key (e.g. my-pipeline/unit-tests)"`
	State       string `json:"state" jsonschema:"Build state: SUCCESSFUL, FAILED, or INPROGRESS"`
	URL         string `json:"url" jsonschema:"URL to the build details"`
	Name        string `json:"name,omitempty" jsonschema:"Human-readable build name"`
	Description string `json:"description,omitempty" jsonschema:"Build description or summary"`
}

// SetBuildStatusOutput reports the status that was recorded. The Bitbucket
// endpoint returns no body, so this echoes the inputs that identify the record
// rather than inventing a payload: it gives the agent something to confirm
// against without implying the server sent it back.
type SetBuildStatusOutput struct {
	CommitID string `json:"commit_id"`
	Key      string `json:"key"`
	State    string `json:"state"`
}

func specSetBuildStatus() Spec {
	tool := &mcp.Tool{
		Name:        "set_build_status",
		Description: "Report a build/CI status for a commit back to Bitbucket. Use this when running CI pipelines that should surface results in PR views.",
		Annotations: mutating(),
		InputSchema: enumInputSchema[SetBuildStatusInput](map[string][]string{
			"state": {"SUCCESSFUL", "FAILED", "INPROGRESS"},
		}),
	}
	return toolSpec(tool, false, func(c Clients) mcp.ToolHandlerFor[SetBuildStatusInput, SetBuildStatusOutput] {
		svc := qualityservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in SetBuildStatusInput) (*mcp.CallToolResult, SetBuildStatusOutput, error) {
			err := svc.SetBuildStatus(ctx, in.CommitID, qualityservice.BuildStatusSetInput{
				Key:         in.Key,
				State:       in.State,
				URL:         in.URL,
				Name:        in.Name,
				Description: in.Description,
			})
			if err != nil {
				return nil, SetBuildStatusOutput{}, fmt.Errorf("set_build_status failed: %w", err)
			}
			return nil, SetBuildStatusOutput{CommitID: in.CommitID, Key: in.Key, State: in.State}, nil
		}
	})
}

// ListRequiredBuildsInput is the argument set for list_required_builds.
type ListRequiredBuildsInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 25)"`
}

// ListRequiredBuildsOutput names the collection it holds.
type ListRequiredBuildsOutput struct {
	RequiredBuilds []openapigenerated.RestRequiredBuildCondition `json:"required_builds"`
}

func specListRequiredBuilds() Spec {
	tool := &mcp.Tool{
		Name:        "list_required_builds",
		Description: "List required build checks that must pass before a pull request can be merged. Check this before attempting a merge to understand what CI must succeed.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[ListRequiredBuildsInput, ListRequiredBuildsOutput] {
		svc := qualityservice.NewService(c.OpenAPI)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in ListRequiredBuildsInput) (*mcp.CallToolResult, ListRequiredBuildsOutput, error) {
			checks, err := svc.ListRequiredBuildChecks(ctx,
				qualityservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo},
				limitOrDefault(in.Limit),
			)
			if err != nil {
				return nil, ListRequiredBuildsOutput{}, fmt.Errorf("list_required_builds failed: %w", err)
			}
			return nil, ListRequiredBuildsOutput{RequiredBuilds: checks}, nil
		}
	})
}
