package diagnostics

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestParseLevelAndFormat(t *testing.T) {
	t.Parallel()

	if _, err := ParseLevel("debug"); err != nil {
		t.Fatalf("expected debug level to parse: %v", err)
	}
	if _, err := ParseLevel("verbose"); err == nil {
		t.Fatal("expected invalid level error")
	}

	if _, err := ParseFormat("jsonl"); err != nil {
		t.Fatalf("expected jsonl format to parse: %v", err)
	}
	if _, err := ParseFormat("yaml"); err == nil {
		t.Fatal("expected invalid format error")
	}
}

func TestRedactFields(t *testing.T) {
	t.Parallel()

	fields := map[string]any{
		"authorization": "Bearer test-token",
		"endpoint":      "https://user:pass@example.test/rest?access_token=abc&ok=true",
		"meta": map[string]any{
			"password": "secret",
			"retry":    2,
		},
	}

	sanitized := RedactFields(fields)
	if sanitized["authorization"] != "[REDACTED]" {
		t.Fatalf("expected authorization redaction, got: %v", sanitized["authorization"])
	}

	endpoint, _ := sanitized["endpoint"].(string)
	if strings.Contains(endpoint, "pass") || strings.Contains(endpoint, "access_token=abc") {
		t.Fatalf("expected endpoint redaction, got: %s", endpoint)
	}

	meta, _ := sanitized["meta"].(map[string]any)
	if meta["password"] != "[REDACTED]" {
		t.Fatalf("expected nested password redaction, got: %v", meta["password"])
	}
}

func TestLoggerJSONLAndLevelFiltering(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}
	logger := NewLogger(Config{Level: LevelWarn, Format: FormatJSONL}, buffer)

	logger.Info("ignored", map[string]any{"status": 200})
	logger.Warn("request retry", map[string]any{"retry_count": 1, "token": "abc", "message": "override"})

	lines := strings.Split(strings.TrimSpace(buffer.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected one log line, got %d: %q", len(lines), buffer.String())
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &payload); err != nil {
		t.Fatalf("expected valid json line: %v", err)
	}

	if payload["level"] != "warn" {
		t.Fatalf("expected warn level, got: %v", payload["level"])
	}
	if payload["message"] != "request retry" {
		t.Fatalf("expected reserved message key not to be overwritten, got: %v", payload["message"])
	}
	if payload["token"] != "[REDACTED]" {
		t.Fatalf("expected token redaction, got: %v", payload["token"])
	}
}

func TestOutputWriterSetterGetter(t *testing.T) {
	t.Parallel()

	SetOutputWriter(nil)
	if writer := OutputWriter(); writer != io.Discard {
		t.Fatalf("expected discard writer when setting nil, got %T", writer)
	}

	buffer := &bytes.Buffer{}
	SetOutputWriter(buffer)
	if writer := OutputWriter(); writer != buffer {
		t.Fatalf("expected configured writer, got %T", writer)
	}
}

func TestEnabledWriter(t *testing.T) {
	t.Parallel()

	buffer := &bytes.Buffer{}

	if writer := EnabledWriter(true, buffer); writer != buffer {
		t.Fatalf("expected configured writer when enabled, got %T", writer)
	}

	if writer := EnabledWriter(false, buffer); writer != io.Discard {
		t.Fatalf("expected discard writer when disabled, got %T", writer)
	}
}

// A redacted URL keeps its shape: the marker stays readable, and the query
// keeps its order and its other values as written. Re-encoding it through
// url.Values printed %5BREDACTED%5D, which a search for the marker misses, and
// sorted the parameters.
func TestRedactFieldsLeavesARedactedURLReadable(t *testing.T) {
	t.Parallel()

	sanitized := RedactFields(map[string]any{
		"endpoint": "https://bob:hunter2@example.test/rest?zeta=1&access_token=abc&alpha=a%20b",
	})

	want := "https://bob:[REDACTED]@example.test/rest?zeta=1&access_token=[REDACTED]&alpha=a%20b"
	if got := sanitized["endpoint"]; got != want {
		t.Fatalf("got  %v\nwant %s", got, want)
	}
}
