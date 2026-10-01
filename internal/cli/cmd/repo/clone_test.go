package repocmd

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/giturl"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/interactive"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
)

type repositorySelector = cloneRepoRef

var buildBitbucketCloneURL = giturl.BuildBitbucketCloneURL

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

func (stub *cloneBackendStub) CurrentBranch(context.Context, string) (string, error) {
	return "", nil
}

func (stub *cloneBackendStub) ListRemotes(context.Context, string) ([]git.Remote, error) {
	return nil, nil
}

func (stub *cloneBackendStub) GetConfig(context.Context, git.ConfigOptions) (string, error) {
	return "", nil
}

func (stub *cloneBackendStub) SetConfig(context.Context, git.ConfigOptions) error {
	return nil
}

func (stub *cloneBackendStub) UnsetConfig(context.Context, git.ConfigOptions) error {
	return nil
}

func TestRepoCloneCommandClonesWithDefaults(t *testing.T) {
	t.Parallel()

	stub := &cloneBackendStub{}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Backend: stub}

	output, err := executeTestCLIWith(t, setup, "repo", "clone", "PRJ/demo")
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
	t.Parallel()

	stub := &cloneBackendStub{}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Backend: stub}

	output, err := executeTestCLIWith(t, setup, "clone", "PRJ/demo")
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
	t.Parallel()

	stub := &cloneBackendStub{}
	setup := testSetup{Host: "https://bitbucket.example.com/bitbucket", ProjectKey: "PRJ", RepoSlug: "demo", Backend: stub}

	output, err := executeTestCLIWith(t, setup, "--json", "repo", "clone", "PRJ/demo", "target-dir", "--", "--depth=7", "--branch=main")
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

	if !strings.Contains(output, `"cloneUrl": "git@bitbucket.example.com:scm/PRJ/demo.git"`) {
		t.Fatalf("unexpected json output: %s", output)
	}
}

func TestRepoCloneCommandFallsBackToStoredHTTPToken(t *testing.T) {
	t.Parallel()

	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Token: "test-token", Backend: stub}

	output, err := executeTestCLIWith(t, setup, "repo", "clone", "PRJ/demo")
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
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := config.SaveLogin(config.LoginInput{Host: "https://bitbucket.example.com/context", Token: "stored-token", SetDefault: true}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}
	if _, err := config.SetHostAliases("https://bitbucket.example.com/context", []string{"git.example.com:7999"}); err != nil {
		t.Fatalf("set aliases failed: %v", err)
	}

	setup := testSetup{Host: "https://other.example.com", ProjectKey: "PRJ", Backend: stub}
	_, err := executeTestCLIWith(t, setup, "repo", "clone", "ssh://git@git.example.com:7999/scm/PRJ/demo.git")
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
		canPromptForCloneLogin,
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
	t.Parallel()

	stub := &cloneBackendStub{}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Token: "test-token", Backend: stub}

	_, err := executeTestCLIWith(t, setup, "repo", "clone", "--https", "PRJ/demo")
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
	t.Parallel()

	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ"}

	_, err := executeTestCLIWith(t, setup, "repo", "clone", "--ssh", "--https", "PRJ/demo")
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
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed")}}

	_, err := executeTestCLIWith(t, testSetup{
		Host:       "https://bitbucket.example.com",
		ProjectKey: "PRJ",
		RepoSlug:   "demo",
		Backend:    stub,
		Stdin:      bytes.NewBufferString("prompt-token\n"),
	}, "repo", "clone", "PRJ/demo")
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
	t.Parallel()

	stub := &cloneBackendStub{cloneErr: errors.New("clone failed")}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Backend: stub}

	// Each is refused before git is asked for anything, and says why: a
	// configuration bb could not load fails them as well. A bare slug is not
	// invalid here, since the setup names a project to put it in.
	for _, testCase := range []struct {
		args []string
		want string
	}{
		{args: []string{"repo", "clone", "PRJ/"}, want: "repository must be in PROJECT/slug format"},
		{args: []string{"repo", "clone", "PRJ/demo", ""}, want: "clone directory cannot be empty"},
		{args: []string{"repo", "clone", "PRJ/demo", "target-dir", "--", "depth=1"}, want: "additional git clone arguments must be passed after --"},
	} {
		_, err := executeTestCLIWith(t, setup, testCase.args...)
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Fatalf("%v: expected %q, got: %v", testCase.args, testCase.want, err)
		}
	}
	if len(stub.cloneCalls) != 0 {
		t.Fatalf("a refused clone still asked git to clone: %v", stub.cloneCalls)
	}

	_, err := executeTestCLIWith(t, setup, "repo", "clone", "PRJ/demo")
	if err == nil {
		t.Fatal("expected backend clone failure")
	}
	if !strings.Contains(err.Error(), "clone failed") {
		t.Fatalf("expected backend error in result, got: %v", err)
	}
}

