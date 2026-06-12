package execgit

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
)

func TestCloneValidation(t *testing.T) {
	backend := New()
	backend.Timeout = time.Second

	err := backend.Clone(context.Background(), "", git.CloneOptions{Directory: "tmp"})
	if err == nil {
		t.Fatal("expected validation error")
	}

	err = backend.Clone(context.Background(), "https://example.local/scm/PRJ/repo.git", git.CloneOptions{Directory: ""})
	if err == nil {
		t.Fatal("expected validation error for empty clone directory")
	}
}

func TestVersion(t *testing.T) {
	backend := New()
	backend.Timeout = 5 * time.Second

	version, err := backend.Version(context.Background())
	if err != nil {
		t.Fatalf("expected git version, got error: %v", err)
	}

	if version == "" {
		t.Fatal("expected non-empty git version")
	}
}

func TestClonePlacesOptionsBeforeRepositoryAndDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("path wrapper script is shell-specific")
	}

	backend := New()
	backend.Timeout = 5 * time.Second

	temporary := t.TempDir()
	logPath := filepath.Join(temporary, "git-args.log")
	gitWrapper := filepath.Join(temporary, "git")
	script := "#!/bin/sh\nprintf '%s\n' \"$@\" > \"$BB_GIT_ARGS_LOG\"\n"
	if err := os.WriteFile(gitWrapper, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write git wrapper: %v", err)
	}

	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", temporary+string(os.PathListSeparator)+originalPath)
	t.Setenv("BB_GIT_ARGS_LOG", logPath)

	cloneDir := filepath.Join(temporary, "clone")
	if err := backend.Clone(context.Background(), "https://example.local/scm/PRJ/repo.git", git.CloneOptions{
		Directory: cloneDir,
		Branch:    "main",
		Depth:     1,
		ExtraArgs: []string{"--filter=blob:none", "--origin", "upstream"},
	}); err != nil {
		t.Fatalf("expected clone to succeed through wrapper, got: %v", err)
	}

	logged, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read logged git args: %v", err)
	}
	args := strings.Split(strings.TrimSpace(string(logged)), "\n")
	expected := []string{
		"clone",
		"--branch",
		"main",
		"--depth",
		"1",
		"--filter=blob:none",
		"--origin",
		"upstream",
		"https://example.local/scm/PRJ/repo.git",
		cloneDir,
	}
	if len(args) != len(expected) {
		t.Fatalf("unexpected git args length: got %#v want %#v", args, expected)
	}
	for index, want := range expected {
		if args[index] != want {
			t.Fatalf("unexpected git arg order at %d: got %#v want %#v", index, args, expected)
		}
	}
}

func TestFetchAndCheckoutValidation(t *testing.T) {
	backend := New()
	backend.Timeout = time.Second

	err := backend.Fetch(context.Background(), "", git.FetchOptions{})
	if err == nil {
		t.Fatal("expected validation error for empty repository directory")
	}

	err = backend.Checkout(context.Background(), "", git.CheckoutOptions{Ref: "main"})
	if err == nil {
		t.Fatal("expected validation error for empty checkout directory")
	}

	err = backend.Checkout(context.Background(), ".", git.CheckoutOptions{Ref: ""})
	if err == nil {
		t.Fatal("expected validation error for empty checkout ref")
	}

	err = backend.AddRemote(context.Background(), "", git.Remote{Name: "upstream", URL: "https://example.local/scm/PRJ/upstream.git"})
	if err == nil {
		t.Fatal("expected validation error for empty add-remote directory")
	}

	err = backend.AddRemote(context.Background(), ".", git.Remote{Name: "", URL: "https://example.local/scm/PRJ/upstream.git"})
	if err == nil {
		t.Fatal("expected validation error for empty remote name")
	}

	err = backend.AddRemote(context.Background(), ".", git.Remote{Name: "upstream", URL: ""})
	if err == nil {
		t.Fatal("expected validation error for empty remote URL")
	}
}

func TestRunValidationAndFailure(t *testing.T) {
	backend := New()
	backend.Timeout = time.Second

	_, err := backend.run(context.Background(), runOptions{})
	if err == nil {
		t.Fatal("expected validation error for empty git args")
	}
	if apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation exit code 2, got %d", apperrors.ExitCode(err))
	}

	_, err = backend.run(context.Background(), runOptions{args: []string{"definitely-not-a-git-command"}})
	if err == nil {
		t.Fatal("expected permanent git command failure")
	}
	if apperrors.ExitCode(err) != 1 {
		t.Fatalf("expected permanent exit code 1, got %d", apperrors.ExitCode(err))
	}
	if !strings.Contains(err.Error(), "git definitely-not-a-git-command failed") {
		t.Fatalf("unexpected error message: %v", err)
	}

	_, err = backend.run(context.Background(), runOptions{cwd: "/path/that/does/not/exist", args: []string{"status"}})
	if err == nil {
		t.Fatal("expected run failure for invalid working directory")
	}
}

