package mcp

import (
	"fmt"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
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

// Spec pairs a tool definition with a registration function so that metadata
// can be listed without a live Bitbucket connection.
//
// Register rather than a handler value because the SDK derives both schemas
// from the handler's type parameters, and mcp.AddTool is a generic free
// function: only a closure that already knows the concrete In and Out types
// can call it. See toolSpec.
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
	Tool     *mcp.Tool
	Register func(*mcp.Server, Clients)
	Safe     bool
}

// toolSpec binds a tool definition to a typed handler factory.
//
// In and Out are the whole output contract. The SDK derives the input schema
// from In and the output schema from Out, validates arguments against the
// former before the handler runs, and validates the marshalled result against
// the latter before it reaches the client — so a handler cannot return a shape
// its declared schema does not describe. Out is also what populates
// structuredContent, with the JSON serialisation of the same value used as the
// text content when the handler does not set one.
//
// Every Out in this package is a named struct with a single collection or
// object field, so structuredContent is always a JSON object. Clients that
// validate results against a pre-SEP-2106 revision of the spec reject a bare
// array with "expected record, received array"; naming the payload avoids that
// by construction rather than by wrapping non-objects at the choke point, which
// is what this package did before (issue #416, ADR-061).
func toolSpec[In, Out any](tool *mcp.Tool, safe bool, handler func(Clients) mcp.ToolHandlerFor[In, Out]) Spec {
	// DestructiveHint is derived, never declared. It and Safe answer the same
	// question, so declaring both invites them to disagree; deriving makes the
	// contradiction unrepresentable rather than merely tested for.
	if tool.Annotations == nil {
		tool.Annotations = &mcp.ToolAnnotations{}
	}
	destructive := !safe
	tool.Annotations.DestructiveHint = &destructive

	return Spec{
		Tool: tool,
		Safe: safe,
		Register: func(server *mcp.Server, clients Clients) {
			mcp.AddTool(server, tool, handler(clients))
		},
	}
}

// enumInputSchema derives the input schema for In and pins named properties to
// a fixed set of permitted values.
//
// The schema is derived rather than hand-written so it cannot drift from In.
// The enum is applied afterwards because a struct tag has no way to express
// one, and publishing the permitted values is worth the extra step: it is the
// difference between a model guessing "APPROVE" and reading that the tool wants
// "approve".
//
// Panics on a property that In does not have, in the same spirit as
// mcp.AddTool panicking on a malformed tool — both are wiring errors fixed at
// the call site, and TestAllSpecsBuild reaches every one of them.
func enumInputSchema[In any](enums map[string][]string) *jsonschema.Schema {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("deriving input schema for %T: %v", *new(In), err))
	}
	for property, values := range enums {
		target, ok := schema.Properties[property]
		if !ok {
			panic(fmt.Sprintf("enum declared for property %q which %T does not have", property, *new(In)))
		}
		target.Enum = make([]any, len(values))
		for i, value := range values {
			target.Enum[i] = value
		}
	}
	return schema
}

// readOnly and mutating say whether a tool writes at all. That is the one thing
// they say: DestructiveHint is not set here, because it answers the same
// question as Safe and is derived from it in toolSpec.
//
// Two hand-written answers to "is this dangerous" can disagree, and the
// disagreement points the wrong way. Safe is what the server enforces;
// DestructiveHint is advice a client may ignore. A tool exposed without --yolo
// while annotating itself destructive would hand an agent something it was
// never gated on, and only one of the two directions was ever checked.
//
// Writing is not the same question as destroying, so this distinction stays:
// create_pull_request writes and destroys nothing.
func readOnly() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{ReadOnlyHint: true}
}

func mutating() *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{}
}

