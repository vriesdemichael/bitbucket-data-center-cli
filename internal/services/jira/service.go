package jira

import (
	"context"
	"fmt"
	"strconv"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
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
	MaxResults int          `json:"limit"`
	IsLastPage bool         `json:"isLastPage"`
	Values     []JiraCommit `json:"values"`
}

type JiraCommit struct {
	ToCommit openapigenerated.RestCommit `json:"toCommit"`
}

// GetIssueCommits retrieves commits associated with a Jira issue key.
// Endpoint: GET /rest/jira/latest/issues/{issueKey}/commits
func (s *Service) GetIssueCommits(ctx context.Context, issueKey string, maxResults int) ([]openapigenerated.RestCommit, error) {
	if maxResults <= 0 {
		maxResults = 25
	}
	path := fmt.Sprintf("/rest/jira/latest/issues/%s/commits", issueKey)

	return openapi.PageThrough(ctx, 0, maxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestCommit], error) {
			query := map[string]string{
				"start": strconv.Itoa(start),
				"limit": strconv.Itoa(limit),
			}

			var response jiraCommitsResponse
			if err := s.client.GetJSON(ctx, path, query, &response); err != nil {
				return openapi.Page[openapigenerated.RestCommit]{}, err
			}

			commits := make([]openapigenerated.RestCommit, 0, len(response.Values))
			for _, value := range response.Values {
				commits = append(commits, value.ToCommit)
			}

			// This endpoint sends no nextPageStart, so the offset is counted
			// here as the old loop counted it. An empty page therefore points
			// at the offset it was served from, which is what PageThrough reads
			// as the end.
			isLastPage := response.IsLastPage
			next := int32(start + len(response.Values))

			return openapi.Page[openapigenerated.RestCommit]{
				Values:        commits,
				IsLastPage:    &isLastPage,
				NextPageStart: &next,
			}, nil
		})
}