func TestCloneAndCheckoutAgainstLocalRepo(t *testing.T) {
	backend := New()
	backend.Timeout = 5 * time.Second

	temporary := t.TempDir()
	remoteDir := filepath.Join(temporary, "remote.git")
	workDir := filepath.Join(temporary, "work")
	cloneDir := filepath.Join(temporary, "clone")

	if _, err := backend.run(context.Background(), runOptions{args: []string{"init", "--bare", remoteDir}}); err != nil {
		t.Fatalf("failed to initialize bare repository: %v", err)
	}

	if _, err := backend.run(context.Background(), runOptions{args: []string{"init", workDir}}); err != nil {
		t.Fatalf("failed to initialize working repository: %v", err)
	}

	if _, err := backend.run(context.Background(), runOptions{cwd: workDir, args: []string{"config", "user.email", "test@example.local"}}); err != nil {
		t.Fatalf("failed to configure user.email: %v", err)
	}
	if _, err := backend.run(context.Background(), runOptions{cwd: workDir, args: []string{"config", "user.name", "Test User"}}); err != nil {
		t.Fatalf("failed to configure user.name: %v", err)
	}

	if writeErr := os.WriteFile(filepath.Join(workDir, "README.md"), []byte("seed\n"), 0o644); writeErr != nil {
		t.Fatalf("failed to write seed file: %v", writeErr)
	}

	if _, err := backend.run(context.Background(), runOptions{cwd: workDir, args: []string{"add", "README.md"}}); err != nil {
		t.Fatalf("failed to git add: %v", err)
	}
	if _, err := backend.run(context.Background(), runOptions{cwd: workDir, args: []string{"commit", "-m", "seed"}}); err != nil {
		t.Fatalf("failed to git commit: %v", err)
	}
	if _, err := backend.run(context.Background(), runOptions{cwd: workDir, args: []string{"branch", "-M", "main"}}); err != nil {
		t.Fatalf("failed to rename branch to main: %v", err)
	}
	if _, err := backend.run(context.Background(), runOptions{cwd: workDir, args: []string{"remote", "add", "origin", remoteDir}}); err != nil {
		t.Fatalf("failed to add origin remote: %v", err)
	}
	if _, err := backend.run(context.Background(), runOptions{cwd: workDir, args: []string{"push", "-u", "origin", "main"}}); err != nil {
		t.Fatalf("failed to push main branch: %v", err)
	}

	if err := backend.Clone(context.Background(), remoteDir, git.CloneOptions{Directory: cloneDir, Branch: "main", Depth: 1}); err != nil {
		t.Fatalf("expected clone to succeed, got: %v", err)
	}

	if err := backend.Fetch(context.Background(), cloneDir, git.FetchOptions{Remote: "origin"}); err != nil {
		t.Fatalf("expected fetch to succeed, got: %v", err)
	}

	if err := backend.Checkout(context.Background(), cloneDir, git.CheckoutOptions{Ref: "main"}); err != nil {
		t.Fatalf("expected checkout to succeed, got: %v", err)
	}

	repoRoot, err := backend.RepositoryRoot(context.Background(), cloneDir)
	if err != nil {
		t.Fatalf("expected repository root resolution to succeed, got: %v", err)
	}
	if repoRoot == "" {
		t.Fatal("expected non-empty repository root")
	}

	remotes, err := backend.ListRemotes(context.Background(), cloneDir)
	if err != nil {
		t.Fatalf("expected list remotes to succeed, got: %v", err)
	}
	if len(remotes) == 0 {
		t.Fatal("expected at least one remote")
	}
	if remotes[0].Name != "origin" {
		t.Fatalf("expected origin remote first, got: %+v", remotes)
	}

	if err := backend.AddRemote(context.Background(), cloneDir, git.Remote{Name: "upstream", URL: remoteDir}); err != nil {
		t.Fatalf("expected add remote to succeed, got: %v", err)
	}

	remotes, err = backend.ListRemotes(context.Background(), cloneDir)
	if err != nil {
		t.Fatalf("expected list remotes after add to succeed, got: %v", err)
	}
	if len(remotes) < 2 {
		t.Fatalf("expected remotes after add, got: %+v", remotes)
	}
}

