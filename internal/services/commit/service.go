package commit

import (
	"context"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

type ListOptions struct {
	Limit  int
	Start  int
	Path   string
	Since  string
	Until  string
	Merges string
}

type CompareOptions struct {
	From  string
	To    string
	Limit int
}

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func (service *Service) List(ctx context.Context, repo RepositoryRef, options ListOptions) ([]openapigenerated.RestCommit, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	if options.Limit <= 0 {
		options.Limit = 25
	}

	if options.Start < 0 {
		options.Start = 0
	}
	start := float32(options.Start)
	results := make([]openapigenerated.RestCommit, 0)

	for {
		remaining := options.Limit - len(results)
		if remaining <= 0 {
			break
		}

		pageLimit := float32(remaining)
		params := &openapigenerated.GetCommitsParams{Start: &start, Limit: &pageLimit}
		if strings.TrimSpace(options.Path) != "" {
			path := strings.TrimSpace(options.Path)
			params.Path = &path
		}
		if strings.TrimSpace(options.Since) != "" {
			since := strings.TrimSpace(options.Since)
			params.Since = &since
		}
		if strings.TrimSpace(options.Until) != "" {
			until := strings.TrimSpace(options.Until)
			params.Until = &until
		}
		if strings.TrimSpace(options.Merges) != "" {
			merges := strings.TrimSpace(options.Merges)
			params.Merges = &merges
		}

		response, err := service.client.GetCommitsWithResponse(ctx, repo.ProjectKey, repo.Slug, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list repository commits", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}
		if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		results = append(results, (*response.ApplicationjsonCharsetUTF8200.Values)...)

		if len(results) >= options.Limit {
			break
		}
		if response.ApplicationjsonCharsetUTF8200.IsLastPage != nil && *response.ApplicationjsonCharsetUTF8200.IsLastPage {
			break
		}
		if response.ApplicationjsonCharsetUTF8200.NextPageStart == nil {
			break
		}

		start = float32(*response.ApplicationjsonCharsetUTF8200.NextPageStart)
	}

	if len(results) > options.Limit {
		results = results[:options.Limit]
	}
	return results, nil
}

func (service *Service) Get(ctx context.Context, repo RepositoryRef, commitID string) (openapigenerated.RestCommit, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestCommit{}, err
	}

	trimmedID := strings.TrimSpace(commitID)
	if trimmedID == "" {
		return openapigenerated.RestCommit{}, apperrors.New(apperrors.KindValidation, "commit id is required", nil)
	}

	response, err := service.client.GetCommitWithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID, nil)
	if err != nil {
		return openapigenerated.RestCommit{}, apperrors.New(apperrors.KindTransient, "failed to get repository commit", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestCommit{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestCommit{}, nil
}

func (service *Service) Compare(ctx context.Context, repo RepositoryRef, options CompareOptions) ([]openapigenerated.RestCommit, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	from := strings.TrimSpace(options.From)
	to := strings.TrimSpace(options.To)

	if from == "" || to == "" {
		return nil, apperrors.New(apperrors.KindValidation, "compare from and to refs are required", nil)
	}

	if options.Limit <= 0 {
		options.Limit = 25
	}

	start := float32(0)
	pageLimit := float32(options.Limit)
	results := make([]openapigenerated.RestCommit, 0)

	for {
		params := &openapigenerated.StreamCommitsParams{
			From:  &from,
			To:    &to,
			Start: &start,
			Limit: &pageLimit,
		}

		response, err := service.client.StreamCommitsWithResponse(ctx, repo.ProjectKey, repo.Slug, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to compare commits", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}

		if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		pageCommits := *response.ApplicationjsonCharsetUTF8200.Values
		results = append(results, pageCommits...)

		if response.ApplicationjsonCharsetUTF8200.IsLastPage != nil && *response.ApplicationjsonCharsetUTF8200.IsLastPage {
			break
		}
		if response.ApplicationjsonCharsetUTF8200.NextPageStart == nil {
			break
		}

		start = float32(*response.ApplicationjsonCharsetUTF8200.NextPageStart)
	}

	return results, nil
}

func (service *Service) ListTagsAndBranches(ctx context.Context, repo RepositoryRef, query string) ([]openapigenerated.RestMinimalRef, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	trimmedQuery := strings.TrimSpace(query)
	filter := &trimmedQuery
	if trimmedQuery == "" {
		filter = nil
	}

	refs := make([]openapigenerated.RestMinimalRef, 0)
	limit := float32(50)

	branchParams := &openapigenerated.GetBranchesParams{Limit: &limit, FilterText: filter}
	branchResp, err := service.client.GetBranchesWithResponse(ctx, repo.ProjectKey, repo.Slug, branchParams)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to query branches for refs", err)
	}
	if err := openapi.MapStatusError(branchResp.StatusCode(), branchResp.Body); err != nil {
		return nil, err
	}
	if branchResp.ApplicationjsonCharsetUTF8200 != nil && branchResp.ApplicationjsonCharsetUTF8200.Values != nil {
		for _, branch := range *branchResp.ApplicationjsonCharsetUTF8200.Values {
			var refType *openapigenerated.RestMinimalRefType
			if branch.Type != nil {
				if typeStr, ok := (*branch.Type).(string); ok {
					t := openapigenerated.RestMinimalRefType(typeStr)
					refType = &t
				}
			}
			refs = append(refs, openapigenerated.RestMinimalRef{
				Id:        branch.Id,
				DisplayId: branch.DisplayId,
				Type:      refType,
			})
		}
	}

	tagParams := &openapigenerated.GetTagsParams{Limit: &limit, FilterText: filter}
	tagResp, err := service.client.GetTagsWithResponse(ctx, repo.ProjectKey, repo.Slug, tagParams)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to query tags for refs", err)
	}
	if err := openapi.MapStatusError(tagResp.StatusCode(), tagResp.Body); err != nil {
		return nil, err
	}
	if tagResp.ApplicationjsonCharsetUTF8200 != nil && tagResp.ApplicationjsonCharsetUTF8200.Values != nil {
		for _, tag := range *tagResp.ApplicationjsonCharsetUTF8200.Values {
			var refType *openapigenerated.RestMinimalRefType
			if tag.Type != nil {
				t := openapigenerated.RestMinimalRefType(string(*tag.Type))
				refType = &t
			}
			refs = append(refs, openapigenerated.RestMinimalRef{
				Id:        tag.Id,
				DisplayId: tag.DisplayId,
				Type:      refType,
			})
		}
	}

	return refs, nil
}

func validateRepositoryRef(repo RepositoryRef) error {
	if strings.TrimSpace(repo.ProjectKey) == "" || strings.TrimSpace(repo.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}
	return nil
}
