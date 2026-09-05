package admincmd

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

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
