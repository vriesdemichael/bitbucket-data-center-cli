//go:build live

package live_test

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestLiveARepositoryDoesNotChooseTheHost reproduces #770 against a real
// Bitbucket: a checkout whose .env and .bb/config.yaml both name another host,
// one nothing listens on. bb runs as a program of its own, from inside that
// checkout, so it finds the files as it does in use, and its warnings reach
// stderr as a person sees them. The .env also names the host in lower case,
// which Windows reads as the same variable, and sets BB_DISABLE_STORED_CONFIG,
// which would set aside the person's own file.
//
// The answer has to come from the host the person's own file names, which is
// the live instance; a command that went where the checkout pointed fails.
func TestLiveARepositoryDoesNotChooseTheHost(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	binary, err := liveCLIBinaryPath(t)
	if err != nil {
		t.Fatalf("build bb: %v", err)
	}

	const elsewhere = "http://127.0.0.1:9"
	checkout := t.TempDir()
	// .git marks the repository root, which is as far up as bb looks.
	for path, content := range map[string]string{
		".git/HEAD":       "ref: refs/heads/master\n",
		".env":            "BITBUCKET_URL=" + elsewhere + "\nbitbucket_url=" + elsewhere + "\nBB_DISABLE_STORED_CONFIG=1\n",
		".bb/config.yaml": "default_host: " + elsewhere + "\n",
	} {
		full := filepath.Join(checkout, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("make %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	own := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(own, []byte("default_host: "+harness.config.BitbucketURL+"\n"), 0o600); err != nil {
		t.Fatalf("write the person's own file: %v", err)
	}

	// The process environment, without anything that names a host or a file
	// to take one from, and with the suite's credential.
	environment := []string{"BB_CONFIG_PATH=" + own}
	for _, entry := range os.Environ() {
		name, _, _ := strings.Cut(entry, "=")
		switch strings.ToUpper(name) {
		case "BITBUCKET_URL", "BB_CONFIG_PATH", "BB_WORKSPACE_CONFIG_PATH", "BB_DISABLE_STORED_CONFIG",
			"BITBUCKET_TOKEN", "BITBUCKET_USERNAME", "BITBUCKET_USER", "BITBUCKET_PASSWORD",
			"BITBUCKET_PROJECT_KEY", "BITBUCKET_REPO_SLUG":
			continue
		}
		environment = append(environment, entry)
	}
	if token := strings.TrimSpace(harness.config.BitbucketToken); token != "" {
		environment = append(environment, "BITBUCKET_TOKEN="+token)
	} else {
		environment = append(environment, "BITBUCKET_USERNAME="+harness.config.BitbucketUsername, "BITBUCKET_PASSWORD="+harness.config.BitbucketPassword)
	}

	command := exec.CommandContext(ctx, binary, "--json", "project", "get", seeded.Key)
	command.Dir = checkout
	command.Env = environment
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	if err := command.Run(); err != nil {
		t.Fatalf("bb project get from the checkout failed, so it did not reach the person's host: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	if project := nestedJSONMap(t, stdout.String(), "project"); project["key"] != seeded.Key {
		t.Errorf("the answer names project %v, want %s from the person's host", project["key"], seeded.Key)
	}
	for _, said := range []string{"ignored BITBUCKET_URL in ", "ignored bitbucket_url in ", "ignored BB_DISABLE_STORED_CONFIG in ", ".env", "ignored default_host in ", "config.yaml"} {
		if !strings.Contains(stderr.String(), said) {
			t.Errorf("stderr does not say %q:\n%s", said, stderr.String())
		}
	}
}
