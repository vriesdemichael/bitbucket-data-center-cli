package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeAPIPath(t *testing.T) {
	cases := map[string]string{
		"/rest/api/latest/projects/%s/repos/%s/pull-requests":      "/api/latest/projects/{}/repos/{}/pull-requests",
		"/api/latest/projects/{projectKey}/repos/{repositorySlug}": "/api/latest/projects/{}/repos/{}",
		"/rest/api/1.0/dashboard/pull-requests":                    "/api/latest/dashboard/pull-requests",
		"/api/latest/projects/%s/repos/%s/commits?since=x":         "/api/latest/projects/{}/repos/{}/commits",
	}
	for input, want := range cases {
		if got := normalizeAPIPath(input); got != want {
			t.Errorf("normalizeAPIPath(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestSubstituteVerbs(t *testing.T) {
	got := substituteVerbs("%s/%s/%s", []string{"/rest/api/latest/projects/{}/repos/{}/pull-requests", "{}", "merge"})
	want := "/rest/api/latest/projects/{}/repos/{}/pull-requests/{}/merge"
	if got != want {
		t.Fatalf("substituteVerbs = %q, want %q", got, want)
	}
	// Surplus verbs without a replacement are preserved for later normalization.
	if got := substituteVerbs("%s/%s/extra", []string{"a"}); got != "a/%s/extra" {
		t.Fatalf("substituteVerbs surplus = %q", got)
	}
}

func TestCartesian(t *testing.T) {
	combos := cartesian([][]string{{"a"}, {"x", "y"}})
	if len(combos) != 2 {
		t.Fatalf("expected 2 combos, got %d", len(combos))
	}
}

// TestCollectRawHTTPPaths exercises helper resolution, fmt.Sprintf assembly,
// local path variables, and parameter-literal substitution end to end.
func TestCollectRawHTTPPaths(t *testing.T) {
	dir := t.TempDir()
	src := `package svc

import (
	"context"
	"fmt"
)

type Repo struct{ ProjectKey, Slug string }

func prPath(r Repo) string {
	return fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", r.ProjectKey, r.Slug)
}

type Service struct{ client *Client }

func (s *Service) list(ctx context.Context, r Repo) {
	s.client.GetJSON(ctx, prPath(r), nil, nil)
}

func (s *Service) create(ctx context.Context, r Repo) {
	s.client.PostJSON(ctx, prPath(r), nil, nil, nil)
}

func (s *Service) transition(ctx context.Context, r Repo, id string, action string) {
	s.client.PostJSON(ctx, fmt.Sprintf("%s/%s/%s", prPath(r), id, action), nil, nil, nil)
}

func (s *Service) merge(ctx context.Context, r Repo, id string) { s.transition(ctx, r, id, "merge") }
func (s *Service) decline(ctx context.Context, r Repo, id string) { s.transition(ctx, r, id, "decline") }

func (s *Service) assign(ctx context.Context, r Repo, id string) {
	path := fmt.Sprintf("%s/%s/participants", prPath(r), id)
	s.client.PostJSON(ctx, path, nil, nil, nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "service.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	covered, err := collectRawHTTPPaths(dir)
	if err != nil {
		t.Fatal(err)
	}

	base := "/api/latest/projects/{}/repos/{}/pull-requests"
	want := []methodPath{
		{Method: "GET", Path: base},
		{Method: "POST", Path: base},
		{Method: "POST", Path: base + "/{}/merge"},
		{Method: "POST", Path: base + "/{}/decline"},
		{Method: "POST", Path: base + "/{}/participants"},
	}
	for _, mp := range want {
		if _, ok := covered[mp]; !ok {
			t.Errorf("expected covered %+v, missing", mp)
		}
	}
}

func TestParseGeneratedOperationPaths(t *testing.T) {
	dir := t.TempDir()
	src := `package openapigenerated

import "net/http"

func NewGetCommitRequest(server string) (*http.Request, error) {
	operationPath := fmt.Sprintf("/api/latest/projects/%s/repos/%s/commits/%s", a, b, c)
	_ = operationPath
	return http.NewRequest("GET", operationPath, nil)
}

// Body operations delegate to the WithBody constructor, which builds the path.
func NewCreateCommentRequest(server string, body any) (*http.Request, error) {
	return NewCreateCommentRequestWithBody(server, "application/json", nil)
}

func NewCreateCommentRequestWithBody(server string, contentType string, body any) (*http.Request, error) {
	operationPath := fmt.Sprintf("/api/latest/projects/%s/repos/%s/commits/%s/comments", a, b, c)
	_ = operationPath
	return http.NewRequest("POST", operationPath, nil)
}
`
	if err := os.WriteFile(filepath.Join(dir, "client.gen.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := parseGeneratedOperationPaths(filepath.Join(dir, "client.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if mp := got["GetCommit"]; mp.Method != "GET" || mp.Path != "/api/latest/projects/{}/repos/{}/commits/{}" {
		t.Errorf("GetCommit = %+v", mp)
	}
	if mp := got["CreateComment"]; mp.Method != "POST" || mp.Path != "/api/latest/projects/{}/repos/{}/commits/{}/comments" {
		t.Errorf("CreateComment (WithBody) = %+v", mp)
	}
}

func TestUsedOperationToName(t *testing.T) {
	cases := map[string]string{
		"GetCommitWithResponse":              "GetCommit",
		"SetSettingsWithBodyWithResponse":    "SetSettings",
		"UpdatePullRequestSettings1WithBody": "UpdatePullRequestSettings1",
		"GetPullRequestSettings1":            "GetPullRequestSettings1",
	}
	for input, want := range cases {
		if got := usedOperationToName(input); got != want {
			t.Errorf("usedOperationToName(%q) = %q, want %q", input, got, want)
		}
	}
}
