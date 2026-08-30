package mcp

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/diagnostics"
)

// Audit event and status vocabulary. These strings are the wire contract a SIEM
// parser keys on, so they are constants rather than inline literals.
const (
	auditEventToolInvocation = "mcp_tool_invocation"

	auditStatusSuccess = "success"
	auditStatusDenied  = "denied"
	auditStatusError   = "error"
)

// AuditRecord is one line of the JSONL audit stream.
//
// The fields answer the questions a reviewer asks of an agent's activity: who
// was acting, what they asked for, what they were aimed at, and whether it was
// allowed. Arguments are included because "read a file" and "read
// .env.production" are different events, and they are redacted on the way in.
type AuditRecord struct {
	Timestamp    string         `json:"timestamp"`
	Event        string         `json:"event"`
	Tool         string         `json:"tool"`
	Project      string         `json:"project,omitempty"`
	Repo         string         `json:"repo,omitempty"`
	Status       string         `json:"status"`
	DurationMS   int64          `json:"duration_ms"`
	UserIdentity string         `json:"user_identity,omitempty"`
	Host         string         `json:"host,omitempty"`
	Scope        string         `json:"scope,omitempty"`
	Arguments    map[string]any `json:"arguments,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	// TraceID carries the W3C trace parent when the client sends one. The
	// 2026-07-28 revision documents OpenTelemetry trace context in _meta, so an
	// audit line can be correlated with the agent's own trace at no cost beyond
	// copying it.
	TraceID string `json:"trace_id,omitempty"`
}

// AuditLogger appends audit records to a sink.
//
// Writes are serialised and synchronous. An asynchronous fire-and-forget write
// would keep the tool-call path marginally faster and lose exactly the records
// that matter — a denial recorded nowhere is a denial that did not happen, as
// far as a reviewer is concerned.
type AuditLogger struct {
	mu     sync.Mutex
	writer io.Writer
	closer io.Closer

	// Identity and Host are constant for the life of the server, so they are
	// captured once rather than threaded through every call.
	Identity string
	Host     string
	Scope    string
}

// auditSinkStderr and auditSinkStdout are the pseudo-paths --audit-file accepts
// in place of a real file.
//
// stderr exists for a containerised or wrapper-managed deployment, where a
// cluster log collector reads the process's own streams — the same escape hatch
// Vault's file audit device offers with file_path: stdout.
//
// stdout is deliberately absent, and rejected: it is the MCP protocol channel,
// so writing audit records to it would corrupt every response.
const (
	auditSinkStderr = "stderr"
	auditSinkStdout = "stdout"
)

// NewAuditLogger opens the audit sink named by path.
//
// The file is opened at startup rather than on first write so that an
// unwritable path fails while the operator is still watching, instead of at the
// first tool call in somebody's IDE.
func NewAuditLogger(path string) (*AuditLogger, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, nil
	}

	switch strings.ToLower(trimmed) {
	case auditSinkStdout, "-":
		return nil, fmt.Errorf("--audit-file cannot be stdout: the MCP server speaks JSON-RPC on stdout, and audit records would corrupt it. Use stderr or a file path")
	case auditSinkStderr:
		return &AuditLogger{writer: os.Stderr}, nil
	}

	if directory := filepath.Dir(trimmed); directory != "" && directory != "." {
		// 0750, not 0755: the audit trail records which repositories an agent
		// touched and what it did to them. ADR-062 makes it a compliance
		// artefact, and a compliance artefact every local account can read is
		// a weaker one.
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, fmt.Errorf("cannot create audit log directory %s: %w", directory, err)
		}
	}

	// Append-only. Rotation is the collector's job: a long-lived server has no
	// good moment to rotate, and every platform this runs on has a tool that
	// does it better than a bespoke size check would.
	file, err := os.OpenFile(trimmed, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("cannot open audit log %s: %w", trimmed, err)
	}
	return &AuditLogger{writer: file, closer: file}, nil
}

// Close releases the sink, if it owns one.
func (a *AuditLogger) Close() error {
	if a == nil || a.closer == nil {
		return nil
	}
	return a.closer.Close()
}

// Log writes one record.
//
// The returned error is fatal to the tool call by design: an audit log that
// silently stops recording is worse than none, because the absence of a record
// then means nothing. bb ai mcp serve --audit-failure=warn relaxes this for an
// operator who would rather lose records than lose the server.
func (a *AuditLogger) Log(record AuditRecord) error {
	if a == nil {
		return nil
	}

	record.Event = auditEventToolInvocation
	record.UserIdentity = a.Identity
	record.Host = a.Host
	record.Scope = a.Scope

	encoded, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode audit record: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.writer.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("write audit record: %w", err)
	}
	return nil
}

// auditArguments decodes a call's arguments for the audit record and strips
// anything sensitive.
//
// Redaction reuses internal/diagnostics rather than reimplementing a token
// list. That package already redacts by key name and rewrites credentials out
// of URL strings, and it is tested; a second copy here would be one more thing
// to keep in step.
func auditArguments(arguments json.RawMessage) map[string]any {
	if len(arguments) == 0 {
		return nil
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		// Unparseable arguments are still worth recording as having been sent.
		return map[string]any{"unparsed": true}
	}
	if len(decoded) == 0 {
		return nil
	}
	return diagnostics.RedactFields(decoded)
}

// auditTimestamp formats an instant the way a SIEM expects it.
func auditTimestamp(at time.Time) string {
	return at.UTC().Format(time.RFC3339)
}
