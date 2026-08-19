package webhookcmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestWebhookCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks":
			_, _ = w.Write([]byte(`{"values":[{"id":123,"name":"wh","url":"http://url","active":true,"events":["repo:refs_changed"]}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/123":
			_, _ = w.Write([]byte(`{"id":123,"name":"wh","url":"http://url","active":true}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/123":
			_, _ = w.Write([]byte(`{"id":123,"name":"wh-new","url":"http://url","active":true}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/test":
			_, _ = w.Write([]byte(`{"status":"ok"}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks/123/statistics":
			_, _ = w.Write([]byte(`{"invocations":[]}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{
		BitbucketURL: server.URL,
		ProjectKey:   "PRJ",
	}

	deps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
	}

	// list
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list: %v", err)
	}
	if !strings.Contains(buf.String(), "wh") {
		t.Fatalf("expected wh in list output: %s", buf.String())
	}

	// get
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"get", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on get: %v", err)
	}
	if !strings.Contains(buf.String(), "wh") {
		t.Fatalf("expected wh in get output: %s", buf.String())
	}

	// update
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "123", "--name", "wh-new", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on update: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated webhook") {
		t.Fatalf("expected Updated webhook in update output: %s", buf.String())
	}

	// test
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"test", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on test: %v", err)
	}
	if !strings.Contains(buf.String(), "status") {
		t.Fatalf("expected status in test output: %s", buf.String())
	}

	// stats
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"stats", "123", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on stats: %v", err)
	}
	if !strings.Contains(buf.String(), "invocations") {
		t.Fatalf("expected invocations in stats output: %s", buf.String())
	}
}
