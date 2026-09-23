package completion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The instances this machine is logged in to, which is a question the stored
// configuration answers and the server cannot.
//
// Asking an instance which instances exist would be both slower and wrong: one
// that is down is still one you may want to name, and naming it is how you
// reach the others.

// storedHosts writes a configuration with three instances and points the
// process at it: two logged in with a password, one with a token, which is
// the kind that stores no user name.
func storedHosts(t *testing.T) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	contents := strings.Join([]string{
		"default_host: https://bitbucket.example.com",
		"hosts:",
		"  https://bitbucket.example.com:",
		"    url: https://bitbucket.example.com",
		"    username: alice",
		"    auth_mode: basic",
		"    aliases:",
		"      - bitbucket.internal",
		"  https://bitbucket.other.com:",
		"    url: https://bitbucket.other.com",
		"    username: bob",
		"    auth_mode: basic",
		"  https://bitbucket.tokens.com:",
		"    url: https://bitbucket.tokens.com",
		"    auth_mode: token",
		"",
	}, "\n")

	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing the stored configuration failed: %v", err)
	}

	t.Setenv("BB_CONFIG_PATH", path)
}

// TestTheHostsYouAreLoggedInToAreOffered covers --host and `bb auth server
// use`, and the ordering that puts the default first.
//
// A shell sorts what it is given unless told otherwise, and the default
// instance is the one a caller means most of the time -- so the source ranks
// it and asks for the ranking to be kept.
func TestTheHostsYouAreLoggedInToAreOffered(t *testing.T) {
	storedHosts(t)

	result, err := hostSource(context.Background(), nil, Request{})
	if err != nil {
		t.Fatalf("listing the stored hosts failed: %v", err)
	}

	if len(result.Candidates) != 3 {
		t.Fatalf("expected every stored instance, got %v", result.Candidates)
	}
	if result.Candidates[0].Value != "https://bitbucket.example.com" {
		t.Errorf("expected the default instance first, got %v", result.Candidates)
	}
	if !result.KeepOrder {
		t.Error("expected the ranking to be kept; a shell would otherwise sort the default away")
	}

	// Every instance described, so none sits blank beside the others.
	for _, want := range []Candidate{
		{Value: "https://bitbucket.example.com", Description: "alice (default)"},
		{Value: "https://bitbucket.other.com", Description: "bob"},
		{Value: "https://bitbucket.tokens.com", Description: "access token"},
	} {
		if got := descriptionOf(result.Candidates, want.Value); got != want.Description {
			t.Errorf("%s was described as %q, want %q", want.Value, got, want.Description)
		}
	}
}

// TestAnAliasIsOfferedWithTheInstanceItNames covers `bb auth alias remove`.
//
// An alias exists because a git remote spells a host differently from the
// configuration, so the ones worth offering are the ones already recorded --
// and which instance each belongs to is the whole reason to show a
// description.
func TestAnAliasIsOfferedWithTheInstanceItNames(t *testing.T) {
	storedHosts(t)

	result, err := hostAliasSource(context.Background(), nil, Request{})
	if err != nil {
		t.Fatalf("listing the stored aliases failed: %v", err)
	}

	if len(result.Candidates) != 1 {
		t.Fatalf("expected the one recorded alias, got %v", result.Candidates)
	}
	if result.Candidates[0].Value != "bitbucket.internal" {
		t.Errorf("expected the alias, got %q", result.Candidates[0].Value)
	}
	if !strings.Contains(result.Candidates[0].Description, "bitbucket.example.com") {
		t.Errorf("expected the instance the alias names, got %q", result.Candidates[0].Description)
	}
}

// TestAMachineWithNoStoredInstancesCompletesNothing is the ordinary state of a
// machine that uses BITBUCKET_URL and a token from the environment.
func TestAMachineWithNoStoredInstancesCompletesNothing(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))

	result, err := hostSource(context.Background(), nil, Request{})
	if err != nil {
		t.Fatalf("expected an absent configuration to be no error, got %v", err)
	}
	if len(result.Candidates) != 0 {
		t.Errorf("expected nothing offered, got %v", result.Candidates)
	}
}
