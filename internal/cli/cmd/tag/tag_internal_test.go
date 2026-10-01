package tagcmd

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func TestTagDefaults(t *testing.T) {
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.DryRunEnabled == nil || d.DryRunEnabled() {
		t.Fatal("expected DryRunEnabled to default to false")
	}
	if d.WriteJSON == nil || d.WriteJSONList == nil {
		t.Fatal("expected WriteJSON and WriteJSONList to default to non-nil")
	}

	// The default loader is the configuration's own. The seal names no host,
	// so it fails the way the configuration does, where a stub would answer
	// or fail in words of its own. Reading BITBUCKET_URL is internal/config's
	// to test.
	if d.LoadConfig == nil || d.LoadConfigAndClient == nil {
		t.Fatal("expected LoadConfig and LoadConfigAndClient to default to non-nil")
	}
	if _, err := d.LoadConfig(); err == nil || !strings.Contains(err.Error(), "no Bitbucket host configured") {
		t.Fatalf("the default LoadConfig is not the configuration's own: %v", err)
	}
	if _, _, err := d.LoadConfigAndClient(); err == nil || !strings.Contains(err.Error(), "no Bitbucket host configured") {
		t.Fatalf("the default LoadConfigAndClient does not load through it: %v", err)
	}

	// Handed a configuration, the default LoadConfigAndClient builds a client for it.
	handed := Dependencies{LoadConfig: func() (config.AppConfig, error) {
		return config.AppConfig{BitbucketURL: "http://bitbucket.invalid"}, nil
	}}
	cfg, client, err := handed.withDefaults().LoadConfigAndClient()
	if err != nil || client == nil || cfg.BitbucketURL != "http://bitbucket.invalid" {
		t.Fatalf("unexpected LoadConfigAndClient: %v", err)
	}
}

func TestTagHelpers(t *testing.T) {
	t.Parallel()

	if safederef.String(nil) != "" {
		t.Fatal("expected empty string for safederef.String(nil)")
	}
	s := "v1.0.0"
	if safederef.String(&s) != "v1.0.0" {
		t.Fatal("expected v1.0.0 for safederef.String(&s)")
	}

	cfg := config.AppConfig{ProjectKey: "PRJ"}
	if _, err := resolveTagRepositoryReference("invalid-selector-no-slash", cfg); err == nil {
		t.Fatal("expected error for invalid repository selector")
	}
}
