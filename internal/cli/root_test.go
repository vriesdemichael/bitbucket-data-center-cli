package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
)

type inferenceGitBackendStub struct {
	repoRoot  string
	rootErr   error
	remotes   []git.Remote
	listErr   error
	branch    string
	branchErr error
}

func (stub inferenceGitBackendStub) Version(context.Context) (string, error) {
	return "", nil
}

func executeTestCLI(t *testing.T, args ...string) (string, error) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	command := NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(args)
	err := command.Execute()
	return output.String(), err
}

func (stub inferenceGitBackendStub) Clone(context.Context, string, git.CloneOptions) error {
	return nil
}

func (stub inferenceGitBackendStub) AddRemote(context.Context, string, git.Remote) error {
	return nil
}

func (stub inferenceGitBackendStub) Fetch(context.Context, string, git.FetchOptions) error {
	return nil
}

func (stub inferenceGitBackendStub) Checkout(context.Context, string, git.CheckoutOptions) error {
	return nil
}

func (stub inferenceGitBackendStub) RepositoryRoot(context.Context, string) (string, error) {
	if stub.rootErr != nil {
		return "", stub.rootErr
	}
	return stub.repoRoot, nil
}

func (stub inferenceGitBackendStub) CurrentBranch(context.Context, string) (string, error) {
	if stub.branchErr != nil {
		return "", stub.branchErr
	}
	return stub.branch, nil
}

func (stub inferenceGitBackendStub) ListRemotes(context.Context, string) ([]git.Remote, error) {
	if stub.listErr != nil {
		return nil, stub.listErr
	}
	return stub.remotes, nil
}

func (stub inferenceGitBackendStub) GetConfig(context.Context, git.ConfigOptions) (string, error) {
	return "", nil
}

func (stub inferenceGitBackendStub) SetConfig(context.Context, git.ConfigOptions) error {
	return nil
}

func (stub inferenceGitBackendStub) UnsetConfig(context.Context, git.ConfigOptions) error {
	return nil
}

func init() {
	// Block external network access during tests by default
	os.Setenv("BB_BLOCK_EXTERNAL_NETWORK", "1")

	// Shorten the retry backoff. Deliberately not BB_RETRY_COUNT=0: the retry
	// loop still runs the same number of attempts down the same paths, so
	// nothing about the mechanism goes untested here — only the sleeping goes
	// away.
	//
	// It is worth this much: the error-path tests in this package point the
	// client at an httptest server that returns 5xx or refuses the connection,
	// so every one of them pays the full backoff for a retry that cannot
	// possibly help against localhost. That was 60% of the package's runtime
	// and bought no assertion.
	//
	// The retry mechanism itself is covered where it lives, in
	// internal/transport/httpclient, by tests that set client.retries directly
	// and assert attempt counts, Retry-After handling and cancellation during
	// backoff. Those are unaffected by this.
	os.Setenv("BB_RETRY_BACKOFF", "1ms")
}

// TestAuthStatusSmoke covers what `bb auth status` reports when there are no
// credentials to report.
//
// It asks nothing of Bitbucket. The version that stood up a server answering 404
// to everything did not need it either -- the command reads the resolved
// configuration and says where each part came from -- and pointing at a host
// that does not resolve says so: the output is identical, and the command still
// exits zero. The live half, what the server says about an account that does
// exist, is TestLiveAuthIdentity.
func TestAuthStatusSmoke(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://bitbucket.invalid")
	t.Setenv("BITBUCKET_VERSION_TARGET", "9.4.16")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"auth", "status"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !strings.Contains(buffer.String(), "Target Bitbucket") {
		t.Fatalf("expected auth status output, got: %s", buffer.String())
	}

	if !strings.Contains(buffer.String(), "auth=none") {
		t.Fatalf("expected auth mode in output, got: %s", buffer.String())
	}

	if !strings.Contains(buffer.String(), "source=env/default") {
		t.Fatalf("expected auth source in output, got: %s", buffer.String())
	}
}
func TestBranchValidationErrors(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	// Every case here is refused before a request is built, so the handler is
	// an assertion rather than a stand-in: reaching it means validation let
	// something through (ADR-079).
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("validation let a request through: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name         string
		args         []string
		expectAppErr bool
	}{
		{name: "branch create missing start-point", args: []string{"branch", "create", "feature/demo"}, expectAppErr: false},
		{name: "branch restriction create missing matcher-id", args: []string{"branch", "restriction", "create", "--type", "read-only"}, expectAppErr: false},
		{name: "branch restriction create access key overflow", args: []string{"branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/main", "--access-key-id", "2147483648"}, expectAppErr: true},
		{name: "branch restriction list invalid matcher-type", args: []string{"branch", "restriction", "list", "--matcher-type", "invalid"}, expectAppErr: true},
		{name: "branch restriction update access key overflow", args: []string{"branch", "restriction", "update", "12", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "refs/heads/main", "--access-key-id", "2147483648"}, expectAppErr: true},
		{name: "branch restriction update invalid id", args: []string{"branch", "restriction", "update", "bad", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "refs/heads/main"}, expectAppErr: true},
		{name: "branch default set blank", args: []string{"branch", "default", "set", " "}, expectAppErr: true},
		{name: "branch model update blank", args: []string{"branch", "model", "update", " "}, expectAppErr: true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected validation error for args: %v", testCase.args)
			}
			exitCode := apperrors.ExitCode(err)
			if testCase.expectAppErr && exitCode != 2 && exitCode != 4 {
				t.Fatalf("expected validation exit code 2 or 4, got %d (%v)", exitCode, err)
			}
		})
	}
}

func TestBranchCommandsFailOnInvalidRepositorySelector(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://example.local")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
	}{
		{name: "list invalid repo selector", args: []string{"branch", "list", "--repo", "bad"}},
		{name: "create invalid repo selector", args: []string{"branch", "create", "feature/demo", "--start-point", "abc", "--repo", "bad"}},
		{name: "delete invalid repo selector", args: []string{"branch", "delete", "feature/demo", "--repo", "bad"}},
		{name: "default get invalid repo selector", args: []string{"branch", "default", "get", "--repo", "bad"}},
		{name: "default set invalid repo selector", args: []string{"branch", "default", "set", "main", "--repo", "bad"}},
		{name: "model inspect invalid repo selector", args: []string{"branch", "model", "inspect", "abc", "--repo", "bad"}},
		{name: "model update invalid repo selector", args: []string{"branch", "model", "update", "main", "--repo", "bad"}},
		{name: "restriction list invalid repo selector", args: []string{"branch", "restriction", "list", "--repo", "bad"}},
		{name: "restriction get invalid repo selector", args: []string{"branch", "restriction", "get", "12", "--repo", "bad"}},
		{name: "restriction create invalid repo selector", args: []string{"branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/main", "--repo", "bad"}},
		{name: "restriction update invalid repo selector", args: []string{"branch", "restriction", "update", "12", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "refs/heads/main", "--repo", "bad"}},
		{name: "restriction delete invalid repo selector", args: []string{"branch", "restriction", "delete", "12", "--repo", "bad"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected repository validation error for args: %v", testCase.args)
			}
			if apperrors.ExitCode(err) != 2 {
				t.Fatalf("expected validation exit code 2 for args %v, got %d (%v)", testCase.args, apperrors.ExitCode(err), err)
			}
		})
	}
}

