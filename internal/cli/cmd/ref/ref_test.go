package refcmd_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	refcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/ref"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func newMockRefServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/branches":
			if r.URL.Query().Get("filterText") == "empty" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
				return
			}
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/heads/main","displayId":"main"}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/tags":
			if r.URL.Query().Get("filterText") == "empty" {
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
				return
			}
			tagType := openapigenerated.RestTagType("ANNOTATED")
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/tags/v1.0","displayId":"v1.0","type":"` + string(tagType) + `"}]}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool) refcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	return refcmd.Dependencies{
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

func TestRefList(t *testing.T) {
	server := newMockRefServer(t)

	// Human mode
	deps := newTestDependencies(t, server.URL, false)
	cmd := refcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "main") || !strings.Contains(out, "v1.0") {
		t.Fatalf("expected 'main' and 'v1.0' in output, got: %s", out)
	}

	// JSON mode
	depsJSON := newTestDependencies(t, server.URL, true)
	cmd = refcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on json list: %v", err)
	}
	if !strings.Contains(buf.String(), "refs") {
		t.Fatalf("expected 'refs' in json output, got: %s", buf.String())
	}

	// Empty list
	cmd = refcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--filter", "empty"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on empty list: %v", err)
	}
	if !strings.Contains(buf.String(), "No refs found") {
		t.Fatalf("expected No refs found in output: %s", buf.String())
	}
}

func TestRefResolve(t *testing.T) {
	server := newMockRefServer(t)

	// Human mode
	deps := newTestDependencies(t, server.URL, false)
	cmd := refcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"resolve", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on resolve: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "refs/heads/main") {
		t.Fatalf("expected 'refs/heads/main' in output, got: %s", out)
	}

	// JSON mode
	depsJSON := newTestDependencies(t, server.URL, true)
	cmd = refcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"resolve", "v1.0"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on resolve json: %v", err)
	}
	if !strings.Contains(buf.String(), "refs/tags/v1.0") {
		t.Fatalf("expected 'refs/tags/v1.0' in json output, got: %s", buf.String())
	}

	// Not found error
	cmd = refcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"resolve", "nonexistent"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected error on nonexistent ref resolve")
	}
}
