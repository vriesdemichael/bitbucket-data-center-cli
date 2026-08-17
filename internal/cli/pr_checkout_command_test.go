package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
)

// checkoutBackendStub records what bb pr checkout asks git to do.
//
// The command's whole job is deciding which remote, refspec, branch and config
// keys are correct for a given pull request, so the assertions are about the
// git commands it chose rather than about a repository's end state. A real
// repository would test git, not this.
type checkoutBackendStub struct {
	repositoryRoot string
	rootErr        error
	remotes        []git.Remote
	branches       map[string]bool
	status         git.WorkingTreeStatus
	fastForwardErr error

	// failOn injects a failure into one backend call by name, so each way git
	// can refuse mid-checkout gets exercised without a real repository.
	failOn map[string]error

	calls          []string
	addedRemotes   []git.Remote
	fetches        []git.FetchOptions
	checkouts      []git.CheckoutOptions
	configSets     map[string]string
	fastForwardRef string
}

func (stub *checkoutBackendStub) failure(name string) error {
	if stub.failOn == nil {
		return nil
	}

	return stub.failOn[name]
}

func newCheckoutBackendStub(remotes ...git.Remote) *checkoutBackendStub {
	return &checkoutBackendStub{
		repositoryRoot: "/repo",
		remotes:        remotes,
		branches:       map[string]bool{},
		configSets:     map[string]string{},
	}
}

func (stub *checkoutBackendStub) Version(context.Context) (string, error) { return "", nil }

func (stub *checkoutBackendStub) Clone(context.Context, string, git.CloneOptions) error { return nil }

func (stub *checkoutBackendStub) AddRemote(_ context.Context, _ string, remote git.Remote) error {
	if err := stub.failure("add-remote"); err != nil {
		return err
	}
	stub.calls = append(stub.calls, "add-remote")
	stub.addedRemotes = append(stub.addedRemotes, remote)
	stub.remotes = append(stub.remotes, remote)
	return nil
}

func (stub *checkoutBackendStub) Fetch(_ context.Context, _ string, options git.FetchOptions) error {
	if err := stub.failure("fetch"); err != nil {
		return err
	}
	stub.calls = append(stub.calls, "fetch")
	stub.fetches = append(stub.fetches, options)
	return nil
}

func (stub *checkoutBackendStub) Checkout(_ context.Context, _ string, options git.CheckoutOptions) error {
	if err := stub.failure("checkout"); err != nil {
		return err
	}
	stub.calls = append(stub.calls, "checkout")
	stub.checkouts = append(stub.checkouts, options)
	return nil
}

func (stub *checkoutBackendStub) RepositoryRoot(context.Context, string) (string, error) {
	if stub.rootErr != nil {
		return "", stub.rootErr
	}
	return stub.repositoryRoot, nil
}

func (stub *checkoutBackendStub) CurrentBranch(context.Context, string) (string, error) {
	return "main", nil
}

func (stub *checkoutBackendStub) WorkingTreeState(context.Context, string) (git.WorkingTreeStatus, error) {
	if err := stub.failure("working-tree-state"); err != nil {
		return git.WorkingTreeStatus{}, err
	}
	return stub.status, nil
}

func (stub *checkoutBackendStub) BranchExists(_ context.Context, _ string, branch string) (bool, error) {
	if err := stub.failure("branch-exists"); err != nil {
		return false, err
	}
	return stub.branches[branch], nil
}

func (stub *checkoutBackendStub) FastForward(_ context.Context, _ string, ref string) error {
	stub.calls = append(stub.calls, "fast-forward")
	stub.fastForwardRef = ref
	return stub.fastForwardErr
}

func (stub *checkoutBackendStub) ListRemotes(context.Context, string) ([]git.Remote, error) {
	if err := stub.failure("list-remotes"); err != nil {
		return nil, err
	}
	return stub.remotes, nil
}

func (stub *checkoutBackendStub) GetConfig(context.Context, git.ConfigOptions) (string, error) {
	return "", nil
}

func (stub *checkoutBackendStub) SetConfig(_ context.Context, options git.ConfigOptions) error {
	if err := stub.failure("set-config"); err != nil {
		return err
	}
	stub.calls = append(stub.calls, "set-config")
	stub.configSets[options.Key] = options.Value
	return nil
}

