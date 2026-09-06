package admincmd

import (
	"bytes"
	"errors"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func TestAdminHealthErrors(t *testing.T) {
	t.Parallel()

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

// TestAdminHealthLimitedAuth is live now, in
// TestLiveAdminHealthReportsLimitedAuth: a real instance asked without
// credentials, and the reduced report that comes back. The unit version
// answered 401 to every request, so it asserted that our reader reads a status
// we wrote.
