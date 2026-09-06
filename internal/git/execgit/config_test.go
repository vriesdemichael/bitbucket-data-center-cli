package execgit

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
)

// newTestRepo initialises a throwaway repository. Every git test must operate
// on a directory it created: a test that shells out to git without -C can write
// into the repository the suite is running inside, which has previously
// persisted a credential into the project's own .git/config.
func newTestRepo(t *testing.T) string {
	t.Helper()

	directory := t.TempDir()
	backend := New()
	if _, err := backend.run(context.Background(), runOptions{args: []string{"-C", directory, "init"}}); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	return directory
}

func TestGetConfigReturnsEmptyForUnsetKey(t *testing.T) {
	t.Parallel()

	backend := New()
	directory := newTestRepo(t)

	// git exits 1 for an absent key. This must read as "not configured" rather
	// than as a failure, otherwise every "is this already set?" check errors.
	value, err := backend.GetConfig(context.Background(), git.ConfigOptions{
		Directory: directory,
		Scope:     git.ConfigScopeLocal,
		Key:       "credential.https://bitbucket.example.com.helper",
	})
	if err != nil {
		t.Fatalf("expected unset key to be a non-error, got %v", err)
	}
	if value != "" {
		t.Fatalf("expected empty value for unset key, got %q", value)
	}
}

func TestSetGetUnsetConfigRoundTrip(t *testing.T) {
	t.Parallel()

	backend := New()
	directory := newTestRepo(t)
	ctx := context.Background()

	options := git.ConfigOptions{
		Directory: directory,
		Scope:     git.ConfigScopeLocal,
		Key:       "credential.https://bitbucket.example.com.helper",
		Value:     "!/usr/local/bin/bb auth git-credential",
	}

	if err := backend.SetConfig(ctx, options); err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}

	value, err := backend.GetConfig(ctx, options)
	if err != nil {
		t.Fatalf("GetConfig failed: %v", err)
	}
	if value != options.Value {
		t.Fatalf("expected %q, got %q", options.Value, value)
	}

	if err := backend.UnsetConfig(ctx, options); err != nil {
		t.Fatalf("UnsetConfig failed: %v", err)
	}

	value, err = backend.GetConfig(ctx, options)
	if err != nil {
		t.Fatalf("GetConfig after unset failed: %v", err)
	}
	if value != "" {
		t.Fatalf("expected key to be gone, got %q", value)
	}
}

func TestUnsetConfigIsIdempotent(t *testing.T) {
	t.Parallel()

	backend := New()
	directory := newTestRepo(t)

	// Remediation runs unconditionally over repositories that may never have
	// had the key, so removing an absent key must succeed.
	err := backend.UnsetConfig(context.Background(), git.ConfigOptions{
		Directory: directory,
		Scope:     git.ConfigScopeLocal,
		Key:       "http.extraHeader",
	})
	if err != nil {
		t.Fatalf("expected unsetting an absent key to succeed, got %v", err)
	}
}

func TestConfigRejectsLocalScopeWithoutDirectory(t *testing.T) {
	t.Parallel()

	backend := New()

	// Without a directory git would operate on the ambient repository, which is
	// exactly the failure this guards against.
	_, err := backend.GetConfig(context.Background(), git.ConfigOptions{
		Scope: git.ConfigScopeLocal,
		Key:   "credential.helper",
	})
	if err == nil {
		t.Fatal("expected an error when local scope has no directory")
	}
	if !strings.Contains(err.Error(), "directory cannot be empty") {
		t.Fatalf("expected a directory validation error, got %v", err)
	}
}

func TestConfigRejectsEmptyKey(t *testing.T) {
	t.Parallel()

	backend := New()

	if err := backend.SetConfig(context.Background(), git.ConfigOptions{
		Directory: t.TempDir(),
		Scope:     git.ConfigScopeLocal,
		Value:     "whatever",
	}); err == nil {
		t.Fatal("expected an error for an empty config key")
	}
}

func TestConfigRejectsAnUnsupportedScope(t *testing.T) {
	t.Parallel()

	backend := New()

	// An unrecognised scope must fail rather than silently defaulting, since
	// the default would be whichever repository the process is standing in.
	_, err := backend.GetConfig(context.Background(), git.ConfigOptions{
		Scope: git.ConfigScope("system"),
		Key:   "credential.helper",
	})
	if err == nil {
		t.Fatal("expected an unsupported scope to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported git config scope") {
		t.Fatalf("expected a scope validation error, got %v", err)
	}
}

func TestGlobalScopeConfigRoundTrip(t *testing.T) {
	// GIT_CONFIG_GLOBAL keeps this out of the developer's ~/.gitconfig. It
	// survives ScopeFreeEnv, which strips only git's repository-scoping vars.
	globalConfig := t.TempDir() + "/gitconfig"
	if err := os.WriteFile(globalConfig, nil, 0o600); err != nil {
		t.Fatalf("create global config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)

	backend := New()
	ctx := context.Background()
	options := git.ConfigOptions{
		Scope: git.ConfigScopeGlobal,
		Key:   "bb.globalscope.probe",
		Value: "set",
	}

	if err := backend.SetConfig(ctx, options); err != nil {
		t.Fatalf("SetConfig global: %v", err)
	}

	value, err := backend.GetConfig(ctx, options)
	if err != nil || value != "set" {
		t.Fatalf("GetConfig global = %q, %v", value, err)
	}

	// Append is what makes reset-then-add possible on a multi-valued key.
	appended := options
	appended.Value = "second"
	appended.Append = true
	if err := backend.SetConfig(ctx, appended); err != nil {
		t.Fatalf("SetConfig append: %v", err)
	}

	// GetConfig reports the effective value, which for a multi-valued key is
	// the last one.
	value, err = backend.GetConfig(ctx, options)
	if err != nil || value != "second" {
		t.Fatalf("expected the last value after append, got %q, %v", value, err)
	}

	if err := backend.UnsetConfig(ctx, options); err != nil {
		t.Fatalf("UnsetConfig global: %v", err)
	}
	if value, err := backend.GetConfig(ctx, options); err != nil || value != "" {
		t.Fatalf("expected the key to be gone, got %q, %v", value, err)
	}
}
