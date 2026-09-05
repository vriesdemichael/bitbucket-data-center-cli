package commitcmd_test

import (
	"bytes"
	"net/http/httptest"
	"testing"

	commitcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/commit"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// newMockCommitServer opens a listener that fails the test if anything reaches it.
//
// Everything still here refuses before it asks. The handwritten Bitbucket it used to be answered commits nobody reads any more.
func newMockCommitServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool) commitcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	return commitcmd.Dependencies{
		JSONEnabled: func() bool { return jsonMode },
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
	}
}

func TestCommitValidationErrors(t *testing.T) {
	server := newMockCommitServer(t)
	deps := newTestDependencies(t, server.URL, false)

	// Invalid repo selector
	cmd := commitcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "invalid-repo-no-slash"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on invalid repo selector")
	}

	// Defaults coverage
	defaultCmd := commitcmd.New(commitcmd.Dependencies{})
	if defaultCmd == nil {
		t.Fatalf("expected default command to be created")
	}
}