func TestBranchCommandsFailOnInvalidConfig(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "://bad-url")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
	}{
		{name: "list invalid config", args: []string{"branch", "list"}},
		{name: "create invalid config", args: []string{"branch", "create", "feature/demo", "--start-point", "abc"}},
		{name: "delete invalid config", args: []string{"branch", "delete", "feature/demo"}},
		{name: "default get invalid config", args: []string{"branch", "default", "get"}},
		{name: "default set invalid config", args: []string{"branch", "default", "set", "main"}},
		{name: "model inspect invalid config", args: []string{"branch", "model", "inspect", "abc"}},
		{name: "model update invalid config", args: []string{"branch", "model", "update", "main"}},
		{name: "restriction list invalid config", args: []string{"branch", "restriction", "list"}},
		{name: "restriction get invalid config", args: []string{"branch", "restriction", "get", "12"}},
		{name: "restriction create invalid config", args: []string{"branch", "restriction", "create", "--type", "read-only", "--matcher-id", "refs/heads/main"}},
		{name: "restriction update invalid config", args: []string{"branch", "restriction", "update", "12", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "refs/heads/main"}},
		{name: "restriction delete invalid config", args: []string{"branch", "restriction", "delete", "12"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected config validation error for args: %v", testCase.args)
			}
			if apperrors.ExitCode(err) != 2 {
				t.Fatalf("expected validation exit code 2 for args %v, got %d (%v)", testCase.args, apperrors.ExitCode(err), err)
			}
		})
	}
}

func TestAuthStatusJSON(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_VERSION_TARGET", "9.4.16")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "auth", "status"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	parsed := decodeJSONEnvelopeDataMap(t, buffer.Bytes())

	if asString(parsed["bitbucketUrl"]) != "http://localhost:7990" {
		t.Fatalf("unexpected bitbucketUrl: %q", asString(parsed["bitbucketUrl"]))
	}

	if asString(parsed["authMode"]) != "none" {
		t.Fatalf("unexpected authMode: %q", asString(parsed["authMode"]))
	}

	if asString(parsed["authSource"]) != "env/default" {
		t.Fatalf("unexpected authSource: %q", asString(parsed["authSource"]))
	}
}

func asString(value any) string {
	if typed, ok := value.(string); ok {
		return typed
	}
	return ""
}

func decodeJSONEnvelopeDataMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()

	var envelope struct {
		Data map[string]any `json:"data"`
		Meta struct {
			BBVersion string `json:"bbVersion"`
		} `json:"meta"`
	}

	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("expected valid json output, got: %s (%v)", string(raw), err)
	}

	if envelope.Meta.BBVersion == "" {
		t.Fatalf("envelope carries no meta.bbVersion: %+v", envelope)
	}

	if envelope.Data == nil {
		t.Fatalf("expected json envelope data payload, got: %s", string(raw))
	}

	return envelope.Data
}

func TestRootTransportFlagsOverrideEnvironment(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BB_REQUEST_TIMEOUT", "not-a-duration")

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--request-timeout", "1s", "auth", "status"})

	err := command.Execute()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestDiffRefsRejectsMultipleOutputModes(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"diff", "refs", "main", "feature", "--patch", "--stat"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected validation error")
	}
}

// mock-inventory: transport-fault — a server answering 503 to everything is injected; the subject is that bb reports it as transient rather than as a bad request, and a live instance cannot be asked to be unavailable.
func TestAdminHealthPropagatesHardFailure(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("unavailable"))
	}))
	defer server.Close()

	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"admin", "health"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected transient failure error")
	}
	if apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestBulkCommandAvailableFromRoot(t *testing.T) {
	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"bulk", "--help"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected bulk help to succeed, got: %v", err)
	}
	if !strings.Contains(buffer.String(), "plan") || !strings.Contains(buffer.String(), "apply") || !strings.Contains(buffer.String(), "status") {
		t.Fatalf("expected bulk subcommands in help output, got: %s", buffer.String())
	}
}

func TestResolveRepositorySelector(t *testing.T) {
	t.Run("falls back to the resolved repository", func(t *testing.T) {
		// The slug comes off the configuration now rather than being read from
		// the environment here, so an inferred context reaches it as a value
		// (issue #458). LoadWithOverrides is what fills it.
		repo, err := resolveRepositorySelector("", config.AppConfig{ProjectKey: "TEST", RepoSlug: "demo"})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if repo.ProjectKey != "TEST" || repo.Slug != "demo" {
			t.Fatalf("unexpected selector: %+v", repo)
		}
	})

	t.Run("rejects missing values", func(t *testing.T) {
		t.Setenv("BITBUCKET_REPO_SLUG", "")

		_, err := resolveRepositorySelector("", config.AppConfig{})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("rejects invalid format", func(t *testing.T) {
		_, err := resolveRepositorySelector("badformat", config.AppConfig{})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("accepts explicit selector", func(t *testing.T) {
		repo, err := resolveRepositorySelector("PRJ/repo", config.AppConfig{})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if repo.ProjectKey != "PRJ" || repo.Slug != "repo" {
			t.Fatalf("unexpected selector: %+v", repo)
		}
	})
}

func TestApplyInferredRepositoryContext(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() {
		gitBackendFactory = originalFactory
	})

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://bitbucket.local:7990")
	t.Setenv("BITBUCKET_PROJECT_KEY", "ENV")
	t.Setenv("BITBUCKET_REPO_SLUG", "env-repo")

	t.Run("sets inferred repository and host", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes: []git.Remote{{
					Name: "origin",
					URL:  "https://bitbucket.local:7990/scm/PRJ/demo.git",
				}},
			}
		}

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")
		errBuffer := &bytes.Buffer{}
		cmd.SetErr(errBuffer)

		// The inferred context is carried as a value now rather than published
		// to the environment, so this asserts where it actually lands.
		options := &rootOptions{}
		if err := options.applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("apply inferred repository context failed: %v", err)
		}

		if got := options.runtime.ProjectKey; got != "PRJ" {
			t.Fatalf("expected inferred project key, got %q", got)
		}
		if got := options.runtime.RepoSlug; got != "demo" {
			t.Fatalf("expected inferred repo slug, got %q", got)
		}
		if !options.repositoryInferred {
			t.Error("inference was not recorded; a destructive command cannot tell it from a named target")
		}
		if !strings.Contains(errBuffer.String(), "Using repository context from git remote") {
			t.Fatalf("expected inference notice, got: %s", errBuffer.String())
		}
	})

	t.Run("nil command is ignored", func(t *testing.T) {
		if err := (&rootOptions{}).applyInferredRepositoryContext(nil, false); err != nil {
			t.Fatalf("expected nil command to be ignored, got: %v", err)
		}
	})

	t.Run("command without repo flag is ignored", func(t *testing.T) {
		cmd := &cobra.Command{Use: "project list"}
		if err := (&rootOptions{}).applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("expected no error when repo flag is absent, got: %v", err)
		}
	})

	t.Run("explicit repo flag skips inference", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes:  []git.Remote{{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"}},
			}
		}

		t.Setenv("BITBUCKET_PROJECT_KEY", "EXPLICIT")
		t.Setenv("BITBUCKET_REPO_SLUG", "keep")

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")
		if err := cmd.Flags().Set("repo", "OVERRIDE/repo"); err != nil {
			t.Fatalf("set repo flag: %v", err)
		}

		if err := (&rootOptions{}).applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("expected no error for explicit repo, got: %v", err)
		}

		if got := os.Getenv("BITBUCKET_PROJECT_KEY"); got != "EXPLICIT" {
			t.Fatalf("expected project key unchanged, got %q", got)
		}
		if got := os.Getenv("BITBUCKET_REPO_SLUG"); got != "keep" {
			t.Fatalf("expected repo slug unchanged, got %q", got)
		}
	})

	t.Run("ambiguous remotes returns validation error", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes: []git.Remote{
					{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"},
					{Name: "upstream", URL: "https://bitbucket.local:7990/scm/ALT/demo.git"},
				},
			}
		}

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")

		err := (&rootOptions{}).applyInferredRepositoryContext(cmd, false)
		if err == nil {
			t.Fatal("expected ambiguity error")
		}
		if apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected validation exit code, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("non-repository git context is ignored", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{rootErr: errors.New("fatal: not a git repository (or any of the parent directories): .git")}
		}

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")

		if err := (&rootOptions{}).applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("expected non-repository error to be ignored, got: %v", err)
		}
	})

	t.Run("json mode does not emit inference banner", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes:  []git.Remote{{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"}},
			}
		}

		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")
		errBuffer := &bytes.Buffer{}
		cmd.SetErr(errBuffer)

		if err := (&rootOptions{}).applyInferredRepositoryContext(cmd, true); err != nil {
			t.Fatalf("json inference failed: %v", err)
		}
		if errBuffer.Len() != 0 {
			t.Fatalf("expected no banner output in json mode, got: %q", errBuffer.String())
		}
	})

	t.Run("load config errors are ignored", func(t *testing.T) {
		t.Setenv("BITBUCKET_URL", "://bad-url")
		cmd := &cobra.Command{Use: "branch list"}
		cmd.Flags().String("repo", "", "")

		if err := (&rootOptions{}).applyInferredRepositoryContext(cmd, false); err != nil {
			t.Fatalf("expected load config error to be ignored, got: %v", err)
		}
	})
}

