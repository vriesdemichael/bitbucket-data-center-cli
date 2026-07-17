package jira

import (
	"context"
	"fmt"
	"strconv"

	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

type JiraIssue struct {
	Key string `json:"key"`
	URL string `json:"url"`
}

type Service struct {
	client *httpclient.Client
}

func NewService(client *httpclient.Client) *Service {
	return &Service{client: client}
}

// GetPRIssues retrieves Jira issues associated with a pull request.
// Endpoint: GET /rest/jira/latest/projects/{projectKey}/repos/{repositorySlug}/pull-requests/{pullRequestId}/issues
func (s *Service) GetPRIssues(ctx context.Context, repo RepositoryRef, prID string) ([]JiraIssue, error) {
	path := fmt.Sprintf("/rest/jira/latest/projects/%s/repos/%s/pull-requests/%s/issues", repo.ProjectKey, repo.Slug, prID)
	var issues []JiraIssue
	err := s.client.GetJSON(ctx, path, nil, &issues)
	if err != nil {
		return nil, err
	}
	return issues, nil
}

type jiraCommitsResponse struct {
	Size       int          `json:"size"`
	Limit      int          `json:"limit"`
	IsLastPage bool         `json:"isLastPage"`
	Values     []JiraCommit `json:"values"`
}

type JiraCommit struct {
	ToCommit openapigenerated.RestCommit `json:"toCommit"`
}

// GetIssueCommits retrieves commits associated with a Jira issue key.
// Endpoint: GET /rest/jira/latest/issues/{issueKey}/commits
func (s *Service) GetIssueCommits(ctx context.Context, issueKey string, limit int) ([]openapigenerated.RestCommit, error) {
	if limit <= 0 {
		limit = 25
	}
	path := fmt.Sprintf("/rest/jira/latest/issues/%s/commits", issueKey)
	start := 0
	var allCommits []openapigenerated.RestCommit

	for {
		query := map[string]string{
			"start": strconv.Itoa(start),
			"limit": strconv.Itoa(limit),
		}

		var response jiraCommitsResponse
		err := s.client.GetJSON(ctx, path, query, &response)
		if err != nil {
			return nil, err
		}

		for _, v := range response.Values {
			allCommits = append(allCommits, v.ToCommit)
		}

		if len(allCommits) >= limit || response.IsLastPage || len(response.Values) == 0 {
			break
		}

		start += len(response.Values)
	}

	if len(allCommits) > limit {
		allCommits = allCommits[:limit]
	}

	return allCommits, nil
}
