package tagcmd

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestTagInternalHelpers(t *testing.T) {
	if safeString(nil) != "" {
		t.Fatal("expected safeString(nil) to be empty")
	}
	s := "test"
	if safeString(&s) != "test" {
		t.Fatal("expected safeString(&s) to be test")
	}

	if safeStringFromTagType(nil) != "" {
		t.Fatal("expected safeStringFromTagType(nil) to be empty")
	}
	tagType := openapigenerated.RestTagTypeTAG
	if safeStringFromTagType(&tagType) != "TAG" {
		t.Fatal("expected safeStringFromTagType to return TAG")
	}
}

func TestTagInternalWithDefaults(t *testing.T) {
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
		t.Fatal("expected write functions to default")
	}

	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig result: %v", err)
		}
	}
	if d.LoadConfigAndClient != nil {
		cfg, client, err := d.LoadConfigAndClient()
		if err != nil || client == nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfigAndClient result: %v", err)
		}
	}
}

func TestResolveTagRepositoryReference(t *testing.T) {
	cfg := config.AppConfig{
		ProjectKey: "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "repo1")

	// Inferred
	ref, err := resolveTagRepositoryReference("", cfg)
	if err != nil || ref.ProjectKey != "PRJ" || ref.Slug != "repo1" {
		t.Fatalf("unexpected inferred repo ref: %+v, %v", ref, err)
	}

	// Explicit
	ref, err = resolveTagRepositoryReference("OTHER/repo2", cfg)
	if err != nil || ref.ProjectKey != "OTHER" || ref.Slug != "repo2" {
		t.Fatalf("unexpected explicit repo ref: %+v, %v", ref, err)
	}
}
