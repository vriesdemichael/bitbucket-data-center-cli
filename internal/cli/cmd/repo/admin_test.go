package repocmd

import (
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

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

// The repo admin CRUD and alias-equivalence suites are live now.
//
// Both drove create, update, delete and their aliases against a fixture
// and compared the output to what the fixture had been told to say.
// TestLiveRepoAdminLifecycle and the alias coverage in the live suite do the
// same against a server that actually holds the repository.
