package repocmd

import (
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestRepoAdminCLIValidation(t *testing.T) {
	t.Parallel()

	setup := testSetup{}

	// The setup names no host, so a refusal that does not name the missing
	// flags could be the configuration rather than the arguments.
	for _, testCase := range []struct {
		args []string
		want string
	}{
		{args: []string{"repo", "admin", "create"}, want: `required flag(s) "name", "project" not set`},
		{args: []string{"repo", "admin", "create", "--project", "PRJ"}, want: `required flag(s) "name" not set`},
		{args: []string{"repo", "create"}, want: `required flag(s) "name", "project" not set`},
		{args: []string{"repo", "create", "--project", "PRJ"}, want: `required flag(s) "name" not set`},
	} {
		_, err := executeTestCLIWith(t, setup, testCase.args...)
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Errorf("%v: expected %q, got: %v", testCase.args, testCase.want, err)
		}
	}
}

// TestRepoCreateRequiresAProjectKey covers the guard that used to report a
// missing project as a defect in bb rather than as the caller's omission.
func TestRepoCreateRequiresAProjectKey(t *testing.T) {
	t.Parallel()

	setup := testSetup{Host: testsupport.RefusedURL, Token: "token"}

	// --project is MarkFlagRequired, so the guard inside RunE is reached by
	// giving the flag an empty value rather than by omitting it. Omitting it
	// is Cobras error, and main classifies that one (#475).
	out, err := executeTestCLIWith(t, setup, "repo", "create", "--name", "demo", "--project", "  ")
	if err == nil {
		t.Fatalf("a repository creation without a project was accepted: %s", out)
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindValidation {
		t.Errorf("kind = %v, want validation (error: %v)", kind, err)
	}
	// A configuration bb could not load is a validation error too.
	if !strings.Contains(err.Error(), "project key is required") {
		t.Errorf("expected the missing project to be what was refused, got: %v", err)
	}
}

// TestRepoArchiveReportsAnUnreachableServerAsTransient covers the stream error
// path, which returned the raw transport error and so read as a defect in bb
// for a server that was simply down (#478).
func TestRepoArchiveReportsAnUnreachableServerAsTransient(t *testing.T) {
	t.Parallel()

	setup := testSetup{Host: testsupport.RefusedURL, Token: "token", ProjectKey: "PRJ", RepoSlug: "demo"}

	out, err := executeTestCLIWith(t, setup, "repo", "archive", "--output", "-")
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
