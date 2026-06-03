package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newPRInspectionServer(t *testing.T) *httptest.Server {
	t.Helper()
	const prefix = "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case prefix + "/commits":
			_, _ = w.Write([]byte(`{"values":[{"id":"abc1234567890","displayId":"abc1234","message":"Add feature\n\nbody"}],"isLastPage":true,"nextPageStart":0}`))
		case prefix + "/changes":
			_, _ = w.Write([]byte(`{"values":[{"path":{"toString":"src/app.go"},"type":"MODIFY"}],"isLastPage":true,"nextPageStart":0}`))
		case prefix + "/merge-base":
			_, _ = w.Write([]byte(`{"id":"base123456789","displayId":"base123","message":"Common ancestor"}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestPRCommitsCommand(t *testing.T) {
	server := newPRInspectionServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "pr", "commits", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, output)
	}
	if !strings.Contains(output, "abc1234") || !strings.Contains(output, "Add feature") {
		t.Fatalf("unexpected commits output: %s", output)
	}
	if strings.Contains(output, "body") {
		t.Fatalf("expected only the first message line, got: %s", output)
	}
}

func TestPRFilesCommandJSON(t *testing.T) {
	server := newPRInspectionServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "files", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, output)
	}
	if !strings.Contains(output, "src/app.go") || !strings.Contains(output, "\"changes\"") {
		t.Fatalf("unexpected files JSON output: %s", output)
	}
}

func TestPRFilesChangesAlias(t *testing.T) {
	server := newPRInspectionServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "pr", "changes", "7")
	if err != nil {
		t.Fatalf("unexpected error via alias: %v (output: %s)", err, output)
	}
	if !strings.Contains(output, "MODIFY") || !strings.Contains(output, "src/app.go") {
		t.Fatalf("unexpected changes alias output: %s", output)
	}
}

func TestPRMergeBaseCommand(t *testing.T) {
	server := newPRInspectionServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "pr", "merge-base", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, output)
	}
	if !strings.Contains(output, "base123") || !strings.Contains(output, "Common ancestor") {
		t.Fatalf("unexpected merge-base output: %s", output)
	}
}

func TestPRInspectionArgValidation(t *testing.T) {
	for _, args := range [][]string{
		{"pr", "commits"},
		{"pr", "files"},
		{"pr", "merge-base"},
	} {
		if _, err := executeTestCLI(t, args...); err == nil {
			t.Fatalf("expected arg validation error for %v", args)
		}
	}
}
