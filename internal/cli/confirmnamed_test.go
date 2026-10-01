package cli

import (
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestYesAppliesOnlyToATargetWrittenDownForThisInvocation is ADR-073's rule for
// --yes, held on the commands the walk guards as repo delete holds it on itself.
//
// The repository comes through the seam BITBUCKET_PROJECT_KEY and
// BITBUCKET_REPO_SLUG feed. It is set for every command rather than written
// down for this one, so it does not name the target, and --yes on it is
// refused, saying what does name it. The same command with --repo gets past
// the confirmation and reaches the network, which at an unreachable host is a
// transient failure rather than the refusal.
func TestYesAppliesOnlyToATargetWrittenDownForThisInvocation(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		refused bool
		// naming is what the refusal must offer as naming the target.
		naming []string
	}{
		{
			name:    "a repository from the environment",
			args:    []string{"branch", "delete", "main", "--yes"},
			refused: true,
			naming:  []string{"--repo PROJECT/slug"},
		},
		{
			name: "a repository named with --repo",
			args: []string{"branch", "delete", "main", "--repo", "PRJ/demo", "--yes"},
		},
		// An empty --repo names nothing: the repository is the environment's.
		{
			name:    "a repository named with an empty --repo",
			args:    []string{"tag", "delete", "v1", "--repo=", "--yes"},
			refused: true,
			naming:  []string{"--repo PROJECT/slug"},
		},
		{
			name:    "repo delete with an empty --repo",
			args:    []string{"repo", "delete", "--repo=", "--yes"},
			refused: true,
			naming:  []string{"PROJECT/slug argument", "--repo PROJECT/slug"},
		},
		// A pull request's URL names its repository, and the command reads
		// neither --repo nor the environment for it.
		{
			name: "a repository named by a pull request's URL",
			args: []string{"pr", "review", "reviewer", "remove", "https://bitbucket.example.com/projects/PRJ/repos/demo/pull-requests/7", "--user", "bob", "--yes"},
		},
		// Where --project names the scope instead, the environment's project
		// does not name it either, and the refusal offers both.
		{
			name:    "a scope from the environment",
			args:    []string{"reviewer-group", "delete", "4", "--yes"},
			refused: true,
			naming:  []string{"--repo PROJECT/slug", "--project"},
		},
		{
			name: "a scope named with --project",
			args: []string{"reviewer-group", "delete", "4", "--project", "PRJ", "--yes"},
		},
		// A token's --repo picks which token, and nothing fills it in but the
		// caller: there is no repository in the target unless it was typed.
		{
			name: "a target with no repository",
			args: []string{"auth", "token", "revoke", "1827364510", "--user", "admin", "--yes"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := append([]string{"--json", "--no-input"}, testCase.args...)
			_, err := executeTestCLI(t, unreachableRepository(), args...)
			if err == nil {
				t.Fatalf("bb %v ran to completion with no server", testCase.args)
			}

			refusal := strings.Contains(err.Error(), "--yes only applies")
			if !testCase.refused {
				if refusal || !apperrors.IsKind(err, apperrors.KindTransient) {
					t.Fatalf("--yes did not get past the confirmation to the network: %v", err)
				}

				return
			}

			if !refusal {
				t.Fatalf("--yes applied to a target nobody wrote down: %v", err)
			}
			if code := apperrors.ExitCode(err); code != 2 {
				t.Errorf("exit code = %d, want 2 (validation): %v", code, err)
			}
			for _, naming := range testCase.naming {
				if !strings.Contains(err.Error(), naming) {
					t.Errorf("the refusal does not offer %s: %v", naming, err)
				}
			}
			for _, unnamed := range []string{"BITBUCKET_PROJECT_KEY", "BITBUCKET_REPO_SLUG"} {
				if strings.Contains(err.Error(), unnamed) {
					t.Errorf("the refusal offers %s, which does not name the target: %v", unnamed, err)
				}
			}
		})
	}
}

// TestAMalformedRepoIsRefusedBeforeTheConfirmation holds the order ADR-073
// sets: input that is invalid fails first. Asked for the confirmation, a person
// typed back the branch and was then told the repository could not exist, and
// a caller with nobody to ask added --yes only to meet the same refusal.
func TestAMalformedRepoIsRefusedBeforeTheConfirmation(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"branch", "delete", "feature/x", "--repo", "not-a-selector"},
		{"branch", "delete", "feature/x", "--repo", "not-a-selector", "--yes"},
	} {
		_, err := executeTestCLI(t, unreachableRepository(), append([]string{"--json", "--no-input"}, args...)...)
		if err == nil || !strings.Contains(err.Error(), "invalid repository selector") {
			t.Errorf("bb %v: want the selector refused, got: %v", args, err)
		}
	}
}
