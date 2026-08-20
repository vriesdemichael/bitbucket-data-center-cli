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

func TestInsightsReportSetAndGet(t *testing.T) {
	server := newMockInsightsServer(t)

	// 1. Set report real execution
	out, err := executeInsights(t, server.URL, "report", "set", "commit1", "report1", "--body", `{"title":"Report 1","result":"PASS"}`)
	if err != nil {
		t.Fatalf("unexpected error on set: %v", err)
	}
	if !strings.Contains(out, "report1") {
		t.Fatalf("expected report1 in set output, got:\n%s", out)
	}

	// 2. Set report in dry-run mode (update when report exists)
	out, err = executeInsights(t, server.URL, "--dry-run", "report", "set", "commit1", "report1", "--body", `{"title":"Report 1","result":"PASS"}`)
	if err != nil {
		t.Fatalf("unexpected error on set dry-run update: %v", err)
	}

	// 3. Set report in dry-run mode (create when report does not exist)
	out, err = executeInsights(t, server.URL, "--dry-run", "report", "set", "commit1", "report-empty", "--body", `{"title":"New Report","result":"PASS"}`)
	if err != nil {
		t.Fatalf("unexpected error on set dry-run create: %v", err)
	}

	// 4. Get report in JSON mode
	out, err = executeInsights(t, server.URL, "--json", "report", "get", "commit1", "report1")
	if err != nil {
		t.Fatalf("unexpected error on get JSON: %v", err)
	}
	if !strings.Contains(out, `"key"`) {
		t.Fatalf("expected key in get JSON output, got:\n%s", out)
	}
}

func TestInsightsReportList(t *testing.T) {
	server := newMockInsightsServer(t)

	// Human mode
	out, err := executeInsights(t, server.URL, "report", "list", "commit1")
	if err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(out, "report1") || !strings.Contains(out, "Report 1") {
		t.Fatalf("expected report1 in list output, got:\n%s", out)
	}

	// JSON mode
	out, err = executeInsights(t, server.URL, "--json", "report", "list", "commit1")
	if err != nil {
		t.Fatalf("unexpected error on list JSON: %v", err)
	}
	if !strings.Contains(out, "report1") {
		t.Fatalf("expected report1 in JSON list output, got:\n%s", out)
	}
}

func TestInsightsReportDelete(t *testing.T) {
	server := newMockInsightsServer(t)

	// Delete in dry-run mode (report exists -> delete)
	out, err := executeInsights(t, server.URL, "--dry-run", "report", "delete", "commit1", "report1")
	if err != nil {
		t.Fatalf("unexpected error on delete dry-run: %v", err)
	}

	// Delete in dry-run mode (report not found -> no-op)
	out, err = executeInsights(t, server.URL, "--dry-run", "report", "delete", "commit1", "report-empty")
	if err != nil {
		t.Fatalf("unexpected error on delete dry-run not-found: %v", err)
	}

	// Delete for real (human & JSON)
	out, err = executeInsights(t, server.URL, "report", "delete", "commit1", "report1")
	if err != nil {
		t.Fatalf("unexpected error on delete: %v", err)
	}
	if !strings.Contains(out, "Deleted report report1 for commit commit1") {
		t.Fatalf("expected delete confirmation, got:\n%s", out)
	}

	out, err = executeInsights(t, server.URL, "--json", "report", "delete", "commit1", "report1")
	if err != nil {
		t.Fatalf("unexpected error on delete JSON: %v", err)
	}
	if !strings.Contains(out, "status") {
		t.Fatalf("expected status in delete JSON, got:\n%s", out)
	}
}