func TestRepoCloneCommandSupportsURLSelectors(t *testing.T) {
	t.Parallel()

	stub := &cloneBackendStub{}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Backend: stub}

	_, err := executeTestCLIWith(t, setup, "repo", "clone", "https://bitbucket.other.example/scm/OPS/tooling.git")
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
	t.Parallel()

	httpCloneURL, err := resolveHTTPCloneURL("ssh://git@bitbucket.example.com/scm/PRJ/demo.git", true, "ssh://git@bitbucket.example.com", repositorySelector{ProjectKey: "PRJ", Slug: "demo"})
	if err != nil {
		t.Fatalf("expected HTTP clone URL resolution to succeed, got: %v", err)
	}
	if httpCloneURL != "https://bitbucket.example.com/scm/PRJ/demo.git" {
		t.Fatalf("unexpected normalized HTTP clone URL: %s", httpCloneURL)
	}
}

func TestBuildBitbucketCloneURL(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

func TestRepoCloneCommandConfigAndFactoryValidation(t *testing.T) {
	originalFactory := gitBackendFactory
	t.Cleanup(func() { gitBackendFactory = originalFactory })

	_, _, _, err := resolveRepositoryCloneInput("demo", config.AppConfig{})
	if err == nil {
		t.Fatal("expected config validation error for slug-only clone without project")
	}

	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ"}

	gitBackendFactory = func() git.Backend { return nil }
	_, err = executeTestCLIWith(t, setup, "repo", "clone", "PRJ/demo")
	if err == nil {
		t.Fatal("expected git backend factory validation error")
	}
	if !strings.Contains(err.Error(), "git backend is not configured") {
		t.Fatalf("unexpected backend validation error: %v", err)
	}

	setup.Host = "://bad-url"
	gitBackendFactory = func() git.Backend { return &cloneBackendStub{} }
	_, err = executeTestCLIWith(t, setup, "repo", "clone", "PRJ/demo")
	if err == nil || !strings.Contains(err.Error(), `is invalid: "://bad-url"`) {
		t.Fatalf("expected the malformed host to be refused, got: %v", err)
	}
}

func TestResolveRepositoryCloneInputValidationBranches(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh: connection refused"), nil}}

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	if _, err := config.SaveLogin(config.LoginInput{Host: "https://otherbucket.example.com", Token: "stored-token", SetDefault: false}); err != nil {
		t.Fatalf("save login failed: %v", err)
	}

	setup := testSetup{Host: "https://main.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Backend: stub}

	// Cloning from a URL for otherbucket.example.com (which has stored creds)
	// triggers the LoadStoredAuthForHost path in resolveCloneHTTPAuth.
	_, err := executeTestCLIWith(t, setup, "repo", "clone", "https://otherbucket.example.com/scm/PRJ/demo.git")
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

// TestTheCloneRefusesAPlaintextCredentialWhenKeyringRequired holds ADR-047's
// requirement where a clone reads the credential it gives git. The clone host
// is not the configured one, so the configuration load, which refuses the
// plaintext fallback, never reads its credential; the clone's own lookup does.
func TestTheCloneRefusesAPlaintextCredentialWhenKeyringRequired(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	stored := strings.Join([]string{
		"hosts:",
		"  https://otherbucket.example.com:",
		"    url: https://otherbucket.example.com",
		"insecure_secrets:",
		"  https://otherbucket.example.com:",
		"    token: plaintext-token",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(stored), 0o600); err != nil {
		t.Fatalf("write stored config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", configPath)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")
	t.Setenv("BB_SYSTEM_CONFIG_PATH", filepath.Join(t.TempDir(), "absent.yaml"))
	t.Setenv("BB_REQUIRE_KEYRING", "1")

	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh: connection refused"), nil}}
	setup := testSetup{Host: "https://main.example.com", Token: "main-token", ProjectKey: "PRJ", Backend: stub}

	_, err := executeTestCLIWith(t, setup, "repo", "clone", "https://otherbucket.example.com/scm/PRJ/demo.git")
	if err == nil {
		t.Fatal("the clone succeeded with a plaintext credential under the requirement")
	}
	if !strings.Contains(err.Error(), "keyring") {
		t.Fatalf("the clone does not report the requirement: %v", err)
	}
	for _, call := range stub.cloneCalls {
		if call.options.AuthToken == "plaintext-token" {
			t.Fatalf("git was given the plaintext credential for %s", call.repositoryURL)
		}
	}
	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected the SSH attempt alone, got %d clone calls", len(stub.cloneCalls))
	}
}

func TestRepoCloneCommandJSONFailsWithNoAuth(t *testing.T) {
	stub := &cloneBackendStub{cloneErr: errors.New("ssh: connection refused")}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Backend: stub}

	_, err := executeTestCLIWith(t, setup, "--json", "repo", "clone", "PRJ/demo")
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
	stub := &cloneBackendStub{cloneErr: errors.New("ssh: connection refused")}

	_, err := executeTestCLIWith(t, testSetup{
		Host:       "https://bitbucket.example.com",
		ProjectKey: "PRJ",
		RepoSlug:   "demo",
		Backend:    stub,
		Stdin:      bytes.NewBufferString("\n"),
	}, "repo", "clone", "PRJ/demo")
	if err == nil {
		t.Fatal("expected auth error when stdin is not a TTY")
	}
	if !strings.Contains(err.Error(), "no stored HTTP credentials") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestNewCloneLoginRequiredError(t *testing.T) {
	t.Parallel()

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
	// No flag carries a secret (ADR-083): the advice names the stdin form.
	if !strings.Contains(err.Error(), "--token-stdin") || strings.Contains(err.Error(), "--token <") {
		t.Fatalf("the advice does not name --token-stdin: %v", err)
	}

	// nil cause should still produce an error
	err = newCloneLoginRequiredError("https://bitbucket.example.com", nil, false)
	if err == nil {
		t.Fatal("expected non-nil error with nil cause")
	}
}

func TestSameCloneHostEdgeCasesAdditional(t *testing.T) {
	t.Parallel()

	// Missing scheme defaults to https and 443.
	if !sameCloneHost("bitbucket.example.com", "https://bitbucket.example.com") {
		t.Fatal("expected true when bare host normalizes to default https endpoint")
	}

	// Path-only → no host → false
	if sameCloneHost("/path/only", "https://bitbucket.example.com") {
		t.Fatal("expected false when left has no host")
	}

	// Configured over http, cloned over https: TLS is added, same host.
	if !sameCloneHost("http://bitbucket.example.com", "https://bitbucket.example.com") {
		t.Fatal("expected true for a clone that upgrades the configured host to https")
	}

	// Configured over https, cloned over http: the credential would cross the
	// network in the clear, so it does not go with the clone (#730).
	if sameCloneHost("https://bitbucket.example.com", "http://bitbucket.example.com") {
		t.Fatal("expected false for a clone link that downgrades the configured host to http")
	}
	if sameCloneHost("bitbucket.example.com", "http://bitbucket.example.com") {
		t.Fatal("expected false for http beside a bare host, which is https")
	}
}

func TestNormalizeHTTPCloneBaseURL(t *testing.T) {
	t.Parallel()

	if got := normalizeHTTPCloneBaseURL("ssh://git@bitbucket.example.com/context?x=1#frag"); got != "https://bitbucket.example.com/context" {
		t.Fatalf("unexpected normalized base url: %s", got)
	}
	if got := normalizeHTTPCloneBaseURL("://bad"); got != "://bad" {
		t.Fatalf("expected invalid url to pass through unchanged, got %s", got)
	}
}

func TestBuildBitbucketSSHCloneURLValidationCases(t *testing.T) {
	t.Parallel()

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
	stub := &cloneBackendStub{}
	command := NewRootCommand()
	cfg := config.AppConfig{BitbucketURL: "https://test.example.com"}
	repo := repositorySelector{ProjectKey: "PRJ", Slug: "demo"}
	opts := git.CloneOptions{Directory: "demo"}

	// buildCloneURL error: non-explicit URL with bad cloneHost
	_, err := cloneRepositoryWithAuthFallback(command, cfg, "", false, "://bad-host", repo, cloneTransportAuto, opts, stub, false, canPromptForCloneLogin)
	if err == nil {
		t.Fatal("expected error for invalid clone host")
	}

	// resolveSSHCloneURL error: explicit URL but empty project/slug causes buildBitbucketSSHCloneURL to fail
	_, err = cloneRepositoryWithAuthFallback(command, cfg,
		"https://test.example.com/scm/PRJ/demo.git", true, "https://test.example.com",
		repositorySelector{ProjectKey: "", Slug: ""},
		cloneTransportAuto, opts, stub, false, canPromptForCloneLogin)
	if err == nil {
		t.Fatal("expected error for empty project in SSH clone URL")
	}
}

func TestRepoCloneCommandSSHExplicitURL(t *testing.T) {
	t.Parallel()

	stub := &cloneBackendStub{}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Backend: stub}

	// Clone using an explicit "git@..." URL - exercises lines 372-373 in resolveSSHCloneURL
	_, err := executeTestCLIWith(t, setup, "repo", "clone", "git@bitbucket.example.com:scm/PRJ/demo.git")
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
	t.Parallel()

	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Token: "stored-token", Backend: stub}

	_, err := executeTestCLIWith(t, setup, "repo", "clone", "git@bitbucket.example.com:scm/PRJ/demo.git")
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
	t.Parallel()

	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Token: "stored-token", Backend: stub}

	_, err := executeTestCLIWith(t, setup, "repo", "clone", "ssh://git@bitbucket.example.com/scm/PRJ/demo.git")
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
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), errors.New("http 401")}}

	_, err := executeTestCLIWith(t, testSetup{
		Host:       "https://bitbucket.example.com",
		ProjectKey: "PRJ",
		RepoSlug:   "demo",
		Backend:    stub,
		Stdin:      bytes.NewBufferString("valid-token\n"),
	}, "repo", "clone", "PRJ/demo")
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
	t.Parallel()

	// A reader that always returns an error (not io.EOF) triggers the error path in readCloneToken
	errReader := &errorReader{err: errors.New("read error")}
	_, err := readCloneToken(errReader, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected read error from readCloneToken")
	}
}

