package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

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

// mock-inventory: unreachable-state — a pull request with no commits and no changes, which Bitbucket will not create; the subject is that each listing says it found nothing rather than printing nothing.
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

// mock-inventory: transport-fault — the failures are injected; the subject is that each command reports rather than rendering an empty result.
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
	// A repository selector with no slash is refused before a request is built,
	// so the listener fails the test if one arrives.
	server := httptest.NewServer(testsupport.UnreachedHandler(t))
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

// Five suites went live: pr commits, pr files, pr changes, pr merge-base and
// their JSON forms are all in TestLivePullRequestInspection, against a pull
// request the harness created. Each one here looked for a commit id and a
// message this file had written into a page it also wrote.
//
// One of them carried something the others did not: that a multi-line commit
// message shows only its subject. That is firstMessageLine, a pure function,
// and internal/cli/cmd/pr/format_test.go calls it with a two-line string --
// no server, and no pull request either.
