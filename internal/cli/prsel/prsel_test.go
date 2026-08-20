package prsel

import (
	"context"
	"errors"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
)

type mockPRLister struct {
	listFunc func(ctx context.Context, repo pullrequestservice.RepositoryRef, options pullrequestservice.ListOptions) ([]pullrequestservice.PullRequest, error)
}

func (m *mockPRLister) List(ctx context.Context, repo pullrequestservice.RepositoryRef, options pullrequestservice.ListOptions) ([]pullrequestservice.PullRequest, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, repo, options)
	}
	return nil, nil
}

func TestResolve(t *testing.T) {
	ctx := context.Background()
	cfg := config.AppConfig{ProjectKey: "DEFAULT_PROJ"}

	t.Run("empty target returns validation error", func(t *testing.T) {
		_, err := Resolve(ctx, "", "PROJ/repo", cfg, nil)
		if err == nil || !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected validation error, got: %v", err)
		}
	})

	t.Run("numeric target with explicit repo selector", func(t *testing.T) {
		target, err := Resolve(ctx, "42", "MYPROJ/my-repo", cfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.ProjectKey != "MYPROJ" || target.RepoSlug != "my-repo" || target.PullRequestID != "42" {
			t.Fatalf("got %+v, want MYPROJ/my-repo #42", target)
		}
		ref := target.RepositoryRef()
		if ref.ProjectKey != "MYPROJ" || ref.Slug != "my-repo" {
			t.Fatalf("got ref %+v", ref)
		}
	})

	t.Run("numeric target with hash prefix", func(t *testing.T) {
		target, err := Resolve(ctx, "#108", "MYPROJ/my-repo", cfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.PullRequestID != "108" {
			t.Fatalf("got ID %q, want 108", target.PullRequestID)
		}
	})

	t.Run("numeric target without repo selector uses default", func(t *testing.T) {
		t.Setenv("BITBUCKET_REPO_SLUG", "env-repo")
		target, err := Resolve(ctx, "42", "", cfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.ProjectKey != "DEFAULT_PROJ" || target.RepoSlug != "env-repo" || target.PullRequestID != "42" {
			t.Fatalf("got %+v, want DEFAULT_PROJ/env-repo #42", target)
		}
	})

	t.Run("full Bitbucket browser URL", func(t *testing.T) {
		target, err := Resolve(ctx, "https://bitbucket.example.com/projects/URLPROJ/repos/url-repo/pull-requests/99", "", cfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.ProjectKey != "URLPROJ" || target.RepoSlug != "url-repo" || target.PullRequestID != "99" {
			t.Fatalf("got %+v, want URLPROJ/url-repo #99", target)
		}
	})

	t.Run("full Bitbucket user personal repo PR URL", func(t *testing.T) {
		target, err := Resolve(ctx, "https://bitbucket.example.com/users/alice/repos/personal-repo/pull-requests/12/overview", "", cfg, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.ProjectKey != "~alice" || target.RepoSlug != "personal-repo" || target.PullRequestID != "12" {
			t.Fatalf("got %+v, want ~alice/personal-repo #12", target)
		}
	})

	t.Run("branch name matches open PR", func(t *testing.T) {
		lister := &mockPRLister{
			listFunc: func(ctx context.Context, repo pullrequestservice.RepositoryRef, options pullrequestservice.ListOptions) ([]pullrequestservice.PullRequest, error) {
				if options.State == "open" && options.SourceBranch == "feature/x" {
					return []pullrequestservice.PullRequest{
						{ID: 55, SourceBranch: "feature/x"},
					}, nil
				}
				return nil, nil
			},
		}

		target, err := Resolve(ctx, "feature/x", "PROJ/repo", cfg, lister)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.ProjectKey != "PROJ" || target.RepoSlug != "repo" || target.PullRequestID != "55" {
			t.Fatalf("got %+v, want PROJ/repo #55", target)
		}
	})

	t.Run("branch name with refs/heads prefix matches open PR", func(t *testing.T) {
		lister := &mockPRLister{
			listFunc: func(ctx context.Context, repo pullrequestservice.RepositoryRef, options pullrequestservice.ListOptions) ([]pullrequestservice.PullRequest, error) {
				if options.State == "open" && options.SourceBranch == "feature/y" {
					return []pullrequestservice.PullRequest{
						{ID: 77, SourceBranch: "refs/heads/feature/y"},
					}, nil
				}
				return nil, nil
			},
		}

		target, err := Resolve(ctx, "refs/heads/feature/y", "PROJ/repo", cfg, lister)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.PullRequestID != "77" {
			t.Fatalf("got ID %q, want 77", target.PullRequestID)
		}
	})

	t.Run("branch name fallback to closed/merged PR", func(t *testing.T) {
		lister := &mockPRLister{
			listFunc: func(ctx context.Context, repo pullrequestservice.RepositoryRef, options pullrequestservice.ListOptions) ([]pullrequestservice.PullRequest, error) {
				if options.State == "open" {
					return nil, nil
				}
				if options.State == "all" && options.SourceBranch == "feature/old" {
					return []pullrequestservice.PullRequest{
						{ID: 12, SourceBranch: "feature/old", State: "MERGED"},
					}, nil
				}
				return nil, nil
			},
		}

		target, err := Resolve(ctx, "feature/old", "PROJ/repo", cfg, lister)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if target.PullRequestID != "12" {
			t.Fatalf("got ID %q, want 12", target.PullRequestID)
		}
	})

	t.Run("branch name not found returns NotFound error", func(t *testing.T) {
		lister := &mockPRLister{
			listFunc: func(ctx context.Context, repo pullrequestservice.RepositoryRef, options pullrequestservice.ListOptions) ([]pullrequestservice.PullRequest, error) {
				return nil, nil
			},
		}

		_, err := Resolve(ctx, "nonexistent-branch", "PROJ/repo", cfg, lister)
		if err == nil || !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Fatalf("expected NotFound error, got: %v", err)
		}
	})

	t.Run("lister error propagated", func(t *testing.T) {
		lister := &mockPRLister{
			listFunc: func(ctx context.Context, repo pullrequestservice.RepositoryRef, options pullrequestservice.ListOptions) ([]pullrequestservice.PullRequest, error) {
				return nil, errors.New("backend error")
			},
		}

		_, err := Resolve(ctx, "some-branch", "PROJ/repo", cfg, lister)
		if err == nil || err.Error() != "backend error" {
			t.Fatalf("expected backend error, got: %v", err)
		}
	})
}
