package repocmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func TestCommentOwnedByUser(t *testing.T) {
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

func TestRepoCommentCommands(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		path := r.URL.Path

		switch {
		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/commits/c123/comments":
			_, _ = w.Write([]byte(`{"values":[{"id":101,"version":1,"text":"Commit comment","author":{"name":"alice"}}]}`))

		case r.Method == http.MethodGet && path == "/rest/api/latest/projects/PRJ/repos/repo1/commits/c123/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":1,"text":"Commit comment","author":{"name":"alice"}}`))

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/commits/c123/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":102,"version":1,"text":"Created comment","author":{"name":"alice"}}`))

		case r.Method == http.MethodPut && path == "/rest/api/latest/projects/PRJ/repos/repo1/commits/c123/comments/101":
			_, _ = w.Write([]byte(`{"id":101,"version":2,"text":"Updated comment","author":{"name":"alice"}}`))

		case r.Method == http.MethodDelete && path == "/rest/api/latest/projects/PRJ/repos/repo1/commits/c123/comments/101":
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodPost && path == "/rest/api/latest/projects/PRJ/repos/repo1/commits/c123/comments/101/comments":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":103,"version":1,"text":"Reply comment","author":{"name":"alice"}}`))

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{
		BitbucketURL:      server.URL,
		ProjectKey:        "PRJ",
		BitbucketUsername: "alice",
	}

	jsonEnabled := false
	dryRunEnabled := false

	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonEnabled },
		DryRunEnabled: func() bool { return dryRunEnabled },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
	}

	// 1. Comment list (human & JSON)
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "list", "--commit", "c123", "--path", "main.go", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on comment list: %v", err)
	}
	if !strings.Contains(buf.String(), "Commit comment") {
		t.Fatalf("expected Commit comment in output: %s", buf.String())
	}

	jsonEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "list", "--commit", "c123", "--path", "main.go", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on comment list JSON: %v", err)
	}
	if !strings.Contains(buf.String(), "comments") {
		t.Fatalf("expected comments in JSON output: %s", buf.String())
	}
	jsonEnabled = false

	// 2. Comment create (dry-run & real)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "create", "--commit", "c123", "--text", "Created comment", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on comment create dry-run: %v", err)
	}
	dryRunEnabled = false

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "create", "--commit", "c123", "--text", "Created comment", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on comment create: %v", err)
	}
	if !strings.Contains(buf.String(), "Created comment") {
		t.Fatalf("expected Created comment in output: %s", buf.String())
	}

	// 3. Comment update (dry-run & real)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "update", "--commit", "c123", "--id", "101", "--text", "Updated comment", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on comment update dry-run: %v", err)
	}
	dryRunEnabled = false

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "update", "--commit", "c123", "--id", "101", "--text", "Updated comment", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on comment update: %v", err)
	}
	if !strings.Contains(buf.String(), "Updated comment") {
		t.Fatalf("expected Updated comment in output: %s", buf.String())
	}

	// 4. Comment delete (dry-run & real)
	dryRunEnabled = true
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "delete", "--commit", "c123", "--id", "101", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on comment delete dry-run: %v", err)
	}
	dryRunEnabled = false

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "delete", "--commit", "c123", "--id", "101", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error on comment delete: %v", err)
	}
	if !strings.Contains(buf.String(), "Deleted comment") {
		t.Fatalf("expected Deleted comment in output: %s", buf.String())
	}

	// 5. Target validation errors (both commit and pr, or neither)
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "list", "--path", "main.go", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected validation error when neither --commit nor --pr is passed")
	}

	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"comment", "list", "--commit", "c123", "--pr", "42", "--path", "main.go", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatalf("expected validation error when both --commit and --pr are passed")
	}

}

func TestCommentHelpers(t *testing.T) {
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
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	cfg := config.AppConfig{ProjectKey: "TEST"}

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
