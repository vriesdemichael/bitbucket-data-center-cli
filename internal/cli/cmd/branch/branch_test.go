package branchcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	branchcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/branch"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type testPermissionChecker struct{}

func (testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

func newMockBranchServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/default-branch":
			_, _ = w.Write([]byte(`{"id":"refs/heads/main","displayId":"main"}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/demo/default-branch":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/demo/branches":
			// The real endpoint filters by filterText, and setting a default
			// branch now checks the branch exists before writing it. A mock
			// that returned the same list whatever was asked would answer
			// "main" to a query for "master" and quietly defeat that check.
			_, _ = w.Write([]byte(branchListingFor(r.URL.Query().Get("filterText"))))

		case r.Method == http.MethodPost && path == "/rest/branch-utils/latest/projects/PRJ/repos/demo/branches":
			_, _ = w.Write([]byte(`{"id":"refs/heads/feature-1","displayId":"feature-1","latestCommit":"2222222"}`))

		case r.Method == http.MethodDelete && path == "/rest/branch-utils/latest/projects/PRJ/repos/demo/branches":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPut && path == "/rest/branch-utils/latest/projects/PRJ/repos/demo/branch-model/configuration":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"development":{"refId":"refs/heads/develop"}}`))

		case r.Method == http.MethodGet && strings.Contains(path, "/branches/info/"):
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":"refs/heads/main","displayId":"main"}]}`))

		case r.Method == http.MethodGet && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions":
			_, _ = w.Write([]byte(`{"isLastPage":true,"values":[{"id":42,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}]}`))

		case r.Method == http.MethodPost && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions":
			_, _ = w.Write([]byte(`[{"id":43,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}]`))

		case r.Method == http.MethodGet && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions/42":
			_, _ = w.Write([]byte(`{"id":42,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}`))

		case r.Method == http.MethodPut && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions/42":
			_, _ = w.Write([]byte(`{"id":42,"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}`))

		case r.Method == http.MethodDelete && path == "/rest/branch-permissions/latest/projects/PRJ/repos/demo/restrictions/42":
			w.WriteHeader(http.StatusNoContent)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func newTestDependencies(t *testing.T, serverURL string, jsonMode bool, dryRun bool) branchcmd.Dependencies {
	cfg := config.AppConfig{
		BitbucketURL: serverURL,
		ProjectKey:   "PRJ",
	}
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")

	return branchcmd.Dependencies{
		JSONEnabled:   func() bool { return jsonMode },
		DryRunEnabled: func() bool { return dryRun },
		LoadConfig: func() (config.AppConfig, error) {
			return cfg, nil
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			return cfg, client, nil
		},
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) branchcmd.PermissionChecker {
			return testPermissionChecker{}
		},
	}
}

func TestBranchList(t *testing.T) {
	server := newMockBranchServer(t)

	// Human mode
	deps := newTestDependencies(t, server.URL, false, false)
	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "main") {
		t.Fatalf("expected 'main' in output, got: %s", buf.String())
	}

	// JSON mode
	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on json list: %v", err)
	}
	if !strings.Contains(buf.String(), "branches") {
		t.Fatalf("expected 'branches' in json output, got: %s", buf.String())
	}
}

func TestBranchCreateAndDelete(t *testing.T) {
	server := newMockBranchServer(t)

	// Create dry-run
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd := branchcmd.New(depsDryRun)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "feature-1", "--start-point", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create dry-run: %v", err)
	}

	// Create real execution
	deps := newTestDependencies(t, server.URL, false, false)
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "feature-1", "--start-point", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create: %v", err)
	}
	if !strings.Contains(buf.String(), "Created branch") {
		t.Fatalf("expected 'Created branch' in output, got: %s", buf.String())
	}

	// Delete dry-run
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "feature-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete dry-run: %v", err)
	}

	// Delete real execution
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "feature-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted branch") {
		t.Fatalf("expected 'Deleted branch' in output, got: %s", buf.String())
	}
}

func TestBranchDefaultGetAndSet(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default", "get"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "main") {
		t.Fatalf("expected 'main' in output, got: %s", buf.String())
	}

	// Set dry-run
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default", "set", "master"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default set dry-run: %v", err)
	}

	// Set real execution
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default", "set", "master"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default set: %v", err)
	}
	if !strings.Contains(buf.String(), "Default branch set to master") {
		t.Fatalf("expected Default branch set in output: %s", buf.String())
	}
}