// The pull request commands reading the inferred context are live now, in
// TestLiveCLIInferRepoContextFromGitRemote.
//
// The inference itself is the same code either way and is tested above without
// a server. What the version here added was three commands driven against a
// hand-written Bitbucket to show the context reached them -- but the inferred
// project and slug only mean anything if they address a repository the server
// agrees exists, and a fixture answering to whatever it is handed cannot show
// that. `pr get` and `pr build status` run against a real remote there; the
// third, `pr review approve`, would need a second account, because Bitbucket
// refuses an author their own approval and the harness user is the author.

// TestMCPServeOptsOutOfAmbientRepositoryInference resolves the real
// ai mcp serve command and checks it carries the annotation
// applyInferredRepositoryContext honours. The command sets the annotation with
// a string literal because its package cannot import this one; this test is
// what keeps that literal and the constant from drifting apart.
//
// Without the annotation, inference fills serve's --repo from the git remote
// of the working directory, and the server is silently confined to that
// repository: every call naming a sibling is refused, and the build-status
// tools vanish from the catalogue. ADR-062's default is the opposite — scope
// is opt-in.
func TestMCPServeOptsOutOfAmbientRepositoryInference(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() {
		gitBackendFactory = originalFactory
	})

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://bitbucket.local:7990")
	t.Setenv("BITBUCKET_PROJECT_KEY", "ENV")
	t.Setenv("BITBUCKET_REPO_SLUG", "env-repo")

	root := NewRootCommand()
	serve, _, err := root.Find([]string{"ai", "mcp", "serve"})
	if err != nil {
		t.Fatalf("resolve ai mcp serve: %v", err)
	}
	if serve.Flags().Lookup("repo") == nil {
		t.Fatal("ai mcp serve must register --repo for this test to mean anything")
	}

	if serve.Annotations[annotationNoAmbientRepoInference] != "true" {
		t.Fatalf("ai mcp serve must opt out of ambient repository inference so its --repo stays a deliberate confinement decision")
	}

	// The annotation must be the whole story: with a backend that would infer,
	// the resolved command comes through the pre-run untouched.
	gitBackendFactory = func() git.Backend {
		return inferenceGitBackendStub{
			repoRoot: "/tmp/repo",
			remotes:  []git.Remote{{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"}},
		}
	}

	errBuffer := &bytes.Buffer{}
	serve.SetErr(errBuffer)

	// A method on rootOptions here rather than the free function it was written
	// against: this branch carries the global flags as values (ADR from the v4
	// milestone) instead of publishing them to the environment.
	if err := (&rootOptions{}).applyInferredRepositoryContext(serve, false); err != nil {
		t.Fatalf("apply inferred repository context failed: %v", err)
	}

	if got := serve.Flags().Lookup("repo").Value.String(); got != "" {
		t.Fatalf("expected serve --repo to stay unset, got %q", got)
	}
	if got := os.Getenv("BITBUCKET_PROJECT_KEY"); got != "ENV" {
		t.Fatalf("expected project key unchanged, got %q", got)
	}
	if got := os.Getenv("BITBUCKET_REPO_SLUG"); got != "env-repo" {
		t.Fatalf("expected repo slug unchanged, got %q", got)
	}
	if errBuffer.Len() != 0 {
		t.Fatalf("expected no inference banner, got %q", errBuffer.String())
	}
}

func TestInferenceHelperFunctions(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() {
		gitBackendFactory = originalFactory
	})

	t.Run("parse bitbucket remote supports ssh and https", func(t *testing.T) {
		host, project, slug, ok := parseBitbucketRemote("git@bitbucket.local:scm/PRJ/repo.git")
		if !ok || host != "bitbucket.local" || project != "PRJ" || slug != "repo" {
			t.Fatalf("unexpected ssh remote parse result: ok=%v host=%q project=%q slug=%q", ok, host, project, slug)
		}

		host, project, slug, ok = parseBitbucketRemote("https://bitbucket.local/scm/PRJ/repo.git")
		if !ok || host != "bitbucket.local" || project != "PRJ" || slug != "repo" {
			t.Fatalf("unexpected https remote parse result: ok=%v host=%q project=%q slug=%q", ok, host, project, slug)
		}
	})

	t.Run("parse bitbucket path invalid input", func(t *testing.T) {
		if _, _, ok := parseBitbucketPath("/"); ok {
			t.Fatal("expected invalid path to fail")
		}
	})

	t.Run("normalize host endpoint keeps explicit or default ports", func(t *testing.T) {
		if got := normalizeHostEndpoint(""); got != "" {
			t.Fatalf("expected empty endpoint to normalize to empty, got %q", got)
		}
		if got := normalizeHostEndpoint("https://Bitbucket.Local:7990"); got != "bitbucket.local:7990" {
			t.Fatalf("unexpected normalized host: %q", got)
		}
		if got := normalizeHostEndpoint("https://bitbucket.local"); got != "bitbucket.local:443" {
			t.Fatalf("unexpected normalized https default port: %q", got)
		}
		if got := normalizeHostEndpoint("http://bitbucket.local"); got != "bitbucket.local:80" {
			t.Fatalf("unexpected normalized http default port: %q", got)
		}
		if got := normalizeHostEndpoint("git@bitbucket.local:scm/PRJ/repo.git"); got != "bitbucket.local:22" {
			t.Fatalf("unexpected normalized scp endpoint: %q", got)
		}
		if got := normalizeHostEndpoint("ssh://git@bitbucket.local/scm/PRJ/repo.git"); got != "bitbucket.local:22" {
			t.Fatalf("unexpected normalized ssh endpoint: %q", got)
		}
		if got := normalizeHostEndpoint("bad host value"); got != "" {
			t.Fatalf("expected invalid host to normalize to empty, got %q", got)
		}
		if got := normalizeHostEndpoint("http://[::1"); got != "" {
			t.Fatalf("expected malformed URL to normalize to empty, got %q", got)
		}
	})

	t.Run("non repository errors are recognized", func(t *testing.T) {
		if isNonRepositoryError(nil) {
			t.Fatal("did not expect nil error to match")
		}
		if !isNonRepositoryError(errors.New("not a git repository")) {
			t.Fatal("expected non-repository error to match")
		}
		if isNonRepositoryError(errors.New("permission denied")) {
			t.Fatal("did not expect unrelated error to match")
		}
	})

	t.Run("authenticated host lookup includes runtime and stored profiles", func(t *testing.T) {
		lookup := authenticatedHostLookup(
			config.AppConfig{BitbucketURL: "http://runtime.local:7990"},
			config.StoredConfig{Hosts: map[string]config.StoredProfile{
				"blank":                    {URL: ""},
				"malformed":                {URL: "http://[::1"},
				"http://stored.local:7990": {URL: "http://stored.local:7990", Aliases: []string{"git.stored.local:7999"}},
			}},
		)

		if lookup["runtime.local:7990"] == "" {
			t.Fatal("expected runtime host in lookup")
		}
		if lookup["stored.local:7990"] == "" {
			t.Fatal("expected stored host in lookup")
		}
		if lookup["git.stored.local:7999"] == "" {
			t.Fatal("expected stored alias in lookup")
		}
	})

	t.Run("normalize host endpoint loose collapses default web ports", func(t *testing.T) {
		if got := normalizeHostEndpointLoose("https://bitbucket.local"); got != "bitbucket.local" {
			t.Fatalf("unexpected https loose endpoint: %q", got)
		}
		if got := normalizeHostEndpointLoose("http://bitbucket.local"); got != "bitbucket.local" {
			t.Fatalf("unexpected http loose endpoint: %q", got)
		}
		if got := normalizeHostEndpointLoose("ssh://git@bitbucket.local:7999/scm/PRJ/repo.git"); got != "bitbucket.local:7999" {
			t.Fatalf("unexpected ssh loose endpoint: %q", got)
		}
	})

	t.Run("normalize remote endpoint supports scp and https forms", func(t *testing.T) {
		if got := normalizeRemoteEndpoint(""); got != "" {
			t.Fatalf("expected empty remote endpoint to normalize to empty, got %q", got)
		}
		if got := normalizeRemoteEndpoint("git@bitbucket.local:scm/PRJ/repo.git"); got != "bitbucket.local:22" {
			t.Fatalf("unexpected scp remote endpoint: %q", got)
		}
		if got := normalizeRemoteEndpoint("https://bitbucket.local/scm/PRJ/repo.git"); got != "bitbucket.local" {
			t.Fatalf("unexpected https remote endpoint: %q", got)
		}
		if got := normalizeRemoteEndpoint("://bad"); got != "" {
			t.Fatalf("expected invalid remote endpoint to normalize to empty, got %q", got)
		}
	})

	t.Run("normalize host endpoint loose handles invalid scp forms", func(t *testing.T) {
		if got := normalizeHostEndpointLoose(""); got != "" {
			t.Fatalf("expected empty loose endpoint to normalize to empty, got %q", got)
		}
		if got := normalizeHostEndpointLoose("git@bitbucket.local"); got != "" {
			t.Fatalf("expected invalid scp endpoint to normalize to empty, got %q", got)
		}
		if got := normalizeHostEndpointLoose("https://bitbucket.local:7990"); got != "bitbucket.local:7990" {
			t.Fatalf("unexpected explicit port endpoint: %q", got)
		}
		if got := normalizeHostEndpointLoose("://bad"); got != "" {
			t.Fatalf("expected invalid endpoint to normalize to empty, got %q", got)
		}
	})

	t.Run("parse bitbucket remote malformed forms", func(t *testing.T) {
		if _, _, _, ok := parseBitbucketRemote("https://%zz"); ok {
			t.Fatal("expected malformed https remote to fail")
		}
		if _, _, _, ok := parseBitbucketRemote("git@bitbucket.local"); ok {
			t.Fatal("expected ssh remote without colon to fail")
		}
		if _, _, _, ok := parseBitbucketRemote("git@bitbucket.local:"); ok {
			t.Fatal("expected ssh remote with empty path to fail")
		}
	})

	t.Run("parse bitbucket path single segment fails", func(t *testing.T) {
		if _, _, ok := parseBitbucketPath("repo"); ok {
			t.Fatal("expected single-segment path parse to fail")
		}
	})

	t.Run("infer context ignores remotes without authenticated host match", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes:  []git.Remote{{Name: "origin", URL: "https://other-host.local/scm/PRJ/repo.git"}},
			}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context for unmatched host, got %+v", inferred)
		}
	})

	t.Run("infer context ignores non-repository errors from remotes", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				listErr:  errors.New("fatal: not a git repository"),
			}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("expected nil error for non-repository remotes listing, got: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context, got %+v", inferred)
		}
	})

	t.Run("infer context returns backend errors", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{rootErr: errors.New("backend failure")}
		}

		_, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err == nil {
			t.Fatal("expected backend error to be returned")
		}
	})

	t.Run("infer context with nil backend returns nil", func(t *testing.T) {
		gitBackendFactory = func() git.Backend { return nil }

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("expected nil error, got: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context, got %+v", inferred)
		}
	})

	t.Run("infer context with no remotes returns nil", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{repoRoot: "/tmp/repo", remotes: nil}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context, got %+v", inferred)
		}
	})

	t.Run("infer context with no authenticated hosts returns nil", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "")
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{repoRoot: "/tmp/repo", remotes: []git.Remote{{Name: "origin", URL: "https://bitbucket.local/scm/PRJ/repo.git"}}}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context, got %+v", inferred)
		}
	})

	t.Run("infer context returns nil when working directory cannot be resolved", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("simulates an unresolvable CWD by deleting it; Windows forbids removing a directory that is the process working directory")
		}
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{repoRoot: "/tmp/repo", remotes: []git.Remote{{Name: "origin", URL: "https://bitbucket.local/scm/PRJ/repo.git"}}}
		}

		originalDirectory, err := os.Getwd()
		if err != nil {
			t.Fatalf("getwd failed: %v", err)
		}
		badDirectory := t.TempDir()
		if err := os.Chdir(badDirectory); err != nil {
			t.Fatalf("chdir failed: %v", err)
		}
		if err := os.RemoveAll(badDirectory); err != nil {
			t.Fatalf("remove temp directory failed: %v", err)
		}
		t.Cleanup(func() { _ = os.Chdir(originalDirectory) })

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("expected nil error when getwd fails, got: %v", err)
		}
		if inferred != nil {
			t.Fatalf("expected nil inferred context when getwd fails, got %+v", inferred)
		}
	})

	t.Run("infer context returns remote listing errors", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{repoRoot: "/tmp/repo", listErr: errors.New("remote listing failed")}
		}

		_, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err == nil {
			t.Fatal("expected remote listing error")
		}
	})

	t.Run("infer context skips invalid remote entries", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes: []git.Remote{
					{Name: "invalid", URL: "not-a-remote"},
					{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/repo.git"},
				},
			}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred == nil || inferred.ProjectKey != "PRJ" {
			t.Fatalf("expected valid remote to be selected, got %+v", inferred)
		}
	})

	t.Run("infer context ambiguity sorts by remote and project details", func(t *testing.T) {
		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes: []git.Remote{
					{Name: "alpha", URL: "https://bitbucket.local:7990/scm/PRJ/b.git"},
					{Name: "alpha", URL: "https://bitbucket.local:7990/scm/PRJ/a.git"},
					{Name: "beta", URL: "https://bitbucket.local:7990/scm/ZZZ/z.git"},
				},
			}
		}

		_, err := inferRepositoryContextFromGit(config.AppConfig{BitbucketURL: "https://bitbucket.local:7990"})
		if err == nil {
			t.Fatal("expected ambiguity error")
		}
		if !strings.Contains(err.Error(), "ambiguous git remote context") {
			t.Fatalf("expected ambiguity guidance, got: %v", err)
		}
	})

	t.Run("infer context matches stored alias remote to canonical host", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
		t.Setenv("BB_CONFIG_PATH", configPath)
		t.Setenv("BB_DISABLE_STORED_CONFIG", "")
		if _, err := config.SaveLogin(config.LoginInput{Host: "https://bitbucket.local", Token: "tok", SetDefault: true}); err != nil {
			t.Fatalf("save login failed: %v", err)
		}
		if _, err := config.SetHostAliases("https://bitbucket.local", []string{"git.bitbucket.local:7999"}); err != nil {
			t.Fatalf("set aliases failed: %v", err)
		}

		gitBackendFactory = func() git.Backend {
			return inferenceGitBackendStub{
				repoRoot: "/tmp/repo",
				remotes:  []git.Remote{{Name: "origin", URL: "ssh://git@git.bitbucket.local:7999/scm/PRJ/repo.git"}},
			}
		}

		inferred, err := inferRepositoryContextFromGit(config.AppConfig{})
		if err != nil {
			t.Fatalf("infer context failed: %v", err)
		}
		if inferred == nil {
			t.Fatal("expected inferred context from alias remote")
		}
		if inferred.Host != "https://bitbucket.local" || inferred.ProjectKey != "PRJ" || inferred.Slug != "repo" {
			t.Fatalf("unexpected inferred context: %+v", inferred)
		}
	})

	t.Run("parse bitbucket remote invalid input", func(t *testing.T) {
		if _, _, _, ok := parseBitbucketRemote("not-a-remote"); ok {
			t.Fatal("expected invalid remote parsing to fail")
		}
	})

	t.Run("parse bitbucket path fallback project slash repo", func(t *testing.T) {
		project, slug, ok := parseBitbucketPath("PRJ/repo.git")
		if !ok || project != "PRJ" || slug != "repo" {
			t.Fatalf("unexpected fallback parse result: ok=%v project=%q slug=%q", ok, project, slug)
		}
	})
}

