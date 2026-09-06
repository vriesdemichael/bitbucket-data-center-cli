package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestEveryToolHasAScopeRule is the guard that keeps the boundary honest.
//
// A scope enforcer that skips its check when it cannot find a project argument
// permits everything it does not understand while reading like enforcement in a
// code review. Requiring a rule per tool, in both directions, means a new tool
// stops the build rather than quietly escaping the boundary.
func TestEveryToolHasAScopeRule(t *testing.T) {
	t.Parallel()

	for _, spec := range AllSpecs() {
		if _, ok := scopeRules[spec.Tool.Name]; !ok {
			t.Errorf("tool %q has no scope rule; add one to scopeRules", spec.Tool.Name)
		}
	}
	for name := range scopeRules {
		found := false
		for _, spec := range AllSpecs() {
			if spec.Tool.Name == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("scopeRules names %q, which is not in AllSpecs", name)
		}
	}
}

func TestParseScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		project     string
		repo        string
		wantProject string
		wantRepo    string
		wantErr     string
	}{
		{name: "unscoped"},
		{name: "project only", project: "PROJ", wantProject: "PROJ"},
		{name: "qualified repo", repo: "PROJ/payments", wantProject: "PROJ", wantRepo: "payments"},
		{name: "bare repo with project", project: "PROJ", repo: "payments", wantProject: "PROJ", wantRepo: "payments"},
		{name: "qualified repo agreeing with project", project: "PROJ", repo: "PROJ/payments", wantProject: "PROJ", wantRepo: "payments"},
		{name: "bare repo without project", repo: "payments", wantErr: "needs a project"},
		{name: "conflicting project", project: "OTHER", repo: "PROJ/payments", wantErr: "conflicts with"},
		{name: "empty half", repo: "PROJ/", wantErr: "not a PROJECT/slug pair"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scope, err := ParseScope(tc.project, tc.repo)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParseScope(%q, %q) error = %v, want it to mention %q", tc.project, tc.repo, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseScope(%q, %q) returned %v", tc.project, tc.repo, err)
			}
			if scope.ProjectKey != tc.wantProject || scope.RepoSlug != tc.wantRepo {
				t.Errorf("ParseScope(%q, %q) = %+v, want project %q repo %q", tc.project, tc.repo, scope, tc.wantProject, tc.wantRepo)
			}
		})
	}
}

// TestScopeRefusesCallsOutsideTheBoundary is the assertion the whole feature
// exists for.
func TestScopeRefusesCallsOutsideTheBoundary(t *testing.T) {
	t.Parallel()

	scope := Scope{ProjectKey: "PROJ", RepoSlug: "payments"}
	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t), Yolo: true, Scope: scope,
	})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_branches",
		Arguments: map[string]any{"project": "OTHER", "repo": "payments"},
	})
	if err != nil {
		t.Fatalf("tools/call returned a protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a call aimed at another project succeeded; the scope does not bound anything")
	}
	if text := contentText(result); !strings.Contains(text, "outside the scope") {
		t.Errorf("error result = %q, want it to say the target is outside the scope", text)
	}
}

// TestScopeRefusesCallsToAnotherRepositoryInTheSameProject covers the half of
// the boundary a project-only check would miss.
func TestScopeRefusesCallsToAnotherRepositoryInTheSameProject(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t), Yolo: true,
		Scope: Scope{ProjectKey: "PROJ", RepoSlug: "payments"},
	})

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_branches",
		Arguments: map[string]any{"project": "PROJ", "repo": "billing"},
	})
	if err != nil {
		t.Fatalf("tools/call returned a protocol error: %v", err)
	}
	if !result.IsError {
		t.Fatal("a call aimed at a sibling repository succeeded")
	}
}

// TestScopeInjectsOmittedArguments covers the case the contributor sketch on
// issue #423 got wrong: a call that simply omits project and repo.
//
// A guard that only compares supplied values permits every unscoped call, which
// is the whole dashboard mode of list_pull_requests. Injecting the scope turns
// the unbounded call into a bounded one.
func TestScopeInjectsOmittedArguments(t *testing.T) {
	t.Parallel()

	var seen map[string]any
	scope := Scope{ProjectKey: "PROJ", RepoSlug: "payments"}

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t), Yolo: true, Scope: scope,
		Audit: recordingAudit(&seen),
	})

	// The call omits both arguments entirely.
	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_pull_requests",
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatalf("tools/call returned a protocol error: %v", err)
	}

	if seen["project"] != "PROJ" || seen["repo"] != "payments" {
		t.Errorf("dispatched arguments = %v, want the scope injected so dashboard mode cannot widen past it", seen)
	}
}

