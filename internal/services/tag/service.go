package tag

import (
	"context"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

type ListOptions struct {
	MaxResults int
	Start      int
	OrderBy    string
	FilterText string
}

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func (service *Service) List(ctx context.Context, repo RepositoryRef, options ListOptions) ([]openapigenerated.RestTag, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	if options.MaxResults <= 0 {
		options.MaxResults = 25
	}

	return openapi.PageThrough(ctx, options.Start, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestTag], error) {
			startValue, limitValue := float32(start), float32(limit)
			params := &openapigenerated.GetTagsParams{Start: &startValue, Limit: &limitValue}
			if orderBy := strings.TrimSpace(options.OrderBy); orderBy != "" {
				params.OrderBy = &orderBy
			}
			if filterText := strings.TrimSpace(options.FilterText); filterText != "" {
				params.FilterText = &filterText
			}

			response, err := service.client.GetTagsWithResponse(ctx, repo.ProjectKey, repo.Slug, params)
			if err != nil {
				return openapi.Page[openapigenerated.RestTag]{}, apperrors.New(apperrors.KindTransient, "failed to list repository tags", err)
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return openapi.Page[openapigenerated.RestTag]{}, err
			}

			page := response.ApplicationjsonCharsetUTF8200
			if page == nil || page.Values == nil {
				return openapi.Page[openapigenerated.RestTag]{}, nil
			}

			return openapi.Page[openapigenerated.RestTag]{
				Values:        *page.Values,
				IsLastPage:    page.IsLastPage,
				NextPageStart: page.NextPageStart,
			}, nil
		})
}

func (service *Service) Create(ctx context.Context, repo RepositoryRef, name string, startPoint string, message string) (openapigenerated.RestTag, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestTag{}, err
	}

	trimmedName := strings.TrimSpace(name)
	trimmedStartPoint := strings.TrimSpace(startPoint)
	if trimmedName == "" {
		return openapigenerated.RestTag{}, apperrors.New(apperrors.KindValidation, "tag name is required", nil)
	}
	if trimmedStartPoint == "" {
		return openapigenerated.RestTag{}, apperrors.New(apperrors.KindValidation, "tag start-point is required", nil)
	}

	body := openapigenerated.CreateTagForRepositoryJSONRequestBody{
		Name:       &trimmedName,
		StartPoint: &trimmedStartPoint,
	}
	if strings.TrimSpace(message) != "" {
		trimmedMessage := strings.TrimSpace(message)
		body.Message = &trimmedMessage
	}

	response, err := service.client.CreateTagForRepositoryWithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return openapigenerated.RestTag{}, apperrors.New(apperrors.KindTransient, "failed to create repository tag", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestTag{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestTag{}, nil
}

func (service *Service) Get(ctx context.Context, repo RepositoryRef, name string) (openapigenerated.RestTag, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestTag{}, err
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return openapigenerated.RestTag{}, apperrors.New(apperrors.KindValidation, "tag name is required", nil)
	}

	response, err := service.client.GetTagWithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedName)
	if err != nil {
		return openapigenerated.RestTag{}, apperrors.New(apperrors.KindTransient, "failed to get repository tag", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestTag{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestTag{}, nil
}

func (service *Service) Delete(ctx context.Context, repo RepositoryRef, name string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return apperrors.New(apperrors.KindValidation, "tag name is required", nil)
	}

	response, err := service.client.DeleteTagWithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedName)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete repository tag", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func validateRepositoryRef(repo RepositoryRef) error {
	return openapi.ValidateRepository(repo.ProjectKey, repo.Slug)
}
