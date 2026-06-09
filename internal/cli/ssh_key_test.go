package cli

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSshKeyCLICommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/ssh/latest/keys":
			if r.URL.Query().Get("limit") == "5" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
			} else {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":123,"label":"MyKey","text":"ssh-rsa AAA","fingerprint":"fp-123"}]}`))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/rest/ssh/latest/keys":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":123,"label":"MyKey","text":"ssh-rsa AAA","fingerprint":"fp-123"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/ssh/latest/keys/123":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && r.URL.Path == "/rest/keys/latest/projects/PRJ/ssh":
			if r.URL.Query().Get("limit") == "5" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
			} else {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"permission":"PROJECT_READ","key":{"id":456,"label":"ProjKey","fingerprint":"fp-456"}}]}`))
			}
		case r.Method == http.MethodPost && r.URL.Path == "/rest/keys/latest/projects/PRJ/ssh":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"permission":"PROJECT_READ","key":{"id":456,"label":"ProjKey","fingerprint":"fp-456"}}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/keys/latest/projects/PRJ/ssh/456":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && r.URL.Path == "/rest/keys/latest/projects/PRJ/repos/repo1/ssh":
			if r.URL.Query().Get("limit") == "5" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
			} else {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"permission":"REPO_WRITE","key":{"id":789,"label":"RepoKey","fingerprint":"fp-789"}}]}`))
			}
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

	// 5. Validation and File reading
	_, err = executeTestCLI(t, "ssh-key", "add", "")
	if err == nil {
		t.Fatal("expected error for empty public key")
	}

	tempDir := t.TempDir()
	keyFile := filepath.Join(tempDir, "id_rsa.pub")
	err = os.WriteFile(keyFile, []byte("ssh-rsa AAA"), 0644)
	if err != nil {
		t.Fatalf("write temp file failed: %v", err)
	}
	out, err = executeTestCLI(t, "ssh-key", "add", keyFile, "--label", "MyKeyFromFile")
	if err != nil {
		t.Fatalf("ssh-key add from file failed: %v", err)
	}
	if !strings.Contains(out, "added successfully") {
		t.Fatalf("unexpected ssh-key add from file output: %s", out)
	}

	_, err = executeTestCLI(t, "ssh-key", "add", tempDir)
	if err == nil {
		t.Fatal("expected error for directory path as public key file")
	}

	_, err = executeTestCLI(t, "repo", "ssh-key", "add", "ssh-rsa AAA", "--project", "PRJ", "--repo", "PRJ/repo1")
	if err == nil || !strings.Contains(err.Error(), "only one of --project or --repo") {
		t.Fatalf("expected scope validation error, got: %v", err)
	}

	_, err = executeTestCLI(t, "repo", "ssh-key", "add", "ssh-rsa AAA")
	if err == nil || !strings.Contains(err.Error(), "either --project or --repo") {
		t.Fatalf("expected scope validation error, got: %v", err)
	}

	// 6. JSON output and empty response checks
	out, err = executeTestCLI(t, "--json", "ssh-key", "list")
	if err != nil {
		t.Fatalf("json ssh-key list failed: %v", err)
	}
	if !strings.Contains(out, `"id": 123`) {
		t.Fatalf("unexpected json list output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "ssh-key", "add", "ssh-rsa AAA", "--label", "MyKey")
	if err != nil {
		t.Fatalf("json ssh-key add failed: %v", err)
	}
	if !strings.Contains(out, `"id": 123`) {
		t.Fatalf("unexpected json add output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "ssh-key", "remove", "123")
	if err != nil {
		t.Fatalf("json ssh-key remove failed: %v", err)
	}
	if !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("unexpected json remove output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "ssh-key", "list", "--project", "PRJ")
	if err != nil {
		t.Fatalf("json repo ssh-key list proj failed: %v", err)
	}
	if !strings.Contains(out, `"id": 456`) {
		t.Fatalf("unexpected json list proj output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "ssh-key", "add", "ssh-rsa AAA", "--project", "PRJ", "--label", "ProjKey", "--read-only")
	if err != nil {
		t.Fatalf("json repo ssh-key add proj failed: %v", err)
	}
	if !strings.Contains(out, `"id": 456`) {
		t.Fatalf("unexpected json add proj output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "ssh-key", "remove", "456", "--project", "PRJ")
	if err != nil {
		t.Fatalf("json repo ssh-key remove proj failed: %v", err)
	}
	if !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("unexpected json remove proj output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "ssh-key", "list", "--repo", "PRJ/repo1")
	if err != nil {
		t.Fatalf("json repo ssh-key list repo failed: %v", err)
	}
	if !strings.Contains(out, `"id": 789`) {
		t.Fatalf("unexpected json list repo output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "ssh-key", "add", "ssh-rsa AAA", "--repo", "PRJ/repo1", "--label", "RepoKey", "--read-write")
	if err != nil {
		t.Fatalf("json repo ssh-key add repo failed: %v", err)
	}
	if !strings.Contains(out, `"id": 789`) {
		t.Fatalf("unexpected json add repo output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "ssh-key", "remove", "789", "--repo", "PRJ/repo1")
	if err != nil {
		t.Fatalf("json repo ssh-key remove repo failed: %v", err)
	}
	if !strings.Contains(out, `"status": "ok"`) {
		t.Fatalf("unexpected json remove repo output: %s", out)
	}

	out, err = executeTestCLI(t, "ssh-key", "list", "--limit", "5")
	if err != nil {
		t.Fatalf("ssh-key list empty failed: %v", err)
	}
	if !strings.Contains(out, "No SSH keys found") {
		t.Fatalf("expected empty message, got: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "ssh-key", "list", "--project", "PRJ", "--limit", "5")
	if err != nil {
		t.Fatalf("repo ssh-key list empty proj failed: %v", err)
	}
	if !strings.Contains(out, "No SSH access keys found") {
		t.Fatalf("expected empty message, got: %s", out)
	}

	out, err = executeTestCLI(t, "repo", "ssh-key", "list", "--repo", "PRJ/repo1", "--limit", "5")
	if err != nil {
		t.Fatalf("repo ssh-key list empty repo failed: %v", err)
	}
	if !strings.Contains(out, "No SSH access keys found") {
		t.Fatalf("expected empty message, got: %s", out)
	}

	// 7. Config/Client load errors
	t.Run("ConfigError", func(t *testing.T) {
		t.Setenv("BITBUCKET_URL", "")
		_, err := executeTestCLI(t, "ssh-key", "list")
		if err == nil {
			t.Fatal("expected error when BITBUCKET_URL is missing")
		}
	})

	t.Run("ClientInitError", func(t *testing.T) {
		t.Setenv("BB_CA_FILE", "/nonexistent/ca/path/for/test")
		_, err := executeTestCLI(t, "ssh-key", "list")
		if err == nil {
			t.Fatal("expected error for nonexistent CA file path")
		}
	})
}
