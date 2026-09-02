package repository

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

type CreateInput struct {
	Name          string
	Description   string
	Forkable      bool
	DefaultBranch string
}

type ForkInput struct {
	Name    string
	Project string
}

type UpdateInput struct {
	Name          string
	Description   string
	DefaultBranch string
}

type AdminService struct {
	client *openapigenerated.ClientWithResponses
}

func NewAdminService(client *openapigenerated.ClientWithResponses) *AdminService {
	return &AdminService{client: client}
}

func (service *AdminService) Create(ctx context.Context, projectKey string, input CreateInput) (openapigenerated.RestRepository, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return openapigenerated.RestRepository{}, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedName := strings.TrimSpace(input.Name)
	if trimmedName == "" {
		return openapigenerated.RestRepository{}, apperrors.New(apperrors.KindValidation, "repository name is required", nil)
	}

	scmId := "git"
	body := openapigenerated.CreateRepositoryJSONRequestBody{
		Name:     &trimmedName,
		ScmId:    &scmId,
		Forkable: &input.Forkable,
	}

	if trimmedDesc := strings.TrimSpace(input.Description); trimmedDesc != "" {
		body.Description = &trimmedDesc
	}
	if trimmedBranch := strings.TrimSpace(input.DefaultBranch); trimmedBranch != "" {
		body.DefaultBranch = &trimmedBranch
	}

	response, err := service.client.CreateRepositoryWithResponse(ctx, trimmedProject, body)
	if err != nil {
		return openapigenerated.RestRepository{}, apperrors.New(apperrors.KindTransient, "failed to create repository", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestRepository{}, err
	}

	if response.ApplicationjsonCharsetUTF8201 != nil {
		return *response.ApplicationjsonCharsetUTF8201, nil
	}

	return openapigenerated.RestRepository{}, nil
}

func (service *AdminService) Fork(ctx context.Context, repo RepositoryRef, input ForkInput) (openapigenerated.RestRepository, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestRepository{}, err
	}

	body := openapigenerated.ForkRepositoryJSONRequestBody{}
	if trimmedName := strings.TrimSpace(input.Name); trimmedName != "" {
		body.Name = &trimmedName
	}

	if trimmedProject := strings.TrimSpace(input.Project); trimmedProject != "" {
		body.Project = &struct {
			Avatar      *string                                     `json:"avatar,omitempty"`
			AvatarUrl   *string                                     `json:"avatarUrl,omitempty"`
			Description *string                                     `json:"description,omitempty"`
			Id          *int32                                      `json:"id,omitempty"`
			Key         string                                      `json:"key"`
			Links       *map[string]interface{}                     `json:"links,omitempty"`
			Name        *string                                     `json:"name,omitempty"`
			Public      *bool                                       `json:"public,omitempty"`
			Scope       *string                                     `json:"scope,omitempty"`
			Type        *openapigenerated.RestRepositoryProjectType `json:"type,omitempty"`
		}{Key: trimmedProject}
	}

	response, err := service.client.ForkRepositoryWithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return openapigenerated.RestRepository{}, apperrors.New(apperrors.KindTransient, "failed to fork repository", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestRepository{}, err
	}

	if response.ApplicationjsonCharsetUTF8201 != nil {
		return *response.ApplicationjsonCharsetUTF8201, nil
	}

	return openapigenerated.RestRepository{}, nil
}

func (service *AdminService) Delete(ctx context.Context, repo RepositoryRef) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}

	response, err := service.client.DeleteRepositoryWithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete repository", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *AdminService) Update(ctx context.Context, repo RepositoryRef, input UpdateInput) (openapigenerated.RestRepository, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestRepository{}, err
	}

	body := openapigenerated.UpdateRepositoryJSONRequestBody{}
	if trimmedName := strings.TrimSpace(input.Name); trimmedName != "" {
		body.Name = &trimmedName
	}
	if trimmedDesc := strings.TrimSpace(input.Description); trimmedDesc != "" {
		body.Description = &trimmedDesc
	}
	if trimmedBranch := strings.TrimSpace(input.DefaultBranch); trimmedBranch != "" {
		body.DefaultBranch = &trimmedBranch
	}

	response, err := service.client.UpdateRepositoryWithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return openapigenerated.RestRepository{}, apperrors.New(apperrors.KindTransient, "failed to update repository", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestRepository{}, err
	}

	if response.ApplicationjsonCharsetUTF8201 != nil {
		return *response.ApplicationjsonCharsetUTF8201, nil
	}

	return openapigenerated.RestRepository{}, nil
}

func validateRepositoryRef(repo RepositoryRef) error {
	if strings.TrimSpace(repo.ProjectKey) == "" || strings.TrimSpace(repo.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}
	return nil
}
