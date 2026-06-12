package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
)

type cloneBackendStub struct {
	cloneCalls []cloneCall
	addCalls   []addRemoteCall
	cloneErr   error
	cloneErrs  []error
	addErr     error
}

type cloneCall struct {
	repositoryURL string
	options       git.CloneOptions
}

type addRemoteCall struct {
	repositoryDirectory string
	remote              git.Remote
}

func (stub *cloneBackendStub) Version(context.Context) (string, error) {
	return "", nil
}

func (stub *cloneBackendStub) Clone(_ context.Context, repositoryURL string, options git.CloneOptions) error {
	stub.cloneCalls = append(stub.cloneCalls, cloneCall{repositoryURL: repositoryURL, options: options})
	if len(stub.cloneErrs) > 0 {
		err := stub.cloneErrs[0]
		stub.cloneErrs = stub.cloneErrs[1:]
		if err != nil {
			return err
		}
		return nil
	}
	if stub.cloneErr != nil {
		return stub.cloneErr
	}
	return nil
}

func (stub *cloneBackendStub) AddRemote(_ context.Context, repositoryDirectory string, remote git.Remote) error {
	stub.addCalls = append(stub.addCalls, addRemoteCall{repositoryDirectory: repositoryDirectory, remote: remote})
	if stub.addErr != nil {
		return stub.addErr
	}
	return nil
}

func (stub *cloneBackendStub) Fetch(context.Context, string, git.FetchOptions) error {
	return nil
}

func (stub *cloneBackendStub) Checkout(context.Context, string, git.CheckoutOptions) error {
	return nil
}

func (stub *cloneBackendStub) RepositoryRoot(context.Context, string) (string, error) {
	return "", nil
}

func (stub *cloneBackendStub) ListRemotes(context.Context, string) ([]git.Remote, error) {
	return nil, nil
}

