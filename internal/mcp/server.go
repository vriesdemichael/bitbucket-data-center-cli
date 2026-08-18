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
//
// Safe decides whether a tool is exposed without --yolo / --allow-writes. Set
// it false when either of two things is true:
//
//  1. The effect is irreversible or hard to undo — merging, or enabling
//     auto-merge, which causes a merge later.
//  2. The effect influences merge gating — reporting a build status, or
//     submitting a review. Both are reversible, and both can unblock a merge
//     the gate exists to hold, so an agent doing them takes part in a control
//     it is meant to be subject to.
//
// The second reason is easy to miss. Reversibility alone once let review
// submission through as "like commenting", when APPROVED is precisely the input
// a required-reviewer check consumes.
//
// Creating things is generally safe even where this package offers no way to
// undo it: opening a pull request or tagging a commit changes no branch and
// gates nothing. Judge by consequence, not by whether a delete tool exists.
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
		// Opening a PR has low consequence: it changes no branch and blocks
		// nothing. Not "easily reversed" — pr decline has no tool here — but it
		// does not need to be.
		{specCreatePullRequest().Tool, specCreatePullRequest().Handler, true},
		// Editing title, description or draft state affects only the request.
		{specUpdatePullRequest().Tool, specUpdatePullRequest().Handler, true},
		{specListPRComments().Tool, specListPRComments().Handler, true},
		{specGetPRDiff().Tool, specGetPRDiff().Handler, true},
		{specGetFileContent().Tool, specGetFileContent().Handler, true},
		// Adding a comment is trivially reversed — safe by default.
		{specAddPRComment().Tool, specAddPRComment().Handler, true},
		// Submitting a review influences merge gating: APPROVED is the input a
		// merge check consumes, so an agent that can approve takes part in the
		// review it is subject to. Gated for the same reason as set_build_status,
		// not because it is irreversible.
		//
		// Gating the whole tool rather than only its APPROVED path: the server
		// filters by tool, not by argument, so splitting would mean threading the
		// yolo setting into every handler. NEEDS_WORK is the conservative outcome
		// and loses least by being withheld.
		{specSubmitPRReview().Tool, specSubmitPRReview().Handler, false},
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
		// Creating a tag marks a commit and gates nothing. Note tag delete has no
		// tool here, so this is low-consequence rather than easily reversed.
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
