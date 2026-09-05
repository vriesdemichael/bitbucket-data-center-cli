package diffcmd_test

import (
	"bytes"
	"net/http/httptest"
	"testing"

	diffcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/diff"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// newMockDiffServer opens a listener that fails the test if anything reaches it.
//
// Everything still here refuses before it asks. The handwritten Bitbucket it used to be answered diffs nobody reads any more.
func newMockDiffServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool) diffcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	return diffcmd.Dependencies{
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

func TestDiffValidationErrors(t *testing.T) {
	server := newMockDiffServer(t)
	deps := newTestDependencies(t, server.URL, false)

	// Mutually exclusive flags in refs
	cmd := diffcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"refs", "main", "feature", "--patch", "--stat"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when multiple diff modes are specified on refs")
	}

	// Mutually exclusive flags in pr
	buf.Reset()
	cmd.SetArgs([]string{"pr", "1", "--patch", "--name-only"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error when multiple diff modes are specified on pr")
	}

	// Invalid repo selector
	buf.Reset()
	cmd.SetArgs([]string{"refs", "main", "feature", "--repo", "invalid-no-slash"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on invalid repo selector")
	}

	// `diff pr <id> --repo` succeeding was here too, which is not a validation
	// error and needed a server to say so. It is live in
	// TestLiveCLICommandCoverage, against a pull request that exists.

	// Defaults coverage
	defaultCmd := diffcmd.New(diffcmd.Dependencies{})
	if defaultCmd == nil {
		t.Fatalf("expected default command to be created")
	}

	repo := "PRJ/repo"
	prAlias := diffcmd.NewDiffPullRequestCommand(diffcmd.Dependencies{}, &repo)
	if prAlias == nil {
		t.Fatalf("expected default pr alias command to be created")
	}
}

func TestDiffDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps diffcmd.Dependencies
	cmd := diffcmd.New(deps)
	if cmd == nil {
		t.Fatal("expected New to succeed with empty deps")
	}
}
