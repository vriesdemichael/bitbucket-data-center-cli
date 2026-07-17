package jira

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func newJiraTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	cfg := config.AppConfig{
		BitbucketURL: server.URL,
	}
	client := httpclient.NewFromConfig(cfg)
	return NewService(client)
}

func TestGetPRIssues(t *testing.T) {
	service := newJiraTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/rest/jira/latest/projects/PROJ/repos/repo/pull-requests/42/issues" {
			_, _ = w.Write([]byte(`[{"key":"TEST-101","url":"http://jira/TEST-101"},{"key":"TEST-102","url":"http://jira/TEST-102"}]`))
			return
		}
		http.NotFound(w, r)
	})

	issues, err := service.GetPRIssues(context.Background(), RepositoryRef{ProjectKey: "PROJ", Slug: "repo"}, "42")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(issues) != 2 {
		t.Fatalf("expected 2 issues, got %d", len(issues))
	}

	if issues[0].Key != "TEST-101" || issues[0].URL != "http://jira/TEST-101" {
		t.Errorf("unexpected issue 0: %+v", issues[0])
	}
	if issues[1].Key != "TEST-102" || issues[1].URL != "http://jira/TEST-102" {
		t.Errorf("unexpected issue 1: %+v", issues[1])
	}
}

func TestGetIssueCommits(t *testing.T) {
	service := newJiraTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/rest/jira/latest/issues/TEST-101/commits" {
			start := r.URL.Query().Get("start")
			if start == "0" || start == "" {
				_, _ = w.Write([]byte(`{
					"size": 1,
					"limit": 1,
					"isLastPage": false,
					"values": [
						{
							"toCommit": {
								"id": "commit1",
								"displayId": "c1",
								"message": "fix commit 1"
							}
						}
					]
				}`))
				return
			} else if start == "1" {
				_, _ = w.Write([]byte(`{
					"size": 1,
					"limit": 1,
					"isLastPage": true,
					"values": [
						{
							"toCommit": {
								"id": "commit2",
								"displayId": "c2",
								"message": "fix commit 2"
							}
						}
					]
				}`))
				return
			}
		}
		http.NotFound(w, r)
	})

	commits, err := service.GetIssueCommits(context.Background(), "TEST-101", 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}

	if *commits[0].Id != "commit1" || *commits[0].DisplayId != "c1" || *commits[0].Message != "fix commit 1" {
		t.Errorf("unexpected commit 0: %+v", commits[0])
	}
	if *commits[1].Id != "commit2" || *commits[1].DisplayId != "c2" || *commits[1].Message != "fix commit 2" {
		t.Errorf("unexpected commit 1: %+v", commits[1])
	}
}

func TestGetPRIssuesError(t *testing.T) {
	service := newJiraTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Unauthorized"))
	})
	_, err := service.GetPRIssues(context.Background(), RepositoryRef{ProjectKey: "PROJ", Slug: "repo"}, "42")
	if err == nil || (!strings.Contains(err.Error(), "401") && !strings.Contains(err.Error(), "authentication")) {
		t.Fatalf("expected authorization error, got: %v", err)
	}
}

func TestGetIssueCommitsLimitDefaults(t *testing.T) {
	service := newJiraTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/rest/jira/latest/issues/TEST-101/commits" {
			limit := r.URL.Query().Get("limit")
			if limit == "25" { // Default limit
				_, _ = w.Write([]byte(`{"isLastPage":true,"values":[]}`))
				return
			}
		}
		http.NotFound(w, r)
	})

	_, err := service.GetIssueCommits(context.Background(), "TEST-101", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetIssueCommitsError(t *testing.T) {
	service := newJiraTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("Internal Server Error"))
	})

	_, err := service.GetIssueCommits(context.Background(), "TEST-101", 5)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("expected 500 error, got: %v", err)
	}
}

func TestGetIssueCommitsTruncates(t *testing.T) {
	service := newJiraTestService(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/rest/jira/latest/issues/TEST-101/commits" {
			_, _ = w.Write([]byte(`{
				"size": 2,
				"limit": 2,
				"isLastPage": true,
				"values": [
					{"toCommit": {"id": "c1", "displayId": "c1", "message": "msg1"}},
					{"toCommit": {"id": "c2", "displayId": "c2", "message": "msg2"}}
				]
			}`))
			return
		}
		http.NotFound(w, r)
	})

	commits, err := service.GetIssueCommits(context.Background(), "TEST-101", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(commits) != 1 {
		t.Fatalf("expected 1 commit (truncated), got %d", len(commits))
	}
}