func (stub *checkoutBackendStub) UnsetConfig(context.Context, git.ConfigOptions) error { return nil }

// newCheckoutServer serves one pull request whose source ref points wherever
// the caller says, which is how the fork and same-repository cases differ.
func newCheckoutServer(t *testing.T, sourceProject string, sourceSlug string, sourceBranch string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !strings.Contains(request.URL.Path, "/repos/demo/pull-requests/42") {
			http.NotFound(writer, request)
			return
		}

		_, _ = fmt.Fprintf(writer, `{
			"id":42,"title":"Fix login","state":"OPEN","open":true,"closed":false,"version":3,
			"fromRef":{"id":"refs/heads/%s","displayId":"%s","latestCommit":"abc123",
				"repository":{"slug":"%s","project":{"key":"%s"}}},
			"toRef":{"id":"refs/heads/main","displayId":"main",
				"repository":{"slug":"demo","project":{"key":"PRJ"}}}
		}`, sourceBranch, sourceBranch, sourceSlug, sourceProject)
	}))
	t.Cleanup(server.Close)

	return server
}

func configureCheckoutEnv(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	// The whole credential fallback chain, not just the primary names.
	// BITBUCKET_USERNAME falls back to BITBUCKET_USER and then ADMIN_USER, and
	// the password to ADMIN_PASSWORD — so clearing only the first of each
	// leaves a test inheriting whatever the surrounding environment has. The
	// live CI job exports ADMIN_USER and ADMIN_PASSWORD for the harness, which
	// is exactly where that assumption broke.
	for _, key := range []string{
		"BITBUCKET_USERNAME",
		"BITBUCKET_USER",
		"BITBUCKET_PASSWORD",
		"ADMIN_USER",
		"ADMIN_PASSWORD",
	} {
		t.Setenv(key, "")
	}
}

func runCheckout(t *testing.T, args ...string) string {
	t.Helper()

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatalf("%v failed: %v\noutput: %s", args, err, buffer.String())
	}

	return buffer.String()
}

func runCheckoutExpectingError(t *testing.T, args ...string) error {
	t.Helper()

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs(args)
	err := command.Execute()
	if err == nil {
		t.Fatalf("expected %v to fail, got output: %s", args, buffer.String())
	}

	return err
}

func decodeCheckoutResult(t *testing.T, output string) map[string]any {
	t.Helper()

	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("checkout output is not JSON: %v\noutput: %s", err, output)
	}
	data, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected an enveloped data object, got: %s", output)
	}

	return data
}

func TestPullRequestCheckoutSameRepository(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	withGitBackend(t, stub)

	output := runCheckout(t, "--json", "pr", "checkout", "42")
	result := decodeCheckoutResult(t, output)

	if result["fork"] != false {
		t.Fatalf("expected a same-repository checkout, got: %v", result)
	}
	if result["remote"] != "origin" {
		t.Fatalf("expected the existing origin remote, got: %v", result)
	}
	if result["remote_added"] != false {
		t.Fatalf("expected no new remote, got: %v", result)
	}
	// The local branch keeps the source name: nothing to collide with.
	if result["branch"] != "feature/login" {
		t.Fatalf("expected the source branch name, got: %v", result)
	}

	if len(stub.addedRemotes) != 0 {
		t.Fatalf("expected no remote to be added, got: %v", stub.addedRemotes)
	}
	if len(stub.fetches) != 1 || stub.fetches[0].Remote != "origin" {
		t.Fatalf("expected one fetch from origin, got: %v", stub.fetches)
	}
	wantRefspec := "+refs/heads/feature/login:refs/remotes/origin/feature/login"
	if len(stub.fetches[0].Refspecs) != 1 || stub.fetches[0].Refspecs[0] != wantRefspec {
		t.Fatalf("expected refspec %q, got: %v", wantRefspec, stub.fetches[0].Refspecs)
	}

	if len(stub.checkouts) != 1 {
		t.Fatalf("expected one checkout, got: %v", stub.checkouts)
	}
	checkout := stub.checkouts[0]
	if checkout.NewBranch != "feature/login" || checkout.Ref != "refs/remotes/origin/feature/login" {
		t.Fatalf("expected a new branch at the fetched ref, got: %#v", checkout)
	}

	// Upstream config is what makes a later bare `git push` work.
	if got := stub.configSets["branch.feature/login.remote"]; got != "origin" {
		t.Fatalf("expected branch remote origin, got %q", got)
	}
	if got := stub.configSets["branch.feature/login.merge"]; got != "refs/heads/feature/login" {
		t.Fatalf("expected branch merge ref, got %q", got)
	}
}