func TestListRemotesValidation(t *testing.T) {
	backend := New()
	backend.Timeout = time.Second

	if _, err := backend.ListRemotes(context.Background(), ""); err == nil || apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation error for empty repository directory, got: %v", err)
	}
}

func TestListRemotesOrderAndDeduplication(t *testing.T) {
	backend := New()
	backend.Timeout = 5 * time.Second

	temporary := t.TempDir()
	repositoryDirectory := filepath.Join(temporary, "repo")
	if _, err := backend.run(context.Background(), runOptions{args: []string{"init", repositoryDirectory}}); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: []string{"remote", "add", "upstream", "https://example.local/scm/PRJ/upstream.git"}}); err != nil {
		t.Fatalf("git remote add upstream failed: %v", err)
	}
	if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: []string{"remote", "add", "origin", "https://example.local/scm/PRJ/origin.git"}}); err != nil {
		t.Fatalf("git remote add origin failed: %v", err)
	}

	remotes, err := backend.ListRemotes(context.Background(), repositoryDirectory)
	if err != nil {
		t.Fatalf("list remotes failed: %v", err)
	}
	if len(remotes) < 2 {
		t.Fatalf("expected at least two remotes, got %+v", remotes)
	}
	if remotes[0].Name != "origin" {
		t.Fatalf("expected origin to be ordered first, got %+v", remotes)
	}
	if remotes[1].Name != "upstream" {
		t.Fatalf("expected upstream to be ordered after origin, got %+v", remotes)
	}
}

func TestRepositoryRootNonRepositoryError(t *testing.T) {
	backend := New()
	backend.Timeout = time.Second

	if _, err := backend.RepositoryRoot(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected repository root resolution to fail for non-repository directory")
	}
}

func TestListRemotesNonRepositoryError(t *testing.T) {
	backend := New()
	backend.Timeout = time.Second

	if _, err := backend.ListRemotes(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected list remotes to fail for non-repository directory")
	}
}

func TestListRemotesMultiURLOriginOrdering(t *testing.T) {
	backend := New()
	backend.Timeout = 5 * time.Second

	repositoryDirectory := filepath.Join(t.TempDir(), "repo")
	if _, err := backend.run(context.Background(), runOptions{args: []string{"init", repositoryDirectory}}); err != nil {
		t.Fatalf("git init failed: %v", err)
	}

	if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: []string{"remote", "add", "origin", "https://example.local/scm/PRJ/one.git"}}); err != nil {
		t.Fatalf("git remote add origin failed: %v", err)
	}
	if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: []string{"remote", "set-url", "--add", "origin", "https://example.local/scm/PRJ/two.git"}}); err != nil {
		t.Fatalf("git remote set-url --add failed: %v", err)
	}
	if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: []string{"remote", "add", "beta", "https://example.local/scm/PRJ/beta.git"}}); err != nil {
		t.Fatalf("git remote add beta failed: %v", err)
	}

	remotes, err := backend.ListRemotes(context.Background(), repositoryDirectory)
	if err != nil {
		t.Fatalf("list remotes failed: %v", err)
	}
	if len(remotes) < 2 {
		t.Fatalf("expected at least two remotes, got %+v", remotes)
	}
	if remotes[0].Name != "origin" {
		t.Fatalf("expected origin to be sorted first, got %+v", remotes)
	}
}

func TestRedact(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{
			input:    "git clone https://x-token-auth:my-secret-token@bitbucket.org/repo.git",
			expected: "git clone https://x-token-auth:***@bitbucket.org/repo.git",
		},
		{
			input:    "git clone http://user:pass@host:8080/repo.git",
			expected: "git clone http://user:***@host:8080/repo.git",
		},
		{
			input:    "git -c http.extraHeader=\"Authorization: Bearer my-secret-token\" clone https://host/repo.git",
			expected: "git -c http.extraHeader=\"Authorization: Bearer ***\" clone https://host/repo.git",
		},
		{
			input:    "git -c http.extraHeader=Authorization: Basic dXNlcjpwYXNz clone https://host/repo.git",
			expected: "git -c http.extraHeader=Authorization: Basic *** clone https://host/repo.git",
		},
		{
			input:    "some clean output without credentials",
			expected: "some clean output without credentials",
		},
	}

	for _, tc := range cases {
		actual := redact(tc.input)
		if actual != tc.expected {
			t.Errorf("redact(%q) = %q; want %q", tc.input, actual, tc.expected)
		}
	}
}