func TestReadCloneTokenSuccessPath(t *testing.T) {
	t.Parallel()

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

	t.Run("a token the keyring cannot hold is used once and stored nowhere", func(t *testing.T) {
		// The clone has no --allow-insecure-storage to ask for plaintext, so the
		// token the person typed serves this clone and is not written down.
		config.UseUnavailableKeyring(t, errors.New("no secret service"))
		before, _ := os.ReadFile(configPath)

		command := NewRootCommand()
		out := &bytes.Buffer{}
		command.SetOut(out)
		command.SetErr(out)
		command.SetIn(bytes.NewBufferString("typed-token\n"))

		auth, prompted, err := promptForCloneLogin(command, cfg, "https://bitbucket.example.com", false)
		if err != nil {
			t.Fatalf("the clone failed because the token could not be stored: %v", err)
		}
		if !prompted || auth.BitbucketToken != "typed-token" {
			t.Fatalf("the typed token was not used for the clone: prompted=%v token=%q", prompted, auth.BitbucketToken)
		}
		if !strings.Contains(out.String(), "used for this clone only") || !strings.Contains(out.String(), "--allow-insecure-storage") {
			t.Errorf("the note does not say the token was not stored, or how to store it: %s", out.String())
		}
		after, _ := os.ReadFile(configPath)
		if !bytes.Equal(before, after) || strings.Contains(string(after), "typed-token") {
			t.Errorf("the configuration file changed, or holds the token:\n%s", after)
		}
	})
}