func TestRootCommandPreRunPropagatesInferenceErrors(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() {
		gitBackendFactory = originalFactory
	})

	gitBackendFactory = func() git.Backend {
		return inferenceGitBackendStub{
			repoRoot: "/tmp/repo",
			remotes: []git.Remote{
				{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"},
				{Name: "upstream", URL: "https://bitbucket.local:7990/scm/ALT/demo.git"},
			},
		}
	}

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.local:7990")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	command := NewRootCommand()
	command.SetOut(&bytes.Buffer{})
	command.SetErr(&bytes.Buffer{})
	command.SetArgs([]string{"branch", "list"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected inference ambiguity error")
	}
	if apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation exit code, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestLoadConfigAndClientAndClientFactoryBranches(t *testing.T) {
	t.Run("load config failure", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "://broken")
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

		_, _, err := (&rootOptions{}).loadConfigAndClient()
		if err == nil {
			t.Fatal("expected config load failure")
		}
	})

	t.Run("client factory success", func(t *testing.T) {
		// A URL, not a server: building a client makes no request, and the
		// listener this used to stand up was never asked anything. Whether the
		// client can then reach Bitbucket is TestLiveTransportIdentityAndHealth.
		client, err := newAPIClientFromConfig(config.AppConfig{BitbucketURL: "http://bitbucket.invalid"})
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})
}

