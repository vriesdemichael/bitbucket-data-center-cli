//go:build live

package live_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveGPGKeyLifecycle covers bb auth gpg-key add/list/remove/clear.
//
// Bitbucket parses the armored key rather than storing it as text, so this is
// one of the cases a stub cannot stand in for: a malformed block, or one bb
// mangles on the way out, is rejected by the server and by nothing else.
func TestLiveGPGKeyLifecycle(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// These keys hang off the authenticated account rather than a project, so
	// the test clears them at the end whatever happens: a leftover key makes the
	// next run fail on a duplicate rather than on anything it is testing.
	t.Cleanup(func() {
		_, _ = executeLiveCLI(t, "--json", "auth", "gpg-key", "clear", "--yes")
	})
	if _, err := executeLiveCLI(t, "--json", "auth", "gpg-key", "clear", "--yes"); err != nil {
		t.Fatalf("clearing GPG keys before the test failed: %v", err)
	}

	keyPath, err := filepath.Abs(filepath.Join("testdata", "live-suite-gpg-public-key.asc"))
	if err != nil {
		t.Fatalf("resolve the fixture key path failed: %v", err)
	}
	if _, err := os.Stat(keyPath); err != nil {
		t.Fatalf("fixture key missing: %v", err)
	}

	addOutput, err := executeLiveCLI(t, "--json", "auth", "gpg-key", "add", keyPath)
	if err != nil {
		t.Fatalf("auth gpg-key add failed: %v\noutput: %s", err, addOutput)
	}

	listOutput, err := executeLiveCLI(t, "--json", "auth", "gpg-key", "list", "--all")
	if err != nil {
		t.Fatalf("auth gpg-key list failed: %v\noutput: %s", err, listOutput)
	}
	// The fingerprint is computed by the server from the key it parsed, so seeing
	// one back is what says the key arrived intact.
	if !strings.Contains(listOutput, "fingerprint") {
		t.Fatalf("expected the added key in the listing, got: %s", listOutput)
	}

	keyID, ok := numericOrStringID(firstOfJSONArray(t, addOutput)["id"])
	if !ok {
		// Bitbucket identifies GPG keys by fingerprint rather than by a numeric
		// id, which remove accepts as well.
		keyID = gpgFingerprintFrom(t, listOutput)
	}

	if _, err := executeLiveCLI(t, "--json", "auth", "gpg-key", "remove", keyID); err != nil {
		t.Fatalf("auth gpg-key remove failed: %v", err)
	}

	afterRemove, err := executeLiveCLI(t, "--json", "auth", "gpg-key", "list", "--all")
	if err != nil {
		t.Fatalf("auth gpg-key list after remove failed: %v\noutput: %s", err, afterRemove)
	}
	if strings.Contains(afterRemove, keyID) {
		t.Fatalf("expected the key to be gone after remove, got: %s", afterRemove)
	}

	// clear is a separate endpoint from remove, so it needs a key of its own to
	// take away rather than being trusted because remove worked.
	if _, err := executeLiveCLI(t, "--json", "auth", "gpg-key", "add", keyPath); err != nil {
		t.Fatalf("auth gpg-key add before clear failed: %v", err)
	}
	if _, err := executeLiveCLI(t, "--json", "auth", "gpg-key", "clear", "--yes"); err != nil {
		t.Fatalf("auth gpg-key clear failed: %v", err)
	}

	afterClear, err := executeLiveCLI(t, "--json", "auth", "gpg-key", "list", "--all")
	if err != nil {
		t.Fatalf("auth gpg-key list after clear failed: %v\noutput: %s", err, afterClear)
	}
	if strings.Contains(afterClear, "fingerprint") {
		t.Fatalf("expected no keys left after clear, got: %s", afterClear)
	}

	// The human rendering of an empty list, against a list that is empty
	// because clear emptied it. A unit test held this against a handwritten
	// empty page, which cannot tell "no keys" from "the shape changed".
	humanAfterClear, err := executeLiveCLI(t, "auth", "gpg-key", "list", "--all")
	if err != nil {
		t.Fatalf("auth gpg-key list (human) after clear failed: %v\noutput: %s", err, humanAfterClear)
	}
	if !strings.Contains(humanAfterClear, "No GPG keys found") {
		t.Fatalf("expected the empty-list message, got: %s", humanAfterClear)
	}
}

// gpgFingerprintFrom pulls the first fingerprint out of a gpg-key list payload.
func gpgFingerprintFrom(t *testing.T, output string) string {
	t.Helper()

	const marker = `"fingerprint": "`
	start := strings.Index(output, marker)
	if start < 0 {
		t.Fatalf("no fingerprint in the listing: %s", output)
	}
	rest := output[start+len(marker):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		t.Fatalf("unterminated fingerprint in the listing: %s", output)
	}
	return rest[:end]
}

// TestLiveAuthSetupGit covers bb auth setup-git, which writes a git credential
// helper entry rather than calling Bitbucket.
//
// It is covered live because what it writes has to be something git accepts:
// the config key is scoped to the host, and a wrong scope silently applies the
// helper to every host or to none.
func TestLiveAuthSetupGit(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A repository of its own to write into, so the developer's global git
	// config is not what the test edits.
	workDir := t.TempDir()
	if err := runGit(workDir, "init"); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	originalDirectory, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd failed: %v", err)
	}
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir into the work directory failed: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalDirectory)
	})

	setupOutput, err := executeLiveCLI(t, "--json", "auth", "setup-git", "--global=false")
	if err != nil {
		t.Fatalf("auth setup-git failed: %v\noutput: %s", err, setupOutput)
	}

	configOutput, err := runGitCapture(workDir, "config", "--local", "--list")
	if err != nil {
		t.Fatalf("read the local git config failed: %v", err)
	}
	// The helper has to be scoped to the Bitbucket host: an unscoped helper
	// would offer these credentials to every host git talks to.
	if !strings.Contains(configOutput, "credential.") || !strings.Contains(configOutput, ".helper=") {
		t.Fatalf("expected a credential helper in the git config, got: %s", configOutput)
	}
	if !strings.Contains(configOutput, "localhost") {
		t.Fatalf("expected the helper to be scoped to the Bitbucket host, got: %s", configOutput)
	}

	// Running it again over an existing entry is refused rather than silently
	// duplicating or replacing it.
	repeatOutput, repeatErr := executeLiveCLI(t, "--json", "auth", "setup-git", "--global=false")
	if repeatErr == nil && !strings.Contains(repeatOutput, "already") {
		forcedOutput, forcedErr := executeLiveCLI(t, "--json", "auth", "setup-git", "--global=false", "--force")
		if forcedErr != nil {
			t.Fatalf("auth setup-git --force failed: %v\noutput: %s", forcedErr, forcedOutput)
		}
	}
}
