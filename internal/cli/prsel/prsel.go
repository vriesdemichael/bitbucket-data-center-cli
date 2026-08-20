package prsel

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/giturl"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

// Target represents a resolved pull request target including repository and numerical ID.
type Target struct {
	ProjectKey    string `json:"project_key"`
	RepoSlug      string `json:"repo_slug"`
	PullRequestID string `json:"pull_request_id"`
}

// RepositoryRef returns the pullrequestservice.RepositoryRef for the target.
func (t Target) RepositoryRef() pullrequestservice.RepositoryRef {
	return pullrequestservice.RepositoryRef{
		ProjectKey: t.ProjectKey,
		Slug:       t.RepoSlug,
	}
}

// PullRequestLister defines the interface for listing PRs to resolve a branch name to a PR ID.
type PullRequestLister interface {
	List(ctx context.Context, repo pullrequestservice.RepositoryRef, options pullrequestservice.ListOptions) ([]pullrequestservice.PullRequest, error)
}

// Resolve resolves a pull request target argument which can be:
// 1. A full Bitbucket browser/API URL (e.g. https://bitbucket.example.com/projects/PROJ/repos/repo/pull-requests/42)
// 2. A numerical ID or #ID (e.g. 42 or #42)
// 3. A head branch name (e.g. feature/my-branch or refs/heads/feature/my-branch)
func Resolve(
	ctx context.Context,
	targetArg string,
	repoSelector string,
	cfg config.AppConfig,
	lister PullRequestLister,
) (Target, error) {
	trimmed := strings.TrimSpace(targetArg)
	if trimmed == "" {
		return Target{}, apperrors.New(apperrors.KindValidation, "pull request identifier is required (expected ID, branch name, or URL)", nil)
	}

	// 1. Try parsing as a Bitbucket PR URL.
	if _, proj, slug, prID, ok := giturl.ParseBitbucketPR(trimmed); ok {
		return Target{
			ProjectKey:    proj,
			RepoSlug:      slug,
			PullRequestID: prID,
		}, nil
	}

	// 2. Try parsing as numerical ID (with optional leading '#').
	numericCandidate := strings.TrimPrefix(trimmed, "#")
	if _, err := strconv.ParseInt(numericCandidate, 10, 64); err == nil {
		proj, slug, err := reposel.Resolve(repoSelector, cfg)
		if err != nil {
			return Target{}, err
		}
		return Target{
			ProjectKey:    proj,
			RepoSlug:      slug,
			PullRequestID: numericCandidate,
		}, nil
	}

	// 3. Treat as a branch name.
	branchName := strings.TrimPrefix(trimmed, "refs/heads/")
	proj, slug, err := reposel.Resolve(repoSelector, cfg)
	if err != nil {
		return Target{}, err
	}
	repo := pullrequestservice.RepositoryRef{ProjectKey: proj, Slug: slug}

	if lister == nil {
		lister = pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
	}

	// Query open pull requests first
	openPRs, err := lister.List(ctx, repo, pullrequestservice.ListOptions{
		State:        "open",
		SourceBranch: branchName,
		Limit:        10,
	})
	if err != nil {
		return Target{}, err
	}

	for _, pr := range openPRs {
		if strings.EqualFold(strings.TrimSpace(pr.SourceBranch), branchName) ||
			strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(pr.SourceBranch), "refs/heads/"), branchName) {
			return Target{
				ProjectKey:    proj,
				RepoSlug:      slug,
				PullRequestID: strconv.FormatInt(pr.ID, 10),
			}, nil
		}
	}

	if len(openPRs) > 0 {
		return Target{
			ProjectKey:    proj,
			RepoSlug:      slug,
			PullRequestID: strconv.FormatInt(openPRs[0].ID, 10),
		}, nil
	}

	// Fallback: check all pull requests (e.g. merged/closed)
	allPRs, err := lister.List(ctx, repo, pullrequestservice.ListOptions{
		State:        "all",
		SourceBranch: branchName,
		Limit:        10,
	})
	if err != nil {
		return Target{}, err
	}

	for _, pr := range allPRs {
		if strings.EqualFold(strings.TrimSpace(pr.SourceBranch), branchName) ||
			strings.EqualFold(strings.TrimPrefix(strings.TrimSpace(pr.SourceBranch), "refs/heads/"), branchName) {
			return Target{
				ProjectKey:    proj,
				RepoSlug:      slug,
				PullRequestID: strconv.FormatInt(pr.ID, 10),
			}, nil
		}
	}

	if len(allPRs) > 0 {
		return Target{
			ProjectKey:    proj,
			RepoSlug:      slug,
			PullRequestID: strconv.FormatInt(allPRs[0].ID, 10),
		}, nil
	}

	return Target{}, apperrors.New(
		apperrors.KindNotFound,
		fmt.Sprintf("no pull request found for branch %q in %s/%s", branchName, proj, slug),
		nil,
	)
}