func TestCloneAuthenticationOptionsAndLocalConfigPersistence(t *testing.T) {
	backend := New()
	backend.Timeout = 5 * time.Second

	temporary := t.TempDir()
	remoteDir := filepath.Join(temporary, "remote.git")
	cloneDir := filepath.Join(temporary, "clone")

	if _, err := backend.run(context.Background(), runOptions{args: []string{"init", "--bare", remoteDir}}); err != nil {
		t.Fatalf("failed to initialize bare repository: %v", err)
	}

	// Clone with a dummy AuthToken
	err := backend.Clone(context.Background(), remoteDir, git.CloneOptions{
		Directory: cloneDir,
		AuthToken: "dummy-secret-token",
	})
	if err != nil {
		t.Fatalf("expected clone to succeed, got: %v", err)
	}

	// Verify that the local configuration http.extraHeader is persisted directly in .git/config file
	configContent, err := os.ReadFile(filepath.Join(cloneDir, ".git", "config"))
	if err != nil {
		t.Fatalf("failed to read local .git/config: %v", err)
	}

	contentStr := string(configContent)
	expectedConfig := "extraHeader = Authorization: Bearer dummy-secret-token"
	if !strings.Contains(contentStr, expectedConfig) {
		t.Fatalf("expected config file to contain %q, but got:\n%s", expectedConfig, contentStr)
	}
}

func TestCloneBasicAuthenticationOptionsAndLocalConfigPersistence(t *testing.T) {
	backend := New()
	backend.Timeout = 5 * time.Second

	temporary := t.TempDir()
	remoteDir := filepath.Join(temporary, "remote.git")
	cloneDir := filepath.Join(temporary, "clone")

	if _, err := backend.run(context.Background(), runOptions{args: []string{"init", "--bare", remoteDir}}); err != nil {
		t.Fatalf("failed to initialize bare repository: %v", err)
	}

	// Clone with AuthUsername and AuthPassword
	err := backend.Clone(context.Background(), remoteDir, git.CloneOptions{
		Directory:    cloneDir,
		AuthUsername: "dummy-user",
		AuthPassword: "dummy-password",
	})
	if err != nil {
		t.Fatalf("expected clone to succeed, got: %v", err)
	}

	// Verify that the local configuration http.extraHeader is persisted directly in .git/config file
	configContent, err := os.ReadFile(filepath.Join(cloneDir, ".git", "config"))
	if err != nil {
		t.Fatalf("failed to read local .git/config: %v", err)
	}

	contentStr := string(configContent)
	expectedConfig := "extraHeader = Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("dummy-user:dummy-password"))
	if !strings.Contains(contentStr, expectedConfig) {
		t.Fatalf("expected config file to contain %q, but got:\n%s", expectedConfig, contentStr)
	}
}

func TestCloneFailureRedactsCredentials(t *testing.T) {
	backend := New()
	backend.Timeout = 5 * time.Second

	err := backend.Clone(context.Background(), "https://bitbucket.example.com/scm/PRJ/does-not-exist.git", git.CloneOptions{
		Directory: "unused-dir",
		AuthToken: "super-secret-token-12345",
	})
	if err == nil {
		t.Fatal("expected clone to fail")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "super-secret-token-12345") {
		t.Fatalf("error message leaked secret token: %s", errMsg)
	}
	if !strings.Contains(errMsg, "Authorization: Bearer ***") {
		t.Fatalf("expected error message to contain redacted token header, got: %s", errMsg)
	}
}

func TestCloneFailureRedactsURLCredentials(t *testing.T) {
	backend := New()
	backend.Timeout = 5 * time.Second

	err := backend.Clone(context.Background(), "https://x-token-auth:super-secret-password-54321@bitbucket.example.com/scm/PRJ/does-not-exist.git", git.CloneOptions{
		Directory: "unused-dir",
	})
	if err == nil {
		t.Fatal("expected clone to fail")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, "super-secret-password-54321") {
		t.Fatalf("error message leaked secret URL password: %s", errMsg)
	}
	if !strings.Contains(errMsg, "https://x-token-auth:***@bitbucket.example.com") {
		t.Fatalf("expected error message to contain redacted URL basic auth, got: %s", errMsg)
	}
}