func TestLoadQualityRepoAndServiceBranches(t *testing.T) {
	t.Run("config load failure", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "://broken")
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")

		_, _, err := (&rootOptions{}).loadQualityRepoAndService("")
		if err == nil {
			t.Fatal("expected config load failure")
		}
	})

	t.Run("invalid selector failure", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "http://localhost:7990")
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
		t.Setenv("BITBUCKET_REPO_SLUG", "demo")

		_, _, err := (&rootOptions{}).loadQualityRepoAndService("bad-format")
		if err == nil {
			t.Fatal("expected repository selector validation error")
		}
		if apperrors.ExitCode(err) != 2 {
			t.Fatalf("expected validation exit code 2, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("success", func(t *testing.T) {
		// Resolving the repository and building the service makes no request,
		// so this needs a URL rather than a listener.
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		t.Setenv("BITBUCKET_URL", "http://bitbucket.invalid")
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
		t.Setenv("BITBUCKET_REPO_SLUG", "demo")

		repo, service, err := (&rootOptions{}).loadQualityRepoAndService("")
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if repo.ProjectKey != "TEST" || repo.Slug != "demo" {
			t.Fatalf("unexpected repository ref: %+v", repo)
		}
		if service == nil {
			t.Fatal("expected non-nil service")
		}
	})
}

func TestResolveDiffOutputModeAndWriters(t *testing.T) {
	_, err := resolveDiffOutputMode(true, true, false)
	if err == nil {
		t.Fatal("expected validation error for multiple output modes")
	}

	mode, err := resolveDiffOutputMode(false, false, true)
	if err != nil || mode != diff.OutputKindNameOnly {
		t.Fatalf("expected name-only mode, got mode=%q err=%v", mode, err)
	}

	result := diff.Result{
		Names: []string{"a.txt", "b.go"},
		// The endpoint returns one summary object, not a row per file. This
		// fixture invented a shape Bitbucket never sends (#526).
		Stats: diff.StatsSummary{"filesChanged": float64(1), "totalInsertions": float64(1), "totalDeletions": float64(2)},
		Patch: "diff --git a/a.txt b/a.txt",
	}

	nameBuffer := &bytes.Buffer{}
	if err := writeDiffResult(nameBuffer, false, diff.OutputKindNameOnly, result); err != nil {
		t.Fatalf("expected no error writing names, got: %v", err)
	}
	if !strings.Contains(nameBuffer.String(), "a.txt") {
		t.Fatalf("expected name output, got: %s", nameBuffer.String())
	}

	statBuffer := &bytes.Buffer{}
	if err := writeDiffResult(statBuffer, true, diff.OutputKindStat, result); err != nil {
		t.Fatalf("expected no error writing stats json, got: %v", err)
	}
	if !strings.Contains(statBuffer.String(), "totalInsertions") {
		t.Fatalf("expected stats json output, got: %s", statBuffer.String())
	}

	patchBuffer := &bytes.Buffer{}
	if err := writeDiffResult(patchBuffer, false, diff.OutputKindPatch, result); err != nil {
		t.Fatalf("expected no error writing patch, got: %v", err)
	}
	if !strings.Contains(patchBuffer.String(), "diff --git") {
		t.Fatalf("expected patch output, got: %s", patchBuffer.String())
	}

	rawMode, err := resolveDiffOutputMode(false, false, false)
	if err != nil || rawMode != diff.OutputKindRaw {
		t.Fatalf("expected raw mode, got mode=%q err=%v", rawMode, err)
	}

	statPlainBuffer := &bytes.Buffer{}
	if err := writeDiffResult(statPlainBuffer, false, diff.OutputKindStat, result); err != nil {
		t.Fatalf("expected no error writing stats plain mode, got: %v", err)
	}
	if !strings.Contains(statPlainBuffer.String(), "totalDeletions") {
		t.Fatalf("expected stats plain output, got: %s", statPlainBuffer.String())
	}

	rawJSONBuffer := &bytes.Buffer{}
	if err := writeDiffResult(rawJSONBuffer, true, diff.OutputKindRaw, result); err != nil {
		t.Fatalf("expected no error writing raw json, got: %v", err)
	}
	if !strings.Contains(rawJSONBuffer.String(), `"patch": "diff --git a/a.txt b/a.txt"`) {
		t.Fatalf("expected raw json patch output, got: %s", rawJSONBuffer.String())
	}
}

