package prcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type checkoutBackendStub struct {
	repositoryRoot string
	rootErr        error
	remotes        []git.Remote
	branches       map[string]bool
	status         git.WorkingTreeStatus
	fastForwardErr error
	failOn         map[string]error

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

func (stub *checkoutBackendStub) Version(context.Context) (string, error)               { return "", nil }
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

func newCheckoutServer(t *testing.T, sourceProject string, sourceSlug string, sourceBranch string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if request.URL.Path != "/rest/api/latest/projects/PRJ/repos/demo/pull-requests/42" &&
			request.URL.Path != "/rest/api/1.0/projects/PRJ/repos/demo/pull-requests/42" {
			http.NotFound(writer, request)
			return
		}

		payload := map[string]any{
			"id":    42,
			"title": "My change",
			"state": "OPEN",
			"open":  true,
			"fromRef": map[string]any{
				"id":         "refs/heads/" + sourceBranch,
				"displayId":  sourceBranch,
				"repository": map[string]any{"slug": sourceSlug, "project": map[string]any{"key": sourceProject}},
			},
			"toRef": map[string]any{
				"id":         "refs/heads/main",
				"displayId":  "main",
				"repository": map[string]any{"slug": "demo", "project": map[string]any{"key": "PRJ"}},
			},
		}

		_ = json.NewEncoder(writer).Encode(payload)
	}))
	t.Cleanup(server.Close)

	return server
}

func testPrCommand(deps Dependencies) *cobra.Command {
	root := &cobra.Command{Use: "bb"}
	root.PersistentFlags().Bool("json", false, "")
	root.PersistentFlags().Bool("dry-run", false, "")
	root.AddCommand(New(deps))
	return root
}

func executeCheckout(t *testing.T, backend git.Backend, serverURL string, args ...string) string {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var jsonFlag bool
	deps := Dependencies{
		JSONEnabled: func() bool { return jsonFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		GitBackend:    func() git.Backend { return backend },
	}

	command := testPrCommand(deps)
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)

	fullArgs := append([]string{"pr", "checkout"}, args...)
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		}
	}
	command.SetArgs(fullArgs)
	if err := command.Execute(); err != nil {
		t.Fatalf("%v failed: %v\noutput: %s", fullArgs, err, buffer.String())
	}

	return buffer.String()
}

func TestCheckoutSameRepositoryNewBranch(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/x")
	backend := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})

	executeCheckout(t, backend, server.URL, "42")

	if len(backend.addedRemotes) != 0 {
		t.Fatalf("expected no added remotes, got %v", backend.addedRemotes)
	}
	if len(backend.fetches) != 1 || backend.fetches[0].Remote != "origin" {
		t.Fatalf("expected fetch on origin, got %+v", backend.fetches)
	}
	wantRefspec := "+refs/heads/feature/x:refs/remotes/origin/feature/x"
	if len(backend.fetches[0].Refspecs) != 1 || backend.fetches[0].Refspecs[0] != wantRefspec {
		t.Fatalf("expected refspec %q, got %v", wantRefspec, backend.fetches[0].Refspecs)
	}
	if len(backend.checkouts) != 1 {
		t.Fatalf("expected 1 checkout call, got %+v", backend.checkouts)
	}
	checkout := backend.checkouts[0]
	if checkout.NewBranch != "feature/x" || checkout.Ref != "refs/remotes/origin/feature/x" {
		t.Fatalf("unexpected checkout options: %+v", checkout)
	}
	if backend.configSets["branch.feature/x.remote"] != "origin" ||
		backend.configSets["branch.feature/x.merge"] != "refs/heads/feature/x" {
		t.Fatalf("unexpected branch config: %+v", backend.configSets)
	}
}

func TestCheckoutForkAddsRemoteAndPrefixesBranch(t *testing.T) {
	server := newCheckoutServer(t, "~alice", "demo", "bugfix")
	backend := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})

	executeCheckout(t, backend, server.URL, "42")

	if len(backend.addedRemotes) != 1 {
		t.Fatalf("expected 1 added remote, got %v", backend.addedRemotes)
	}
	remote := backend.addedRemotes[0]
	if remote.Name != "alice" || remote.URL != server.URL+"/scm/~alice/demo.git" {
		t.Fatalf("unexpected added remote: %+v", remote)
	}
	if len(backend.checkouts) != 1 || backend.checkouts[0].NewBranch != "alice/bugfix" {
		t.Fatalf("expected local branch alice/bugfix, got %+v", backend.checkouts)
	}
	if backend.configSets["branch.alice/bugfix.remote"] != "alice" ||
		backend.configSets["branch.alice/bugfix.merge"] != "refs/heads/bugfix" {
		t.Fatalf("unexpected branch config: %+v", backend.configSets)
	}
}

func TestCheckoutExistingBranchFastForwards(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/x")
	backend := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	backend.branches["feature/x"] = true

	executeCheckout(t, backend, server.URL, "42")

	if len(backend.checkouts) != 1 || backend.checkouts[0].Ref != "feature/x" || backend.checkouts[0].NewBranch != "" {
		t.Fatalf("expected checkout of existing branch without -b, got %+v", backend.checkouts)
	}
	if backend.fastForwardRef != "refs/remotes/origin/feature/x" {
		t.Fatalf("expected fast-forward of origin/feature/x, got %q", backend.fastForwardRef)
	}
}

func TestCheckoutDetachOption(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/x")
	backend := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})

	executeCheckout(t, backend, server.URL, "42", "--detach")

	if len(backend.checkouts) != 1 || !backend.checkouts[0].Detach || backend.checkouts[0].Ref != "refs/remotes/origin/feature/x" {
		t.Fatalf("expected detached checkout, got %+v", backend.checkouts)
	}
	if len(backend.configSets) != 0 {
		t.Fatalf("expected no branch config when detached, got %+v", backend.configSets)
	}
}

func TestCheckoutRefusesDirtyWorkingTree(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/x")
	backend := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	backend.status = git.WorkingTreeStatus{Dirty: true, Entries: []string{" M modified.go"}}

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", server.URL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	deps := Dependencies{
		JSONEnabled: func() bool { return false },
		LoadConfig:  func() (config.AppConfig, error) { return config.LoadFromEnv() },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		WriteJSON:  jsonoutput.Write,
		GitBackend: func() git.Backend { return backend },
	}

	command := testPrCommand(deps)
	command.SetArgs([]string{"pr", "checkout", "42"})
	err := command.Execute()
	if err == nil {
		t.Fatal("expected checkout to fail on dirty working tree")
	}
	if apperrors.ExitCode(err) != 2 {
		t.Fatalf("expected validation exit code 2, got %d (err: %v)", apperrors.ExitCode(err), err)
	}
}
