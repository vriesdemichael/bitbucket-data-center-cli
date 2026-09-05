package searchcmd

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
)

func TestSearchDefaultsAndSafeString(t *testing.T) {
	if safederef.String(nil) != "" {
		t.Fatal("expected empty string for safederef.String(nil)")
	}
	s := "test"
	if safederef.String(&s) != "test" {
		t.Fatal("expected test for safederef.String(&s)")
	}

	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.WriteJSONList == nil {
		t.Fatal("expected WriteJSONList to default to non-nil")
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
