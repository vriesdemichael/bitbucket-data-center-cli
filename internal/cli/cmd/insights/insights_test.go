package insightscmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type testPermissionChecker struct{}

func (testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

func newMockInsightsServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=UTF-8")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodPut && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1":
			_, _ = w.Write([]byte(`{"key":"report1","title":"Report 1","result":"PASS"}`))

		case r.Method == http.MethodGet && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1":
			_, _ = w.Write([]byte(`{"key":"report1","title":"Report 1","result":"PASS","details":"All tests passed"}`))

		case r.Method == http.MethodGet && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report-empty":
			http.NotFound(w, r)

		case r.Method == http.MethodDelete && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodDelete && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report-empty":
			http.NotFound(w, r)

		case r.Method == http.MethodGet && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports":
			if r.URL.Query().Get("empty") == "true" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"report1","title":"Report 1","result":"PASS"}]}`))

		case r.Method == http.MethodPost && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1/annotations":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"totalAdded":1}`))

		case r.Method == http.MethodGet && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1/annotations":
			if r.URL.Query().Get("empty") == "true" {
				_, _ = w.Write([]byte(`{"annotations":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"annotations":[{"externalId":"ann1","severity":"HIGH","message":"Issue found","path":"main.go","line":42}]}`))

		case r.Method == http.MethodGet && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/annotations":
			_, _ = w.Write([]byte(`{"annotations":[{"externalId":"ann1","severity":"HIGH","message":"Issue found"}]}`))

		case r.Method == http.MethodPut && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1/annotations/ann1":
			_, _ = w.Write([]byte(`{"externalId":"ann1","severity":"HIGH","message":"Issue found","path":"main.go","line":42}`))

		case r.Method == http.MethodDelete && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1/annotations":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func executeInsights(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var jsonFlag bool
	var dryRunFlag bool
	filteredArgs := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		} else if a == "--dry-run" {
			dryRunFlag = true
		} else {
			filteredArgs = append(filteredArgs, a)
		}
	}

	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonFlag },
		DryRunEnabled: func() bool { return dryRunFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) PermissionChecker {
			return testPermissionChecker{}
		},
	}

	root := &cobra.Command{Use: "bb"}
	root.AddCommand(New(deps))

	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)

	fullArgs := append([]string{"insights"}, filteredArgs...)
	root.SetArgs(fullArgs)
	err := root.Execute()
	return buffer.String(), err
}

func TestInsightsValidationErrors(t *testing.T) {
	server := newMockInsightsServer(t)

	// Invalid body in report set
	_, err := executeInsights(t, server.URL, "report", "set", "commit1", "report1", "--body", "invalid-json")
	if err == nil {
		t.Fatalf("expected error on invalid JSON report body")
	}

	// Invalid body in annotation add
	_, err = executeInsights(t, server.URL, "annotation", "add", "commit1", "report1", "--body", "invalid-json")
	if err == nil {
		t.Fatalf("expected error on invalid JSON annotation body")
	}

	// Missing required flags in annotation set
	_, err = executeInsights(t, server.URL, "annotation", "set", "commit1", "report1", "ann1")
	if err == nil {
		t.Fatalf("expected error when required flags are missing in annotation set")
	}
}

// assertDryRunPreview fails when a --dry-run invocation produced no preview.
//
// The preview is the entire product of a dry run: it is what tells the caller
// what would happen. These tests used to capture the output and check only that
// the command did not error, which cannot tell a real preview from an empty one
// -- and a --dry-run that quietly does nothing is the shape of #481, where a
// command pre-flighted as read-only and then created a commit.
//
// The text comes from the shared writer in internal/cli/dryrunpreview, which
// renders "Dry-run (<mode>, capability=<capability>)" for every command.