func TestRepoCloneCommandClonesWithDefaults(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	output, err := executeTestCLI(t, "repo", "clone", "PRJ/demo")
	if err != nil {
		t.Fatalf("repo clone failed: %v", err)
	}

	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one clone call, got %d", len(stub.cloneCalls))
	}

	call := stub.cloneCalls[0]
	if call.repositoryURL != "git@bitbucket.example.com:scm/PRJ/demo.git" {
		t.Fatalf("unexpected clone url: %s", call.repositoryURL)
	}
	if call.options.Directory != "demo" {
		t.Fatalf("unexpected clone directory: %s", call.options.Directory)
	}
	if len(call.options.ExtraArgs) != 0 {
		t.Fatalf("expected no extra git args, got: %#v", call.options.ExtraArgs)
	}

	if !strings.Contains(output, "Cloned PRJ/demo into demo") {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestTopLevelCloneCommandClonesWithDefaults(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	output, err := executeTestCLI(t, "clone", "PRJ/demo")
	if err != nil {
		t.Fatalf("top-level clone failed: %v", err)
	}

	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one clone call, got %d", len(stub.cloneCalls))
	}

	call := stub.cloneCalls[0]
	if call.repositoryURL != "git@bitbucket.example.com:scm/PRJ/demo.git" {
		t.Fatalf("unexpected clone url: %s", call.repositoryURL)
	}
	if call.options.Directory != "demo" {
		t.Fatalf("unexpected clone directory: %s", call.options.Directory)
	}
	if !strings.Contains(output, "Cloned PRJ/demo into demo") {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestRepoCloneCommandHonorsDirectoryAndGitFlags(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com/bitbucket")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	output, err := executeTestCLI(t, "--json", "repo", "clone", "PRJ/demo", "target-dir", "--", "--depth=7", "--branch=main")
	if err != nil {
		t.Fatalf("repo clone json failed: %v", err)
	}

	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one clone call, got %d", len(stub.cloneCalls))
	}

	call := stub.cloneCalls[0]
	if call.repositoryURL != "git@bitbucket.example.com:scm/PRJ/demo.git" {
		t.Fatalf("unexpected clone url: %s", call.repositoryURL)
	}
	if call.options.Directory != "target-dir" {
		t.Fatalf("unexpected clone directory: %s", call.options.Directory)
	}
	if len(call.options.ExtraArgs) != 2 {
		t.Fatalf("unexpected extra args: %#v", call.options.ExtraArgs)
	}
	if call.options.ExtraArgs[0] != "--depth=7" || call.options.ExtraArgs[1] != "--branch=main" {
		t.Fatalf("unexpected extra args: %#v", call.options.ExtraArgs)
	}

	if !strings.Contains(output, `"clone_url": "git@bitbucket.example.com:scm/PRJ/demo.git"`) {
		t.Fatalf("unexpected json output: %s", output)
	}
}

func TestRepoCloneCommandFallsBackToStoredHTTPToken(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	output, err := executeTestCLI(t, "repo", "clone", "PRJ/demo")
	if err != nil {
		t.Fatalf("repo clone failed: %v", err)
	}

	if len(stub.cloneCalls) != 2 {
		t.Fatalf("expected two clone attempts, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[0].repositoryURL != "git@bitbucket.example.com:scm/PRJ/demo.git" {
		t.Fatalf("unexpected ssh clone url: %s", stub.cloneCalls[0].repositoryURL)
	}
	if stub.cloneCalls[1].repositoryURL != "https://bitbucket.example.com/scm/PRJ/demo.git" {
		t.Fatalf("unexpected authenticated clone url: %s", stub.cloneCalls[1].repositoryURL)
	}
	if stub.cloneCalls[1].options.AuthToken != "test-token" {
		t.Fatalf("expected AuthToken to be 'test-token', got '%s'", stub.cloneCalls[1].options.AuthToken)
	}
	if !strings.Contains(output, "Cloned PRJ/demo into demo") {
		t.Fatalf("unexpected output: %s", output)
	}
}

func TestResolveCloneHTTPAuthUsesStoredAliasMatch(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := config.SaveLogin(config.LoginInput{Host: "https://bitbucket.example.com", Token: "stored-token", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}
	if _, err := config.SetHostAliases("https://bitbucket.example.com", []string{"git.example.com:7999"}); err != nil {
		t.Fatalf("set aliases failed: %v", err)
	}

	resolved, matchedHost, ok, err := resolveCloneHTTPAuth(config.AppConfig{BitbucketURL: "https://other.example.com"}, "ssh://git@git.example.com:7999/scm/PRJ/demo.git")
	if err != nil {
		t.Fatalf("resolve clone http auth failed: %v", err)
	}
	if !ok {
		t.Fatal("expected stored alias auth to be found")
	}
	if resolved.BitbucketURL != "https://bitbucket.example.com" || resolved.BitbucketToken != "stored-token" {
		t.Fatalf("unexpected resolved auth: %+v", resolved)
	}
	if matchedHost != "https://bitbucket.example.com" {
		t.Fatalf("expected canonical matched host, got %q", matchedHost)
	}
}

func TestResolveCloneHTTPAuthAliasLookupError(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", t.TempDir())
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, _, _, err := resolveCloneHTTPAuth(config.AppConfig{BitbucketURL: "https://bitbucket.example.com"}, "ssh://git@git.example.com:7999/scm/PRJ/demo.git"); err == nil {
		t.Fatal("expected config load error when config path is a directory")
	}
}

func TestResolveCloneHTTPAuthFallbackBranches(t *testing.T) {
	t.Run("falls back to matching runtime config when clone host matches", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		resolved, matchedHost, ok, err := resolveCloneHTTPAuth(config.AppConfig{BitbucketURL: "https://bitbucket.example.com", BitbucketToken: "tok"}, "https://bitbucket.example.com/scm/PRJ/demo.git")
		if err != nil {
			t.Fatalf("resolve clone auth failed: %v", err)
		}
		if !ok || resolved.BitbucketToken != "tok" {
			t.Fatalf("expected runtime auth fallback, got ok=%v cfg=%+v", ok, resolved)
		}
		if matchedHost != "https://bitbucket.example.com" {
			t.Fatalf("expected runtime matched host to stay canonical, got %q", matchedHost)
		}
	})

	t.Run("returns no auth when alias and host do not match", func(t *testing.T) {
		t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
		resolved, matchedHost, ok, err := resolveCloneHTTPAuth(config.AppConfig{BitbucketURL: "https://bitbucket.example.com", BitbucketToken: "tok"}, "https://other.example.com/scm/PRJ/demo.git")
		if err != nil {
			t.Fatalf("resolve clone auth failed: %v", err)
		}
		if ok || resolved.AuthMode() != "none" {
			t.Fatalf("expected no auth match, got ok=%v cfg=%+v", ok, resolved)
		}
		if matchedHost != "" {
			t.Fatalf("expected no matched host, got %q", matchedHost)
		}
	})

	t.Run("uses stored auth directly for matching clone host", func(t *testing.T) {
		configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
		t.Setenv("BB_CONFIG_PATH", configPath)
		t.Setenv("BB_DISABLE_STORED_CONFIG", "")

		if _, err := config.SaveLogin(config.LoginInput{Host: "https://bitbucket.example.com", Token: "stored-token", SetDefault: true}); err != nil {
			t.Fatalf("save login failed: %v", err)
		}

		resolved, matchedHost, ok, err := resolveCloneHTTPAuth(config.AppConfig{BitbucketURL: "https://other.example.com"}, "https://bitbucket.example.com/scm/PRJ/demo.git")
		if err != nil {
			t.Fatalf("resolve clone auth failed: %v", err)
		}
		if !ok || resolved.BitbucketToken != "stored-token" {
			t.Fatalf("expected stored auth match, got ok=%v cfg=%+v", ok, resolved)
		}
		if matchedHost != "https://bitbucket.example.com" {
			t.Fatalf("expected canonical stored host to be returned, got %q", matchedHost)
		}
	})
}

func TestCloneRepositoryWithAuthFallbackUsesCanonicalHostForAliasHTTPSRetry(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_URL", "https://other.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	if _, err := config.SaveLogin(config.LoginInput{Host: "https://bitbucket.example.com/context", Token: "stored-token", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}
	if _, err := config.SetHostAliases("https://bitbucket.example.com/context", []string{"git.example.com:7999"}); err != nil {
		t.Fatalf("set aliases failed: %v", err)
	}

	_, err := executeTestCLI(t, "repo", "clone", "ssh://git@git.example.com:7999/scm/PRJ/demo.git")
	if err != nil {
		t.Fatalf("repo clone failed: %v", err)
	}

	if len(stub.cloneCalls) != 2 {
		t.Fatalf("expected two clone attempts, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[1].repositoryURL != "https://bitbucket.example.com/context/scm/PRJ/demo.git" {
		t.Fatalf("expected canonical host https retry, got %s", stub.cloneCalls[1].repositoryURL)
	}
	if stub.cloneCalls[1].options.AuthToken != "stored-token" {
		t.Fatalf("expected AuthToken to be 'stored-token', got '%s'", stub.cloneCalls[1].options.AuthToken)
	}
}

func TestCloneRepositoryWithAuthFallbackRebuildsHTTPSRetryWhenContextPathDiffers(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BITBUCKET_URL", "https://other.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	if _, err := config.SaveLogin(config.LoginInput{Host: "https://bitbucket.example.com/context", Token: "stored-token", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}

	_, err := cloneRepositoryWithAuthFallback(
		NewRootCommand(),
		config.AppConfig{BitbucketURL: "https://other.example.com"},
		"https://bitbucket.example.com/scm/PRJ/demo.git",
		true,
		"https://bitbucket.example.com",
		repositorySelector{ProjectKey: "PRJ", Slug: "demo"},
		cloneTransportHTTPS,
		git.CloneOptions{Directory: "demo"},
		stub,
		false,
	)
	if err != nil {
		t.Fatalf("clone with auth fallback failed: %v", err)
	}

	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one https clone attempt, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[0].repositoryURL != "https://bitbucket.example.com/context/scm/PRJ/demo.git" {
		t.Fatalf("expected rebuilt canonical context-path clone url, got %s", stub.cloneCalls[0].repositoryURL)
	}
	if stub.cloneCalls[0].options.AuthToken != "stored-token" {
		t.Fatalf("expected AuthToken to be 'stored-token', got '%s'", stub.cloneCalls[0].options.AuthToken)
	}
}

func TestRepoCloneCommandHTTPSFlagSkipsSSHAndUsesTokenUsername(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_USERNAME", "admin")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	_, err := executeTestCLI(t, "repo", "clone", "--https", "PRJ/demo")
	if err != nil {
		t.Fatalf("repo clone with --https failed: %v", err)
	}

	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one HTTPS clone attempt, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[0].repositoryURL != "https://bitbucket.example.com/scm/PRJ/demo.git" {
		t.Fatalf("unexpected https clone url: %s", stub.cloneCalls[0].repositoryURL)
	}
	if stub.cloneCalls[0].options.AuthToken != "test-token" {
		t.Fatalf("expected AuthToken to be 'test-token', got '%s'", stub.cloneCalls[0].options.AuthToken)
	}
}

func TestRepoCloneCommandRejectsConflictingTransportFlags(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	_, err := executeTestCLI(t, "repo", "clone", "--ssh", "--https", "PRJ/demo")
	if err == nil {
		t.Fatal("expected transport flag validation error")
	}
	if !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("unexpected transport flag error: %v", err)
	}
}

func TestRepoCloneCommandPromptsForTokenAfterSSHFailure(t *testing.T) {
	// With a non-TTY (bytes.Buffer) stdin, canPromptForCloneLogin returns false and
	// the command falls through to a "no stored credentials" error.
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed")}}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetIn(bytes.NewBufferString("prompt-token\n"))
	command.SetArgs([]string{"repo", "clone", "PRJ/demo"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected auth error when stdin is not a TTY")
	}
	if !strings.Contains(err.Error(), "no stored HTTP credentials") {
		t.Fatalf("expected credentials error, got: %v", err)
	}
	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected only SSH clone attempt, got %d", len(stub.cloneCalls))
	}
}

func TestRepoCloneCommandValidationAndBackendFailure(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErr: errors.New("clone failed")}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	_, err := executeTestCLI(t, "repo", "clone", "badformat")
	if err == nil {
		t.Fatal("expected invalid selector error")
	}

	_, err = executeTestCLI(t, "repo", "clone", "PRJ/demo", "")
	if err == nil {
		t.Fatal("expected empty directory validation error")
	}

	_, err = executeTestCLI(t, "repo", "clone", "PRJ/demo", "target-dir", "--", "depth=1")
	if err == nil {
		t.Fatal("expected invalid extra git args error")
	}

	_, err = executeTestCLI(t, "repo", "clone", "PRJ/demo")
	if err == nil {
		t.Fatal("expected backend clone failure")
	}
	if !strings.Contains(err.Error(), "clone failed") {
		t.Fatalf("expected backend error in result, got: %v", err)
	}
}

func TestRepoCloneCommandSupportsURLSelectors(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	_, err := executeTestCLI(t, "repo", "clone", "https://bitbucket.other.example/scm/OPS/tooling.git")
	if err != nil {
		t.Fatalf("repo clone with URL selector failed: %v", err)
	}

	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one clone call, got %d", len(stub.cloneCalls))
	}

	call := stub.cloneCalls[0]
	if call.repositoryURL != "git@bitbucket.other.example:scm/OPS/tooling.git" {
		t.Fatalf("unexpected clone URL: %s", call.repositoryURL)
	}
	if call.options.Directory != "tooling" {
		t.Fatalf("unexpected clone directory: %s", call.options.Directory)
	}
}

