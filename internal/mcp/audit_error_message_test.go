package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The error message is the one free-text field in an audit record, and the one
// that carried an upstream body with a credential in it straight to the file.
func TestAnAuditRecordsErrorMessageIsRedacted(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "audit.jsonl")
	logger, err := NewAuditLogger(path)
	if err != nil {
		t.Fatalf("open audit log: %v", err)
	}
	t.Cleanup(func() { _ = logger.Close() })

	if err := logger.Log(AuditRecord{
		Tool:         "get_file_content",
		Status:       auditStatusError,
		ErrorMessage: "get_file_content failed: clone https://svc:hunter2@git.example/scm/p/r.git refused; authorization: Bearer abc123def",
	}); err != nil {
		t.Fatalf("log: %v", err)
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read audit log: %v", err)
	}
	for _, secret := range []string{"hunter2", "abc123def"} {
		if strings.Contains(string(written), secret) {
			t.Errorf("the audit record kept %q: %s", secret, written)
		}
	}
	if !strings.Contains(string(written), "get_file_content failed") {
		t.Errorf("redaction took the message with it: %s", written)
	}
}