// TestRepoCloneCommandHTTPFallbackFailsBothSSHAndHTTP exercises the case where SSH fails
// AND the HTTP clone (using stored token credentials) also fails.  This covers the
// "hasStoredHTTPAuth=true, HTTP clone fails" else-branch in cloneRepositoryWithAuthFallback.
func TestRepoCloneCommandHTTPFallbackFailsBothSSHAndHTTP(t *testing.T) {
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), errors.New("http 401 unauthorized")}}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "PRJ", RepoSlug: "demo", Token: "stored-token", Backend: stub}

	_, err := executeTestCLIWith(t, setup, "repo", "clone", "PRJ/demo")
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
	stub := &cloneBackendStub{cloneErr: errors.New("ssh failed")}

	outText, err := executeTestCLIWith(t, testSetup{
		Host:       "https://bitbucket.example.com",
		ProjectKey: "PRJ",
		RepoSlug:   "demo",
		Backend:    stub,
		CanPrompt:  func(interactive.Options) bool { return true },
		Stdin:      bytes.NewBufferString("\n"),
	}, "repo", "clone", "PRJ/demo")
	_ = outText

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
	// First call (SSH) fails; second call (prompted HTTP) succeeds.
	stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}

	configPath := filepath.Join(t.TempDir(), "bb", "config.yaml")
	t.Setenv("BB_CONFIG_PATH", configPath)

	outText, err := executeTestCLIWith(t, testSetup{
		Host:       "https://bitbucket.example.com",
		ProjectKey: "PRJ",
		RepoSlug:   "demo",
		Backend:    stub,
		CanPrompt:  func(interactive.Options) bool { return true },
		Stdin:      bytes.NewBufferString("my-secret-token\n"),
	}, "repo", "clone", "PRJ/demo")
	if err != nil {
		t.Fatalf("expected successful clone after prompt, got: %v", err)
	}
	if len(stub.cloneCalls) != 2 {
		t.Fatalf("expected 2 clone calls (SSH + prompted HTTP), got %d", len(stub.cloneCalls))
	}
	if !strings.Contains(outText, "Cloned PRJ/demo into demo") {
		t.Fatalf("unexpected output: %s", outText)
	}
}

