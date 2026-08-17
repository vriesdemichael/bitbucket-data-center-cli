package mcp

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	mcpgo "github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	repositoryservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
)

func specSearchRepositories() Spec {
	tool := mcpgo.NewTool("search_repositories",
		mcpgo.WithDescription("Search for repositories by name, optionally filtered by project. Returns project key, slug, and display name."),
		mcpgo.WithString("name", mcpgo.Description("Repository name filter (substring match)")),
		mcpgo.WithString("project", mcpgo.Description("Restrict results to this project key")),
		mcpgo.WithNumber("limit", mcpgo.Description("Maximum number of results (default 25)")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := repositoryservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			opts := repositoryservice.ListOptions{
				Name:  req.GetString("name", ""),
				Limit: req.GetInt("limit", 25),
			}
			project := strings.TrimSpace(req.GetString("project", ""))
			var repos []repositoryservice.Repository
			var err error
			if project != "" {
				repos, err = svc.ListByProject(ctx, project, opts)
			} else {
				repos, err = svc.List(ctx, opts)
			}
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("search_repositories failed", err), nil
			}
			return resultJSON(repos)
		}
	}}
}

// cloneInfo is the result type for get_repository_clone_info.
type cloneInfo struct {
	ProjectKey    string `json:"project_key"`
	Slug          string `json:"slug"`
	Name          string `json:"name"`
	CloneURLHTTPS string `json:"clone_url_https"`
	CloneURLSSH   string `json:"clone_url_ssh"`
}

func specGetRepositoryCloneInfo() Spec {
	tool := mcpgo.NewTool("get_repository_clone_info",
		mcpgo.WithDescription("Get HTTPS and SSH clone URLs for a repository. Use these URLs with git clone to check out the repository locally."),
		mcpgo.WithString("project", mcpgo.Required(), mcpgo.Description("Bitbucket project key")),
		mcpgo.WithString("repo", mcpgo.Required(), mcpgo.Description("Repository slug")),
	)
	return Spec{Tool: tool, Handler: func(c Clients) server.ToolHandlerFunc {
		svc := repositoryservice.NewService(c.HTTP)
		return func(ctx context.Context, req mcpgo.CallToolRequest) (*mcpgo.CallToolResult, error) {
			project, _ := req.RequireString("project")
			repo, _ := req.RequireString("repo")

			httpsURL, sshURL, err := buildCloneURLs(c.BaseURL, project, repo)
			if err != nil {
				return mcpgo.NewToolResultErrorFromErr("get_repository_clone_info failed", err), nil
			}

			// Look up display name; tolerate failure.
			repos, _ := svc.ListByProject(ctx, project, repositoryservice.ListOptions{Name: repo, Limit: 5})
			name := repo
			for _, r := range repos {
				if strings.EqualFold(r.Slug, repo) {
					name = r.Name
					break
				}
			}

			return resultJSON(cloneInfo{
				ProjectKey:    project,
				Slug:          repo,
				Name:          name,
				CloneURLHTTPS: httpsURL,
				CloneURLSSH:   sshURL,
			})
		}
	}}
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
