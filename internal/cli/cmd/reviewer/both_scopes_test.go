package reviewercmd

import (
	"bytes"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestConditionRefusesProjectBesideRepo covers the other half of #725. Given
// both, every condition command acted on the repository and said nothing of
// the --project it had been given, so a condition asked for on the project was
// written to one of its repositories. bb reviewer-group already refused the
// pair.
//
// Refused before any configuration is loaded, so no server is needed to see it.
func TestConditionRefusesProjectBesideRepo(t *testing.T) {
	t.Parallel()

	const condition = `{"requiredApprovals":1}`

	for name, args := range map[string][]string{
		"list":   {"condition", "list"},
		"create": {"condition", "create", condition},
		"update": {"condition", "update", "1", condition},
		"delete": {"condition", "delete", "1"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			command := New(Dependencies{
				JSONEnabled:   func() bool { return false },
				DryRunEnabled: func() bool { return false },
				LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
					t.Error("the configuration was loaded, so the command went on to act on one of the two")
					return config.AppConfig{}, nil, apperrors.New(apperrors.KindInternal, "not reached", nil)
				},
			})
			buf := new(bytes.Buffer)
			command.SetOut(buf)
			command.SetErr(buf)
			command.SetArgs(append(args, "--project", "PRJ", "--repo", "PRJ/repo1"))

			err := command.Execute()
			if err == nil {
				t.Fatalf("--project beside --repo was accepted:\n%s", buf)
			}
			if got, want := err.Error(), "validation: cannot specify both --project and --repo"; got != want {
				t.Errorf("error = %q, want %q", got, want)
			}
		})
	}
}
