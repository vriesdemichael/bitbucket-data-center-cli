package admincmd

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func newMockAdminServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/rest/api/1.0/projects" || r.URL.Path == "/rest/api/latest/projects" {
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	return server
}

func executeAdmin(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var jsonFlag bool
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		}
	}

	deps := Dependencies{
		JSONEnabled: func() bool { return jsonFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON: jsonoutput.Write,
	}

	root := &cobra.Command{Use: "bb"}
	root.PersistentFlags().Bool("json", false, "")
	root.AddCommand(New(deps))

	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)

	fullArgs := append([]string{"admin"}, args...)
	root.SetArgs(fullArgs)
	err := root.Execute()
	return buffer.String(), err
}

func TestAdminHealth(t *testing.T) {
	server := newMockAdminServer(t)

	out, err := executeAdmin(t, server.URL, "health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Bitbucket health: OK") {
		t.Fatalf("expected health OK, got:\n%s", out)
	}
}

func TestAdminHealthJSON(t *testing.T) {
	server := newMockAdminServer(t)

	out, err := executeAdmin(t, server.URL, "health", "--json")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, `"authenticated": true`) && !strings.Contains(out, `"authenticated":true`) {
		t.Fatalf("expected JSON authenticated, got:\n%s", out)
	}
}

func TestAdminHealthLimitedAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"message":"Unauthorized"}]}`))
	}))
	t.Cleanup(server.Close)

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "")

	deps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{BitbucketURL: server.URL}, nil
		},
	}

	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"health"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on limited auth: %v", err)
	}
	if !strings.Contains(buf.String(), "auth=limited") {
		t.Fatalf("expected auth=limited in output: %s", buf.String())
	}
}

func TestAdminHealthErrors(t *testing.T) {
	// Error on LoadConfig
	badDeps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{}, errors.New("load config error")
		},
	}
	cmd := New(badDeps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"health"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error on load config failure")
	}

	// Error on health check network failure
	netErrDeps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{BitbucketURL: "http://127.0.0.1:1"}, nil
		},
	}
	cmd = New(netErrDeps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"health"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected network error on invalid host")
	}
}

func TestAdminDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.WriteJSON == nil {
		t.Fatal("expected WriteJSON to default to non-nil")
	}
	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig result: %v", err)
		}
	}
}
