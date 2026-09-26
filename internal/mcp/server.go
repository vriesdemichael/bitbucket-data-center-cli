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
// Asks says whether a call asks the person to confirm it through the client
// before it runs (see Asking). The server enforces it; the annotations only
// describe the tool, and a client may ignore them.
type Spec struct {
	Tool     *mcp.Tool
	Register func(*mcp.Server, Clients)
	Asks     Asking
}

// ReadOnly reports whether the tool changes nothing, as its annotation says.
// It is what --read-only exposes.
func (s Spec) ReadOnly() bool {
	return s.Tool.Annotations != nil && s.Tool.Annotations.ReadOnlyHint
}

// toolSpec binds a tool definition to a typed handler factory, for a tool that
// runs when called.
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
func toolSpec[In, Out any](tool *mcp.Tool, handler func(Clients) mcp.ToolHandlerFor[In, Out]) Spec {
	titled(tool)

	return Spec{
		Tool: tool,
		Asks: AsksNever,
		Register: func(server *mcp.Server, clients Clients) {
			mcp.AddTool(server, tool, handler(clients))
		},
	}
}

// askingSpec is toolSpec for a tool that asks the person to confirm a call
// before it runs, through the client (see confirmed).
func askingSpec[In, Out any](tool *mcp.Tool, asking Asking, policy ask[In], handler func(Clients) mcp.ToolHandlerFor[In, Out]) Spec {
	titled(tool)

	return Spec{
		Tool: tool,
		Asks: asking,
		Register: func(server *mcp.Server, clients Clients) {
			mcp.AddTool(server, tool, confirmed(tool.Name, policy, clients, handler(clients)))
		},
	}
}

// titled gives the tool the display title its annotations carry. Clients on
// 2025-06-18 and later read the tool's own title, and 2025-03-26 clients only
// the annotation's, so both say the same.
func titled(tool *mcp.Tool) {
	if tool.Annotations != nil && tool.Title == "" {
		tool.Title = tool.Annotations.Title
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

// describedInputSchema derives the input schema for In and replaces named
// properties' descriptions.
//
// For a description built from the values a validator checks, which a struct
// tag cannot hold: a vocabulary written into the tag is a second copy, and the
// second copy is the one that drifted (#577). Panics on a property In does
// not have, as enumInputSchema does.
func describedInputSchema[In any](descriptions map[string]string) *jsonschema.Schema {
	schema, err := jsonschema.For[In](nil)
	if err != nil {
		panic(fmt.Sprintf("deriving input schema for %T: %v", *new(In), err))
	}
	for property, description := range descriptions {
		target, ok := schema.Properties[property]
		if !ok {
			panic(fmt.Sprintf("description declared for property %q which %T does not have", property, *new(In)))
		}
		target.Description = description
	}

	return schema
}

// readOnly and writes declare a tool's annotations: all four hints, and the
// title a client shows. Each hint says what the spec defines it to say, and
// none of them says whether the tool asks, which is the server's own policy
// (Spec.Asks) and is enforced rather than advised.
//
//   - readOnlyHint: the tool changes nothing.
//   - destructiveHint: a write may overwrite or remove something, rather than
//     only add. create_tag only adds, though it asks; update_pull_request
//     overwrites a title, though it asks only about the draft flag.
//   - idempotentHint: calling again with the same arguments changes nothing
//     more. Most writes here are, because Bitbucket refuses the repeat or
//     finds it already done; add_pr_comment is not.
//   - openWorldHint: false on every tool. bb talks to the one Bitbucket Data
//     Center instance it is configured for, which is a closed domain, and the
//     people who write its content are that instance's users.
//
// If bb ever serves Bitbucket Cloud, set openWorldHint true on the tools that
// read or publish content there. Cloud is an open world: its public
// repositories take code and comments from anyone, and a client is meant to
// treat what such a tool returns as having crossed a trust boundary.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    true,
		DestructiveHint: boolRef(false),
		IdempotentHint:  true,
		OpenWorldHint:   boolRef(false),
	}
}

func writes(title string, destructive, idempotent bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{
		Title:           title,
		DestructiveHint: boolRef(destructive),
		IdempotentHint:  idempotent,
		OpenWorldHint:   boolRef(false),
	}
}

func boolRef(value bool) *bool {
	return &value
}

