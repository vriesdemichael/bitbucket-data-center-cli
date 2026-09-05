package deploymentcmd_test

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	deploymentcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/deployment"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
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

	cfg := config.AppConfig{BitbucketURL: serverURL, ProjectKey: "PRJ"}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	var recorded []openapi.RepositoryPermission

	deps := deploymentcmd.Dependencies{
		JSONEnabled:   func() bool { return false },
		DryRunEnabled: func() bool { return true },
		LoadConfig: func() (config.AppConfig, error) {
			return cfg, nil
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			return cfg, client, nil
		},
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) deploymentcmd.PermissionChecker {
			return refusingChecker{permissions: &recorded}
		},
	}

	command := deploymentcmd.New(deps)
	command.SilenceUsage = true
	command.SilenceErrors = true
	buffer := new(bytes.Buffer)
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs(args)

	return recorded, command.Execute()
}

// Recording or removing a deployment mutates repository state, so each command
// has to establish the caller may write before planning and stop when told no.
func TestDeploymentCommandsHonourRefusedPermission(t *testing.T) {
	// A listener that fails the test if it is reached, which is the
	// assertion: every case here is refused before a request exists.
	guard := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(guard.Close)
	serverURL := guard.URL

	tests := []struct {
		name string
		args []string
		want openapi.RepositoryPermission
	}{
		{name: "create", args: []string{"create", "c123", "--deployment-sequence-number", "1", "--display-name", "d", "--key", "k", "--state", "SUCCESSFUL", "--url", "http://cd.example/1", "--env-key", "prod", "--env-name", "Production"}, want: openapi.RepoWrite},
		{name: "delete", args: []string{"delete", "c123", "--key", "k", "--env-key", "prod", "--deployment-sequence-number", "1"}, want: openapi.RepoWrite},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			recorded, err := executeRefusing(t, serverURL, testCase.args...)
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