func TestBranchModelInspectAndUpdate(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	// Inspect human mode
	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"model", "inspect", "1111111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on model inspect: %v", err)
	}
	if !strings.Contains(buf.String(), "main") {
		t.Fatalf("expected main in inspect output: %s", buf.String())
	}

	// Inspect JSON mode
	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"model", "inspect", "1111111"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on model inspect JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "refs") {
		t.Fatalf("expected refs in json output: %s", buf.String())
	}

	// Model update dry run
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"model", "update", "develop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on model update dry-run: %v", err)
	}

	// Model update real execution
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"model", "update", "develop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on model update: %v", err)
	}
	if !strings.Contains(buf.String(), "Branch model default updated to") {
		t.Fatalf("expected Branch model default updated in output: %s", buf.String())
	}
}

func TestBranchRestrictions(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	// List
	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "read-only") {
		t.Fatalf("expected 'read-only' in output, got: %s", buf.String())
	}

	// Get (human and JSON)
	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "get", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction get: %v", err)
	}
	if !strings.Contains(buf.String(), "read-only") {
		t.Fatalf("expected read-only in get output: %s", buf.String())
	}

	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "get", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "restriction") {
		t.Fatalf("expected restriction in JSON output: %s", buf.String())
	}

	// Create (dry-run and real)
	depsDryRun := newTestDependencies(t, server.URL, false, true)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "create", "--type", "read-only", "--matcher-id", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction create dry-run: %v", err)
	}

	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "create", "--type", "read-only", "--matcher-id", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction create: %v", err)
	}
	if !strings.Contains(buf.String(), "Created restriction") {
		t.Fatalf("expected Created restriction in output: %s", buf.String())
	}

	// Update (dry-run and real)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "update", "42", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction update dry-run: %v", err)
	}

	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "update", "42", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction update: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated restriction") {
		t.Fatalf("expected Updated restriction in output: %s", buf.String())
	}

	// Delete (dry-run and real)
	cmd = branchcmd.New(depsDryRun)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "delete", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction delete dry-run: %v", err)
	}

	cmd = branchcmd.New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "delete", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted restriction") {
		t.Fatalf("expected Deleted restriction in output: %s", buf.String())
	}
}

func TestBranchRestrictionMatching(t *testing.T) {
	var restriction openapigenerated.RestRefRestriction
	_ = json.Unmarshal([]byte(`{"type":"read-only","matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}}}`), &restriction)

	// Signature matches
	if !branchcmd.MatchesRestrictionSignature(restriction, "read-only", "BRANCH", "main") {
		t.Fatal("expected match for read-only BRANCH main")
	}
	if !branchcmd.MatchesRestrictionSignature(restriction, "READ-ONLY", "branch", "refs/heads/main") {
		t.Fatal("expected case-insensitive match with refs/heads prefix")
	}

	// Signature mismatches
	if branchcmd.MatchesRestrictionSignature(restriction, "fast-forward-only", "BRANCH", "main") {
		t.Fatal("expected mismatch for different restriction type")
	}
	if branchcmd.MatchesRestrictionSignature(restriction, "read-only", "MODEL_BRANCH", "main") {
		t.Fatal("expected mismatch for different matcher type")
	}
	if branchcmd.MatchesRestrictionSignature(restriction, "read-only", "BRANCH", "feature") {
		t.Fatal("expected mismatch for different matcher id")
	}
	if branchcmd.MatchesRestrictionSignature(openapigenerated.RestRefRestriction{}, "read-only", "BRANCH", "main") {
		t.Fatal("expected mismatch for empty restriction")
	}

	// Update matching with users, groups, access keys
	var restrictionWithEntities openapigenerated.RestRefRestriction
	_ = json.Unmarshal([]byte(`{
		"type":"read-only",
		"matcher":{"id":"refs/heads/main","displayId":"main","type":{"id":"BRANCH"}},
		"users":[{"name":"alice"},{"name":"bob"}],
		"groups":["developers"],
		"accessKeys":[{"key":{"id":101}}]
	}`), &restrictionWithEntities)

	if !branchcmd.MatchesRestrictionUpdate(restrictionWithEntities, "read-only", "BRANCH", "main", []string{"bob", "alice"}, []string{"developers"}, []int32{101}) {
		t.Fatal("expected MatchesRestrictionUpdate to match regardless of user order")
	}
	if branchcmd.MatchesRestrictionUpdate(restrictionWithEntities, "read-only", "BRANCH", "main", []string{"bob"}, []string{"developers"}, []int32{101}) {
		t.Fatal("expected MatchesRestrictionUpdate to fail on user count mismatch")
	}
	if branchcmd.MatchesRestrictionUpdate(restrictionWithEntities, "read-only", "BRANCH", "main", []string{"bob", "alice"}, []string{"admins"}, []int32{101}) {
		t.Fatal("expected MatchesRestrictionUpdate to fail on group mismatch")
	}
	if branchcmd.MatchesRestrictionUpdate(restrictionWithEntities, "read-only", "BRANCH", "main", []string{"bob", "alice"}, []string{"developers"}, []int32{999}) {
		t.Fatal("expected MatchesRestrictionUpdate to fail on key mismatch")
	}
}