// TestPullRequestCheckoutFromFork is the case the issue calls out as the one
// agents reliably get wrong: the source branch does not exist in the repository
// the caller is standing in.
func TestPullRequestCheckoutFromFork(t *testing.T) {
	server := newCheckoutServer(t, "~jdoe", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	withGitBackend(t, stub)

	result := decodeCheckoutResult(t, runCheckout(t, "--json", "pr", "checkout", "42"))

	if result["fork"] != true {
		t.Fatalf("expected a fork checkout, got: %v", result)
	}
	if result["remote"] != "jdoe" || result["remote_added"] != true {
		t.Fatalf("expected a new remote named after the fork owner, got: %v", result)
	}
	// Prefixed so it cannot collide with a local branch of the same name, or
	// with the same branch from a second fork.
	if result["branch"] != "jdoe/feature/login" {
		t.Fatalf("expected a fork-prefixed branch name, got: %v", result)
	}

	if len(stub.addedRemotes) != 1 {
		t.Fatalf("expected exactly one remote to be added, got: %v", stub.addedRemotes)
	}
	if !strings.Contains(stub.addedRemotes[0].URL, "/scm/~jdoe/demo.git") {
		t.Fatalf("expected the fork clone URL, got: %q", stub.addedRemotes[0].URL)
	}

	// Pushing must go back to the fork, not to the repository being stood in.
	if got := stub.configSets["branch.jdoe/feature/login.remote"]; got != "jdoe" {
		t.Fatalf("expected the fork remote as upstream, got %q", got)
	}
	if got := stub.configSets["branch.jdoe/feature/login.merge"]; got != "refs/heads/feature/login" {
		t.Fatalf("expected the fork's branch as the merge ref, got %q", got)
	}
}

// TestPullRequestCheckoutReusesAnExistingForkRemote keeps a second name for a
// place the caller already tracks from appearing. Two remotes for one
// repository means a later push can go to whichever git resolves first.
func TestPullRequestCheckoutReusesAnExistingForkRemote(t *testing.T) {
	server := newCheckoutServer(t, "~jdoe", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(
		git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"},
		git.Remote{Name: "myfork", URL: server.URL + "/scm/~jdoe/demo.git"},
	)
	withGitBackend(t, stub)

	result := decodeCheckoutResult(t, runCheckout(t, "--json", "pr", "checkout", "42"))

	if result["remote"] != "myfork" || result["remote_added"] != false {
		t.Fatalf("expected the existing fork remote to be reused, got: %v", result)
	}
	if len(stub.addedRemotes) != 0 {
		t.Fatalf("expected no remote to be added, got: %v", stub.addedRemotes)
	}
	if got := stub.configSets["branch.jdoe/feature/login.remote"]; got != "myfork" {
		t.Fatalf("expected upstream to name the existing remote, got %q", got)
	}
}

func TestPullRequestCheckoutAvoidsRemoteNameCollisions(t *testing.T) {
	server := newCheckoutServer(t, "~jdoe", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	// A remote already called jdoe, pointing somewhere else entirely.
	stub := newCheckoutBackendStub(
		git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"},
		git.Remote{Name: "jdoe", URL: "https://elsewhere.local/scm/OTHER/thing.git"},
	)
	withGitBackend(t, stub)

	result := decodeCheckoutResult(t, runCheckout(t, "--json", "pr", "checkout", "42"))

	if result["remote"] != "jdoe-2" {
		t.Fatalf("expected a suffixed remote name, got: %v", result)
	}
	if len(stub.addedRemotes) != 1 || stub.addedRemotes[0].Name != "jdoe-2" {
		t.Fatalf("expected the suffixed remote to be added, got: %v", stub.addedRemotes)
	}
}

// TestPullRequestCheckoutUpdatesAnExistingBranch covers the second run. Leaving
// the branch where an earlier checkout put it means silently reviewing an old
// revision, which looks identical to reviewing the current one.
func TestPullRequestCheckoutUpdatesAnExistingBranch(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	stub.branches["feature/login"] = true
	withGitBackend(t, stub)

	result := decodeCheckoutResult(t, runCheckout(t, "--json", "pr", "checkout", "42"))

	if result["fast_forwarded"] != true {
		t.Fatalf("expected the existing branch to be fast-forwarded, got: %v", result)
	}
	if stub.fastForwardRef != "refs/remotes/origin/feature/login" {
		t.Fatalf("expected a fast-forward to the fetched ref, got %q", stub.fastForwardRef)
	}
	if len(stub.checkouts) != 1 || stub.checkouts[0].NewBranch != "" || stub.checkouts[0].Ref != "feature/login" {
		t.Fatalf("expected the existing branch to be checked out, not recreated: %#v", stub.checkouts)
	}
}

// TestPullRequestCheckoutSurfacesADivergedBranch: --ff-only failing is the
// answer, not a problem to work around. Resetting would discard local commits.
func TestPullRequestCheckoutSurfacesADivergedBranch(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	stub.branches["feature/login"] = true
	stub.fastForwardErr = errors.New("fatal: Not possible to fast-forward, aborting.")
	withGitBackend(t, stub)

	err := runCheckoutExpectingError(t, "--json", "pr", "checkout", "42")
	if !strings.Contains(err.Error(), "fast-forward") {
		t.Fatalf("expected the fast-forward failure to surface, got: %v", err)
	}
}

func TestPullRequestCheckoutRefusesADirtyWorkingTree(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	stub.status = git.WorkingTreeStatus{Dirty: true, Entries: []string{" M internal/cli/root.go", " M go.mod"}}
	withGitBackend(t, stub)

	err := runCheckoutExpectingError(t, "pr", "checkout", "42")
	for _, expected := range []string{"uncommitted changes", "--force", "internal/cli/root.go"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected %q in the refusal, got: %v", expected, err)
		}
	}

	// Nothing may have happened: a refusal that already fetched is not a refusal.
	if len(stub.calls) != 0 {
		t.Fatalf("expected the repository to be untouched, got calls: %v", stub.calls)
	}
}

func TestPullRequestCheckoutForceProceedsOnADirtyTree(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	stub.status = git.WorkingTreeStatus{Dirty: true, Entries: []string{" M go.mod"}}
	withGitBackend(t, stub)

	runCheckout(t, "pr", "checkout", "42", "--force")

	if len(stub.checkouts) != 1 || !stub.checkouts[0].Force {
		t.Fatalf("expected --force to reach the checkout, got: %#v", stub.checkouts)
	}
}

func TestPullRequestCheckoutDetached(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	withGitBackend(t, stub)

	result := decodeCheckoutResult(t, runCheckout(t, "--json", "pr", "checkout", "42", "--detach"))

	if result["detached"] != true || result["branch"] != nil {
		t.Fatalf("expected a detached checkout with no branch, got: %v", result)
	}
	if len(stub.checkouts) != 1 || !stub.checkouts[0].Detach || stub.checkouts[0].NewBranch != "" {
		t.Fatalf("expected a detached checkout, got: %#v", stub.checkouts)
	}
	// No branch means no upstream to configure.
	if len(stub.configSets) != 0 {
		t.Fatalf("expected no branch config on a detached checkout, got: %v", stub.configSets)
	}
}

func TestPullRequestCheckoutHonoursAnExplicitBranchName(t *testing.T) {
	server := newCheckoutServer(t, "~jdoe", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	withGitBackend(t, stub)

	result := decodeCheckoutResult(t, runCheckout(t, "--json", "pr", "checkout", "42", "--branch", "review-42"))

	if result["branch"] != "review-42" {
		t.Fatalf("expected the requested branch name, got: %v", result)
	}
	// The upstream still points at the fork's real branch, not the local alias.
	if got := stub.configSets["branch.review-42.merge"]; got != "refs/heads/feature/login" {
		t.Fatalf("expected the merge ref to name the source branch, got %q", got)
	}
}

func TestPullRequestCheckoutRejectsBadInvocations(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	withGitBackend(t, stub)

	if err := runCheckoutExpectingError(t, "pr", "checkout", "42", "--branch", "x", "--detach"); err == nil {
		t.Fatal("expected --branch and --detach to be mutually exclusive")
	}
	runCheckoutExpectingError(t, "pr", "checkout")
	runCheckoutExpectingError(t, "pr", "checkout", "99")
}

// TestPullRequestCheckoutOutsideARepository is the one place this command is
// stricter than bb pr status: there is nothing useful it can do without a
// working copy, so it says what to do instead of degrading.
func TestPullRequestCheckoutOutsideARepository(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub()
	stub.rootErr = errors.New("fatal: not a git repository (or any of the parent directories): .git")
	withGitBackend(t, stub)

	err := runCheckoutExpectingError(t, "pr", "checkout", "42")
	for _, expected := range []string{"needs a git repository", "bb repo clone"} {
		if !strings.Contains(err.Error(), expected) {
			t.Fatalf("expected %q in the error, got: %v", expected, err)
		}
	}
}

func TestPullRequestCheckoutHumanOutput(t *testing.T) {
	server := newCheckoutServer(t, "~jdoe", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	withGitBackend(t, stub)

	output := runCheckout(t, "pr", "checkout", "42")
	for _, expected := range []string{
		"Added remote jdoe",
		"Checked out #42 on branch jdoe/feature/login tracking jdoe/feature/login",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in the output, got:\n%s", expected, output)
		}
	}
}

// TestForkRemoteDoesNotBreakRepositoryInference guards the trap this command
// would otherwise lay for every later bb invocation.
//
// bb pr checkout adds a remote for the fork a pull request comes from. Before
// this, a second Bitbucket remote made repository inference refuse as
// ambiguous — so running the command once broke `bb pr list` in that
// repository until the caller passed --repo forever after.
func TestForkRemoteDoesNotBreakRepositoryInference(t *testing.T) {
	cases := []struct {
		name        string
		remotes     []git.Remote
		wantProject string
		wantError   bool
	}{
		{
			name: "fork remote alongside origin resolves to origin",
			remotes: []git.Remote{
				{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"},
				{Name: "jdoe", URL: "https://bitbucket.local:7990/scm/~jdoe/demo.git"},
			},
			wantProject: "PRJ",
		},
		{
			name: "several side remotes still resolve to origin",
			remotes: []git.Remote{
				{Name: "jdoe", URL: "https://bitbucket.local:7990/scm/~jdoe/demo.git"},
				{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"},
				{Name: "mirror", URL: "https://bitbucket.local:7990/scm/MIRROR/demo.git"},
			},
			wantProject: "PRJ",
		},
		{
			// upstream is the one name that conventionally outranks origin, so
			// there is no defensible default between the two.
			name: "upstream alongside origin stays ambiguous",
			remotes: []git.Remote{
				{Name: "origin", URL: "https://bitbucket.local:7990/scm/PRJ/demo.git"},
				{Name: "upstream", URL: "https://bitbucket.local:7990/scm/ALT/demo.git"},
			},
			wantError: true,
		},
		{
			name: "no origin at all stays ambiguous",
			remotes: []git.Remote{
				{Name: "jdoe", URL: "https://bitbucket.local:7990/scm/~jdoe/demo.git"},
				{Name: "asmith", URL: "https://bitbucket.local:7990/scm/~asmith/demo.git"},
			},
			wantError: true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
			t.Setenv("BITBUCKET_URL", "https://bitbucket.local:7990")
			t.Setenv("BITBUCKET_PROJECT_KEY", "TEST")
			t.Setenv("BITBUCKET_REPO_SLUG", "demo")
			withGitBackend(t, inferenceGitBackendStub{repoRoot: "/tmp/repo", remotes: testCase.remotes})

			command := &cobra.Command{Use: "pr list"}
			command.Flags().String("repo", "", "")

			err := applyInferredRepositoryContext(command, false)
			if testCase.wantError {
				if err == nil {
					t.Fatal("expected an ambiguity error")
				}
				if apperrors.ExitCode(err) != 2 {
					t.Fatalf("expected a validation exit code, got %d (%v)", apperrors.ExitCode(err), err)
				}
				return
			}

			if err != nil {
				t.Fatalf("expected inference to succeed, got: %v", err)
			}
			if got := os.Getenv("BITBUCKET_PROJECT_KEY"); got != testCase.wantProject {
				t.Fatalf("expected project %q, got %q", testCase.wantProject, got)
			}
		})
	}
}
