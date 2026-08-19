package insightscmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type testPermissionChecker struct{}

func (testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

func newMockInsightsServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodPut && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1":
			_, _ = w.Write([]byte(`{"key":"report1","title":"Report 1","result":"PASS"}`))

		case r.Method == http.MethodGet && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1":
			_, _ = w.Write([]byte(`{"key":"report1","title":"Report 1","result":"PASS"}`))

		case r.Method == http.MethodDelete && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"key":"report1","title":"Report 1","result":"PASS"}]}`))

		case r.Method == http.MethodPost && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1/annotations":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1/annotations":
			_, _ = w.Write([]byte(`{"annotations":[{"externalId":"ann1","severity":"HIGH","message":"Issue found"}]}`))

		case r.Method == http.MethodPut && path == "/rest/insights/latest/projects/PRJ/repos/demo/commits/commit1/reports/report1/annotations/ann1":
			_, _ = w.Write([]byte(`{"externalId":"ann1","severity":"HIGH","message":"Issue found"}`))

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
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		}
		if a == "--dry-run" {
			dryRunFlag = true
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
	root.PersistentFlags().Bool("json", false, "")
	root.PersistentFlags().Bool("dry-run", false, "")
	root.AddCommand(New(deps))

	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)

	fullArgs := append([]string{"insights"}, args...)
	root.SetArgs(fullArgs)
	err := root.Execute()
	return buffer.String(), err
}

func TestInsightsReportSetAndGet(t *testing.T) {
	server := newMockInsightsServer(t)

	// Set report
	out, err := executeInsights(t, server.URL, "report", "set", "commit1", "report1", "--body", `{"title":"Report 1","result":"PASS"}`)
	if err != nil {
		t.Fatalf("unexpected error on set: %v", err)
	}
	if !strings.Contains(out, "report1") {
		t.Fatalf("expected report1 in set output, got:\n%s", out)
	}

	// Get report
	out, err = executeInsights(t, server.URL, "report", "get", "commit1", "report1")
	if err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}
	if !strings.Contains(out, "report1") {
		t.Fatalf("expected report1 in get output, got:\n%s", out)
	}
}

func TestInsightsReportList(t *testing.T) {
	server := newMockInsightsServer(t)

	out, err := executeInsights(t, server.URL, "report", "list", "commit1")
	if err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(out, "report1") || !strings.Contains(out, "Report 1") {
		t.Fatalf("expected report1 in list output, got:\n%s", out)
	}
}

func TestInsightsReportDelete(t *testing.T) {
	server := newMockInsightsServer(t)

	out, err := executeInsights(t, server.URL, "report", "delete", "commit1", "report1")
	if err != nil {
		t.Fatalf("unexpected error on delete: %v", err)
	}
	if !strings.Contains(out, "Deleted report report1 for commit commit1") {
		t.Fatalf("expected delete confirmation, got:\n%s", out)
	}
}

func TestInsightsAnnotationAddAndList(t *testing.T) {
	server := newMockInsightsServer(t)

	// Add annotation
	out, err := executeInsights(t, server.URL, "annotation", "add", "commit1", "report1", "--body", `[{"message":"Issue found","severity":"HIGH"}]`)
	if err != nil {
		t.Fatalf("unexpected error on add: %v", err)
	}
	if !strings.Contains(out, "Added 1 annotations to report report1") {
		t.Fatalf("expected annotation add confirmation, got:\n%s", out)
	}

	// List annotations
	out, err = executeInsights(t, server.URL, "annotation", "list", "commit1", "report1")
	if err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(out, "ann1") || !strings.Contains(out, "Issue found") {
		t.Fatalf("expected ann1 in list output, got:\n%s", out)
	}
}
