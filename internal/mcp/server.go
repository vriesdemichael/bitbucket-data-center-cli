package mcp

import (
	"fmt"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

// Clients bundles the two HTTP client variants and the resolved base URL consumed by the service layer.
type Clients struct {
	HTTP    *httpclient.Client
	OpenAPI *openapigenerated.ClientWithResponses
	BaseURL string // normalised Bitbucket base URL (no trailing slash)
}

// ClientsFromConfig builds both client types from a resolved AppConfig.
func ClientsFromConfig(cfg config.AppConfig) (Clients, error) {
	httpClient := httpclient.NewFromConfig(cfg)
	openAPIClient, err := openapi.NewClientWithResponsesFromConfig(cfg)
	if err != nil {
		return Clients{}, fmt.Errorf("failed to create openapi client: %w", err)
	}
	return Clients{
		HTTP:    httpClient,
		OpenAPI: openAPIClient,
		BaseURL: strings.TrimRight(cfg.BitbucketURL, "/"),
	}, nil
}

// Spec pairs a tool definition with a handler factory so that metadata
// can be listed without a live Bitbucket connection.
// Safe marks tools whose side-effects are low-blast-radius and easily reversed
// (e.g. opening a PR, adding a comment). Unsafe tools (e.g. merge_pull_request)
// require opt-in via --yolo / --allow-writes.
type Spec struct {
	Tool    mcpgo.Tool
	Handler func(Clients) server.ToolHandlerFunc
	Safe    bool
}

// AllSpecs returns the full catalog of MCP tool specifications in stable order.
func AllSpecs() []Spec {
	return []Spec{
		// Pull request group
		// Reading PR state is always safe.
		{specGetPullRequest().Tool, specGetPullRequest().Handler, true},
		{specListPullRequests().Tool, specListPullRequests().Handler, true},
		// Opening a PR is low-blast-radius and easily closed — safe by default.
		{specCreatePullRequest().Tool, specCreatePullRequest().Handler, true},
		{specListPRComments().Tool, specListPRComments().Handler, true},
		// Adding a comment is trivially reversed — safe by default.
		{specAddPRComment().Tool, specAddPRComment().Handler, true},
		{specListPRTasks().Tool, specListPRTasks().Handler, true},
		// Submitting a review is like commenting and can be dismissed — safe by default.
		{specSubmitPRReview().Tool, specSubmitPRReview().Handler, true},
		// Merging is irreversible and affects the target branch — requires --yolo.
		{specMergePullRequest().Tool, specMergePullRequest().Handler, false},
		// Enabling auto-merge can trigger an irreversible merge — requires --yolo.
		{specEnableAutoMerge().Tool, specEnableAutoMerge().Handler, false},
		// Disabling auto-merge stops automation — safe (easily re-enabled).
		{specDisableAutoMerge().Tool, specDisableAutoMerge().Handler, true},
		// Repository group
		{specSearchRepositories().Tool, specSearchRepositories().Handler, true},
		{specGetRepositoryCloneInfo().Tool, specGetRepositoryCloneInfo().Handler, true},
		// Branch / ref group
		{specListBranches().Tool, specListBranches().Handler, true},
		{specResolveRef().Tool, specResolveRef().Handler, true},
		// Tag group
		{specListTags().Tool, specListTags().Handler, true},
		// Creating a tag is a low-risk marker operation — safe by default.
		{specCreateTag().Tool, specCreateTag().Handler, true},
		// Build / quality group
		{specGetBuildStatus().Tool, specGetBuildStatus().Handler, true},
		// Setting a build status is a write operation that affects CI signal — requires --yolo.
		{specSetBuildStatus().Tool, specSetBuildStatus().Handler, false},
		{specListRequiredBuilds().Tool, specListRequiredBuilds().Handler, true},
		// Commit group
		{specListCommits().Tool, specListCommits().Handler, true},
		{specGetCommit().Tool, specGetCommit().Handler, true},
		{specCompareRefs().Tool, specCompareRefs().Handler, true},
	}
}

// SafeSpecs returns only the tools marked as safe for use without --yolo.
func SafeSpecs() []Spec {
	all := AllSpecs()
	out := make([]Spec, 0, len(all))
	for _, s := range all {
		if s.Safe {
			out = append(out, s)
		}
	}
	return out
}

// NewServer creates a configured MCPServer with optional tool filtering.
// allow is a list of tool names to expose exclusively (empty = all).
// exclude is a list of tool names to suppress.
// yolo enables unrestricted mode: all tools are exposed including unsafe ones
// (e.g. merge_pull_request). In safe mode (yolo=false), only tools marked
// Safe are exposed unless an explicit allow list is provided.
// When allow is non-empty it takes full precedence over the safety filter;
// exclude is still applied afterwards in all modes.
func NewServer(name, version string, clients Clients, allow, exclude []string, yolo bool) *server.MCPServer {
	s := server.NewMCPServer(name, version, server.WithToolCapabilities(false))

	allowSet := toSet(allow)
	excludeSet := toSet(exclude)

	for _, spec := range AllSpecs() {
		toolName := spec.Tool.Name
		if len(allowSet) > 0 {
			// Explicit allowlist takes full precedence over the safety filter.
			if !allowSet[toolName] {
				continue
			}
		} else if !yolo && !spec.Safe {
			// Safe mode: skip tools not marked as safe.
			continue
		}
		if excludeSet[toolName] {
			continue
		}
		s.AddTool(spec.Tool, spec.Handler(clients))
	}

	return s
}

// toSet converts a string slice into a presence map, trimming whitespace.
func toSet(items []string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, item := range items {
		if t := strings.TrimSpace(item); t != "" {
			m[t] = true
		}
	}
	return m
}

// resultJSON serialises data as a JSON tool result.
// Serialisation errors are surfaced as error results rather than Go errors
// because tool handlers report operational errors through the result value.
func resultJSON(data any) (*mcpgo.CallToolResult, error) {
	result, serErr := mcpgo.NewToolResultJSON(data)
	if serErr != nil {
		return mcpgo.NewToolResultErrorFromErr("failed to serialize result", serErr), nil
	}
	return result, nil
}