func TestCommentHelpersAndSafeHelpers(t *testing.T) {
	comment := openapigenerated.RestComment{}
	if commentIDString(comment) != "unknown" {
		t.Fatalf("expected unknown id")
	}
	if formatCommentSummary(comment) != "[unknown v?] <empty>" {
		t.Fatalf("unexpected summary for empty comment: %s", formatCommentSummary(comment))
	}

	id := int64(42)
	version := int32(3)
	text := " hello "
	comment = openapigenerated.RestComment{Id: &id, Version: &version, Text: &text}
	if commentIDString(comment) != "42" {
		t.Fatalf("expected comment id 42")
	}
	if formatCommentSummary(comment) != "[42 v3] hello" {
		t.Fatalf("unexpected comment summary: %s", formatCommentSummary(comment))
	}

	if safederef.String(nil) != "" {
		t.Fatal("expected empty safe string")
	}
	if safederef.Int32(nil) != 0 {
		t.Fatal("expected zero safe int32")
	}
	if safederef.Int64(nil) != 0 {
		t.Fatal("expected zero safe int64")
	}
	if len(safederef.StringSlice(nil)) != 0 {
		t.Fatal("expected empty safe string slice")
	}

	s := "x"
	i32 := int32(9)
	i64 := int64(10)
	if safederef.String(&s) != "x" || safederef.Int32(&i32) != 9 || safederef.Int64(&i64) != 10 {
		t.Fatal("expected pointer helper values")
	}

	tagType := openapigenerated.RestTagTypeTAG
	buildState := openapigenerated.RestBuildStatusStateSUCCESSFUL
	insight := openapigenerated.PASS
	if safeStringFromTagType(&tagType) != "TAG" {
		t.Fatal("unexpected tag type conversion")
	}
	if safeStringFromBuildState(&buildState) != "SUCCESSFUL" {
		t.Fatal("unexpected build state conversion")
	}
	if safeStringFromInsightResult(&insight) != "PASS" {
		t.Fatal("unexpected insight result conversion")
	}

	if safeStringFromTagType(nil) != "" {
		t.Fatal("expected empty string for nil tag type")
	}
	if safeStringFromBuildState(nil) != "" {
		t.Fatal("expected empty string for nil build state")
	}
	if safeStringFromInsightResult(nil) != "" {
		t.Fatal("expected empty string for nil insight result")
	}

	var detailedComment openapigenerated.RestComment
	if err := json.Unmarshal([]byte(`{"id":42,"version":3,"text":" hello ","state":"OPEN","author":{"name":"octocat","displayName":"Octo Cat"},"anchor":{"path":{"parent":"src","name":"main.go"}}}`), &detailedComment); err != nil {
		t.Fatalf("unmarshal detailed comment: %v", err)
	}
	detail := formatCommentDetail(detailedComment)
	if !strings.Contains(detail, "Path: src/main.go") || !strings.Contains(detail, "Author: Octo Cat") || !strings.Contains(detail, "State: OPEN") {
		t.Fatalf("unexpected comment detail output: %s", detail)
	}
	if commentAnchorPath(detailedComment) != "src/main.go" {
		t.Fatalf("unexpected anchor path: %s", commentAnchorPath(detailedComment))
	}
	if commentAuthorName(detailedComment) != "Octo Cat" {
		t.Fatalf("unexpected author display name: %s", commentAuthorName(detailedComment))
	}

	detailedComment = openapigenerated.RestComment{}
	if err := json.Unmarshal([]byte(`{"author":{"name":"octocat"},"anchor":{"path":{"name":"main.go"}}}`), &detailedComment); err != nil {
		t.Fatalf("unmarshal fallback comment: %v", err)
	}
	if commentAuthorName(detailedComment) != "octocat" {
		t.Fatalf("expected fallback author name, got: %s", commentAuthorName(detailedComment))
	}
	if commentAnchorPath(detailedComment) != "main.go" {
		t.Fatalf("expected filename anchor path, got: %s", commentAnchorPath(detailedComment))
	}

	detailedComment = openapigenerated.RestComment{}
	if commentAuthorName(detailedComment) != "" {
		t.Fatalf("expected empty author name for nil author, got: %s", commentAuthorName(detailedComment))
	}
	if commentAnchorPath(detailedComment) != "" {
		t.Fatalf("expected empty anchor path for nil anchor, got: %s", commentAnchorPath(detailedComment))
	}
	if err := json.Unmarshal([]byte(`{"anchor":{"path":{"parent":"src","name":""}}}`), &detailedComment); err != nil {
		t.Fatalf("unmarshal empty-name comment: %v", err)
	}
	if commentAnchorPath(detailedComment) != "src" {
		t.Fatalf("expected parent-only anchor path, got: %s", commentAnchorPath(detailedComment))
	}
	if err := json.Unmarshal([]byte(`{"anchor":{"path":{"parent":"","name":"main.go"}}}`), &detailedComment); err != nil {
		t.Fatalf("unmarshal empty-parent comment: %v", err)
	}
	if commentAnchorPath(detailedComment) != "main.go" {
		t.Fatalf("expected child-only anchor path, got: %s", commentAnchorPath(detailedComment))
	}
}

func TestWriteJSONMarshalError(t *testing.T) {
	err := writeJSON(&bytes.Buffer{}, map[string]any{"bad": func() {}})
	if err == nil {
		t.Fatal("expected marshal error")
	}
}

func TestSafeUsersHelper(t *testing.T) {
	if len(safeUsers(nil)) != 0 {
		t.Fatal("expected safeUsers(nil) to return empty slice")
	}

	name := "alice"
	users := []openapigenerated.RestApplicationUser{{Name: &name}}
	if len(safeUsers(&users)) != 1 {
		t.Fatal("expected safeUsers to return provided users")
	}
}

