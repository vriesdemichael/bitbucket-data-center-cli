package branchcmd_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	branchcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/branch"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// errPermissionRefused stands in for the error a real checker returns when the
// caller lacks the permission a command requires.
var errPermissionRefused = errors.New("permission refused by test checker")

// refusingChecker records the permission a command demands and refuses it.
//
// Refusing is what makes this worth testing. The existing checker allows
// everything, which only proves the call was reached; refusing proves the
// command stops, which is the point of checking at all.
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

	deps := branchcmd.Dependencies{
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
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) branchcmd.PermissionChecker {
			return refusingChecker{permissions: &recorded}
		},
	}

	command := branchcmd.New(deps)
	command.SilenceUsage = true
	command.SilenceErrors = true
	buffer := new(bytes.Buffer)
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs(args)

	return recorded, command.Execute()
}

// Each mutating branch command has to establish the caller may act before it
// plans anything, and stop when told no.
func TestBranchCommandsHonourRefusedPermission(t *testing.T) {
	server := newMockBranchServer(t)

	tests := []struct {
		name string
		args []string
		want openapi.RepositoryPermission
	}{
		{name: "create", args: []string{"create", "feature/x", "--start-point", "main"}, want: openapi.RepoWrite},
		{name: "default set", args: []string{"default", "set", "main"}, want: openapi.RepoAdmin},
		{name: "model update", args: []string{"model", "update", "main"}, want: openapi.RepoAdmin},
		{name: "restriction create", args: []string{"restriction", "create", "--type", "read-only", "--matcher-id", "main", "--matcher-type", "BRANCH"}, want: openapi.RepoAdmin},
		{name: "restriction update", args: []string{"restriction", "update", "1", "--type", "read-only", "--matcher-id", "main", "--matcher-type", "BRANCH"}, want: openapi.RepoAdmin},
		{name: "restriction delete", args: []string{"restriction", "delete", "1"}, want: openapi.RepoAdmin},
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
