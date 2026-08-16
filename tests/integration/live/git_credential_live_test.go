//go:build live

package live_test

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli"
)

// executeLiveCLIWithStdin runs the CLI with input attached, which the credential
// helper needs because git passes its request on stdin.
func executeLiveCLIWithStdin(t *testing.T, stdin string, args ...string) (string, error) {
	t.Helper()

	command := cli.NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetIn(strings.NewReader(stdin))
	command.SetArgs(args)

	err := command.Execute()
	return output.String(), err
}

// liveCLIBinaryPath builds bb so git can invoke it as a credential helper.
//
// The rest of the live suite drives the CLI in-process, which is faster, but a
// credential helper is by definition executed by git as a separate program.
// Proving that path requires a real executable.
func liveCLIBinaryPath(t *testing.T) (string, error) {
	t.Helper()

	moduleFile, err := exec.Command("go", "env", "GOMOD").Output()
	if err != nil {
		return "", err
	}

	moduleRoot := filepath.Dir(strings.TrimSpace(string(moduleFile)))
	binaryName := "bb-live-helper"
	if runtime.GOOS == "windows" {
		binaryName += ".exe"
	}

	binaryPath := filepath.Join(t.TempDir(), binaryName)
	build := exec.Command("go", "build", "-o", binaryPath, "./cmd/bb")
	build.Dir = moduleRoot

	if output, err := build.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build bb: %w: %s", err, output)
	}

	return binaryPath, nil
}

// TestLiveGitCredentialHelperAuthenticatesClone proves the credential helper
// against a real Bitbucket.
//
// The helper can only supply a username and password, because that is all git's
// credential protocol carries — it cannot send the Authorization: Bearer header
// the REST client uses. Whether Bitbucket accepts a personal access token in the
// password position over Basic auth is therefore the assumption the whole design
// rests on, and it is only answerable against a running server.
func TestLiveGitCredentialHelperAuthenticatesClone(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// A dedicated config file keeps this test out of the developer's real
	// stored configuration and keyring.
	configPath := filepath.Join(t.TempDir(), "bb-config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	loginArgs := []string{"auth", "login", harness.config.BitbucketURL, "--discover-aliases=false"}
	if token := strings.TrimSpace(harness.config.BitbucketToken); token != "" {
		loginArgs = append(loginArgs, "--token", token)
	} else {
		loginArgs = append(loginArgs,
			"--username", harness.config.BitbucketUsername,
			"--password", harness.config.BitbucketPassword,
		)
	}

	if output, err := executeLiveCLI(t, loginArgs...); err != nil {
		t.Fatalf("auth login failed: %v\noutput: %s", err, output)
	}

	parsedHost, err := url.Parse(harness.config.BitbucketURL)
	if err != nil {
		t.Fatalf("parse bitbucket url: %v", err)
	}

	t.Run("supplies credentials for the configured host", func(t *testing.T) {
		request := "protocol=" + parsedHost.Scheme + "\nhost=" + parsedHost.Host + "\n\n"

		output, err := executeLiveCLIWithStdin(t, request, "auth", "git-credential", "get")
		if err != nil {
			t.Fatalf("git-credential get failed: %v\noutput: %s", err, output)
		}
		if !strings.Contains(output, "username=") || !strings.Contains(output, "password=") {
			t.Fatalf("expected a credential pair, got: %s", output)
		}
	})

	// Regression guard for a real defect: an early version resolved credentials
	// through the lookup that falls back to the configured default host, so
	// asking for github.com returned the Bitbucket token.
	t.Run("stays silent for unrelated hosts", func(t *testing.T) {
		for _, host := range []string{"github.com", "gitlab.com", "evil.example.org"} {
			output, err := executeLiveCLIWithStdin(t, "protocol=https\nhost="+host+"\n\n", "auth", "git-credential", "get")
			if err != nil {
				t.Fatalf("git-credential get for %s should exit 0, got: %v", host, err)
			}
			if strings.TrimSpace(output) != "" {
				t.Fatalf("leaked credentials for unrelated host %s: %s", host, output)
			}
		}
	})

	t.Run("git clones through the helper without persisting a credential", func(t *testing.T) {
		executablePath, err := liveCLIBinaryPath(t)
		if err != nil {
			t.Fatalf("resolve bb binary: %v", err)
		}

		cloneParent := t.TempDir()
		cloneDir := filepath.Join(cloneParent, "clone")

		repoURL := *parsedHost
		repoURL.Path = strings.TrimRight(repoURL.Path, "/") + "/scm/" + strings.ToUpper(seeded.Key) + "/" + repo.Slug + ".git"

		helperKey := "credential." + parsedHost.Scheme + "://" + parsedHost.Host + ".helper"
		helperValue := "!\"" + executablePath + "\" auth git-credential"

		// -c rather than a written config keeps the assertion about bb's helper
		// rather than about setup-git, which is covered by unit tests.
		if err := runGit(cloneParent,
			"-c", helperKey+"=",
			"-c", helperKey+"="+helperValue,
			"clone", repoURL.String(), cloneDir,
		); err != nil {
			t.Fatalf("clone through the bb credential helper failed: %v", err)
		}

		configBytes, err := os.ReadFile(filepath.Join(cloneDir, ".git", "config"))
		if err != nil {
			t.Fatalf("read cloned .git/config: %v", err)
		}

		// The property this whole change exists to establish: nothing secret is
		// left behind in the repository.
		contents := string(configBytes)
		if strings.Contains(contents, "extraHeader") {
			t.Fatalf("clone persisted an extraHeader into .git/config:\n%s", contents)
		}
		if token := strings.TrimSpace(harness.config.BitbucketToken); token != "" && strings.Contains(contents, token) {
			t.Fatalf("clone leaked the token into .git/config:\n%s", contents)
		}
		if password := strings.TrimSpace(harness.config.BitbucketPassword); password != "" && strings.Contains(contents, password) {
			t.Fatalf("clone leaked the password into .git/config:\n%s", contents)
		}
	})
}

// TestLiveRepoCloneLeavesNoCredentialBehind covers the same property for
// `bb repo clone`, which supplies credentials to git itself.
func TestLiveRepoCloneLeavesNoCredentialBehind(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}

	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	cloneParent := t.TempDir()
	cloneDir := filepath.Join(cloneParent, "cloned")

	output, err := executeLiveCLI(t, "repo", "clone", seeded.Key+"/"+repo.Slug, cloneDir)
	if err != nil {
		t.Fatalf("repo clone failed: %v\noutput: %s", err, output)
	}

	configBytes, err := os.ReadFile(filepath.Join(cloneDir, ".git", "config"))
	if err != nil {
		t.Fatalf("read cloned .git/config: %v", err)
	}

	contents := string(configBytes)
	if strings.Contains(contents, "extraHeader") {
		t.Fatalf("repo clone persisted an extraHeader into .git/config:\n%s", contents)
	}
	if password := strings.TrimSpace(harness.config.BitbucketPassword); password != "" && strings.Contains(contents, password) {
		t.Fatalf("repo clone leaked the password into .git/config:\n%s", contents)
	}
	if token := strings.TrimSpace(harness.config.BitbucketToken); token != "" && strings.Contains(contents, token) {
		t.Fatalf("repo clone leaked the token into .git/config:\n%s", contents)
	}
}
