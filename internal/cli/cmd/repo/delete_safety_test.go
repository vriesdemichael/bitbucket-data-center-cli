package repocmd

import (
	"strings"
	"testing"
)

// TestRepoDeleteWillNotActOnAnInferredTarget is issue #472.
//
// bb repo delete took no positional argument, had no --yes, and let
// applyInferredRepositoryContext fill the target from the git remote. Typed
// with no arguments inside a checkout, it deleted that repository. ADR-054
// used this very command as its example of one made safe by explicit flags,
// target arguments or --dry-run, and it had none of the three.
//
// The rule that replaces it (ADR-073): the target must be named, and --yes
// does nothing when it is not.
func TestRepoDeleteWillNotActOnAnInferredTarget(t *testing.T) {
	// The dangerous shape: a target is available without the caller naming it.
	// This is what applyInferredRepositoryContext produces from a git remote,
	// and it is indistinguishable from an operator setting the variables.
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_TOKEN", "token")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	cases := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "no target and no confirmation",
			args: []string{"repo", "delete"},
			want: "--yes is required",
		},
		{
			name: "confirmation without a named target is inert",
			args: []string{"repo", "delete", "--yes"},
			want: "--yes only applies when the target is named explicitly",
		},
		{
			name: "the alias behaves the same way",
			args: []string{"repo", "admin", "delete", "--yes"},
			want: "--yes only applies when the target is named explicitly",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := executeTestCLI(t, testCase.args...)
			if err == nil {
				t.Fatal("the repository was deleted without an explicitly named target")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("error = %q, want it to mention %q", err.Error(), testCase.want)
			}
		})
	}
}

// TestRepoDeleteRefusalNamesTheFlag is the half ADR-054 left unsaid.
//
// Declining to prompt is not the same as proceeding: a run with nobody to ask
// must still say which flag would have supplied the answer, or the caller
// learns nothing it can act on. An agent that reads this message can fix its
// own invocation.
func TestRepoDeleteRefusalNamesTheFlag(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_TOKEN", "token")

	_, err := executeTestCLI(t, "repo", "delete", "PRJ/repo")
	if err == nil {
		t.Fatal("the repository was deleted with no confirmation and no --yes")
	}

	message := err.Error()
	for _, expected := range []string{"--yes", "PRJ/repo"} {
		if !strings.Contains(message, expected) {
			t.Errorf("error = %q, want it to mention %q", message, expected)
		}
	}
}
