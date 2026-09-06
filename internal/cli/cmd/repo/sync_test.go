package repocmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// mock-inventory: transport-fault — the failures are injected; the subject is that each subcommand reports rather than claiming success.
func TestRepoSyncCLICommandsErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)

	setup := testSetup{Host: server.URL, Token: "test-token", ProjectKey: "PRJ", RepoSlug: "repo"}

	if _, err := executeTestCLIWith(t, setup, "repo", "sync", "status"); err == nil {
		t.Fatal("expected error getting sync status on 500 response")
	}
	if _, err := executeTestCLIWith(t, setup, "repo", "sync", "enable"); err == nil {
		t.Fatal("expected error enabling sync on 500 response")
	}
	if _, err := executeTestCLIWith(t, setup, "repo", "sync", "disable"); err == nil {
		t.Fatal("expected error disabling sync on 500 response")
	}
	if _, err := executeTestCLIWith(t, setup, "repo", "sync"); err == nil {
		t.Fatal("expected error triggering sync on 500 response")
	}
}
