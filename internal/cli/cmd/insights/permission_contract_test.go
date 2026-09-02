package insightscmd

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// errPermissionRefused stands in for the error a real checker returns when the
// caller lacks the permission a command requires.
var errPermissionRefused = errors.New("permission refused by test checker")

// refusingChecker records the permission a command demands and refuses it.
// The existing checker allows everything, which only proves the call was
// reached; refusing proves the command stops, which is the point of checking.
type refusingChecker struct {
	permissions *[]openapi.RepositoryPermission
}

func (c refusingChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	*c.permissions = append(*c.permissions, permission)
	return errPermissionRefused
}

func executeRefusing(t *testing.T, serverURL string, args ...string) ([]openapi.RepositoryPermission, error) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var recorded []openapi.RepositoryPermission

	deps := Dependencies{
		JSONEnabled:   func() bool { return false },
		DryRunEnabled: func() bool { return true },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, clientErr := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, clientErr
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) PermissionChecker {
			return refusingChecker{permissions: &recorded}
		},
	}

	root := &cobra.Command{Use: "bb"}
	root.SilenceUsage = true
	root.SilenceErrors = true
	root.AddCommand(New(deps))
	buffer := new(bytes.Buffer)
	root.SetOut(buffer)
	root.SetErr(buffer)
	root.SetArgs(append([]string{"insights"}, args...))

	return recorded, root.Execute()
}

// Writing a code insights report or annotation mutates repository state, so each
// command has to establish the caller may write before planning and stop when
// told no.
func TestInsightsCommandsHonourRefusedPermission(t *testing.T) {
	server := newMockInsightsServer(t)

	tests := []struct {
		name string
		args []string
		want openapi.RepositoryPermission
	}{
		{name: "report set", args: []string{"report", "set", "c123", "k", "--body", `{"title":"t"}`}, want: openapi.RepoWrite},
		{name: "annotation set", args: []string{"annotation", "set", "c123", "k", "ext-1", "--message", "m", "--severity", "LOW"}, want: openapi.RepoWrite},
		{name: "annotation delete", args: []string{"annotation", "delete", "c123", "k", "--external-id", "ext-1"}, want: openapi.RepoWrite},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			recorded, err := executeRefusing(t, server.URL, testCase.args...)
			if len(recorded) == 0 {
				t.Fatal("command planned a mutation without checking any repository permission")
			}
			if recorded[0] != testCase.want {
				t.Fatalf("checked %q, want %q", recorded[0], testCase.want)
			}
			if !errors.Is(err, errPermissionRefused) {
				t.Fatalf("command continued past a refused permission check: err = %v", err)
			}
		})
	}
}
