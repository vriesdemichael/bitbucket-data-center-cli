package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	repositoryservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
)

// SearchRepositoriesInput is the argument set for search_repositories.
type SearchRepositoriesInput struct {
	Name    string `json:"name,omitempty" jsonschema:"Repository name filter (substring match)"`
	Project string `json:"project,omitempty" jsonschema:"Restrict results to this project key"`
	Limit   int    `json:"limit,omitempty" jsonschema:"Maximum number of results (default 25)"`
}

// SearchRepositoriesOutput names the collection it holds.
type SearchRepositoriesOutput struct {
	Repositories []repositoryservice.Repository `json:"repositories"`
}

func specSearchRepositories() Spec {
	tool := &mcp.Tool{
		Name:        "search_repositories",
		Description: "Search for repositories by name, optionally filtered by project. Returns project key, slug, and display name.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[SearchRepositoriesInput, SearchRepositoriesOutput] {
		svc := repositoryservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in SearchRepositoriesInput) (*mcp.CallToolResult, SearchRepositoriesOutput, error) {
			opts := repositoryservice.ListOptions{
				Name:       in.Name,
				MaxResults: limitOrDefault(in.Limit),
			}
			project := strings.TrimSpace(in.Project)
			var repos []repositoryservice.Repository
			var err error
			if project != "" {
				repos, err = svc.ListByProject(ctx, project, opts)
			} else {
				repos, err = svc.List(ctx, opts)
			}
			if err != nil {
				return nil, SearchRepositoriesOutput{}, fmt.Errorf("search_repositories failed: %w", err)
			}
			return nil, SearchRepositoriesOutput{Repositories: repos}, nil
		}
	})
}

// GetRepositoryCloneInfoInput is the argument set for get_repository_clone_info.
type GetRepositoryCloneInfoInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
}

// CloneInfo is the clone URL payload for get_repository_clone_info.
type CloneInfo struct {
	ProjectKey    string `json:"project_key"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	CloneURLHTTPS string `json:"clone_url_https"`
	CloneURLSSH   string `json:"clone_url_ssh"`
}

// GetRepositoryCloneInfoOutput names the payload it holds.
type GetRepositoryCloneInfoOutput struct {
	Repository CloneInfo `json:"repository"`
}

func specGetRepositoryCloneInfo() Spec {
	tool := &mcp.Tool{
		Name:        "get_repository_clone_info",
		Description: "Get HTTPS and SSH clone URLs for a repository. Use these URLs with git clone to check out the repository locally.",
		Annotations: readOnly(),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[GetRepositoryCloneInfoInput, GetRepositoryCloneInfoOutput] {
		svc := repositoryservice.NewService(c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in GetRepositoryCloneInfoInput) (*mcp.CallToolResult, GetRepositoryCloneInfoOutput, error) {
			httpsURL, sshURL, err := buildCloneURLs(c.BaseURL, in.Project, in.Repo)
			if err != nil {
				return nil, GetRepositoryCloneInfoOutput{}, fmt.Errorf("get_repository_clone_info failed: %w", err)
			}

			// Look up display name; tolerate failure.
			repos, _ := svc.ListByProject(ctx, in.Project, repositoryservice.ListOptions{Name: in.Repo, MaxResults: 5})
			name := in.Repo
			for _, r := range repos {
				if strings.EqualFold(r.Slug, in.Repo) {
					name = r.Name
					break
				}
			}

			return nil, GetRepositoryCloneInfoOutput{Repository: CloneInfo{
				ProjectKey:    in.Project,
				Slug:          in.Repo,
				Name:          name,
				CloneURLHTTPS: httpsURL,
				CloneURLSSH:   sshURL,
			}}, nil
		}
	})
}

// buildCloneURLs derives HTTPS and SSH clone URLs from the Bitbucket base URL.
func buildCloneURLs(baseURL, project, repo string) (httpsURL, sshURL string, err error) {
	parsed, parseErr := url.Parse(baseURL)
	if parseErr != nil {
		return "", "", fmt.Errorf("invalid base URL %q: %w", baseURL, parseErr)
	}

	lowerProject := strings.ToLower(project)
	lowerRepo := strings.ToLower(repo)

	httpsURL = fmt.Sprintf("%s://%s%s/scm/%s/%s.git",
		parsed.Scheme,
		parsed.Host,
		strings.TrimRight(parsed.Path, "/"),
		url.PathEscape(lowerProject),
		url.PathEscape(lowerRepo),
	)
	sshURL = fmt.Sprintf("git@%s:scm/%s/%s.git",
		parsed.Host,
		url.PathEscape(lowerProject),
		url.PathEscape(lowerRepo),
	)

	return httpsURL, sshURL, nil
}
