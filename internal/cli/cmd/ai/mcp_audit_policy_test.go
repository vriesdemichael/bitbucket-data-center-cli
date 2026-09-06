package ai

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
)

// writeSystemPolicy points the config loader at a system config file holding
// the given policy block.
func writeSystemPolicy(t *testing.T, body string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write system config: %v", err)
	}
	t.Setenv("BB_SYSTEM_CONFIG_PATH", path)
}

// TestResolveAuditFileWithoutPolicy verifies the flag stands alone when no
// administrator has mandated anything, which is the default everywhere.
func TestResolveAuditFileWithoutPolicy(t *testing.T) {
	writeSystemPolicy(t, "default_host: https://bitbucket.example.com\n")

	resolved, err := resolveAuditFile("/tmp/mine.jsonl")
	if err != nil {
		t.Fatalf("resolveAuditFile: %v", err)
	}
	if resolved != "/tmp/mine.jsonl" {
		t.Errorf("resolved = %q, want the flag value", resolved)
	}

	// And auditing stays off when neither names a path.
	resolved, err = resolveAuditFile("")
	if err != nil {
		t.Fatalf("resolveAuditFile: %v", err)
	}
	if resolved != "" {
		t.Errorf("resolved = %q, want auditing to stay off by default", resolved)
	}
}

// TestResolveAuditFileAppliesPolicyWhenFlagOmitted is the fleet case: an
// administrator mandates the destination and the developer does not have to
// know. A policy that only took effect when the flag was also passed would be
// documentation rather than enforcement.
func TestResolveAuditFileAppliesPolicyWhenFlagOmitted(t *testing.T) {
	writeSystemPolicy(t, "policy:\n  mcp_audit_file: /var/log/bb/mcp-audit.jsonl\n")

	resolved, err := resolveAuditFile("")
	if err != nil {
		t.Fatalf("resolveAuditFile: %v", err)
	}
	if resolved != "/var/log/bb/mcp-audit.jsonl" {
		t.Errorf("resolved = %q, want the mandated path", resolved)
	}
}

// TestResolveAuditFileRejectsConflictingFlag covers the other half: an operator
// cannot redirect the trail somewhere the fleet does not collect.
func TestResolveAuditFileRejectsConflictingFlag(t *testing.T) {
	writeSystemPolicy(t, "policy:\n  mcp_audit_file: /var/log/bb/mcp-audit.jsonl\n")

	if _, err := resolveAuditFile("/tmp/somewhere-else.jsonl"); err == nil {
		t.Fatal("redirecting a policy-mandated audit destination was allowed")
	} else if !strings.Contains(err.Error(), "administrative policy") {
		t.Errorf("error = %v, want it to name administrative policy", err)
	}
}

// TestResolveAuditFileAcceptsMatchingFlag verifies an operator who passes the
// mandated path is not punished for being explicit.
func TestResolveAuditFileAcceptsMatchingFlag(t *testing.T) {
	writeSystemPolicy(t, "policy:\n  mcp_audit_file: /var/log/bb/mcp-audit.jsonl\n")

	resolved, err := resolveAuditFile("/var/log/bb/mcp-audit.jsonl")
	if err != nil {
		t.Fatalf("resolveAuditFile: %v", err)
	}
	if resolved != "/var/log/bb/mcp-audit.jsonl" {
		t.Errorf("resolved = %q", resolved)
	}
}

// TestResolveAuditFileHonoursTopLevelPolicyKey covers the shortcut spelling
// every other policy key supports.
func TestResolveAuditFileHonoursTopLevelPolicyKey(t *testing.T) {
	writeSystemPolicy(t, "mcp_audit_file: /var/log/bb/top-level.jsonl\n")

	resolved, err := resolveAuditFile("")
	if err != nil {
		t.Fatalf("resolveAuditFile: %v", err)
	}
	if resolved != "/var/log/bb/top-level.jsonl" {
		t.Errorf("resolved = %q, want the top-level policy key to be honoured", resolved)
	}
}

// TestParseAuditFailure pins the flag vocabulary, including the fail-closed
// default an omitted flag must produce.
func TestParseAuditFailure(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input   string
		want    bbmcp.AuditFailureMode
		wantErr bool
	}{
		{input: "", want: bbmcp.AuditFailureDeny},
		{input: "deny", want: bbmcp.AuditFailureDeny},
		{input: "DENY", want: bbmcp.AuditFailureDeny},
		{input: " warn ", want: bbmcp.AuditFailureWarn},
		{input: "ignore", wantErr: true},
	}
	for _, tc := range cases {
		got, err := parseAuditFailure(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseAuditFailure(%q) succeeded, want an error", tc.input)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAuditFailure(%q) returned %v", tc.input, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseAuditFailure(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}