// AllSpecs returns the full catalog of MCP tool specifications in stable order.
//
// This is the order bb ai mcp tools prints. tools/list is ordered by the SDK,
// which sorts by name; either way the listing is deterministic, as the
// 2026-07-28 revision asks, so clients can cache it.
func AllSpecs() []Spec {
	return []Spec{
		// Pull request group
		specGetPullRequest(),
		specListPullRequests(),
		specCreatePullRequest(),
		// Asks only to change the draft flag, which decides whether the pull
		// request can merge and cancels its auto-merge.
		specUpdatePullRequest(),
		specListPRComments(),
		specGetPRDiff(),
		specGetFileContent(),
		specAddPRComment(),
		// Asks: APPROVED is the input a required-reviewer check consumes, and
		// NEEDS_WORK holds a merge back.
		specSubmitPRReview(),
		// Asks: merges now, and cannot be undone.
		specMergePullRequest(),
		// Asks: merges later, or at once when the checks already pass.
		specEnableAutoMerge(),
		// Asks: changes when a pull request merges.
		specDisableAutoMerge(),
		// Repository group
		specSearchRepositories(),
		specGetRepositoryCloneInfo(),
		// Branch / ref group
		specListBranches(),
		specResolveRef(),
		// Tag group
		specListTags(),
		// Asks: release pipelines commonly act on a new tag, and no tool here
		// deletes one.
		specCreateTag(),
		// Build / quality group
		specGetBuildStatus(),
		// Asks: a successful required build can let a pull request merge.
		specSetBuildStatus(),
		specListRequiredBuilds(),
		// Commit group
		specListCommits(),
		specGetCommit(),
		specCompareRefs(),
	}
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

	// Allow exposes only these tools. Exclude suppresses tools afterwards.
	Allow   []string
	Exclude []string

	// ReadOnly exposes only the tools that change nothing (readOnlyHint).
	//
	// It is for a client the operator does not trust with the annotations and
	// the confirmations tools ask for through elicitation. A client that cannot
	// be trusted with those should not act in Bitbucket through this server at
	// all; the person makes those changes themselves. Allow cannot override it.
	ReadOnly bool

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
// Every tool is exposed unless a filter withholds it: Allow keeps only the
// tools it names, Exclude removes tools, ReadOnly keeps only the tools that
// change nothing, and a Scope withholds the tools it cannot bound. The filters
// are server configuration, the same for every connection, so tools/list does
// not vary per connection as the 2026-07-28 revision requires. In particular
// it does not depend on whether a client can confirm a call: a tool that asks
// is listed to every client and refuses the call where it cannot ask.
func NewServer(opts ServerOptions) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: opts.Name, Version: opts.Version}, &mcp.ServerOptions{
		Instructions: instructions(opts),
		// Tools, and nothing else. Left to itself the SDK also advertises
		// logging, which the 2026-07-28 revision deprecates and this server
		// never sends, and a tool list that changes, which this one never does.
		Capabilities: &mcp.ServerCapabilities{Tools: &mcp.ToolCapabilities{}},
	})

	allowSet := toSet(opts.Allow)
	excludeSet := toSet(opts.Exclude)

	for _, spec := range AllSpecs() {
		toolName := spec.Tool.Name
		if len(allowSet) > 0 && !allowSet[toolName] {
			continue
		}
		if excludeSet[toolName] {
			continue
		}
		// The read-only and scope filters come last, and naming a tool in
		// --tools overrides neither. The first two express what an operator
		// wants exposed; these express what the server may do for this client,
		// and what it is able to bound. Naming a tool cannot make a commit SHA
		// belong to a project.
		if opts.ReadOnly && !spec.ReadOnly() {
			continue
		}
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

// instructions is what the server tells a model about using its tools
// together, in the few lines a client loads beside the tool names.
//
// It repeats no tool description, and it is guidance rather than a control:
// what it says the server does, the server enforces. It follows the server's
// configuration, which is the same for every connection.
func instructions(opts ServerOptions) string {
	var paragraphs []string

	if opts.ReadOnly {
		paragraphs = append(paragraphs, "This server is read-only: it offers only the tools that read.")
	} else {
		paragraphs = append(paragraphs, "Some tools ask the person to confirm a call in the client before they run, and their descriptions say so. "+
			"If the person declines or closes the confirmation, do not call the tool again unless they ask.")
	}

	paragraphs = append(paragraphs, "A list tool stops at its limit. When limit_reached is true there may be more: call it again with a higher limit.")

	if opts.Scope.IsSet() {
		paragraphs = append(paragraphs, fmt.Sprintf("This server is confined to %s. Tools fill in project and repo when you leave them out, "+
			"and refuse calls aimed anywhere else.", opts.Scope))
	}

	return strings.Join(paragraphs, "\n\n")
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

// capped cuts a result to limit and reports whether it reached it (#573).
//
// Every list tool returns through it. An agent that asked for 25 and received
// 25 cannot otherwise tell a full page from all there is, and a field that is
// only sometimes present reads as "not truncated" when it is absent. Reaching
// the limit is the signal the CLI gives as meta.limitReached: there may be
// more, so call again with a higher limit.
//
// The cut is belt and braces, as paging.Truncate is for the CLI (ADR-074): the
// services already stop at the limit, and this keeps the flag honest if one
// ever does not.
func capped[T any](limit int, results []T) ([]T, bool) {
	if len(results) > limit {
		results = results[:limit]
	}
	return results, len(results) >= limit
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