func TestResolveHTTPCloneURLNormalizesSSHCloneHost(t *testing.T) {
	httpCloneURL, err := resolveHTTPCloneURL("ssh://git@bitbucket.example.com/scm/PRJ/demo.git", true, "ssh://git@bitbucket.example.com", repositorySelector{ProjectKey: "PRJ", Slug: "demo"})
	if err != nil {
		t.Fatalf("expected HTTP clone URL resolution to succeed, got: %v", err)
	}
	if httpCloneURL != "https://bitbucket.example.com/scm/PRJ/demo.git" {
		t.Fatalf("unexpected normalized HTTP clone URL: %s", httpCloneURL)
	}
}

func TestRepoCloneCommandNoUpstreamAndAddRemoteFailure(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{addErr: errors.New("add remote failed")}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/rest/api/1.0/projects/PRJ/repos/demo" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"origin":{"project":{"key":"UP"},"slug":"upstream-demo"}}`))
			return
		}
		http.NotFound(writer, request)
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	_, err := executeTestCLI(t, "repo", "clone", "--no-upstream", "PRJ/demo")
	if err != nil {
		t.Fatalf("repo clone with --no-upstream failed: %v", err)
	}
	if len(stub.addCalls) != 0 {
		t.Fatalf("expected no add-remote calls with --no-upstream, got: %#v", stub.addCalls)
	}

	_, err = executeTestCLI(t, "repo", "clone", "--upstream-remote-name", "@owner", "PRJ/demo")
	if err == nil {
		t.Fatal("expected add-remote failure")
	}
	if !strings.Contains(err.Error(), "add remote failed") {
		t.Fatalf("unexpected add-remote error: %v", err)
	}
	if len(stub.addCalls) == 0 {
		t.Fatal("expected add-remote to be attempted")
	}
	if stub.addCalls[len(stub.addCalls)-1].remote.Name != "up" {
		t.Fatalf("expected @owner to resolve to project key remote name, got: %s", stub.addCalls[len(stub.addCalls)-1].remote.Name)
	}
}

