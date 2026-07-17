package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRepoSyncCLICommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/rest/sync/latest/projects/PRJ/repos/repo":
			_, _ = w.Write([]byte(`{"enabled":true,"available":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/sync/latest/projects/PRJ/repos/repo":
			_, _ = w.Write([]byte(`{"enabled":false,"available":true}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/sync/latest/projects/PRJ/repos/repo/synchronize":
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/repos":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"slug":"repo","project":{"key":"PRJ"}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	// 1. Sync status
	out, err := executeTestCLI(t, "repo", "sync", "status")
	if err != nil {
		t.Fatalf("sync status failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "Auto-sync enabled: true") {
		t.Fatalf("unexpected sync status output: %s", out)
	}

	// 2. Sync enable
	out, err = executeTestCLI(t, "repo", "sync", "enable")
	if err != nil {
		t.Fatalf("sync enable failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "Automatic synchronization enabled for fork PRJ/repo") {
		t.Fatalf("unexpected sync enable output: %s", out)
	}

	// 3. Sync disable
	out, err = executeTestCLI(t, "repo", "sync", "disable")
	if err != nil {
		t.Fatalf("sync disable failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "Automatic synchronization disabled for fork PRJ/repo") {
		t.Fatalf("unexpected sync disable output: %s", out)
	}

	// 4. Sync trigger (default action)
	out, err = executeTestCLI(t, "repo", "sync")
	if err != nil {
		t.Fatalf("sync trigger failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, "Synchronization triggered for fork PRJ/repo from upstream") {
		t.Fatalf("unexpected sync trigger output: %s", out)
	}

	// 5. Dry run sync trigger
	out, err = executeTestCLI(t, "repo", "sync", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("dry run sync trigger failed: %v, out: %s", err, out)
	}
	if !strings.Contains(out, `"intent": "repo.sync.trigger"`) {
		t.Fatalf("unexpected dry run sync trigger output: %s", out)
	}
}

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

func TestRepoSyncCLICommandsAdditionalCoverage(t *testing.T) {
	// 1. Config loading error (BB_INSECURE_SKIP_VERIFY is invalid)
	t.Run("Config loading error", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "http://localhost:7990")
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "invalid")
		if _, err := executeTestCLI(t, "repo", "sync", "status"); err == nil {
			t.Fatal("expected error on invalid BB_INSECURE_SKIP_VERIFY configuration")
		}
	})

	// 2. Repository settings reference resolution error (BITBUCKET_REPO_SLUG is empty and no --repo flag)
	t.Run("Empty repository slug", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "http://localhost:7990")
		t.Setenv("BITBUCKET_TOKEN", "test-token")
		t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
		t.Setenv("BITBUCKET_REPO_SLUG", "")
		if _, err := executeTestCLI(t, "repo", "sync", "status"); err == nil {
			t.Fatal("expected error when BITBUCKET_REPO_SLUG is empty")
		}
	})

	// 3. Dry-run enable
	t.Run("Dry-run enable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/rest/api/latest/repos" {
				// Mock permission check response
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"slug":"repo","project":{"key":"PRJ"}}]}`))
			} else {
				http.NotFound(w, r)
			}
		}))
		defer server.Close()
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", server.URL)
		t.Setenv("BITBUCKET_TOKEN", "test-token")
		t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
		t.Setenv("BITBUCKET_REPO_SLUG", "repo")

		out, err := executeTestCLI(t, "repo", "sync", "enable", "--dry-run", "--json")
		if err != nil {
			t.Fatalf("dry run sync enable failed: %v", err)
		}
		if !strings.Contains(out, `"intent": "repo.sync.enable"`) {
			t.Fatalf("unexpected dry run sync enable output: %s", out)
		}
	})

	// 4. Dry-run disable
	t.Run("Dry-run disable", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/rest/api/latest/repos" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"slug":"repo","project":{"key":"PRJ"}}]}`))
			} else {
				http.NotFound(w, r)
			}
		}))
		defer server.Close()
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", server.URL)
		t.Setenv("BITBUCKET_TOKEN", "test-token")
		t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
		t.Setenv("BITBUCKET_REPO_SLUG", "repo")

		out, err := executeTestCLI(t, "repo", "sync", "disable", "--dry-run", "--json")
		if err != nil {
			t.Fatalf("dry run sync disable failed: %v", err)
		}
		if !strings.Contains(out, `"intent": "repo.sync.disable"`) {
			t.Fatalf("unexpected dry run sync disable output: %s", out)
		}
	})

	// 5. Dry-run permission check failure (forbidden status code)
	t.Run("Dry-run permission failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}))
		defer server.Close()
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", server.URL)
		t.Setenv("BITBUCKET_TOKEN", "test-token")
		t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
		t.Setenv("BITBUCKET_REPO_SLUG", "repo")

		if _, err := executeTestCLI(t, "repo", "sync", "--dry-run"); err == nil {
			t.Fatal("expected error on dry run when permission check returns 403")
		}
	})
}

