package tagcmd_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	tagcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/tag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type testPermissionChecker struct {
	err error
}

func (t testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return t.err
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool, dryRun bool) tagcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	return tagcmd.Dependencies{
		JSONEnabled:   func() bool { return jsonMode },
		DryRunEnabled: func() bool { return dryRun },
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
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) tagcmd.PermissionChecker {
			return testPermissionChecker{}
		},
	}
}

func TestTagWithDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	d := tagcmd.Dependencies{}
	cmd := tagcmd.New(d)
	if cmd == nil {
		t.Fatal("expected non-nil root tag command")
	}

	// Test default loaders
	defaults := (&d)
	_ = cmd // cmd was initialized with defaults
	if d.LoadConfig == nil {
		cfg, err := config.LoadFromEnv()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig: %v", err)
		}
	}
	_ = defaults
}

// #470 is live now, in TestLiveResourceDryRunPredictionsReadRealState.
//
// The prediction filtered a list capped at 200. FilterText is a substring
// match, so `v1.0` also matched v1.0.1 ... v1.0.240, and if the exact tag fell
// past the cap the scan found nothing and the preview reported create, at
// confidence full, for a tag that already exists.
//
// The version here built 250 near-misses in a fixture and counted requests. The
// live one seeds enough tags to cross a real page boundary and asks the preview
// about the last of them, which is the same defect asked of a server: a
// repository with one tag cannot tell a direct lookup from a scan, because both
// find it.

// TestTagCreateDryRunSurfacesANonNotFoundLookupFailure covers the branch that
// separates "the tag is not there" from "the lookup failed".
//
// Only a not-found answer means the tag can be created. Anything else -- a 500,
// a revoked token -- has to reach the caller, or the preview would predict
// create because the question could not be asked.
// mock-inventory: transport-fault — a 500 on the tag lookup is injected, which no live instance can be asked for; the subject is that a failed question is not read as "the tag is not there".
func TestTagCreateDryRunSurfacesANonNotFoundLookupFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")

		if strings.Contains(request.URL.Path, "/tags/") {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"boom"}]}`))

			return
		}
		_, _ = writer.Write([]byte(`{"id":"refs/heads/main","displayId":"main"}`))
	}))
	t.Cleanup(server.Close)

	deps := newTestDependencies(t, server.URL, true, true)
	cmd := tagcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "v9", "--start-point", "main"})

	err := cmd.Execute()
	if err == nil {
		t.Fatalf("a failed lookup was reported as a create prediction: %s", buf.String())
	}
	if strings.Contains(buf.String(), `"predictedAction"`) {
		t.Errorf("a preview was published despite the lookup failing: %s", buf.String())
	}
}
