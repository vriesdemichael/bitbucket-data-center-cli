package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

type Repository struct {
	ProjectKey string `json:"project_key"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Public     bool   `json:"public"`
}

type Service struct {
	client *httpclient.Client
}

func NewService(client *httpclient.Client) *Service {
	return &Service{client: client}
}

// AllResults asks for every repository rather than a page of them. Callers
// that need a complete set -- bulk planning, an existence check -- must say so,
// because the zero value is a default page rather than everything (#468).
const AllResults = 1_000_000

type ListOptions struct {
	// MaxResults caps the total number of repositories returned across all
	// pages. It is not forwarded as a caller-controlled Bitbucket page size,
	// because `bb repo list --limit` is defined as a maximum result count at
	// the CLI layer. Zero means the default page, not everything: pass
	// AllResults for that.
	MaxResults  int
	Start       int
	Name        string
	ProjectName string
}

func (service *Service) List(ctx context.Context, opts ListOptions) ([]Repository, error) {
	return service.listPaged(ctx, "/rest/api/1.0/repos", opts)
}

func (service *Service) ListByProject(ctx context.Context, projectKey string, opts ListOptions) ([]Repository, error) {
	if projectKey == "" {
		return nil, fmt.Errorf("project key is required")
	}

	return service.listPaged(ctx, "/rest/api/1.0/projects/"+projectKey+"/repos", opts)
}

const defaultPageSize = 25

func (service *Service) listPaged(ctx context.Context, path string, opts ListOptions) ([]Repository, error) {
	if opts.MaxResults <= 0 {
		opts.MaxResults = defaultPageSize
	}

	results := []Repository{}
	start := opts.Start

	for {
		remaining := opts.MaxResults - len(results)
		if remaining <= 0 {
			break
		}

		pageSize := defaultPageSize
		if remaining < pageSize {
			pageSize = remaining
		}

		var response pagedRepoResponse

		queryParams := map[string]string{
			"limit": strconv.Itoa(pageSize),
			"start": strconv.Itoa(start),
		}
		if opts.Name != "" {
			queryParams["name"] = opts.Name
		}
		if opts.ProjectName != "" {
			queryParams["projectname"] = opts.ProjectName
		}

		err := service.client.GetJSON(
			ctx,
			path,
			queryParams,
			&response,
		)
		if err != nil {
			return nil, err
		}

		for _, value := range response.Values {
			results = append(results, Repository{
				ProjectKey: value.Project.Key,
				Slug:       value.Slug,
				Name:       value.Name,
				Public:     value.Public,
			})
			if len(results) >= opts.MaxResults {
				return results, nil
			}
		}

		if response.IsLastPage {
			break
		}

		start = response.NextPageStart
	}

	return results, nil
}

type pagedRepoResponse struct {
	Values        []repoValue `json:"values"`
	IsLastPage    bool        `json:"isLastPage"`
	NextPageStart int         `json:"nextPageStart"`
}

type repoValue struct {
	Slug    string      `json:"slug"`
	Name    string      `json:"name"`
	Public  bool        `json:"public"`
	Project projectInfo `json:"project"`
}

type projectInfo struct {
	Key string `json:"key"`
}