// TestTheCloneTokenPromptHonoursNoInput: --no-input and machine output refuse
// every prompt (ADR-072, ADR-073), the clone's token prompt included, even with
// a person at the terminal. The clone hands both to the shared decision, as
// every other prompt does.
//
// Not parallel: a prompt that is answered stores the token, so the test points
// BB_CONFIG_PATH at a file of its own rather than the one the sealed process
// shares between its tests.
func TestTheCloneTokenPromptHonoursNoInput(t *testing.T) {
	t.Setenv("BB_CONFIG_PATH", filepath.Join(t.TempDir(), "config.yaml"))

	cases := []struct {
		flag  string
		given func(interactive.Options) bool
	}{
		{flag: "--no-input", given: func(options interactive.Options) bool { return options.Disabled }},
		{flag: "--json", given: func(options interactive.Options) bool { return options.MachineOutput }},
	}

	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			var asked []interactive.Options
			// A person at the terminal: the shared decision refuses only for
			// what the invocation says.
			personAtTheTerminal := func(options interactive.Options) bool {
				asked = append(asked, options)
				return !options.Disabled && !options.MachineOutput
			}

			stub := &cloneBackendStub{cloneErrs: []error{errors.New("ssh failed"), nil}}
			output, err := executeTestCLIWith(t, testSetup{
				Host:       "https://bitbucket.example.com",
				ProjectKey: "PRJ",
				Backend:    stub,
				CanPrompt:  personAtTheTerminal,
				Stdin:      bytes.NewBufferString("typed-token\n"),
			}, tc.flag, "repo", "clone", "PRJ/demo")

			if len(asked) != 1 || !tc.given(asked[0]) {
				t.Fatalf("the decision was not told about %s: %+v", tc.flag, asked)
			}
			if strings.Contains(output, "Token:") {
				t.Fatalf("the clone prompted for a token under %s: %s", tc.flag, output)
			}
			if err == nil || !strings.Contains(err.Error(), "no stored HTTP credentials") {
				t.Fatalf("expected the login-required refusal, got: %v", err)
			}
			if len(stub.cloneCalls) != 1 {
				t.Fatalf("expected the SSH attempt alone, got %d clone calls", len(stub.cloneCalls))
			}
		})
	}
}

