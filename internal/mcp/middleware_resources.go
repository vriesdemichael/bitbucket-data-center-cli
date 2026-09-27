package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// governor holds what the governance middleware enforces and where it
// records.
type governor struct {
	scope     Scope
	audit     *AuditLogger
	onFailure AuditFailureMode
	warn      func(string)
}

func (g governor) write(record AuditRecord) error {
	return writeAudit(g.audit, record, g.onFailure, g.warn)
}

// settle fills in how a request ended.
func settle(record *AuditRecord, started time.Time, err error) {
	record.DurationMS = time.Since(started).Milliseconds()
	if err != nil {
		record.Status = auditStatusError
		record.ErrorMessage = err.Error()
		return
	}
	record.Status = auditStatusSuccess
}

// readResource bounds a resource read to the scope, and records it.
//
// A URI no template matches is left to the SDK, which answers that the
// resource does not exist; it is recorded all the same, since a model probing
// for URIs is worth seeing.
func (g governor) readResource(ctx context.Context, next mcp.MethodHandler, method string, req *mcp.ReadResourceRequest) (mcp.Result, error) {
	started := time.Now()
	uri := req.Params.URI
	record := AuditRecord{
		Timestamp: auditTimestamp(started),
		Event:     auditEventResourceRead,
		Resource:  uri,
		TraceID:   traceParent(req.Params.Meta),
	}

	if _, ref, ok := matchResource(uri); ok {
		record.Project, record.Repo = ref.project, ref.repo
		if err := resourceInScope(ref.project, ref.repo, g.scope); err != nil {
			record.Status = auditStatusDenied
			record.DurationMS = time.Since(started).Milliseconds()
			record.ErrorMessage = err.Error()
			if auditErr := g.write(record); auditErr != nil {
				return nil, auditErr
			}
			// There is no tool error for a resource: a refusal is a
			// protocol error, -32602, with the reason.
			return nil, &jsonrpc.Error{
				Code:    jsonrpc.CodeInvalidParams,
				Message: err.Error(),
				Data:    json.RawMessage(fmt.Sprintf(`{"uri":%q}`, uri)),
			}
		}
	}

	result, err := next(ctx, method, req)
	settle(&record, started, err)
	if auditErr := g.write(record); auditErr != nil {
		return nil, auditErr
	}

	return result, err
}

// listResources records the resource list, and keeps only what the scope
// allows in it: the list builder narrows by scope too, and this is the check
// that does not depend on it remembering.
func (g governor) listResources(ctx context.Context, next mcp.MethodHandler, method string, req *mcp.ListResourcesRequest) (mcp.Result, error) {
	started := time.Now()
	record := AuditRecord{Timestamp: auditTimestamp(started), Event: auditEventResourceList}
	if req.Params != nil {
		record.TraceID = traceParent(req.Params.Meta)
	}

	result, err := next(ctx, method, req)
	if list, ok := result.(*mcp.ListResourcesResult); ok && list != nil && g.scope.IsSet() {
		kept := make([]*mcp.Resource, 0, len(list.Resources))
		for _, resource := range list.Resources {
			// A resource no template matches cannot be bounded, so under a
			// scope it is dropped rather than passed through.
			if _, ref, ok := matchResource(resource.URI); ok && resourceInScope(ref.project, ref.repo, g.scope) == nil {
				kept = append(kept, resource)
			}
		}
		list.Resources = kept
	}

	settle(&record, started, err)
	if auditErr := g.write(record); auditErr != nil {
		return nil, auditErr
	}

	return result, err
}

// getPrompt binds a prompt's project and repository to the scope, as a tool
// call's are, and records it: a prompt reads the resources it embeds.
func (g governor) getPrompt(ctx context.Context, next mcp.MethodHandler, method string, req *mcp.GetPromptRequest) (mcp.Result, error) {
	started := time.Now()
	record := AuditRecord{
		Timestamp: auditTimestamp(started),
		Event:     auditEventPromptGet,
		Prompt:    req.Params.Name,
		TraceID:   traceParent(req.Params.Meta),
	}

	arguments := map[string]string{}
	for name, value := range req.Params.Arguments {
		arguments[name] = value
	}
	scopeErr := bindPromptScope(arguments, g.scope)
	record.Project, record.Repo = arguments["project"], arguments["repo"]
	if encoded, err := json.Marshal(arguments); err == nil {
		record.Arguments = auditArguments(encoded)
	}

	if scopeErr != nil {
		record.Status = auditStatusDenied
		record.DurationMS = time.Since(started).Milliseconds()
		record.ErrorMessage = scopeErr.Error()
		if auditErr := g.write(record); auditErr != nil {
			return nil, auditErr
		}
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: scopeErr.Error()}
	}
	req.Params.Arguments = arguments

	result, err := next(ctx, method, req)
	settle(&record, started, err)
	if auditErr := g.write(record); auditErr != nil {
		return nil, auditErr
	}

	return result, err
}

