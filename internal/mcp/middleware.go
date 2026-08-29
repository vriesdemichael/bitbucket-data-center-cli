package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// methodCallTool is the JSON-RPC method the governance middleware acts on.
// The SDK keeps its own copy unexported, so it is spelled out here.
const methodCallTool = "tools/call"

// traceParentKey is the _meta key the 2026-07-28 revision documents for W3C
// trace context propagation.
const traceParentKey = "traceparent"

// AuditFailureMode decides what happens when an audit record cannot be written.
type AuditFailureMode string

const (
	// AuditFailureDeny fails the tool call when its record cannot be written.
	// This is the default: an audit trail that silently stops recording is
	// worse than no audit trail, because the absence of a record then carries
	// no information.
	AuditFailureDeny AuditFailureMode = "deny"

	// AuditFailureWarn lets the call proceed and reports the write failure on
	// the diagnostics stream. For an operator who would rather lose records
	// than lose the server.
	AuditFailureWarn AuditFailureMode = "warn"
)

// governanceMiddleware enforces the scope boundary and writes the audit trail.
//
// Both live in one receiving middleware rather than in the 24 handlers. A
// single choke point over tools/call is the only place where "every tool call
// is checked" is a property of the code rather than a property of 24 people
// remembering, and it is the same reason the audit record cannot be forgotten
// by a handler that returns early.
func governanceMiddleware(scope Scope, audit *AuditLogger, onFailure AuditFailureMode, warn func(string)) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != methodCallTool {
				return next(ctx, method, req)
			}
			call, ok := req.(*mcp.CallToolRequest)
			if !ok {
				return next(ctx, method, req)
			}

			started := time.Now()
			toolName := call.Params.Name
			arguments := rawArguments(call.Params.Arguments)

			record := AuditRecord{
				Timestamp: auditTimestamp(started),
				Tool:      toolName,
				TraceID:   traceParent(call.Params.Meta),
			}

			scoped, scopeErr := applyScope(toolName, arguments, scope)
			if scopeErr != nil {
				record.Status = auditStatusDenied
				record.DurationMS = time.Since(started).Milliseconds()
				record.Arguments = auditArguments(arguments)
				record.Project, record.Repo = scopedTarget(arguments)
				record.ErrorMessage = scopeErr.Error()
				if err := writeAudit(audit, record, onFailure, warn); err != nil {
					return nil, err
				}
				// A tool error rather than a protocol error: the agent can read
				// it and correct course, which is the whole point of telling it
				// the call was out of scope.
				return toolError(scopeErr), nil
			}

			call.Params.Arguments = scoped
			record.Project, record.Repo = scopedTarget(scoped)
			record.Arguments = auditArguments(scoped)

			result, err := next(ctx, method, req)

			record.DurationMS = time.Since(started).Milliseconds()
			switch {
			case err != nil:
				record.Status = auditStatusError
				record.ErrorMessage = err.Error()
			case isErrorResult(result):
				record.Status = auditStatusError
				record.ErrorMessage = resultErrorText(result)
			default:
				record.Status = auditStatusSuccess
			}

			if auditErr := writeAudit(audit, record, onFailure, warn); auditErr != nil {
				return nil, auditErr
			}
			return result, err
		}
	}
}

// writeAudit records one event, honouring the configured failure mode.
func writeAudit(audit *AuditLogger, record AuditRecord, onFailure AuditFailureMode, warn func(string)) error {
	if audit == nil {
		return nil
	}
	err := audit.Log(record)
	if err == nil {
		return nil
	}
	if onFailure == AuditFailureWarn {
		if warn != nil {
			warn(fmt.Sprintf("bb: audit record for %s could not be written: %v", record.Tool, err))
		}
		return nil
	}
	return fmt.Errorf("refusing to serve a tool call that cannot be audited: %w", err)
}

// rawArguments normalises the arguments field to JSON bytes.
//
// On the server side the SDK hands middleware a json.RawMessage, but the field
// is typed as any and a client-constructed request can hold a map, so both are
// handled rather than assuming the shape.
func rawArguments(arguments any) json.RawMessage {
	switch typed := arguments.(type) {
	case nil:
		return nil
	case json.RawMessage:
		return typed
	case []byte:
		return typed
	default:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return nil
		}
		return encoded
	}
}

// traceParent reads the W3C trace parent out of a request's _meta, if present.
func traceParent(meta mcp.Meta) string {
	if meta == nil {
		return ""
	}
	value, _ := meta[traceParentKey].(string)
	return value
}

// toolError builds the error result an out-of-scope call returns.
func toolError(err error) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
	}
}

// isErrorResult reports whether a dispatched call ended as a tool error.
func isErrorResult(result mcp.Result) bool {
	callResult, ok := result.(*mcp.CallToolResult)
	return ok && callResult.IsError
}

// resultErrorText extracts an error result's message for the audit record.
func resultErrorText(result mcp.Result) string {
	callResult, ok := result.(*mcp.CallToolResult)
	if !ok {
		return ""
	}
	for _, item := range callResult.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}
