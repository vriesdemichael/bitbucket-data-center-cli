package repocmd

import (
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
)

func TestCommentOwnedByUser(t *testing.T) {
	t.Parallel()

	username := "alice"
	commentWithName := openapigenerated.RestComment{Author: &struct {
		Active       *bool                                  `json:"active,omitempty"`
		AvatarUrl    *string                                `json:"avatarUrl,omitempty"`
		DisplayName  string                                 `json:"displayName"`
		EmailAddress *string                                `json:"emailAddress,omitempty"`
		Id           *int32                                 `json:"id,omitempty"`
		Links        *map[string]interface{}                `json:"links,omitempty"`
		Name         string                                 `json:"name"`
		Slug         string                                 `json:"slug"`
		Type         openapigenerated.RestCommentAuthorType `json:"type"`
	}{Name: username}}
	if !commentOwnedByUser(commentWithName, " alice ") {
		t.Fatal("expected comment ownership match by name")
	}

	slug := "alice"
	commentWithSlug := openapigenerated.RestComment{Author: &struct {
		Active       *bool                                  `json:"active,omitempty"`
		AvatarUrl    *string                                `json:"avatarUrl,omitempty"`
		DisplayName  string                                 `json:"displayName"`
		EmailAddress *string                                `json:"emailAddress,omitempty"`
		Id           *int32                                 `json:"id,omitempty"`
		Links        *map[string]interface{}                `json:"links,omitempty"`
		Name         string                                 `json:"name"`
		Slug         string                                 `json:"slug"`
		Type         openapigenerated.RestCommentAuthorType `json:"type"`
	}{Slug: slug}}
	if !commentOwnedByUser(commentWithSlug, "alice") {
		t.Fatal("expected comment ownership match by slug")
	}

	if commentOwnedByUser(openapigenerated.RestComment{}, "alice") {
		t.Fatal("expected missing author to fail ownership check")
	}
	if commentOwnedByUser(commentWithName, "") {
		t.Fatal("expected blank username to fail ownership check")
	}
	if commentOwnedByUser(commentWithName, "bob") {
		t.Fatal("expected mismatched username to fail ownership check")
	}
}

func TestCommentHelpers(t *testing.T) {
	t.Parallel()

	if commentIDString(openapigenerated.RestComment{}) != "?" {
		t.Fatal("expected ? for comment with nil ID")
	}
	id := int64(42)
	if commentIDString(openapigenerated.RestComment{Id: &id}) != "42" {
		t.Fatal("expected 42 for comment with ID")
	}
}

// TestResolveCommentTargetRequiresExactlyOneContext moved here from
// internal/cli, where it exercised a copy of this function left behind by the
// ADR-032 modularization. The copy had no callers; this one has four, and had
// no test of its own.
func TestResolveCommentTargetRequiresExactlyOneContext(t *testing.T) {
	t.Parallel()

	cfg := config.AppConfig{ProjectKey: "TEST", RepoSlug: "demo"}

	if _, err := resolveCommentTarget("", "", "", cfg); err == nil {
		t.Fatal("expected validation error for missing commit/pr")
	}

	if _, err := resolveCommentTarget("", "abc123", "77", cfg); err == nil {
		t.Fatal("expected validation error for both commit and pr")
	}

	target, err := resolveCommentTarget("", "abc123", "", cfg)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if target.CommitID != "abc123" || target.PullRequestID != "" {
		t.Fatalf("unexpected target: %+v", target)
	}

	target, err = resolveCommentTarget("", "", " 77 ", cfg)
	if err != nil {
		t.Fatalf("expected no error for pull request target, got: %v", err)
	}
	if target.CommitID != "" || target.PullRequestID != "77" {
		t.Fatalf("unexpected pull request target: %+v", target)
	}
}

// TestCreateTargetPreviewNamesTheParentOnlyWhenThereIsOne covers the dry-run
// description of a reply.
//
// A zero parent id in the preview would read as "replies to comment 0", which
// is neither a reply nor a new thread.
func TestCreateTargetPreviewNamesTheParentOnlyWhenThereIsOne(t *testing.T) {
	t.Parallel()

	target := commentservice.Target{
		Repository: commentservice.RepositoryRef{ProjectKey: "PRJ", Slug: "payments"},
		CommitID:   "abc123",
	}

	root := createTargetPreview(target, "the root", 0)
	if _, named := root["parentId"]; named {
		t.Errorf("a new thread named a parent: %+v", root)
	}
	if root["text"] != "the root" {
		t.Errorf("preview = %+v", root)
	}

	reply := createTargetPreview(target, "a reply", 42)
	if reply["parentId"] != int64(42) {
		t.Errorf("a reply did not name what it answers: %+v", reply)
	}
}

// TestCommentListLimitActuallyLimits is #473 end to end.
//
// commentservice.List takes its limit as a page size and reads to exhaustion,
// so --limit sized the requests and truncated nothing: a smaller --limit made
// more round trips and printed the same complete answer.

// The repo comment command suite and the #473 --limit regression are live
// now.
//
// The suite drove create, get, list, update and delete against a
// hand-written Bitbucket and read each command's line back out of the fixture
// it had just written. The regression served two pages of ten and checked
// --limit 3 did not return twenty; TestLiveInlineCommentAnchoring now puts
// four comments on a real file and asks for three, which is the same question
// of a server that decides how many there are.
