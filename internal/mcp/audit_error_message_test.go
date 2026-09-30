package mcp

import (
	"encoding/json"
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

// TestAnAuditRecordsArgumentsAndResourceAreRedacted is #731.
//
// The arguments were redacted by key name, and a URL only when it was the
// whole value, while the error message beside them went through the free-text
// redactor: the same credential was withheld in one field and written out in
// the next. The resource went through nothing. Each payload is one an agent
// can put in a free-text argument such as a path or a ref, and Bitbucket
// echoes back in its error.
func TestAnAuditRecordsArgumentsAndResourceAreRedacted(t *testing.T) {
	t.Parallel()

	const secret = "SUPERSECRET123"

	for name, payload := range map[string]string{
		"a URL with userinfo inside text":       "pre https://bob:" + secret + "@h.example/x post",
		"a query token inside text":             "pre https://h.example/x?token=" + secret + " post",
		"a query token with no URL":             "q?token=" + secret,
		"an Authorization header":               "Authorization: Bearer " + secret,
		"a ref that is an assignment":           "token=" + secret,
		"a path Bitbucket cleaned to one slash": "https:/bob:" + secret + "@h.example/x",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "audit.jsonl")
			logger, err := NewAuditLogger(path)
			if err != nil {
				t.Fatalf("open audit log: %v", err)
			}
			t.Cleanup(func() { _ = logger.Close() })

			arguments, err := json.Marshal(map[string]any{
				"project": "P", "repo": "repo", "path": "README.md", "at": payload,
				"nested": map[string]any{"refs": []any{payload}},
			})
			if err != nil {
				t.Fatalf("encode arguments: %v", err)
			}
			if err := logger.Log(AuditRecord{
				Event:     auditEventResourceRead,
				Resource:  "bitbucket://projects/P/repos/repo/files/README.md?at=" + payload,
				Status:    auditStatusError,
				Arguments: auditArguments(arguments),
			}); err != nil {
				t.Fatalf("log: %v", err)
			}

			written, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read audit log: %v", err)
			}
			if strings.Contains(string(written), secret) {
				t.Fatalf("the audit record kept the secret: %s", written)
			}
			if strings.Contains(string(written), "%5BREDACTED%5D") {
				t.Errorf("the marker was percent-encoded, which a reader searching for it misses: %s", written)
			}
			if !strings.Contains(string(written), `"path":"README.md"`) {
				t.Errorf("redaction changed an argument with nothing in it: %s", written)
			}
		})
	}
}
