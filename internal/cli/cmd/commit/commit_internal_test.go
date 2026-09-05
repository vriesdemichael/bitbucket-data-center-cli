package commitcmd

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
)

func TestCommitSafeHelpers(t *testing.T) {
	if safederef.String(nil) != "" {
		t.Fatal("expected empty string for safederef.String(nil)")
	}
	s := "commit1"
	if safederef.String(&s) != "commit1" {
		t.Fatal("expected commit1 for safederef.String(&s)")
	}
}

func TestCommitDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
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

// TestCommitListEmptyState is live now, in TestLivePRInspectionEmptyResults:
// `commit list --path no/such/path.txt` against a real repository, which is
// the cheapest genuinely empty answer Bitbucket will give. The unit version
// held the message against an empty page it wrote itself, so it agreed that
// the page was empty by construction.
