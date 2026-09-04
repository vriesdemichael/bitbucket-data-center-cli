package repocmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// mock-inventory: transport-fault — the failures are injected; the subject is that each subcommand reports rather than claiming success.
func TestRepoSyncCLICommandsErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	if _, err := executeTestCLI(t, "repo", "sync", "status"); err == nil {
		t.Fatal("expected error getting sync status on 500 response")
	}
	if _, err := executeTestCLI(t, "repo", "sync", "enable"); err == nil {
		t.Fatal("expected error enabling sync on 500 response")
	}
	if _, err := executeTestCLI(t, "repo", "sync", "disable"); err == nil {
		t.Fatal("expected error disabling sync on 500 response")
	}
	if _, err := executeTestCLI(t, "repo", "sync"); err == nil {
		t.Fatal("expected error triggering sync on 500 response")
	}
}