// complete keeps suggestions inside the scope. A project, or a repository
// under a repository scope, is the scoped one or nothing, answered here without
// asking Bitbucket, which would list what the scope leaves out. For any other
// argument the arguments already chosen are pinned to the scope, so what is
// listed for a pull request, a path or a ref comes from the scoped repository.
// Completions are not recorded: the person types them in a picker, one request
// per keystroke.
//
// Answering here is safe for a completion, unlike a tool call or a read: the
// SDK sets a completion's resultType after the middleware has run.
func (g governor) complete(ctx context.Context, next mcp.MethodHandler, method string, req *mcp.CompleteRequest) (mcp.Result, error) {
	if !g.scope.IsSet() {
		return next(ctx, method, req)
	}

	typed := req.Params.Argument.Value
	switch {
	case req.Params.Argument.Name == "project":
		return &mcp.CompleteResult{Completion: onlyScoped(g.scope.ProjectKey, typed)}, nil
	case req.Params.Argument.Name == "repo" && g.scope.RepoSlug != "":
		return &mcp.CompleteResult{Completion: onlyScoped(g.scope.RepoSlug, typed)}, nil
	}

	arguments := map[string]string{}
	if req.Params.Context != nil {
		for name, value := range req.Params.Context.Arguments {
			arguments[name] = value
		}
	}
	if g.scope.ProjectKey != "" {
		arguments["project"] = g.scope.ProjectKey
	}
	if g.scope.RepoSlug != "" {
		arguments["repo"] = g.scope.RepoSlug
	}
	req.Params.Context = &mcp.CompleteContext{Arguments: arguments}

	return next(ctx, method, req)
}

// onlyScoped is the one suggestion a scope leaves for its own argument.
func onlyScoped(scoped, typed string) mcp.CompletionResultDetails {
	if strings.HasPrefix(strings.ToLower(scoped), strings.ToLower(strings.TrimSpace(typed))) {
		return mcp.CompletionResultDetails{Values: []string{scoped}}
	}

	return mcp.CompletionResultDetails{Values: []string{}}
}

// resourceInScope reports why a project and repository are outside the scope,
// or nil when they are inside it.
func resourceInScope(project, repo string, scope Scope) error {
	if scope.ProjectKey != "" && !strings.EqualFold(strings.TrimSpace(project), scope.ProjectKey) {
		return fmt.Errorf("access denied: project %q is outside the scope this MCP server is confined to (%s)", project, scope)
	}
	if scope.RepoSlug != "" && !strings.EqualFold(strings.TrimSpace(repo), scope.RepoSlug) {
		return fmt.Errorf("access denied: repo %q is outside the scope this MCP server is confined to (%s)", repo, scope)
	}

	return nil
}

// bindPromptScope pins a prompt's project and repository to the scope, as
// bindScopedArgument does a tool's: filled in when left out, refused when they
// name something else.
func bindPromptScope(arguments map[string]string, scope Scope) error {
	for _, binding := range []struct{ key, want string }{{"project", scope.ProjectKey}, {"repo", scope.RepoSlug}} {
		if binding.want == "" {
			continue
		}
		got := strings.TrimSpace(arguments[binding.key])
		if got != "" && !strings.EqualFold(got, binding.want) {
			return fmt.Errorf("access denied: %s %q is outside the scope this MCP server is confined to (%s)", binding.key, got, scope)
		}
		arguments[binding.key] = binding.want
	}

	return nil
}

// resourceMiddleware adds what the SDK cannot: the part of resources/list that
// depends on whose credentials the server holds, when the server serves it,
// and the resource_link beside a tool result that has a matching resource,
// for a client whose revision has resource links.
//
// It changes the result the SDK produced rather than answering in its place.
// A tool result built outside the SDK's dispatcher goes out without the
// resultType the 2026-07-28 revision requires (go-sdk#1225).
func resourceMiddleware(clients Clients, scope Scope, listed bool) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			result, err := next(ctx, method, req)
			if err != nil {
				return result, err
			}

			switch typed := req.(type) {
			case *mcp.ListResourcesRequest:
				list, ok := result.(*mcp.ListResourcesResult)
				// The listed pull requests come on the first page. The
				// templates' own list has one page, so there is no second.
				if !listed || !ok || list == nil || (typed.Params != nil && typed.Params.Cursor != "") {
					return result, nil
				}
				listed, listErr := listedResources(ctx, clients, scope)
				if listErr != nil {
					return nil, &jsonrpc.Error{Code: jsonrpc.CodeInternalError, Message: listErr.Error()}
				}
				list.Resources = append(list.Resources, listed...)
			case *mcp.CallToolRequest:
				if callResult, ok := result.(*mcp.CallToolResult); ok && supportsResourceLinks(typed.ProtocolVersion()) {
					linkResource(typed.Params.Name, rawArguments(typed.Params.Arguments), callResult)
				}
			}

			return result, nil
		}
	}
}