func TestInsightsAnnotationAddAndList(t *testing.T) {
	server := newMockInsightsServer(t)

	// 1. Add annotations in dry-run mode
	out, err := executeInsights(t, server.URL, "--dry-run", "annotation", "add", "commit1", "report1", "--body", `[{"message":"Issue found","severity":"HIGH"}]`)
	if err != nil {
		t.Fatalf("unexpected error on add dry-run: %v", err)
	}

	// 2. Add annotations (human & JSON)
	out, err = executeInsights(t, server.URL, "annotation", "add", "commit1", "report1", "--body", `[{"message":"Issue found","severity":"HIGH"}]`)
	if err != nil {
		t.Fatalf("unexpected error on add: %v", err)
	}
	if !strings.Contains(out, "Added 1 annotations to report report1") {
		t.Fatalf("expected annotation add confirmation, got:\n%s", out)
	}

	out, err = executeInsights(t, server.URL, "--json", "annotation", "add", "commit1", "report1", "--body", `[{"message":"Issue found","severity":"HIGH"}]`)
	if err != nil {
		t.Fatalf("unexpected error on add JSON: %v", err)
	}
	if !strings.Contains(out, "count") {
		t.Fatalf("expected count in add JSON output, got:\n%s", out)
	}

	// 3. Set single annotation with all flags (dry-run create & update, real, JSON)
	out, err = executeInsights(t, server.URL, "--dry-run", "annotation", "set", "commit1", "report1", "ann1", "--message", "Issue found", "--severity", "HIGH")
	if err != nil {
		t.Fatalf("unexpected error on set annotation dry-run update: %v", err)
	}

	out, err = executeInsights(t, server.URL, "--dry-run", "annotation", "set", "commit1", "report1", "ann-new", "--message", "New issue", "--severity", "LOW")
	if err != nil {
		t.Fatalf("unexpected error on set annotation dry-run create: %v", err)
	}

	out, err = executeInsights(t, server.URL, "annotation", "set", "commit1", "report1", "ann1",
		"--message", "Issue found",
		"--severity", "HIGH",
		"--path", "main.go",
		"--line", "42",
		"--link", "http://ci.example.com",
		"--type", "BUG",
	)
	if err != nil {
		t.Fatalf("unexpected error on set annotation: %v", err)
	}
	if !strings.Contains(out, "Annotation ann1 set on report report1 for commit commit1") {
		t.Fatalf("expected Annotation set confirmation, got:\n%s", out)
	}

	out, err = executeInsights(t, server.URL, "--json", "annotation", "set", "commit1", "report1", "ann1", "--message", "Issue found", "--severity", "HIGH")
	if err != nil {
		t.Fatalf("unexpected error on set annotation JSON: %v", err)
	}
	if !strings.Contains(out, "ann1") {
		t.Fatalf("expected ann1 in set annotation JSON, got:\n%s", out)
	}

	// 4. List report annotations (human & JSON)
	out, err = executeInsights(t, server.URL, "annotation", "list", "commit1", "report1")
	if err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(out, "ann1") || !strings.Contains(out, "Issue found") {
		t.Fatalf("expected ann1 in list output, got:\n%s", out)
	}

	out, err = executeInsights(t, server.URL, "--json", "annotation", "list", "commit1", "report1")
	if err != nil {
		t.Fatalf("unexpected error on list JSON: %v", err)
	}
	if !strings.Contains(out, "ann1") {
		t.Fatalf("expected ann1 in JSON list output, got:\n%s", out)
	}

	// 5. List commit-level annotations (without report key)
	out, err = executeInsights(t, server.URL, "annotation", "list", "commit1")
	if err != nil {
		t.Fatalf("unexpected error on list commit annotations: %v", err)
	}
	if !strings.Contains(out, "ann1") {
		t.Fatalf("expected ann1 in commit annotations output, got:\n%s", out)
	}

	// 6. Delete annotations (dry-run matching, dry-run no-op, real, JSON)
	out, err = executeInsights(t, server.URL, "--dry-run", "annotation", "delete", "commit1", "report1", "--external-id", "ann1")
	if err != nil {
		t.Fatalf("unexpected error on delete annotation dry-run: %v", err)
	}

	out, err = executeInsights(t, server.URL, "--dry-run", "annotation", "delete", "commit1", "report1", "--external-id", "ann-nonexistent")
	if err != nil {
		t.Fatalf("unexpected error on delete annotation dry-run no-op: %v", err)
	}

	out, err = executeInsights(t, server.URL, "annotation", "delete", "commit1", "report1", "--external-id", "ann1")
	if err != nil {
		t.Fatalf("unexpected error on delete annotation: %v", err)
	}
	if !strings.Contains(out, "Deleted annotations for external id ann1") {
		t.Fatalf("expected delete annotation confirmation, got:\n%s", out)
	}

	out, err = executeInsights(t, server.URL, "--json", "annotation", "delete", "commit1", "report1", "--external-id", "ann1")
	if err != nil {
		t.Fatalf("unexpected error on delete annotation JSON: %v", err)
	}
	if !strings.Contains(out, "status") {
		t.Fatalf("expected status in delete annotation JSON, got:\n%s", out)
	}
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
