package repocmd

import (
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
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

// TestRepoCreateRequiresAProjectKey covers the guard that used to report a
// missing project as a defect in bb rather than as the caller's omission.
func TestRepoCreateRequiresAProjectKey(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://127.0.0.1:1")
	t.Setenv("BITBUCKET_TOKEN", "token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "")

	// --project is MarkFlagRequired, so the guard inside RunE is reached by
	// giving the flag an empty value rather than by omitting it. Omitting it
	// is Cobras error, and main classifies that one (#475).
	out, err := executeTestCLI(t, "repo", "create", "--name", "demo", "--project", "  ")
	if err == nil {
		t.Fatalf("a repository creation without a project was accepted: %s", out)
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindValidation {
		t.Errorf("kind = %v, want validation (error: %v)", kind, err)
	}
}

// TestRepoArchiveReportsAnUnreachableServerAsTransient covers the stream error
// path, which returned the raw transport error and so read as a defect in bb
// for a server that was simply down (#478).
func TestRepoArchiveReportsAnUnreachableServerAsTransient(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://127.0.0.1:1")
	t.Setenv("BITBUCKET_TOKEN", "token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	out, err := executeTestCLI(t, "repo", "archive", "--output", "-")
	if err == nil {
		t.Fatalf("an unreachable server produced no error: %s", out)
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindTransient {
		t.Errorf("kind = %v, want transient (error: %v)", kind, err)
	}
}
