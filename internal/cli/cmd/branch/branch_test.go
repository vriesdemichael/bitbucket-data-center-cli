package branchcmd_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	branchcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/branch"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

type testPermissionChecker struct{}

func (testPermissionChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}

// newMockBranchServer opens a listener that fails the test if anything reaches it.
//
// Everything still here refuses before it asks -- bad flags, a missing selector, a permission that is denied. The handwritten Bitbucket it used to be answered branches nobody reads any more.
func newMockBranchServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(testsupport.UnreachedHandler(t))
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

func TestBranchRestrictionMatching(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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

func TestBranchValidationErrors(t *testing.T) {
	server := newMockBranchServer(t)
	deps := newTestDependencies(t, server.URL, false, false)

	// A non-numeric restriction id is not refused here; it goes to the server
	// and the server refuses it. That is live in
	// TestLiveCLIBranchRestrictionLifecycle, which asks a real instance what it
	// makes of "abc" -- the version here asked a stub whose default was 404.
	cases := [][]string{
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

// branchListingFor went with the mock server. It reimplemented Bitbucket's
// filterText -- narrowing on a substring, answering empty for an unmatched
// filter -- so a test using it checked our CLI against our second guess at
// the rule, and the two agreed because the same person wrote both.
