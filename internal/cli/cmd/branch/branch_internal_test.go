package branchcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/safederef"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestBranchInternalHelpers(t *testing.T) {
	// safeString
	s := "test-branch"
	if safederef.String(&s) != "test-branch" || safederef.String(nil) != "" {
		t.Fatal("unexpected safeString result")
	}

	// safeInt32
	var i int32 = 42
	if safederef.Int32(&i) != 42 || safederef.Int32(nil) != 0 {
		t.Fatal("unexpected safeInt32 result")
	}

	// safeUsers
	users := []openapigenerated.RestApplicationUser{{Name: &s}}
	if len(safeUsers(&users)) != 1 || len(safeUsers(nil)) != 0 {
		t.Fatal("unexpected safeUsers result")
	}

	// safeStringSlice
	slice := []string{"group1", "group2"}
	if len(safederef.StringSlice(&slice)) != 2 || len(safederef.StringSlice(nil)) != 0 {
		t.Fatal("unexpected safeStringSlice result")
	}

	// normalizeAccessKeyIDs
	validKeys, err := normalizeAccessKeyIDs([]int{1, 2, 3})
	if err != nil || len(validKeys) != 3 || validKeys[0] != 1 {
		t.Fatalf("unexpected normalizeAccessKeyIDs valid error: %v", err)
	}

	_, err = normalizeAccessKeyIDs([]int{-1})
	if err == nil {
		t.Fatal("expected error for negative access key id")
	}

	// resolveBranchRepositoryReference
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")
	cfg := config.AppConfig{ProjectKey: "PRJ"}
	ref, err := resolveBranchRepositoryReference("", cfg)
	if err != nil || ref.ProjectKey != "PRJ" || ref.Slug != "repo" {
		t.Fatalf("unexpected resolveBranchRepositoryReference result: %+v, %v", ref, err)
	}

	ref, err = resolveBranchRepositoryReference("OTHER/custom", cfg)
	if err != nil || ref.ProjectKey != "OTHER" || ref.Slug != "custom" {
		t.Fatalf("unexpected resolveBranchRepositoryReference result: %+v, %v", ref, err)
	}

	_, err = resolveBranchRepositoryReference("invalid/repo/format", cfg)
	if err == nil {
		t.Fatal("expected error for invalid repository selector")
	}

	// withDefaults
	cmd := New(Dependencies{})
	if cmd == nil {
		t.Fatal("expected New with empty Dependencies to succeed")
	}
}

func TestBranchDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.DryRunEnabled == nil || d.DryRunEnabled() {
		t.Fatal("expected DryRunEnabled to default to false")
	}
	if d.WriteJSON == nil || d.WriteJSONList == nil {
		t.Fatal("expected WriteJSON and WriteJSONList to default to non-nil")
	}
	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig: %v", err)
		}
	}
	if d.LoadConfigAndClient != nil {
		cfg, client, err := d.LoadConfigAndClient()
		if err != nil || client == nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfigAndClient: %v", err)
		}
	}
}

func TestBranchRestrictionMatching(t *testing.T) {
	branchType := openapigenerated.RestRefRestrictionMatcherTypeIdBRANCH
	matcherID := "refs/heads/main"
	displayID := "main"
	resType := "read-only"
	alice := "alice"

	restriction := openapigenerated.RestRefRestriction{
		Type: &resType,
		Matcher: &struct {
			DisplayId *string `json:"displayId,omitempty"`
			Id        *string `json:"id,omitempty"`
			Type      *struct {
				Id   openapigenerated.RestRefRestrictionMatcherTypeId `json:"id"`
				Name string                                           `json:"name"`
			} `json:"type,omitempty"`
		}{
			Id:        &matcherID,
			DisplayId: &displayID,
			Type: &struct {
				Id   openapigenerated.RestRefRestrictionMatcherTypeId `json:"id"`
				Name string                                           `json:"name"`
			}{
				Id: branchType,
			},
		},
		Users: &[]openapigenerated.RestApplicationUser{{Name: &alice}},
	}

	if !MatchesRestrictionSignature(restriction, "read-only", "BRANCH", "refs/heads/main") {
		t.Fatal("expected MatchesRestrictionSignature to match full ref")
	}
	if !MatchesRestrictionSignature(restriction, "read-only", "BRANCH", "main") {
		t.Fatal("expected MatchesRestrictionSignature to match short branch name")
	}
	if MatchesRestrictionSignature(restriction, "no-deletes", "BRANCH", "main") {
		t.Fatal("expected MatchesRestrictionSignature to fail for different type")
	}
	if MatchesRestrictionSignature(restriction, "read-only", "TAG", "main") {
		t.Fatal("expected MatchesRestrictionSignature to fail for different matcher type")
	}
	if MatchesRestrictionSignature(openapigenerated.RestRefRestriction{}, "read-only", "BRANCH", "main") {
		t.Fatal("expected MatchesRestrictionSignature to fail for empty restriction")
	}

	// Update matching
	if !MatchesRestrictionUpdate(restriction, "read-only", "BRANCH", "main", []string{"alice"}, nil, nil) {
		t.Fatal("expected MatchesRestrictionUpdate to match")
	}
	if MatchesRestrictionUpdate(restriction, "read-only", "BRANCH", "main", []string{"bob"}, nil, nil) {
		t.Fatal("expected MatchesRestrictionUpdate to fail for different users")
	}
}