func TestBranchNormalize(t *testing.T) {
	if branchcmd.NormalizeBranchName("main") != "refs/heads/main" {
		t.Fatalf("expected refs/heads/main, got: %s", branchcmd.NormalizeBranchName("main"))
	}
	if branchcmd.NormalizeBranchName("refs/heads/feature") != "refs/heads/feature" {
		t.Fatalf("expected refs/heads/feature, got: %s", branchcmd.NormalizeBranchName("refs/heads/feature"))
	}
	if branchcmd.NormalizeBranchName("  hotfix  ") != "refs/heads/hotfix" {
		t.Fatalf("expected refs/heads/hotfix, got: %s", branchcmd.NormalizeBranchName("  hotfix  "))
	}
}

func TestBranchJSONAndFlagModes(t *testing.T) {
	server := newMockBranchServer(t)

	// List with filter and flags
	deps := newTestDependencies(t, server.URL, false, false)
	cmd := branchcmd.New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--filter", "main", "--order-by", "ALPHABETICAL", "--details"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on list with flags: %v", err)
	}

	// Create JSON mode
	depsJSON := newTestDependencies(t, server.URL, true, false)
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "feature-1", "--start-point", "main"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on create JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "feature-1") {
		t.Fatalf("expected feature-1 in JSON output: %s", buf.String())
	}

	// Delete JSON mode
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "feature-1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on delete JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "branch") {
		t.Fatalf("expected branch in JSON output: %s", buf.String())
	}

	// Default get JSON mode
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default", "get"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default get JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "defaultBranch") {
		t.Fatalf("expected defaultBranch in JSON output: %s", buf.String())
	}

	// Default set JSON mode
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"default", "set", "master"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on default set JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "defaultBranch") {
		t.Fatalf("expected defaultBranch in JSON output: %s", buf.String())
	}

	// Model update JSON mode
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"model", "update", "develop"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on model update JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "defaultBranch") {
		t.Fatalf("expected defaultBranch in JSON output: %s", buf.String())
	}

	// Restriction list JSON mode & with filter flags
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "list", "--matcher-id", "main", "--matcher-type", "BRANCH", "--type", "read-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction list JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "restrictions") {
		t.Fatalf("expected restrictions in JSON output: %s", buf.String())
	}

	// Restriction create JSON mode
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "create", "--type", "read-only", "--matcher-id", "main", "--user", "alice", "--group", "devs"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction create JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "restriction") {
		t.Fatalf("expected restriction in JSON output: %s", buf.String())
	}

	// Restriction update JSON mode
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "update", "42", "--type", "read-only", "--matcher-type", "BRANCH", "--matcher-id", "main", "--user", "alice"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction update JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "restriction") {
		t.Fatalf("expected restriction in JSON output: %s", buf.String())
	}

	// Restriction delete JSON mode
	cmd = branchcmd.New(depsJSON)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"restriction", "delete", "42"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on restriction delete JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "restriction") {
		t.Fatalf("expected restriction in JSON output: %s", buf.String())
	}
}

func TestBranchValidationErrors(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	cases := [][]string{
		{"restriction", "get", "abc"},
		{"restriction", "update", "abc"},
		{"restriction", "delete", "abc"},
		{"restriction", "create", "--type", "read-only", "--matcher-id", ""},
		{"restriction", "create", "--type", "read-only", "--matcher-id", "main", "--access-key-id", "-1"},
		{"create", ""},
	}

	for _, args := range cases {
		cmd := branchcmd.New(deps)
		buf := new(bytes.Buffer)
		cmd.SetOut(buf)
		cmd.SetErr(buf)
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected error for args %v, got nil", args)
		}
	}
}

// branchListingFor answers a branch listing the way Bitbucket does: filterText
// narrows the result, and an unmatched filter comes back empty rather than
// returning everything.
func branchListingFor(filterText string) string {
	branches := map[string]string{
		"main":      `{"id":"refs/heads/main","displayId":"main","latestCommit":"1111111"}`,
		"master":    `{"id":"refs/heads/master","displayId":"master","latestCommit":"3333333"}`,
		"feature-1": `{"id":"refs/heads/feature-1","displayId":"feature-1","latestCommit":"2222222"}`,
		"develop":   `{"id":"refs/heads/develop","displayId":"develop","latestCommit":"4444444"}`,
	}

	matched := []string{}
	for _, name := range []string{"main", "master", "feature-1", "develop"} {
		if filterText == "" || strings.Contains(name, filterText) {
			matched = append(matched, branches[name])
		}
	}

	return `{"isLastPage":true,"values":[` + strings.Join(matched, ",") + `]}`
}
