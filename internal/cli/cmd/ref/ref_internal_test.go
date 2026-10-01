package refcmd

import (
	"strings"
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
		t.Fatalf("unexpected LoadConfigAndClient result: %v", err)
	}
}

func TestResolveRefRepositoryReference(t *testing.T) {
	// The slug travels in the configuration beside the project key. It used to
	// be read from BITBUCKET_REPO_SLUG one layer down, past the layer that
	// resolves everything else.
	cfg := config.AppConfig{
		ProjectKey: "PRJ",
		RepoSlug:   "repo1",
	}

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
