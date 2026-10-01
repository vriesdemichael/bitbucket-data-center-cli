package admincmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
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
			return config.AppConfig{BitbucketURL: testsupport.RefusedURL}, nil
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
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.WriteJSON == nil {
		t.Fatal("expected WriteJSON to default to non-nil")
	}

	// The default loader is the configuration's own. The seal names no host,
	// so it fails the way the configuration does, where a stub would answer
	// or fail in words of its own. Reading BITBUCKET_URL is internal/config's
	// to test.
	if d.LoadConfig == nil {
		t.Fatal("expected LoadConfig to default to non-nil")
	}
	if _, err := d.LoadConfig(); err == nil || !strings.Contains(err.Error(), "no Bitbucket host configured") {
		t.Fatalf("the default LoadConfig is not the configuration's own: %v", err)
	}
}

// TestAdminHealthLimitedAuth is live now, in
// TestLiveAdminHealthReportsLimitedAuth: a real instance asked without
// credentials, and the reduced report that comes back. The unit version
// answered 401 to every request, so it asserted that our reader reads a status
// we wrote.