func TestAuthLoginAndLogoutJSON(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "auth-config.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	loginCommand := NewRootCommand()
	loginBuffer := &bytes.Buffer{}
	loginCommand.SetOut(loginBuffer)
	loginCommand.SetErr(loginBuffer)
	loginCommand.SetIn(strings.NewReader("abc123"))
	loginCommand.SetArgs([]string{"--json", "auth", "login", "http://localhost:7990", "--token-stdin", "--set-default"})
	if err := loginCommand.Execute(); err != nil {
		t.Fatalf("auth login json failed: %v", err)
	}
	if !strings.Contains(loginBuffer.String(), `"authMode": "token"`) {
		t.Fatalf("expected token auth mode in login output, got: %s", loginBuffer.String())
	}

	logoutCommand := NewRootCommand()
	logoutBuffer := &bytes.Buffer{}
	logoutCommand.SetOut(logoutBuffer)
	logoutCommand.SetErr(logoutBuffer)
	logoutCommand.SetArgs([]string{"--json", "auth", "logout", "--host", "http://localhost:7990"})
	if err := logoutCommand.Execute(); err != nil {
		t.Fatalf("auth logout json failed: %v", err)
	}
	if !strings.Contains(logoutBuffer.String(), `"status": "ok"`) {
		t.Fatalf("expected status ok in logout output, got: %s", logoutBuffer.String())
	}
}

func TestAuthLoginWithPositionalHost(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "auth-config-positional.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	loginCommand := NewRootCommand()
	loginBuffer := &bytes.Buffer{}
	loginCommand.SetOut(loginBuffer)
	loginCommand.SetErr(loginBuffer)
	loginCommand.SetIn(strings.NewReader("abc123"))
	loginCommand.SetArgs([]string{"--json", "auth", "login", "http://positional.local:7990", "--token-stdin", "--set-default"})
	if err := loginCommand.Execute(); err != nil {
		t.Fatalf("auth login json with positional host failed: %v", err)
	}
	if !strings.Contains(loginBuffer.String(), `"host": "http://positional.local:7990"`) {
		t.Fatalf("expected positional host in login output, got: %s", loginBuffer.String())
	}
}

func TestAuthStatusHostOverrideAndHumanLoginLogout(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "auth-config.yaml"))
	t.Setenv("BB_DISABLE_STORED_CONFIG", "0")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	statusCommand := NewRootCommand()
	statusBuffer := &bytes.Buffer{}
	statusCommand.SetOut(statusBuffer)
	statusCommand.SetErr(statusBuffer)
	statusCommand.SetArgs([]string{"auth", "status", "--host", "http://example.local:7990"})
	if err := statusCommand.Execute(); err != nil {
		t.Fatalf("auth status with host override failed: %v", err)
	}
	if !strings.Contains(statusBuffer.String(), "http://example.local:7990") {
		t.Fatalf("expected overridden host in status output, got: %s", statusBuffer.String())
	}

	loginCommand := NewRootCommand()
	loginBuffer := &bytes.Buffer{}
	loginCommand.SetOut(loginBuffer)
	loginCommand.SetErr(loginBuffer)
	loginCommand.SetIn(strings.NewReader("abc123"))
	loginCommand.SetArgs([]string{"auth", "login", "http://example.local:7990", "--token-stdin", "--set-default"})
	if err := loginCommand.Execute(); err != nil {
		t.Fatalf("auth login human failed: %v", err)
	}
	if !strings.Contains(loginBuffer.String(), "Stored credentials for") {
		t.Fatalf("expected human login output, got: %s", loginBuffer.String())
	}

	logoutCommand := NewRootCommand()
	logoutBuffer := &bytes.Buffer{}
	logoutCommand.SetOut(logoutBuffer)
	logoutCommand.SetErr(logoutBuffer)
	logoutCommand.SetArgs([]string{"auth", "logout", "--host", "http://example.local:7990"})
	if err := logoutCommand.Execute(); err != nil {
		t.Fatalf("auth logout human failed: %v", err)
	}
	if !strings.Contains(logoutBuffer.String(), "Stored credentials removed") {
		t.Fatalf("expected human logout output, got: %s", logoutBuffer.String())
	}
}

func TestAuthTokenURLCommand(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")

	human := NewRootCommand()
	humanBuffer := &bytes.Buffer{}
	human.SetOut(humanBuffer)
	human.SetErr(humanBuffer)
	human.SetArgs([]string{"auth", "token-url", "--host", "https://bitbucket.acme.corp"})
	if err := human.Execute(); err != nil {
		t.Fatalf("auth token-url human failed: %v", err)
	}
	if !strings.Contains(humanBuffer.String(), "https://bitbucket.acme.corp/plugins/servlet/access-tokens/manage") {
		t.Fatalf("expected PAT URL in human output, got: %s", humanBuffer.String())
	}

	jsonCmd := NewRootCommand()
	jsonBuffer := &bytes.Buffer{}
	jsonCmd.SetOut(jsonBuffer)
	jsonCmd.SetErr(jsonBuffer)
	jsonCmd.SetArgs([]string{"--json", "auth", "token-url"})
	if err := jsonCmd.Execute(); err != nil {
		t.Fatalf("auth token-url json failed: %v", err)
	}
	if !strings.Contains(jsonBuffer.String(), "/plugins/servlet/access-tokens/manage") && !strings.Contains(jsonBuffer.String(), "/plugins/servlet/access-tokens/users/") {
		t.Fatalf("expected token_url in json output, got: %s", jsonBuffer.String())
	}
}

func TestResolveRepositoryReferenceWrappers(t *testing.T) {
	// The slug is carried on the configuration rather than read from the
	// environment at the point of use, so no t.Setenv is needed -- and this test
	// can be parallel.
	t.Parallel()

	cfg := config.AppConfig{ProjectKey: "TEST", RepoSlug: "demo"}

	diffRepo, err := resolveRepositoryReference("", cfg)
	if err != nil || diffRepo.ProjectKey != "TEST" || diffRepo.Slug != "demo" {
		t.Fatalf("unexpected diff repository reference: %+v err=%v", diffRepo, err)
	}

	settingsRepo, err := resolveRepositorySettingsReference("", cfg)
	if err != nil || settingsRepo.ProjectKey != "TEST" || settingsRepo.Slug != "demo" {
		t.Fatalf("unexpected settings repository reference: %+v err=%v", settingsRepo, err)
	}

	tagRepo, err := resolveTagRepositoryReference("", cfg)
	if err != nil || tagRepo.ProjectKey != "TEST" || tagRepo.Slug != "demo" {
		t.Fatalf("unexpected tag repository reference: %+v err=%v", tagRepo, err)
	}

	branchRepo, err := resolveBranchRepositoryReference("", cfg)
	if err != nil || branchRepo.ProjectKey != "TEST" || branchRepo.Slug != "demo" {
		t.Fatalf("unexpected branch repository reference: %+v err=%v", branchRepo, err)
	}

	qualityRepo, err := resolveQualityRepositoryReference("", cfg)
	if err != nil || qualityRepo.ProjectKey != "TEST" || qualityRepo.Slug != "demo" {
		t.Fatalf("unexpected quality repository reference: %+v err=%v", qualityRepo, err)
	}

	_, err = resolveRepositoryReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid diff repository selector")
	}

	_, err = resolveRepositorySettingsReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid settings repository selector")
	}

	_, err = resolveTagRepositoryReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid tag repository selector")
	}

	_, err = resolveBranchRepositoryReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid branch repository selector")
	}

	_, err = resolveQualityRepositoryReference("bad-format", config.AppConfig{})
	if err == nil {
		t.Fatal("expected validation error for invalid quality repository selector")
	}
}

func TestBuildAndInsightsValidationErrorPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	tests := []struct {
		name string
		args []string
	}{
		{name: "build required create invalid json", args: []string{"build", "required", "create", "--body", "{"}},
		{name: "build required update invalid id", args: []string{"build", "required", "update", "bad", "--body", `{"buildParentKeys":["ci"]}`}},
		{name: "build required update invalid json", args: []string{"build", "required", "update", "12", "--body", "{"}},
		{name: "build required delete invalid id", args: []string{"build", "required", "delete", "bad"}},
		{name: "insights report set invalid json", args: []string{"insights", "report", "set", "abc", "lint", "--body", "{"}},
		{name: "insights annotation add invalid json", args: []string{"insights", "annotation", "add", "abc", "lint", "--body", "{"}},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := NewRootCommand()
			command.SetOut(&bytes.Buffer{})
			command.SetErr(&bytes.Buffer{})
			command.SetArgs(testCase.args)

			err := command.Execute()
			if err == nil {
				t.Fatalf("expected validation error for args: %v", testCase.args)
			}
			if apperrors.ExitCode(err) != 2 {
				t.Fatalf("expected validation exit code 2, got %d (%v)", apperrors.ExitCode(err), err)
			}
		})
	}
}

// TestDiffPullRequestRejectsBadInvocations covers the error paths of the shared
// diff-pull-request command, which both `bb diff pr` and `bb pr diff` use.
func TestDiffPullRequestRejectsBadInvocations(t *testing.T) {
	t.Run("conflicting output modes", func(t *testing.T) {
		t.Setenv("BITBUCKET_URL", "https://bitbucket.example.invalid")
		t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
		t.Setenv("BITBUCKET_REPO_SLUG", "demo")
		t.Setenv("BITBUCKET_TOKEN", "token")
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")

		command := NewRootCommand()
		buffer := &bytes.Buffer{}
		command.SetOut(buffer)
		command.SetErr(buffer)
		command.SetArgs([]string{"pr", "diff", "1", "--patch", "--stat"})

		if err := command.Execute(); err == nil {
			t.Fatal("expected --patch with --stat to be rejected")
		}
	})

	t.Run("no repository context", func(t *testing.T) {
		t.Setenv("BITBUCKET_URL", "https://bitbucket.example.invalid")
		t.Setenv("BITBUCKET_PROJECT_KEY", "")
		t.Setenv("BITBUCKET_REPO_SLUG", "")
		t.Setenv("BITBUCKET_TOKEN", "token")
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")

		command := NewRootCommand()
		buffer := &bytes.Buffer{}
		command.SetOut(buffer)
		command.SetErr(buffer)
		command.SetArgs([]string{"pr", "diff", "1"})

		if err := command.Execute(); err == nil {
			t.Fatal("expected a missing repository to be rejected")
		}
	})
}

func (stub inferenceGitBackendStub) WorkingTreeState(context.Context, string) (git.WorkingTreeStatus, error) {
	return git.WorkingTreeStatus{}, nil
}

func (stub inferenceGitBackendStub) BranchExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (stub inferenceGitBackendStub) FastForward(context.Context, string, string) error {
	return nil
}

// TestRuntimeFlagsBecomeOverridesNotEnvironment replaces a test that asserted
// the opposite.
//
// The bridge used to write each global flag into a BB_* variable, and the old
// test checked those variables afterwards. That is the defect issue #458 is
// about: the write destroyed a value the user had set rather than outranking it
// for one invocation, and it outlived the command.
//
// Because the flags are values now, this test sets no environment at all --
// which is why it can be parallel. The old one could not: it needed six
// t.Setenv calls simply to stop the bridge leaking into later tests, and
// t.Setenv disqualifies a test from t.Parallel.
func TestRuntimeFlagsBecomeOverridesNotEnvironment(t *testing.T) {
	t.Parallel()

	options := &rootOptions{}
	if err := options.applyRuntimeFlagOverrides(nil); err != nil {
		t.Fatalf("a nil command should be a no-op, got: %v", err)
	}

	command := NewRootCommand()
	for flagName, value := range map[string]string{
		"ca-file":              " ",
		"client-cert":          "/path/to/client.crt",
		"client-key":           "/path/to/client.key",
		"insecure-skip-verify": "true",
		"request-timeout":      "30s",
		"retry-count":          "4",
		"retry-backoff":        "500ms",
	} {
		if err := command.PersistentFlags().Set(flagName, value); err != nil {
			t.Fatalf("set %s: %v", flagName, err)
		}
	}

	if err := options.applyRuntimeFlagOverrides(command); err != nil {
		t.Fatalf("applying overrides failed: %v", err)
	}

	runtime := options.runtime
	if runtime.CAFile == nil || *runtime.CAFile != "" {
		t.Errorf("a flag passed blank should be carried as empty, not dropped: %v", runtime.CAFile)
	}
	if runtime.ClientCert == nil || *runtime.ClientCert != "/path/to/client.crt" {
		t.Errorf("client cert = %v, want /path/to/client.crt", runtime.ClientCert)
	}
	if runtime.InsecureSkipVerify == nil || !*runtime.InsecureSkipVerify {
		t.Errorf("insecure-skip-verify = %v, want true", runtime.InsecureSkipVerify)
	}
	if runtime.RetryCount == nil || *runtime.RetryCount != 4 {
		t.Errorf("retry count = %v, want 4", runtime.RetryCount)
	}
	if runtime.RequestTimeout == nil || *runtime.RequestTimeout != "30s" {
		t.Errorf("request timeout = %v, want 30s", runtime.RequestTimeout)
	}
	if runtime.ClientKey == nil || *runtime.ClientKey != "/path/to/client.key" {
		t.Errorf("client key = %v, want /path/to/client.key", runtime.ClientKey)
	}
	if runtime.RetryBackoff == nil || *runtime.RetryBackoff != "500ms" {
		t.Errorf("retry backoff = %v, want 500ms", runtime.RetryBackoff)
	}
}

// TestAFlagLeftAloneDoesNotDisplaceTheEnvironment is the precedence layer
// ADR-021 describes and the implementation did not have.
func TestAFlagLeftAloneDoesNotDisplaceTheEnvironment(t *testing.T) {
	t.Parallel()

	options := &rootOptions{}
	if err := options.applyRuntimeFlagOverrides(NewRootCommand()); err != nil {
		t.Fatalf("applying overrides failed: %v", err)
	}

	if options.runtime.RetryCount != nil {
		t.Errorf("an unpassed flag produced an override (%v); the environment must keep its slot", options.runtime.RetryCount)
	}
	if options.runtime.CAFile != nil {
		t.Errorf("an unpassed flag produced an override (%v)", options.runtime.CAFile)
	}
}

// TestIssueCommandIsNotOffered pins that `bb issue` does not exist.
//
// gh has one and the muscle memory carries over, so an agent reaching for it
// has to be told the command is unknown rather than have it silently do
// something else. Cobra answers this without a server; the half of the old test
// that ran `pr list` against a mocked Bitbucket and read "#22" back out of its
// own fixture is live in TestLivePullRequestHumanOutput.
func TestIssueCommandIsNotOffered(t *testing.T) {
	command := NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs([]string{"issue", "list"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected `bb issue` to be unknown")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected an unknown-command error, got: %v", err)
	}
}

// TestRepoSettingsPullRequestsUpdateDryRunStateful is live now, in
// TestLiveGovernanceDryRunPredictionsReadRealState.
//
// It predicted an update from settings this file wrote, and proved the dry run
// did not write by failing the test from inside its own handler. The live
// version sets the value for real, asks for the opposite, requires "update",
// and then reads the settings back -- which is the same guarantee asked of the
// server rather than of a switch statement.