func TestBuildBitbucketCloneURL(t *testing.T) {
	cloneURL, err := buildBitbucketCloneURL("https://bitbucket.example.com/context", "PRJ", "demo")
	if err != nil {
		t.Fatalf("expected valid clone url, got: %v", err)
	}
	if cloneURL != "https://bitbucket.example.com/context/scm/PRJ/demo.git" {
		t.Fatalf("unexpected clone url: %s", cloneURL)
	}

	_, err = buildBitbucketCloneURL("bad-url", "PRJ", "demo")
	if err == nil {
		t.Fatal("expected invalid base URL error")
	}

	sshCloneURL, err := buildBitbucketSSHCloneURL("https://bitbucket.example.com/context", "PRJ", "demo")
	if err != nil {
		t.Fatalf("expected valid ssh clone url, got: %v", err)
	}
	if sshCloneURL != "git@bitbucket.example.com:scm/PRJ/demo.git" {
		t.Fatalf("unexpected ssh clone url: %s", sshCloneURL)
	}
}

func TestResolveRepositoryCloneInputParsesSelectors(t *testing.T) {
	cfg := config.AppConfig{BitbucketURL: "https://bitbucket.example.com", ProjectKey: "PRJ"}

	repo, host, usedURL, err := resolveRepositoryCloneInput("PRJ/repo", cfg)
	if err != nil {
		t.Fatalf("parse PROJECT/slug failed: %v", err)
	}
	if repo.ProjectKey != "PRJ" || repo.Slug != "repo" || host != "https://bitbucket.example.com" || usedURL {
		t.Fatalf("unexpected PROJECT/slug parse result: %+v host=%s usedURL=%v", repo, host, usedURL)
	}

	repo, host, usedURL, err = resolveRepositoryCloneInput("bb.company.local/OPS/tooling", cfg)
	if err != nil {
		t.Fatalf("parse host-qualified selector failed: %v", err)
	}
	if repo.ProjectKey != "OPS" || repo.Slug != "tooling" || host != "https://bb.company.local" || !usedURL {
		t.Fatalf("unexpected host-qualified parse result: %+v host=%s usedURL=%v", repo, host, usedURL)
	}

	repo, host, usedURL, err = resolveRepositoryCloneInput("https://bb.company.local/scm/OPS/tooling.git", cfg)
	if err != nil {
		t.Fatalf("parse URL selector failed: %v", err)
	}
	if repo.ProjectKey != "OPS" || repo.Slug != "tooling" || host != "https://bb.company.local" || !usedURL {
		t.Fatalf("unexpected URL parse result: %+v host=%s usedURL=%v", repo, host, usedURL)
	}
}

func TestCloneHelpersAndValidation(t *testing.T) {
	dir, extra := splitCloneDirectoryAndExtraArgs("repo", []string{"target", "--", "--depth=1"})
	if dir != "target" || len(extra) != 2 {
		t.Fatalf("unexpected split result: dir=%q extra=%#v", dir, extra)
	}

	dir, extra = splitCloneDirectoryAndExtraArgs("repo", []string{"--", "--depth=1"})
	if dir != "repo" || len(extra) != 2 {
		t.Fatalf("unexpected split without explicit directory: dir=%q extra=%#v", dir, extra)
	}

	args, err := normalizeCloneExtraArgs([]string{"--", "--depth=1", "--branch=main"})
	if err != nil {
		t.Fatalf("normalize clone extra args failed: %v", err)
	}
	if len(args) != 2 {
		t.Fatalf("unexpected normalized args: %#v", args)
	}

	_, err = normalizeCloneExtraArgs([]string{"depth=1"})
	if err == nil {
		t.Fatal("expected validation error when extra arg is not a flag")
	}

	name, err := normalizeUpstreamRemoteName("@owner", "OPS")
	if err != nil || name != "ops" {
		t.Fatalf("unexpected @owner mapping: name=%q err=%v", name, err)
	}

	_, err = normalizeUpstreamRemoteName("bad name", "OPS")
	if err == nil {
		t.Fatal("expected validation error for invalid remote name")
	}

	name, err = normalizeUpstreamRemoteName("@owner", "")
	if err != nil || name != "owner" {
		t.Fatalf("unexpected @owner fallback mapping: name=%q err=%v", name, err)
	}
}

func TestLookupParentCloneURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/rest/api/1.0/projects/PRJ/repos/demo":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"origin":{"project":{"key":"UP"},"slug":"upstream-demo"}}`))
		case "/rest/api/1.0/projects/PRJ/repos/no-origin":
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"slug":"no-origin"}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{BitbucketURL: server.URL}

	owner, parentURL, err := lookupParentCloneURL(context.Background(), cfg, server.URL, repositorySelector{ProjectKey: "PRJ", Slug: "demo"})
	if err != nil {
		t.Fatalf("lookup parent clone URL failed: %v", err)
	}
	if owner != "UP" {
		t.Fatalf("unexpected owner: %s", owner)
	}
	if !strings.Contains(parentURL, "/scm/UP/upstream-demo.git") {
		t.Fatalf("unexpected parent clone URL: %s", parentURL)
	}

	owner, parentURL, err = lookupParentCloneURL(context.Background(), cfg, server.URL, repositorySelector{ProjectKey: "PRJ", Slug: "no-origin"})
	if err != nil {
		t.Fatalf("lookup parent clone URL without origin failed: %v", err)
	}
	if owner != "" || parentURL != "" {
		t.Fatalf("expected no parent URL when origin missing, got owner=%q url=%q", owner, parentURL)
	}

	owner, parentURL, err = lookupParentCloneURL(context.Background(), cfg, server.URL, repositorySelector{ProjectKey: "PRJ", Slug: "does-not-exist"})
	if err != nil {
		t.Fatalf("expected graceful lookup when repo not found, got: %v", err)
	}
	if owner != "" || parentURL != "" {
		t.Fatalf("expected empty parent on lookup failure, got owner=%q url=%q", owner, parentURL)
	}
}

