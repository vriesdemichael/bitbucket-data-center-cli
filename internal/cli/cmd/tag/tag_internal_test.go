package tagcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func TestTagDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
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
	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig: %v", err)
		}
	}
	if d.LoadConfigAndClient != nil {
		cfg, client, err := d.LoadConfigAndClient()
		if err != nil || client == nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfigAndClient: %v", err)
		}
	}
}

func TestTagHelpers(t *testing.T) {
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
