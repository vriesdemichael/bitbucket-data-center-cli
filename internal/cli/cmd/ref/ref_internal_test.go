package refcmd

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

func TestRefInternalHelpers(t *testing.T) {
	t.Parallel()

	if safederef.String(nil) != "" {
		t.Fatal("expected safederef.String(nil) to be empty")
	}
	s := "test"
	if safederef.String(&s) != "test" {
		t.Fatal("expected safederef.String(&s) to be test")
	}
}

func TestRefInternalWithDefaults(t *testing.T) {
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
	if d.LoadConfigAndClient != nil {
		cfg, client, err := d.LoadConfigAndClient()
		if err != nil || client == nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfigAndClient result: %v", err)
		}
	}
}

func TestResolveRefRepositoryReference(t *testing.T) {
	cfg := config.AppConfig{
		ProjectKey: "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "repo1")

	// Inferred
	ref, err := resolveRefRepositoryReference("", cfg)
	if err != nil || ref.ProjectKey != "PRJ" || ref.Slug != "repo1" {
		t.Fatalf("unexpected inferred repo ref: %+v, %v", ref, err)
	}

	// Explicit
	ref, err = resolveRefRepositoryReference("OTHER/repo2", cfg)
	if err != nil || ref.ProjectKey != "OTHER" || ref.Slug != "repo2" {
		t.Fatalf("unexpected explicit repo ref: %+v, %v", ref, err)
	}
}
