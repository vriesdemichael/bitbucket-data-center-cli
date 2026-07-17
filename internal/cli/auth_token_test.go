package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthTokenCLICommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/users/alice":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-1","name":"UserToken"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-2","name":"ProjToken"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/projects/PRJ/repos/repo1":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"tok-3","name":"RepoToken"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserToken"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/access-tokens/latest/users/alice":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserToken","token":"secret-token-123"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			_, _ = w.Write([]byte(`{"id":"tok-1","name":"UserTokenUpdated"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/access-tokens/latest/users/alice/tok-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	// 1. List
	out, err := executeTestCLI(t, "auth", "token", "list", "--user", "alice")
	if err != nil {
		t.Fatalf("list user failed: %v", err)
	}
	if !strings.Contains(out, "tok-1") || !strings.Contains(out, "UserToken") {
		t.Fatalf("unexpected list user output: %s", out)
	}

	out, err = executeTestCLI(t, "auth", "token", "list", "--project", "PRJ")
	if err != nil {
		t.Fatalf("list proj failed: %v", err)
	}
	if !strings.Contains(out, "tok-2") || !strings.Contains(out, "ProjToken") {
		t.Fatalf("unexpected list proj output: %s", out)
	}

	out, err = executeTestCLI(t, "auth", "token", "list", "--repo", "PRJ/repo1")
	if err != nil {
		t.Fatalf("list repo failed: %v", err)
	}
	if !strings.Contains(out, "tok-3") || !strings.Contains(out, "RepoToken") {
		t.Fatalf("unexpected list repo output: %s", out)
	}

	// 2. Get
	out, err = executeTestCLI(t, "auth", "token", "get", "tok-1", "--user", "alice")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if !strings.Contains(out, "tok-1") || !strings.Contains(out, "UserToken") {
		t.Fatalf("unexpected get output: %s", out)
	}

	// 3. Create
	out, err = executeTestCLI(t, "auth", "token", "create", "UserToken", "--user", "alice", "--permission", "PROJECT_READ", "--expiry-days", "30")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !strings.Contains(out, "tok-1") || !strings.Contains(out, "secret-token-123") {
		t.Fatalf("unexpected create output: %s", out)
	}

	// 4. Update
	out, err = executeTestCLI(t, "auth", "token", "update", "tok-1", "--user", "alice", "--name", "UserTokenUpdated")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !strings.Contains(out, "updated successfully") {
		t.Fatalf("unexpected update output: %s", out)
	}

	// 5. Revoke
	out, err = executeTestCLI(t, "auth", "token", "revoke", "tok-1", "--user", "alice")
	if err != nil {
		t.Fatalf("revoke failed: %v", err)
	}
	if !strings.Contains(out, "revoked successfully") {
		t.Fatalf("unexpected revoke output: %s", out)
	}

	// 6. JSON outputs
	out, err = executeTestCLI(t, "--json", "auth", "token", "list", "--user", "alice")
	if err != nil {
		t.Fatalf("list json failed: %v", err)
	}
	if !strings.Contains(out, `"id"`) || !strings.Contains(out, `"UserToken"`) {
		t.Fatalf("unexpected list json output: %s", out)
	}

	// 7. Dry run
	out, err = executeTestCLI(t, "--dry-run", "auth", "token", "create", "UserToken", "--user", "alice")
	if err != nil {
		t.Fatalf("create dry-run failed: %v", err)
	}
	if !strings.Contains(out, "Dry-run") || !strings.Contains(out, "intent=auth.token.create") {
		t.Fatalf("unexpected dry-run output: %s", out)
	}
}