// TestScopeWithholdsUnboundableTools covers the tools that have no argument to
// constrain. Withholding is the honest outcome; a commit SHA is global in
// Bitbucket's API and cannot be bounded to a project.
func TestScopeWithholdsUnboundableTools(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t), Yolo: true,
		Scope: Scope{ProjectKey: "PROJ"},
	})

	for _, name := range listToolNames(t, session) {
		if name == "get_build_status" || name == "set_build_status" {
			t.Errorf("tool %q cannot be scoped but is still listed", name)
		}
	}

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "get_build_status",
		Arguments: map[string]any{"commit_id": "abc123"},
	})
	if err == nil && (result == nil || !result.IsError) {
		t.Error("get_build_status succeeded under a scope it cannot honour")
	}
}

// TestScopeWithholdsRepositorySearchUnderARepositoryScope covers the tool whose
// project argument is a filter rather than a target. Pinning the filter still
// lists sibling repositories, so a filter is not a boundary.
func TestScopeWithholdsRepositorySearchUnderARepositoryScope(t *testing.T) {
	t.Parallel()

	repoScoped := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t), Yolo: true,
		Scope: Scope{ProjectKey: "PROJ", RepoSlug: "payments"},
	})
	for _, name := range listToolNames(t, repoScoped) {
		if name == "search_repositories" {
			t.Error("search_repositories is listed under a single-repository scope")
		}
	}

	// Under a project scope it is available, with the filter pinned.
	projectScoped := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t), Yolo: true,
		Scope: Scope{ProjectKey: "PROJ"},
	})
	found := false
	for _, name := range listToolNames(t, projectScoped) {
		if name == "search_repositories" {
			found = true
		}
	}
	if !found {
		t.Error("search_repositories should remain available under a project scope")
	}
}

// TestUnscopedServerIsUnchanged pins the default. Scoping is opt-in, so a
// server without it must expose and dispatch exactly what it did before.
func TestUnscopedServerIsUnchanged(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: Clients{}, Yolo: true,
	})
	if got, want := len(listToolNames(t, session)), len(AllSpecs()); got != want {
		t.Errorf("unscoped tools/list returned %d tools, want %d", got, want)
	}
}

// TestAuditRecordsSuccessAndDenial covers the audit trail end to end, including
// the denial case — the event that exists nowhere else, because a refused call
// never reaches Bitbucket and so leaves no trace in its audit log.
func TestAuditRecordsSuccessAndDenial(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	audit, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	audit.Identity = "alice"
	audit.Host = "https://bitbucket.example.com"
	audit.Scope = "PROJ/payments"

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t), Yolo: true,
		Scope: Scope{ProjectKey: "PROJ", RepoSlug: "payments"},
		Audit: audit,
	})

	ctx := context.Background()
	// Denied: another project.
	_, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_branches",
		Arguments: map[string]any{"project": "OTHER", "repo": "payments"},
	})
	// Errored: in scope, but the backing server returns 500.
	_, _ = session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_branches",
		Arguments: map[string]any{"project": "PROJ", "repo": "payments"},
	})
	_ = audit.Close()

	records := readAuditRecords(t, path)
	if len(records) != 2 {
		t.Fatalf("expected 2 audit records, got %d", len(records))
	}

	denied := records[0]
	if denied.Status != auditStatusDenied {
		t.Errorf("first record status = %q, want %q", denied.Status, auditStatusDenied)
	}
	if denied.Tool != "list_branches" {
		t.Errorf("first record tool = %q", denied.Tool)
	}
	if denied.UserIdentity != "alice" || denied.Host != "https://bitbucket.example.com" {
		t.Errorf("first record identity/host = %q/%q", denied.UserIdentity, denied.Host)
	}
	if denied.Scope != "PROJ/payments" {
		t.Errorf("first record scope = %q", denied.Scope)
	}
	if !strings.Contains(denied.ErrorMessage, "outside the scope") {
		t.Errorf("first record error = %q, want the scope violation", denied.ErrorMessage)
	}
	if denied.Event != auditEventToolInvocation {
		t.Errorf("first record event = %q", denied.Event)
	}

	if records[1].Status != auditStatusError {
		t.Errorf("second record status = %q, want %q", records[1].Status, auditStatusError)
	}
	if records[1].Project != "PROJ" || records[1].Repo != "payments" {
		t.Errorf("second record target = %q/%q, want PROJ/payments", records[1].Project, records[1].Repo)
	}
}

