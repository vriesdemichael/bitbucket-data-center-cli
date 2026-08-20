package branchcmd

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestBranchInternalHelpers(t *testing.T) {
	// safeString
	s := "test-branch"
	if safeString(&s) != "test-branch" || safeString(nil) != "" {
		t.Fatal("unexpected safeString result")
	}

	// safeInt32
	var i int32 = 42
	if safeInt32(&i) != 42 || safeInt32(nil) != 0 {
		t.Fatal("unexpected safeInt32 result")
	}

	// safeUsers
	users := []openapigenerated.RestApplicationUser{{Name: &s}}
	if len(safeUsers(&users)) != 1 || len(safeUsers(nil)) != 0 {
		t.Fatal("unexpected safeUsers result")
	}

	// safeStringSlice
	slice := []string{"group1", "group2"}
	if len(safeStringSlice(&slice)) != 2 || len(safeStringSlice(nil)) != 0 {
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
