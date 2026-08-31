package repocmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRepoAdminCLICommandsMock(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"slug":"repo","name":"repo"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"slug":"forked","name":"forked"}`))
		case r.Method == http.MethodPut && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"slug":"repo","name":"Updated Repo"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo":
			w.WriteHeader(http.StatusAccepted)
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

	// Create
	out, err := executeTestCLI(t, "repo", "admin", "create", "--project", "PRJ", "--name", "repo")
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}
	if !strings.Contains(out, "Created repository PRJ/repo") {
		t.Fatalf("unexpected create output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "admin", "create", "--project", "PRJ", "--name", "repo")
	if err != nil {
		t.Fatalf("create json failed: %v", err)
	}
	if !strings.Contains(out, `"repository"`) {
		t.Fatalf("unexpected create json output: %s", out)
	}

	// Fork
	out, err = executeTestCLI(t, "repo", "admin", "fork", "--name", "forked")
	if err != nil {
		t.Fatalf("fork failed: %v", err)
	}
	if !strings.Contains(out, "Forked repository to forked") {
		t.Fatalf("unexpected fork output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "admin", "fork", "--name", "forked")
	if err != nil {
		t.Fatalf("fork json failed: %v", err)
	}
	if !strings.Contains(out, `"repository"`) {
		t.Fatalf("unexpected fork json output: %s", out)
	}

	// Update
	out, err = executeTestCLI(t, "repo", "admin", "update", "--name", "Updated Repo")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}
	if !strings.Contains(out, "Updated repository Updated Repo") {
		t.Fatalf("unexpected update output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "admin", "update", "--name", "Updated Repo")
	if err != nil {
		t.Fatalf("update json failed: %v", err)
	}
	if !strings.Contains(out, `"repository"`) {
		t.Fatalf("unexpected update json output: %s", out)
	}

	// Delete
	out, err = executeTestCLI(t, "repo", "admin", "delete", "PRJ/repo", "--yes")
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if !strings.Contains(out, "Deleted repository PRJ/repo") {
		t.Fatalf("unexpected delete output: %s", out)
	}

	out, err = executeTestCLI(t, "--json", "repo", "admin", "delete", "PRJ/repo", "--yes")
	if err != nil {
		t.Fatalf("delete json failed: %v", err)
	}
	if !strings.Contains(out, `"status"`) {
		t.Fatalf("unexpected delete json output: %s", out)
	}
}

func TestRepoAdminCLIValidation(t *testing.T) {
	_, err := executeTestCLI(t, "repo", "admin", "create")
	if err == nil {
		t.Fatal("expected create missing arg error")
	}

	_, err = executeTestCLI(t, "repo", "admin", "create", "--project", "PRJ")
	if err == nil {
		t.Fatal("expected create missing name error")
	}

	_, err = executeTestCLI(t, "repo", "create")
	if err == nil {
		t.Fatal("expected repo create missing arg error")
	}

	_, err = executeTestCLI(t, "repo", "create", "--project", "PRJ")
	if err == nil {
		t.Fatal("expected repo create missing name error")
	}
}

func TestRepoCanonicalCRUDAndAliasEquivalence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"slug":"repo","name":"repo"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"slug":"forked","name":"forked"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo":
			w.WriteHeader(http.StatusAccepted)
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

	// Canonical Create vs Alias Create
	outCanonical, err := executeTestCLI(t, "repo", "create", "--project", "PRJ", "--name", "repo")
	if err != nil {
		t.Fatalf("canonical create failed: %v", err)
	}
	outAlias, err := executeTestCLI(t, "repo", "admin", "create", "--project", "PRJ", "--name", "repo")
	if err != nil {
		t.Fatalf("alias create failed: %v", err)
	}
	if outCanonical != outAlias {
		t.Fatalf("expected identical output between canonical and alias create:\ncanonical: %q\nalias: %q", outCanonical, outAlias)
	}

	// Canonical Fork vs Alias Fork
	outCanonical, err = executeTestCLI(t, "repo", "fork", "--name", "forked")
	if err != nil {
		t.Fatalf("canonical fork failed: %v", err)
	}
	outAlias, err = executeTestCLI(t, "repo", "admin", "fork", "--name", "forked")
	if err != nil {
		t.Fatalf("alias fork failed: %v", err)
	}
	if outCanonical != outAlias {
		t.Fatalf("expected identical output between canonical and alias fork:\ncanonical: %q\nalias: %q", outCanonical, outAlias)
	}

	// Canonical Delete vs Alias Delete
	outCanonical, err = executeTestCLI(t, "repo", "delete", "PRJ/repo", "--yes")
	if err != nil {
		t.Fatalf("canonical delete failed: %v", err)
	}
	outAlias, err = executeTestCLI(t, "repo", "admin", "delete", "PRJ/repo", "--yes")
	if err != nil {
		t.Fatalf("alias delete failed: %v", err)
	}
	if outCanonical != outAlias {
		t.Fatalf("expected identical output between canonical and alias delete:\ncanonical: %q\nalias: %q", outCanonical, outAlias)
	}
}