// TestAuditRedactsSensitiveArguments verifies the audit trail cannot become a
// place secrets accumulate. Arguments are recorded because "read a file" and
// "read the deployment secrets" are different events, which only holds if the
// values themselves are safe to keep.
func TestAuditRedactsSensitiveArguments(t *testing.T) {
	t.Parallel()

	arguments, err := json.Marshal(map[string]any{
		"project":  "PROJ",
		"token":    "super-secret-pat",
		"password": "hunter2",
		"url":      "https://user:pw@bitbucket.example.com/x?api_key=abc",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	redacted := auditArguments(arguments)
	if redacted["project"] != "PROJ" {
		t.Errorf("project was altered: %v", redacted["project"])
	}
	for _, key := range []string{"token", "password"} {
		if value, _ := redacted[key].(string); !strings.Contains(value, "REDACTED") {
			t.Errorf("%s = %v, want it redacted", key, redacted[key])
		}
	}
	if value, _ := redacted["url"].(string); strings.Contains(value, "hunter2") || strings.Contains(value, "pw@") {
		t.Errorf("url = %q, want the credentials stripped", value)
	}
}

// TestAuditFailureDeniesTheCall covers the fail-closed default. An audit log
// that silently stops recording is worse than none, because the absence of a
// record then carries no information.
func TestAuditFailureDeniesTheCall(t *testing.T) {
	t.Parallel()

	failing := &AuditLogger{writer: failingWriter{}}

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t), Yolo: true,
		Audit: failing, AuditFailure: AuditFailureDeny,
	})

	_, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_branches",
		Arguments: map[string]any{"project": "PROJ", "repo": "payments"},
	})
	if err == nil {
		t.Fatal("a call whose audit record could not be written succeeded")
	}
}

// TestAuditFailureWarnLetsTheCallProceed covers the documented escape hatch.
func TestAuditFailureWarnLetsTheCallProceed(t *testing.T) {
	t.Parallel()

	var warnings []string
	failing := &AuditLogger{writer: failingWriter{}}

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t), Yolo: true,
		Audit: failing, AuditFailure: AuditFailureWarn,
		Warn: func(message string) { warnings = append(warnings, message) },
	})

	if _, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "list_branches",
		Arguments: map[string]any{"project": "PROJ", "repo": "payments"},
	}); err != nil {
		t.Fatalf("tools/call returned a protocol error: %v", err)
	}
	if len(warnings) == 0 {
		t.Error("a failed audit write in warn mode produced no warning")
	}
}

// TestAuditRejectsStdout covers the sink that would corrupt the protocol.
func TestAuditRejectsStdout(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"stdout", "STDOUT", "-"} {
		logger, err := NewAuditLogger(path)
		if err == nil {
			t.Errorf("NewAuditLogger(%q) succeeded; stdout is the protocol channel", path)
			_ = logger.Close()
			continue
		}
		if !strings.Contains(err.Error(), "stdout") {
			t.Errorf("NewAuditLogger(%q) error = %v, want it to explain the stdout conflict", path, err)
		}
	}
}

// TestAuditCreatesMissingDirectories covers the fleet case, where the log path
// is handed down by policy and its directory may not exist yet.
func TestAuditCreatesMissingDirectories(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "dir", "audit.jsonl")
	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("NewAuditLogger: %v", err)
	}
	defer func() { _ = logger.Close() }()

	if err := logger.Log(AuditRecord{Timestamp: "2026-08-29T00:00:00Z", Tool: "list_tags", Status: auditStatusSuccess}); err != nil {
		t.Fatalf("Log: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("audit file was not created: %v", err)
	}
}

// TestNilAuditLoggerIsInert pins the default: auditing is off unless asked for,
// so a nil logger must be safe to call rather than something every call site
// has to guard.
func TestNilAuditLoggerIsInert(t *testing.T) {
	t.Parallel()

	var logger *AuditLogger
	if err := logger.Log(AuditRecord{Tool: "list_tags"}); err != nil {
		t.Errorf("Log on a nil logger returned %v", err)
	}
	if err := logger.Close(); err != nil {
		t.Errorf("Close on a nil logger returned %v", err)
	}
}

// failingWriter is an io.Writer whose every write fails.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) {
	return 0, os.ErrPermission
}

// recordingAudit returns a logger that captures the arguments of the last call
// it records, so a test can assert what was actually dispatched.
func recordingAudit(target *map[string]any) *AuditLogger {
	return &AuditLogger{writer: argumentCapture{target: target}}
}

type argumentCapture struct {
	target *map[string]any
}

func (c argumentCapture) Write(p []byte) (int, error) {
	var record AuditRecord
	if err := json.Unmarshal(p, &record); err == nil {
		*c.target = record.Arguments
	}
	return len(p), nil
}

// readAuditRecords parses a JSONL audit file.
func readAuditRecords(t *testing.T, path string) []AuditRecord {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open audit log: %v", err)
	}
	defer func() { _ = file.Close() }()

	var records []AuditRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record AuditRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("audit line is not valid JSON: %v\nline: %s", err, line)
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	return records
}

// contentText flattens a result's text content for failure messages.
func contentText(result *mcp.CallToolResult) string {
	var parts []string
	for _, item := range result.Content {
		if text, ok := item.(*mcp.TextContent); ok {
			parts = append(parts, text.Text)
		}
	}
	return strings.Join(parts, "\n")
}