// AllSpecs returns the full catalog of MCP tool specifications in stable order.
//
// The order is part of the contract: the 2026-07-28 revision asks servers to
// return tools/list in a deterministic order so clients can cache the listing
// and keep prompt-cache hit rates up.
func AllSpecs() []Spec {
	return []Spec{
		// Pull request group
		// Reading PR state is always safe.
		specGetPullRequest(),
		specListPullRequests(),
		// Opening a PR has low consequence: it changes no branch and blocks
		// nothing. Not "easily reversed" — pr decline has no tool here — but it
		// does not need to be.
		specCreatePullRequest(),
		// Editing title, description or draft state affects only the request.
		specUpdatePullRequest(),
		specListPRComments(),
		specGetPRDiff(),
		specGetFileContent(),
		// Adding a comment is trivially reversed — safe by default.
		specAddPRComment(),
		// Submitting a review influences merge gating: APPROVED is the input a
		// merge check consumes, so an agent that can approve takes part in the
		// review it is subject to. Gated for the same reason as set_build_status,
		// not because it is irreversible.
		//
		// Gating the whole tool rather than only its APPROVED path: the server
		// filters by tool, not by argument, so splitting would mean threading the
		// yolo setting into every handler. NEEDS_WORK is the conservative outcome
		// and loses least by being withheld.
		specSubmitPRReview(),
		// Merging is irreversible and affects the target branch — requires --yolo.
		specMergePullRequest(),
		// Enabling auto-merge can trigger an irreversible merge — requires --yolo.
		specEnableAutoMerge(),
		// Disabling auto-merge stops automation — safe (easily re-enabled).
		specDisableAutoMerge(),
		// Repository group
		specSearchRepositories(),
		specGetRepositoryCloneInfo(),
		// Branch / ref group
		specListBranches(),
		specResolveRef(),
		// Tag group
		specListTags(),
		// Creating a tag marks a commit and gates nothing. Note tag delete has no
		// tool here, so this is low-consequence rather than easily reversed.
		specCreateTag(),
		// Build / quality group
		specGetBuildStatus(),
		// Setting a build status is a write operation that affects CI signal — requires --yolo.
		specSetBuildStatus(),
		specListRequiredBuilds(),
		// Commit group
		specListCommits(),
		specGetCommit(),
		specCompareRefs(),
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

// ServerOptions configures a server.
//
// A struct rather than a parameter list because the governance controls added
// for issue #423 pushed this past the point where positional booleans and
// string slices could be read at the call site.
type ServerOptions struct {
	Name    string
	Version string
	Clients Clients

	// Allow exposes exactly these tools, taking full precedence over the
	// safety filter. Exclude suppresses tools afterwards, in every mode.
	Allow   []string
	Exclude []string

	// Yolo lifts the safety filter, exposing tools whose effects are
	// irreversible or which influence merge gating. See Spec.Safe.
	Yolo bool

	// Scope confines the server to one project or repository. The zero value
	// is unscoped.
	Scope Scope

	// Audit, when non-nil, receives one record per tool call.
	Audit *AuditLogger

	// AuditFailure decides whether a call proceeds when its record cannot be
	// written. Empty means AuditFailureDeny.
	AuditFailure AuditFailureMode

	// Warn receives operational messages that must not go to stdout, which is
	// the protocol channel. Optional.
	Warn func(string)
}

// NewServer creates a configured MCP server.
//
// Tool exposure is decided by three filters in order: an explicit Allow list
// wins over the safety filter, Exclude is applied afterwards in every mode, and
// a Scope withholds the tools it cannot bound.
func NewServer(opts ServerOptions) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: opts.Name, Version: opts.Version}, nil)

	allowSet := toSet(opts.Allow)
	excludeSet := toSet(opts.Exclude)

	for _, spec := range AllSpecs() {
		toolName := spec.Tool.Name
		if len(allowSet) > 0 {
			// Explicit allowlist takes full precedence over the safety filter.
			if !allowSet[toolName] {
				continue
			}
		} else if !opts.Yolo && !spec.Safe {
			// Safe mode: skip tools not marked as safe.
			continue
		}
		if excludeSet[toolName] {
			continue
		}
		// The scope filter is deliberately last and not overridable by Allow.
		// The other two express what an operator wants exposed; this one
		// expresses what the server is able to bound, and naming a tool in
		// --tools cannot make a commit SHA belong to a project.
		if withheldUnderScope(toolName, opts.Scope) {
			continue
		}
		spec.Register(server, opts.Clients)
	}

	if opts.Scope.IsSet() || opts.Audit != nil {
		failure := opts.AuditFailure
		if failure == "" {
			failure = AuditFailureDeny
		}
		server.AddReceivingMiddleware(governanceMiddleware(opts.Scope, opts.Audit, failure, opts.Warn))
	}

	return server
}

// defaultLimit is the page size every list tool falls back to.
const defaultLimit = 25

// limitOrDefault substitutes the default page size for an omitted limit.
//
// A typed input struct cannot distinguish "limit was absent" from "limit was
// 0" without making the field a pointer, and a pointer per list tool buys
// nothing: 0 is not a useful page size, so both cases want the default. The
// tool descriptions say so.
func limitOrDefault(limit int) int {
	if limit <= 0 {
		return defaultLimit
	}
	return limit
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