// TestCanPromptForCloneLoginDefersToTheSharedDecision checks the delegation.
//
// os.Stdin is an *os.File but is not a terminal under `go test`, so the shared
// rules refuse. The point of the assertion is that clone asks them at all
// rather than keeping a check of its own.
func TestCanPromptForCloneLoginDefersToTheSharedDecision(t *testing.T) {
	t.Parallel()

	if canPromptForCloneLogin(interactive.Options{Stdin: os.Stdin, Stdout: os.Stdout}) {
		t.Fatal("prompting was permitted with no terminal attached")
	}
}

type errorReader struct {
	err error
}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, r.err
}

func TestRepoCloneCommandUserNamespace(t *testing.T) {
	t.Parallel()

	stub := &cloneBackendStub{}
	setup := testSetup{Host: "https://bitbucket.example.com", ProjectKey: "", RepoSlug: "", Backend: stub}

	// 1. Test cloning using tilde username format
	output, err := executeTestCLIWith(t, setup, "repo", "clone", "~userid/somerepo")
	if err != nil {
		t.Fatalf("repo clone with user namespace failed: %v", err)
	}

	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one clone call, got %d", len(stub.cloneCalls))
	}

	call := stub.cloneCalls[0]
	if call.repositoryURL != "git@bitbucket.example.com:scm/~userid/somerepo.git" {
		t.Fatalf("unexpected clone URL: %s", call.repositoryURL)
	}
	if call.options.Directory != "somerepo" {
		t.Fatalf("unexpected clone directory: %s", call.options.Directory)
	}

	if !strings.Contains(output, "Cloned ~userid/somerepo into somerepo") {
		t.Fatalf("unexpected output: %s", output)
	}

	// 2. Test cloning using full URL with unescaped tilde
	stub.cloneCalls = nil
	_, err = executeTestCLIWith(t, setup, "repo", "clone", "https://bitbucket.example.com/scm/~userid/somerepo.git")
	if err != nil {
		t.Fatalf("repo clone with full URL containing tilde failed: %v", err)
	}
	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one clone call, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[0].repositoryURL != "git@bitbucket.example.com:scm/~userid/somerepo.git" {
		t.Fatalf("unexpected clone URL: %s", stub.cloneCalls[0].repositoryURL)
	}

	// 3. Test cloning using full URL with escaped tilde (%7E)
	stub.cloneCalls = nil
	_, err = executeTestCLIWith(t, setup, "repo", "clone", "https://bitbucket.example.com/scm/%7Euserid/somerepo.git")
	if err != nil {
		t.Fatalf("repo clone with full URL containing escaped tilde failed: %v", err)
	}
	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one clone call, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[0].repositoryURL != "git@bitbucket.example.com:scm/~userid/somerepo.git" {
		t.Fatalf("unexpected clone URL: %s", stub.cloneCalls[0].repositoryURL)
	}

	// 4. Test cloning using SSH URL with escaped tilde (%7E)
	stub.cloneCalls = nil
	_, err = executeTestCLIWith(t, setup, "repo", "clone", "git@bitbucket.example.com:scm/%7Euserid/somerepo.git")
	if err != nil {
		t.Fatalf("repo clone with SSH URL containing escaped tilde failed: %v", err)
	}
	if len(stub.cloneCalls) != 1 {
		t.Fatalf("expected one clone call, got %d", len(stub.cloneCalls))
	}
	if stub.cloneCalls[0].repositoryURL != "git@bitbucket.example.com:scm/~userid/somerepo.git" {
		t.Fatalf("unexpected clone URL: %s", stub.cloneCalls[0].repositoryURL)
	}
}

func (stub *cloneBackendStub) WorkingTreeState(context.Context, string) (git.WorkingTreeStatus, error) {
	return git.WorkingTreeStatus{}, nil
}

func (stub *cloneBackendStub) BranchExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (stub *cloneBackendStub) FastForward(context.Context, string, string) error {
	return nil
}

// Three suites are live now, in TestLiveRepoCloneAddsTheUpstreamRemote.
//
// They asserted the upstream remote against a repository payload carrying an
// origin this file had written, with a stub git backend recording the remote
// it was asked to add. Two things could actually be wrong there and neither
// was under test: whether Bitbucket reports a fork's parent in that field, and
// whether the URL built from it is one git accepts. The live version forks a
// real repository, clones the fork, and reads `git remote -v` -- then clones
// it again with --no-upstream and requires the parent to be absent.
// Sabotage-checked by skipping the AddRemote call.