func TestRepoCloneCommandJSONAndUpstreamSuccess(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/rest/api/1.0/projects/PRJ/repos/demo" {
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"origin":{"project":{"key":"UP"},"slug":"upstream-demo"}}`))
			return
		}
		http.NotFound(writer, request)
	}))
	t.Cleanup(server.Close)

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	output, err := executeTestCLI(t, "--json", "repo", "clone", "PRJ/demo")
	if err != nil {
		t.Fatalf("repo clone json failed: %v", err)
	}

	if len(stub.addCalls) != 1 {
		t.Fatalf("expected add remote call, got %d", len(stub.addCalls))
	}
	if stub.addCalls[0].remote.Name != "upstream" {
		t.Fatalf("unexpected upstream remote name: %s", stub.addCalls[0].remote.Name)
	}
	if !strings.Contains(stub.addCalls[0].remote.URL, "/scm/UP/upstream-demo.git") {
		t.Fatalf("unexpected upstream remote URL: %s", stub.addCalls[0].remote.URL)
	}
	if !strings.Contains(output, `"configured": true`) {
		t.Fatalf("expected upstream configured in json output, got: %s", output)
	}
}

func TestRepoCloneCommandConfigAndFactoryValidation(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	_, _, _, err := resolveRepositoryCloneInput("demo", config.AppConfig{})
	if err == nil {
		t.Fatal("expected config validation error for slug-only clone without project")
	}

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")

	gitBackendFactory = func() git.Backend { return nil }
	_, err = executeTestCLI(t, "repo", "clone", "PRJ/demo")
	if err == nil {
		t.Fatal("expected git backend factory validation error")
	}
	if !strings.Contains(err.Error(), "git backend is not configured") {
		t.Fatalf("unexpected backend validation error: %v", err)
	}

	t.Setenv("BITBUCKET_URL", "://bad-url")
	gitBackendFactory = func() git.Backend { return &cloneBackendStub{} }
	_, err = executeTestCLI(t, "repo", "clone", "PRJ/demo")
	if err == nil {
		t.Fatal("expected clone URL validation error")
	}
}

func TestResolveRepositoryCloneInputValidationBranches(t *testing.T) {
	_, _, _, err := resolveRepositoryCloneInput("", config.AppConfig{BitbucketURL: "https://bitbucket.example.com", ProjectKey: "PRJ"})
	if err == nil {
		t.Fatal("expected empty repository validation error")
	}

	_, _, _, err = resolveRepositoryCloneInput("PRJ/repo", config.AppConfig{})
	if err == nil {
		t.Fatal("expected missing host validation for PROJECT/slug")
	}

	_, _, _, err = resolveRepositoryCloneInput("bad/", config.AppConfig{BitbucketURL: "https://bitbucket.example.com", ProjectKey: "PRJ"})
	if err == nil {
		t.Fatal("expected invalid selector error with slash")
	}
}

func TestNormalizeCloneHostFallback(t *testing.T) {
	host := normalizeCloneHost("git@bb.example.local:OPS/tooling.git", "bb.example.local")
	if host != "https://bb.example.local" {
		t.Fatalf("unexpected normalized host: %s", host)
	}

	host = normalizeCloneHost("invalid", "")
	if host != "" {
		t.Fatalf("expected empty host for empty parsed host, got: %s", host)
	}
}

func TestBuildBitbucketCloneURLEmptySelectorValidation(t *testing.T) {
	_, err := buildBitbucketCloneURL("https://bitbucket.example.com", "", "demo")
	if err == nil {
		t.Fatal("expected empty project validation error")
	}

	_, err = buildBitbucketCloneURL("https://bitbucket.example.com", "PRJ", "")
	if err == nil {
		t.Fatal("expected empty slug validation error")
	}
}

func TestRepoCloneCommandUsesStoredConfigForOtherHost(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh: connection refused"), nil}}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := config.SaveLogin(config.LoginInput{Host: "https://otherbucket.example.com", Token: "stored-token", SetDefault: false}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}

	t.Setenv("BITBUCKET_URL", "https://main.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	// Cloning from a URL for otherbucket.example.com (which has stored creds)
	// triggers the LoadStoredAuthForHost path in resolveCloneHTTPAuth.
	_, err := executeTestCLI(t, "repo", "clone", "https://otherbucket.example.com/scm/PRJ/demo.git")
	if err != nil {
		t.Fatalf("repo clone with stored config for other host failed: %v", err)
	}

	if len(stub.cloneCalls) != 2 {
		t.Fatalf("expected two clone attempts (SSH then HTTP), got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[1].repositoryURL != "https://otherbucket.example.com/scm/PRJ/demo.git" {
		t.Fatalf("unexpected HTTP clone URL: %s", stub.cloneCalls[1].repositoryURL)
	}
	if stub.cloneCalls[1].options.AuthToken != "stored-token" {
		t.Fatalf("expected AuthToken to be 'stored-token', got '%s'", stub.cloneCalls[1].options.AuthToken)
	}
}

func TestRepoCloneCommandJSONFailsWithNoAuth(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErr: errors.New("ssh: connection refused")}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	_, err := executeTestCLI(t, "--json", "repo", "clone", "PRJ/demo")
	if err == nil {
		t.Fatal("expected auth error in JSON mode with no credentials")
	}
	if !strings.Contains(err.Error(), "no stored HTTP credentials") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestRepoCloneCommandEmptyTokenPrompt(t *testing.T) {
	// With a non-TTY (bytes.Buffer) stdin, the prompt gate blocks before reaching the
	// empty-token check; the error reflects the missing credentials, not the empty token.
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErr: errors.New("ssh: connection refused")}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetIn(bytes.NewBufferString("\n"))
	command.SetArgs([]string{"repo", "clone", "PRJ/demo"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected auth error when stdin is not a TTY")
	}
	if !strings.Contains(err.Error(), "no stored HTTP credentials") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestNewCloneLoginRequiredError(t *testing.T) {
	cause := errors.New("connect failed")
	err := newCloneLoginRequiredError("https://bitbucket.example.com", cause, true)
	if err == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(err.Error(), "https://bitbucket.example.com") {
		t.Fatalf("expected host in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "no stored HTTP credentials") {
		t.Fatalf("expected credential message in error, got: %v", err)
	}

	// nil cause should still produce an error
	err = newCloneLoginRequiredError("https://bitbucket.example.com", nil, false)
	if err == nil {
		t.Fatal("expected non-nil error with nil cause")
	}
}


func TestSameCloneHostEdgeCasesAdditional(t *testing.T) {
	// Missing scheme defaults to https and 443.
	if !sameCloneHost("bitbucket.example.com", "https://bitbucket.example.com") {
		t.Fatal("expected true when bare host normalizes to default https endpoint")
	}

	// Path-only → no host → false
	if sameCloneHost("/path/only", "https://bitbucket.example.com") {
		t.Fatal("expected false when left has no host")
	}

	// Cross-scheme same host → true (scheme is ignored for credential matching)
	if !sameCloneHost("http://bitbucket.example.com", "https://bitbucket.example.com") {
		t.Fatal("expected true when same host with different schemes")
	}
}

func TestNormalizeHTTPCloneBaseURL(t *testing.T) {
	if got := normalizeHTTPCloneBaseURL("ssh://git@bitbucket.example.com/context?x=1#frag"); got != "https://bitbucket.example.com/context" {
		t.Fatalf("unexpected normalized base url: %s", got)
	}
	if got := normalizeHTTPCloneBaseURL("://bad"); got != "://bad" {
		t.Fatalf("expected invalid url to pass through unchanged, got %s", got)
	}
}

func TestBuildBitbucketSSHCloneURLValidationCases(t *testing.T) {
	// Invalid base URL (no scheme, empty host)
	_, err := buildBitbucketSSHCloneURL("no-scheme-here", "PRJ", "demo")
	if err == nil {
		t.Fatal("expected error for URL with no scheme/host")
	}

	// Empty project key
	_, err = buildBitbucketSSHCloneURL("https://bitbucket.example.com", "", "demo")
	if err == nil {
		t.Fatal("expected error for empty project key")
	}

	// Empty slug
	_, err = buildBitbucketSSHCloneURL("https://bitbucket.example.com", "PRJ", "")
	if err == nil {
		t.Fatal("expected error for empty slug")
	}

	// URL with port but no hostname (e.g. "http://:8080/") → hostname is empty
	_, err = buildBitbucketSSHCloneURL("http://:8080/", "PRJ", "demo")
	if err == nil {
		t.Fatal("expected error for URL with port but no hostname")
	}
}

func TestCloneRepositoryWithAuthFallbackEdgeCases(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	stub := &cloneBackendStub{}
	command := NewRootCommand()
	cfg := config.AppConfig{BitbucketURL: "https://test.example.com"}
	repo := repositorySelector{ProjectKey: "PRJ", Slug: "demo"}
	opts := git.CloneOptions{Directory: "demo"}

	// buildCloneURL error: non-explicit URL with bad cloneHost
	_, err := cloneRepositoryWithAuthFallback(command, cfg, "", false, "://bad-host", repo, cloneTransportAuto, opts, stub, false)
	if err == nil {
		t.Fatal("expected error for invalid clone host")
	}

	// resolveSSHCloneURL error: explicit URL but empty project/slug causes buildBitbucketSSHCloneURL to fail
	_, err = cloneRepositoryWithAuthFallback(command, cfg,
		"https://test.example.com/scm/PRJ/demo.git", true, "https://test.example.com",
		repositorySelector{ProjectKey: "", Slug: ""},
		cloneTransportAuto, opts, stub, false)
	if err == nil {
		t.Fatal("expected error for empty project in SSH clone URL")
	}
}

func TestRepoCloneCommandSSHExplicitURL(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	// Clone using an explicit "git@..." URL - exercises lines 372-373 in resolveSSHCloneURL
	_, err := executeTestCLI(t, "repo", "clone", "git@bitbucket.example.com:scm/PRJ/demo.git")
	if err != nil {
		t.Fatalf("clone with explicit git@ URL failed: %v", err)
	}
	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one clone call, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[0].repositoryURL != "git@bitbucket.example.com:scm/PRJ/demo.git" {
		t.Fatalf("unexpected clone URL: %s", stub.cloneCalls[0].repositoryURL)
	}
}

func TestRepoCloneCommandExplicitSSHURLFallsBackToHTTPS(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "stored-token")
	t.Setenv("BITBUCKET_USERNAME", "admin")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	_, err := executeTestCLI(t, "repo", "clone", "git@bitbucket.example.com:scm/PRJ/demo.git")
	if err != nil {
		t.Fatalf("expected fallback clone to succeed, got: %v", err)
	}
	if len(stub.cloneCalls) != 2 {
		t.Fatalf("expected two clone attempts, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[1].repositoryURL != "https://bitbucket.example.com/scm/PRJ/demo.git" {
		t.Fatalf("unexpected fallback clone URL: %s", stub.cloneCalls[1].repositoryURL)
	}
	if stub.cloneCalls[1].options.AuthToken != "stored-token" {
		t.Fatalf("expected AuthToken to be 'stored-token', got '%s'", stub.cloneCalls[1].options.AuthToken)
	}
}

func TestRepoCloneCommandExplicitSSHSchemeURLFallsBackToHTTPS(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "stored-token")
	t.Setenv("BITBUCKET_USERNAME", "admin")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	_, err := executeTestCLI(t, "repo", "clone", "ssh://git@bitbucket.example.com/scm/PRJ/demo.git")
	if err != nil {
		t.Fatalf("expected ssh:// fallback clone to succeed, got: %v", err)
	}
	if len(stub.cloneCalls) != 2 {
		t.Fatalf("expected two clone attempts, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[0].repositoryURL != "ssh://git@bitbucket.example.com/scm/PRJ/demo.git" {
		t.Fatalf("unexpected initial ssh clone URL: %s", stub.cloneCalls[0].repositoryURL)
	}
	if stub.cloneCalls[1].repositoryURL != "https://bitbucket.example.com/scm/PRJ/demo.git" {
		t.Fatalf("unexpected ssh:// fallback clone URL: %s", stub.cloneCalls[1].repositoryURL)
	}
	if stub.cloneCalls[1].options.AuthToken != "stored-token" {
		t.Fatalf("expected AuthToken to be 'stored-token', got '%s'", stub.cloneCalls[1].options.AuthToken)
	}
}

func TestRepoCloneCommandBackendFailsAfterTokenPrompt(t *testing.T) {
	// With a non-TTY stdin, the prompt gate fires before the backend is reached,
	// so only the SSH attempt occurs and we get a credentials error.
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), errors.New("http 401")}}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetIn(bytes.NewBufferString("valid-token\n"))
	command.SetArgs([]string{"repo", "clone", "PRJ/demo"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected clone error when stdin is not a TTY")
	}
	if !strings.Contains(err.Error(), "no stored HTTP credentials") {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected 1 clone attempt (SSH only), got %d", len(stub.cloneCalls))
	}
}

func TestReadCloneTokenErrorPath(t *testing.T) {
	// A reader that always returns an error (not io.EOF) triggers the error path in readCloneToken
	errReader := &errorReader{err: errors.New("read error")}
	_, err := readCloneToken(errReader, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected read error from readCloneToken")
	}
}

func TestReadCloneTokenSuccessPath(t *testing.T) {
	// Non-terminal reader: readCloneToken falls back to bufio line reading
	got, err := readCloneToken(strings.NewReader("my-token\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "my-token" {
		t.Fatalf("expected 'my-token', got %q", got)
	}
}

// TestPromptForCloneLoginDirect calls promptForCloneLogin directly (bypassing the TTY guard
// in cloneRepositoryWithAuthFallback) to cover the body of the function.
func TestPromptForCloneLoginDirect(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	cfg := config.AppConfig{BitbucketURL: "https://bitbucket.example.com"}

	t.Run("success with token", func(t *testing.T) {
		command := NewRootCommand()
		out := &bytes.Buffer{}
		command.SetOut(out)
		command.SetErr(out)
		command.SetIn(bytes.NewBufferString("my-token\n"))

		auth, prompted, err := promptForCloneLogin(command, cfg, "https://bitbucket.example.com", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !prompted {
			t.Fatal("expected prompted = true")
		}
		if auth.BitbucketToken != "my-token" {
			t.Fatalf("expected 'my-token', got %q", auth.BitbucketToken)
		}
		if !strings.Contains(out.String(), "Token:") {
			t.Fatalf("expected Token: prompt in output, got: %s", out.String())
		}
	})

	t.Run("empty token returns not-prompted", func(t *testing.T) {
		command := NewRootCommand()
		out := &bytes.Buffer{}
		command.SetOut(out)
		command.SetErr(out)
		command.SetIn(bytes.NewBufferString("\n"))

		_, prompted, err := promptForCloneLogin(command, cfg, "https://bitbucket.example.com", false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if prompted {
			t.Fatal("expected prompted = false for empty token")
		}
		if !strings.Contains(out.String(), "No stored HTTP credentials were found") {
			t.Fatalf("expected HTTPS-only prompt message, got: %s", out.String())
		}
	})
}

// TestRepoCloneCommandHTTPFallbackFailsBothSSHAndHTTP exercises the case where SSH fails
// AND the HTTP clone (using stored token credentials) also fails.  This covers the
// "hasStoredHTTPAuth=true, HTTP clone fails" else-branch in cloneRepositoryWithAuthFallback.
func TestRepoCloneCommandHTTPFallbackFailsBothSSHAndHTTP(t *testing.T) {
	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), errors.New("http 401 unauthorized")}}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "stored-token")

	_, err := executeTestCLI(t, "repo", "clone", "PRJ/demo")
	if err == nil {
		t.Fatal("expected error when both SSH and HTTP clone fail")
	}
	if !strings.Contains(err.Error(), "http 401") {
		t.Fatalf("expected http 401 error, got: %v", err)
	}
	if len(stub.cloneCalls) != 2 {
		t.Fatalf("expected 2 clone attempts (SSH + HTTP), got %d", len(stub.cloneCalls))
	}
}

// TestCloneRepositoryWithAuthFallbackPromptPathEmptyToken exercises the interactive-prompt
// path in cloneRepositoryWithAuthFallback when an empty token is provided.  The test injects
// canPromptForCloneLoginFunc to bypass the TTY guard.
func TestCloneRepositoryWithAuthFallbackPromptPathEmptyToken(t *testing.T) {
	originalPromptFunc := canPromptForCloneLoginFunc
	canPromptForCloneLoginFunc = func(io.Reader) bool { return true }
	t.Cleanup(func() { canPromptForCloneLoginFunc = originalPromptFunc })

	originalFactory := gitBackendFactory
	stub := &cloneBackendStub{cloneErr: errors.New("ssh failed")}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetErr(out)
	command.SetIn(bytes.NewBufferString("\n")) // empty token → prompted=false
	command.SetArgs([]string{"repo", "clone", "PRJ/demo"})

	err := command.Execute()
	if err == nil {
		t.Fatal("expected error: empty token should not succeed")
	}
	if !strings.Contains(err.Error(), "no stored HTTP credentials") {
		t.Fatalf("expected no-credentials error, got: %v", err)
	}
}

// TestCloneRepositoryWithAuthFallbackPromptPathSuccess exercises the full interactive-prompt
// path through cloneRepositoryWithAuthFallback when a valid token is entered and the clone
// succeeds.  canPromptForCloneLoginFunc is injected to bypass the TTY guard.
func TestCloneRepositoryWithAuthFallbackPromptPathSuccess(t *testing.T) {
	originalPromptFunc := canPromptForCloneLoginFunc
	canPromptForCloneLoginFunc = func(io.Reader) bool { return true }
	t.Cleanup(func() { canPromptForCloneLoginFunc = originalPromptFunc })

	originalFactory := gitBackendFactory
	// First call (SSH) fails; second call (prompted HTTP) succeeds.
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}
	gitBackendFactory = func() git.Backend { return stub }
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")
	t.Setenv("BITBUCKET_USER", "")
	t.Setenv("ADMIN_USER", "")
	t.Setenv("ADMIN_PASSWORD", "")

	command := NewRootCommand()
	out := &bytes.Buffer{}
	command.SetOut(out)
	command.SetErr(out)
	command.SetIn(bytes.NewBufferString("my-secret-token\n"))
	command.SetArgs([]string{"repo", "clone", "PRJ/demo"})

	if err := command.Execute(); err != nil {
		t.Fatalf("expected successful clone after prompt, got: %v", err)
	}
	if len(stub.cloneCalls) != 2 {
		t.Fatalf("expected 2 clone calls (SSH + prompted HTTP), got %d", len(stub.cloneCalls))
	}
	if !strings.Contains(out.String(), "Cloned PRJ/demo into demo") {
		t.Fatalf("unexpected output: %s", out.String())
	}
}

// TestCanPromptForCloneLoginOsFile exercises the *os.File branch of canPromptForCloneLogin.
// os.Stdin is a *os.File but is not a TTY in a test environment, so the function returns false.
func TestCanPromptForCloneLoginOsFile(t *testing.T) {
	result := canPromptForCloneLogin(os.Stdin)
	// In CI/test environments os.Stdin is not a terminal.
	if result {
		t.Fatal("expected canPromptForCloneLogin(os.Stdin) = false in non-TTY test environment")
	}
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}
