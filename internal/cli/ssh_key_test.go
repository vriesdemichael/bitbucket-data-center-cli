package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSshKeyCLICommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ssh/latest/keys":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":123,"label":"MyKey","text":"ssh-rsa AAA","fingerprint":"fp-123"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/ssh/latest/keys":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":123,"label":"MyKey","text":"ssh-rsa AAA","fingerprint":"fp-123"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/ssh/latest/keys/123":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && r.URL.Path == "/rest/keys/latest/projects/PRJ/ssh":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"permission":"PROJECT_READ","key":{"id":456,"label":"ProjKey","fingerprint":"fp-456"}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/keys/latest/projects/PRJ/ssh":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"permission":"PROJECT_READ","key":{"id":456,"label":"ProjKey","fingerprint":"fp-456"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/keys/latest/projects/PRJ/ssh/456":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && r.URL.Path == "/rest/keys/latest/projects/PRJ/repos/repo1/ssh":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"permission":"REPO_WRITE","key":{"id":789,"label":"RepoKey","fingerprint":"fp-789"}}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/keys/latest/projects/PRJ/repos/repo1/ssh":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"permission":"REPO_WRITE","key":{"id":789,"label":"RepoKey","fingerprint":"fp-789"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/keys/latest/projects/PRJ/repos/repo1/ssh/789":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	// 1. User SSH keys
	out, err := executeTestCLI(t, "ssh-key", "list")
	if err != nil {
		t.Fatalf("ssh-key list failed: %v", err)
	}
	if !strings.Contains(out, "123") || !strings.Contains(out, "MyKey") || !strings.Contains(out, "fp-123") {
		t.Fatalf("unexpected ssh-key list output: %s", out)
	}

	out, err = executeTestCLI(t, "ssh-key", "add", "ssh-rsa AAA", "--label", "MyKey")
	if err != nil {
		t.Fatalf("ssh-key add failed: %v", err)
	}
	if !strings.Contains(out, "added successfully") {
		t.Fatalf("unexpected ssh-key add output: %s", out)
	}

	out, err = executeTestCLI(t, "ssh-key", "remove", "123")
	if err != nil {
		t.Fatalf("ssh-key remove failed: %v", err)
	}
	if !strings.Contains(out, "removed successfully") {
		t.Fatalf("unexpected ssh-key remove output: %s", out)
	}

	// 2. Project access keys
	out, err = executeTestCLI(t, "repo", "ssh-key", "list", "--project", "PRJ")
	if err != nil {
		t.Fatalf("repo ssh-key list proj failed: %v", err)
	}
	if !strings.Contains(out, "456") || !strings.Contains(out, "PROJECT_READ") || !strings.Contains(out, "fp-456") {
		t.Fatalf("unexpected repo ssh-key list proj output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "ssh-key", "add", "ssh-rsa AAA", "--project", "PRJ", "--label", "ProjKey", "--read-only")
	if err != nil {
		t.Fatalf("repo ssh-key add proj failed: %v", err)
	}
	if !strings.Contains(out, "added successfully") || !strings.Contains(out, "PROJECT_READ") {
		t.Fatalf("unexpected repo ssh-key add proj output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "ssh-key", "remove", "456", "--project", "PRJ")
	if err != nil {
		t.Fatalf("repo ssh-key remove proj failed: %v", err)
	}
	if !strings.Contains(out, "removed successfully") {
		t.Fatalf("unexpected repo ssh-key remove proj output: %s", out)
	}

	// 3. Repo access keys
	out, err = executeTestCLI(t, "repo", "ssh-key", "list", "--repo", "PRJ/repo1")
	if err != nil {
		t.Fatalf("repo ssh-key list repo failed: %v", err)
	}
	if !strings.Contains(out, "789") || !strings.Contains(out, "REPO_WRITE") || !strings.Contains(out, "fp-789") {
		t.Fatalf("unexpected repo ssh-key list repo output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "ssh-key", "add", "ssh-rsa AAA", "--repo", "PRJ/repo1", "--label", "RepoKey", "--read-write")
	if err != nil {
		t.Fatalf("repo ssh-key add repo failed: %v", err)
	}
	if !strings.Contains(out, "added successfully") || !strings.Contains(out, "REPO_WRITE") {
		t.Fatalf("unexpected repo ssh-key add repo output: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "ssh-key", "remove", "789", "--repo", "PRJ/repo1")
	if err != nil {
		t.Fatalf("repo ssh-key remove repo failed: %v", err)
	}
	if !strings.Contains(out, "removed successfully") {
		t.Fatalf("unexpected repo ssh-key remove repo output: %s", out)
	}

	// 4. Dry runs
	out, err = executeTestCLI(t, "--dry-run", "ssh-key", "add", "ssh-rsa AAA")
	if err != nil {
		t.Fatalf("ssh-key add dry-run failed: %v", err)
	}
	if !strings.Contains(out, "Dry-run") || !strings.Contains(out, "intent=ssh-key.add") {
		t.Fatalf("unexpected ssh-key add dry-run output: %s", out)
	}

	out, err = executeTestCLI(t, "--dry-run", "repo", "ssh-key", "add", "ssh-rsa AAA", "--project", "PRJ")
	if err != nil {
		t.Fatalf("repo ssh-key add dry-run failed: %v", err)
	}
	if !strings.Contains(out, "Dry-run") || !strings.Contains(out, "intent=repo.ssh-key.add") {
		t.Fatalf("unexpected repo ssh-key add dry-run output: %s", out)
	}
}
