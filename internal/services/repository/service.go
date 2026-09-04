package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"

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

	return openapi.PageThrough(ctx, opts.Start, opts.MaxResults,
		func(ctx context.Context, start, remaining int) (openapi.Page[Repository], error) {
			// The window stays at defaultPageSize unless less than that is
			// still wanted, which is what this loop did before the cap moved
			// into the shared one.
			pageSize := defaultPageSize
			if remaining < pageSize {
				pageSize = remaining
			}

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

			var response pagedRepoResponse
			if err := service.client.GetJSON(ctx, path, queryParams, &response); err != nil {
				return openapi.Page[Repository]{}, err
			}

			repositories := make([]Repository, 0, len(response.Values))
			for _, value := range response.Values {
				repositories = append(repositories, Repository{
					ProjectKey: value.Project.Key,
					Slug:       value.Slug,
					Name:       value.Name,
					Public:     value.Public,
				})
			}

			// isLastPage is a plain bool here and the next start is a plain int,
			// so both are adapted. A zero next start with more to come would be
			// refused by the loop as a non-advancing offset, which is the right
			// answer: it is the shape of a server repeating itself.
			isLastPage := response.IsLastPage
			page := openapi.Page[Repository]{Values: repositories, IsLastPage: &isLastPage}
			if !isLastPage {
				next := int32(response.NextPageStart)
				page.NextPageStart = &next
			}

			return page, nil
		})
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
