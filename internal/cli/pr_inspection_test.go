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

func TestPRCommitsAndMergeBaseJSON(t *testing.T) {
	server := newPRInspectionServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	commitsOut, err := executeTestCLI(t, "--json", "pr", "commits", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, commitsOut)
	}
	if !strings.Contains(commitsOut, "\"commits\"") || !strings.Contains(commitsOut, "abc1234") {
		t.Fatalf("unexpected commits JSON: %s", commitsOut)
	}

	mergeBaseOut, err := executeTestCLI(t, "--json", "pr", "merge-base", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, mergeBaseOut)
	}
	if !strings.Contains(mergeBaseOut, "\"merge_base\"") || !strings.Contains(mergeBaseOut, "base123") {
		t.Fatalf("unexpected merge-base JSON: %s", mergeBaseOut)
	}
}

// TestPRFilesRendersChangeTypesAndRenames covers the default-type fallback and
// the rename source-path branch of the files command's text output.
func TestPRFilesRendersChangeTypesAndRenames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/latest/projects/TEST/repos/demo/pull-requests/7/changes" {
			_, _ = w.Write([]byte(`{"values":[{"path":{"toString":"renamed.go"},"srcPath":{"toString":"old.go"}},{"path":{"toString":"kept.go"},"type":"ADD"}],"isLastPage":true,"nextPageStart":0}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "pr", "files", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, output)
	}
	if !strings.Contains(output, "MODIFY\trenamed.go (from old.go)") {
		t.Fatalf("expected rename rendering with default type, got: %s", output)
	}
	if !strings.Contains(output, "ADD\tkept.go") {
		t.Fatalf("expected explicit change type, got: %s", output)
	}
}

func TestPRInspectionEmptyResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"values":[],"isLastPage":true,"nextPageStart":0}`))
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	commitsOut, err := executeTestCLI(t, "pr", "commits", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, commitsOut)
	}
	if !strings.Contains(commitsOut, "No commits found") {
		t.Fatalf("expected empty commits message, got: %s", commitsOut)
	}

	filesOut, err := executeTestCLI(t, "pr", "files", "7")
	if err != nil {
		t.Fatalf("unexpected error: %v (output: %s)", err, filesOut)
	}
	if !strings.Contains(filesOut, "No changes found") {
		t.Fatalf("expected empty changes message, got: %s", filesOut)
	}
}

func TestPRInspectionServiceErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"errors":[{"message":"boom"}]}`, http.StatusInternalServerError)
	}))
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	for _, args := range [][]string{
		{"pr", "commits", "7"},
		{"pr", "files", "7"},
		{"pr", "merge-base", "7"},
	} {
		if _, err := executeTestCLI(t, args...); err == nil {
			t.Fatalf("expected transport error for %v", args)
		}
	}
}

func TestPRInspectionRepositoryResolutionError(t *testing.T) {
	server := newPRInspectionServer(t)
	defer server.Close()
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	for _, args := range [][]string{
		{"pr", "--repo", "missing-slash", "commits", "7"},
		{"pr", "--repo", "missing-slash", "files", "7"},
		{"pr", "--repo", "missing-slash", "merge-base", "7"},
	} {
		if _, err := executeTestCLI(t, args...); err == nil {
			t.Fatalf("expected repository resolution error for %v", args)
		}
	}
}
