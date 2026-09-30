package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// actAsBB is set in the environment of a git this package starts, so that the
// helper line bb auth setup-git writes -- which names os.Executable(), this
// test binary -- runs bb rather than the tests. TestMain reads it.
const actAsBB = "BB_TEST_ACT_AS_BB"

// identityTestHost is the Bitbucket both logins below are for.
const identityTestHost = "https://bitbucket.example.com"

// writeIdentityLogin writes a stored configuration holding one login for
// identityTestHost, its secret kept in the file so no keyring is involved.
func writeIdentityLogin(t *testing.T, directory, name, username, token string) string {
	t.Helper()

	path := filepath.Join(directory, name)
	stored := strings.Join([]string{
		"default_host: " + identityTestHost,
		"hosts:",
		"  " + identityTestHost + ":",
		"    url: " + identityTestHost,
		"    username: " + username,
		"insecure_secrets:",
		"  " + identityTestHost + ":",
		"    token: " + token,
		"",
	}, "\n")
	if err := os.WriteFile(path, []byte(stored), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}

	return path
}

// TestSetupGitPinsTheIdentityGitAuthenticatesAs is #733.
//
// Two logins for one host live in two configuration files, which the keyring
// tells apart since #587. The helper line setup-git wrote named bb and nothing
// else, so the git-credential it ran read whichever BB_CONFIG_PATH was in
// git's environment at the time, and git pushed as that identity rather than
// the one set up.
//
// Everything here is what the user runs: setup-git writes the line into a git
// configuration of its own, and git itself runs the line to answer `git
// credential fill`, with the other login's file in its environment.
func TestSetupGitPinsTheIdentityGitAuthenticatesAs(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Fatalf("git is not on PATH, and this test is git running bb: %v", err)
	}

	directory := t.TempDir()
	alice := writeIdentityLogin(t, directory, "alice.yaml", "alice", "alice-token")
	bob := writeIdentityLogin(t, directory, "bob.yaml", "bob", "bob-token")

	// A global git configuration of the test's own, and no system one, so the
	// user's credential helpers are neither consulted nor touched.
	globalConfig := filepath.Join(directory, "gitconfig")
	if err := os.WriteFile(globalConfig, nil, 0o600); err != nil {
		t.Fatalf("write git config: %v", err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")

	setupGit := func(configPath string, extra ...string) (int, string) {
		t.Helper()
		t.Setenv("BB_CONFIG_PATH", configPath)

		var stdout, stderr bytes.Buffer
		code := run(append([]string{"auth", "setup-git", "--host", identityTestHost}, extra...), &stdout, &stderr)

		return code, stdout.String() + stderr.String()
	}

	// fill asks git for the credential with ambient as BB_CONFIG_PATH, which
	// is what a shell that exported it for other work hands git.
	fill := func(ambient string) string {
		t.Helper()

		command := exec.Command("git", "credential", "fill")
		command.Dir = directory
		command.Env = append(os.Environ(),
			"BB_CONFIG_PATH="+ambient,
			actAsBB+"=1",
			"GIT_TERMINAL_PROMPT=0",
		)
		command.Stdin = strings.NewReader("protocol=https\nhost=bitbucket.example.com\n\n")
		var stderr bytes.Buffer
		command.Stderr = &stderr

		output, err := command.Output()
		if err != nil {
			t.Fatalf("git credential fill with BB_CONFIG_PATH=%s: %v\n%s", filepath.Base(ambient), err, stderr.String())
		}

		return string(output)
	}

	if code, output := setupGit(alice); code != 0 {
		t.Fatalf("setup-git as alice exited %d:\n%s", code, output)
	}

	if got := fill(alice); !strings.Contains(got, "username=alice\n") || !strings.Contains(got, "password=alice-token\n") {
		t.Fatalf("git does not authenticate as the login set up, even with its own file in the environment:\n%s", got)
	}
	if got := fill(bob); !strings.Contains(got, "username=alice\n") || !strings.Contains(got, "password=alice-token\n") {
		t.Fatalf("git authenticated as the login in its environment rather than the one set up:\n%s", got)
	}

	// Set up again as the same login: the line is the one already there.
	if code, output := setupGit(alice); code != 0 {
		t.Fatalf("setting up the same login again exited %d:\n%s", code, output)
	}

	// Another login for the host is another identity, and replacing one is
	// what --force is for.
	if code, output := setupGit(bob); code == 0 {
		t.Fatalf("setup-git as bob replaced alice's helper without --force:\n%s", output)
	} else if !strings.Contains(output, "--force") {
		t.Errorf("the refusal does not say how to replace the helper:\n%s", output)
	}
	if code, output := setupGit(bob, "--force"); code != 0 {
		t.Fatalf("setup-git as bob with --force exited %d:\n%s", code, output)
	}
	if got := fill(alice); !strings.Contains(got, "username=bob\n") || !strings.Contains(got, "password=bob-token\n") {
		t.Fatalf("after --force git does not authenticate as bob:\n%s", got)
	}
}

// TestSetupGitReplacesAnUnpinnedHelperOfItsOwn is the upgrade from a helper
// line an earlier bb wrote, which names no configuration. It is bb's own line
// and says nothing about an identity, so setting up over it needs no --force;
// a helper that is not bb's still does.
func TestSetupGitReplacesAnUnpinnedHelperOfItsOwn(t *testing.T) {
	directory := t.TempDir()
	alice := writeIdentityLogin(t, directory, "alice.yaml", "alice", "alice-token")

	globalConfig := filepath.Join(directory, "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("BB_CONFIG_PATH", alice)

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("resolve the test binary: %v", err)
	}

	key := "credential." + identityTestHost + ".helper"
	for _, existing := range []struct {
		helper    string
		replaced  bool
		described string
	}{
		{`!"` + filepath.ToSlash(executable) + `" auth git-credential`, true, "an unpinned bb helper"},
		{`!"/opt/old/bin/bb" auth git-credential`, true, "an unpinned bb helper at another path"},
		{"manager", false, "another credential manager"},
	} {
		if err := os.WriteFile(globalConfig, nil, 0o600); err != nil {
			t.Fatalf("reset git config: %v", err)
		}
		if output, err := exec.Command("git", "config", "--global", key, existing.helper).CombinedOutput(); err != nil {
			t.Fatalf("seed %s: %v\n%s", existing.described, err, output)
		}

		var stdout, stderr bytes.Buffer
		code := run([]string{"auth", "setup-git", "--host", identityTestHost}, &stdout, &stderr)

		if replaced := code == 0; replaced != existing.replaced {
			t.Errorf("over %s, setup-git exited %d, want replaced=%v:\n%s%s", existing.described, code, existing.replaced, stdout.String(), stderr.String())
		}
	}
}
