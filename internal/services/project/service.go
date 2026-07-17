package project

import (
	"context"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type ListOptions struct {
	Limit int
	Start int
	Name  string
}

type CreateInput struct {
	Key         string
	Name        string
	Description string
}

type UpdateInput struct {
	Name        string
	Description string
}

type PermissionUser struct {
	Name       string `json:"name"`
	Display    string `json:"display_name,omitempty"`
	Permission string `json:"permission,omitempty"`
}

type PermissionGroup struct {
	Name       string `json:"name"`
	Permission string `json:"permission,omitempty"`
}

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func (service *Service) List(ctx context.Context, options ListOptions) ([]openapigenerated.RestProject, error) {
	if options.Limit <= 0 {
		options.Limit = 25
	}

	if options.Start < 0 {
		options.Start = 0
	}
	start := float32(options.Start)
	results := make([]openapigenerated.RestProject, 0)

	for {
		remaining := options.Limit - len(results)
		if remaining <= 0 {
			break
		}

		pageLimit := float32(remaining)
		params := &openapigenerated.GetProjectsParams{Start: &start, Limit: &pageLimit}
		if strings.TrimSpace(options.Name) != "" {
			name := strings.TrimSpace(options.Name)
			params.Name = &name
		}

		response, err := service.client.GetProjectsWithResponse(ctx, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list projects", err)
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

func (service *Service) Get(ctx context.Context, key string) (openapigenerated.RestProject, error) {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return openapigenerated.RestProject{}, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	response, err := service.client.GetProjectWithResponse(ctx, trimmedKey)
	if err != nil {
		return openapigenerated.RestProject{}, apperrors.New(apperrors.KindTransient, "failed to get project", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestProject{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestProject{}, nil
}

func (service *Service) Create(ctx context.Context, input CreateInput) (openapigenerated.RestProject, error) {
	trimmedKey := strings.TrimSpace(input.Key)
	if trimmedKey == "" {
		return openapigenerated.RestProject{}, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedName := strings.TrimSpace(input.Name)
	if trimmedName == "" {
		return openapigenerated.RestProject{}, apperrors.New(apperrors.KindValidation, "project name is required", nil)
	}

	body := openapigenerated.CreateProjectJSONRequestBody{
		Key:  &trimmedKey,
		Name: &trimmedName,
	}
	if trimmedDesc := strings.TrimSpace(input.Description); trimmedDesc != "" {
		body.Description = &trimmedDesc
	}

	response, err := service.client.CreateProjectWithResponse(ctx, body)
	if err != nil {
		return openapigenerated.RestProject{}, apperrors.New(apperrors.KindTransient, "failed to create project", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestProject{}, err
	}

	if response.ApplicationjsonCharsetUTF8201 != nil {
		return *response.ApplicationjsonCharsetUTF8201, nil
	}

	return openapigenerated.RestProject{}, nil
}

func (service *Service) Update(ctx context.Context, key string, input UpdateInput) (openapigenerated.RestProject, error) {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return openapigenerated.RestProject{}, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	body := openapigenerated.UpdateProjectJSONRequestBody{}
	if trimmedName := strings.TrimSpace(input.Name); trimmedName != "" {
		body.Name = &trimmedName
	}
	if trimmedDesc := strings.TrimSpace(input.Description); trimmedDesc != "" {
		body.Description = &trimmedDesc
	}

	response, err := service.client.UpdateProjectWithResponse(ctx, trimmedKey, body)
	if err != nil {
		return openapigenerated.RestProject{}, apperrors.New(apperrors.KindTransient, "failed to update project", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestProject{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestProject{}, nil
}

func (service *Service) Delete(ctx context.Context, key string) error {
	trimmedKey := strings.TrimSpace(key)
	if trimmedKey == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	response, err := service.client.DeleteProjectWithResponse(ctx, trimmedKey)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete project", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}
func (service *Service) ListProjectPermissionUsers(ctx context.Context, projectKey string, limit int) ([]PermissionUser, error) {
	if strings.TrimSpace(projectKey) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	if limit <= 0 {
		limit = 100
	}

	start := float32(0)
	pageLimit := float32(limit)
	results := make([]PermissionUser, 0)

	for {
		response, err := service.client.GetUsersWithAnyPermission1WithResponse(ctx, projectKey, &openapigenerated.GetUsersWithAnyPermission1Params{
			Start: &start,
			Limit: &pageLimit,
		})
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list project user permissions", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}
		if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		for _, value := range *response.ApplicationjsonCharsetUTF8200.Values {
			entry := PermissionUser{}
			if value.User != nil {
				if value.User.Name != nil {
					entry.Name = *value.User.Name
				}
				if value.User.DisplayName != nil {
					entry.Display = *value.User.DisplayName
				}
			}
			if value.Permission != nil {
				entry.Permission = string(*value.Permission)
			}
			results = append(results, entry)
		}

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

func (service *Service) GrantProjectUserPermission(ctx context.Context, projectKey string, username string, permission string) error {
	if strings.TrimSpace(projectKey) == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	trimmedUser := strings.TrimSpace(username)
	if trimmedUser == "" {
		return apperrors.New(apperrors.KindValidation, "username is required", nil)
	}

	normalizedPermission, err := normalizeProjectPermission(permission)
	if err != nil {
		return err
	}

	response, err := service.client.SetPermissionForUsers1WithResponse(ctx, projectKey, &openapigenerated.SetPermissionForUsers1Params{
		Name:       &trimmedUser,
		Permission: &normalizedPermission,
	})
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to grant project user permission", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) RevokeProjectUserPermission(ctx context.Context, projectKey string, username string) error {
	if strings.TrimSpace(projectKey) == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	trimmedUser := strings.TrimSpace(username)
	if trimmedUser == "" {
		return apperrors.New(apperrors.KindValidation, "username is required", nil)
	}

	response, err := service.client.RevokePermissionsForUser1WithResponse(ctx, projectKey, &openapigenerated.RevokePermissionsForUser1Params{
		Name: &trimmedUser,
	})
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to revoke project user permission", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) ListProjectPermissionGroups(ctx context.Context, projectKey string, limit int) ([]PermissionGroup, error) {
	if strings.TrimSpace(projectKey) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	if limit <= 0 {
		limit = 100
	}

	start := float32(0)
	pageLimit := float32(limit)
	results := make([]PermissionGroup, 0)

	for {
		response, err := service.client.GetGroupsWithAnyPermission1WithResponse(ctx, projectKey, &openapigenerated.GetGroupsWithAnyPermission1Params{
			Start: &start,
			Limit: &pageLimit,
		})
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list project group permissions", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}
		if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		for _, value := range *response.ApplicationjsonCharsetUTF8200.Values {
			entry := PermissionGroup{}
			if value.Group != nil {
				if value.Group.Name != nil {
					entry.Name = *value.Group.Name
				}
			}
			if value.Permission != nil {
				entry.Permission = string(*value.Permission)
			}
			results = append(results, entry)
		}

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

func (service *Service) GrantProjectGroupPermission(ctx context.Context, projectKey string, group string, permission string) error {
	if strings.TrimSpace(projectKey) == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	trimmedGroup := strings.TrimSpace(group)
	if trimmedGroup == "" {
		return apperrors.New(apperrors.KindValidation, "group name is required", nil)
	}

	normalizedPermission, err := normalizeProjectPermission(permission)
	if err != nil {
		return err
	}

	response, err := service.client.SetPermissionForGroups1WithResponse(ctx, projectKey, &openapigenerated.SetPermissionForGroups1Params{
		Name:       &trimmedGroup,
		Permission: &normalizedPermission,
	})
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to grant project group permission", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) RevokeProjectGroupPermission(ctx context.Context, projectKey string, group string) error {
	if strings.TrimSpace(projectKey) == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	trimmedGroup := strings.TrimSpace(group)
	if trimmedGroup == "" {
		return apperrors.New(apperrors.KindValidation, "group name is required", nil)
	}

	response, err := service.client.RevokePermissionsForGroup1WithResponse(ctx, projectKey, &openapigenerated.RevokePermissionsForGroup1Params{
		Name: &trimmedGroup,
	})
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to revoke project group permission", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func normalizeProjectPermission(permission string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(permission)) {
	case "PROJECT_READ":
		return "PROJECT_READ", nil
	case "PROJECT_WRITE":
		return "PROJECT_WRITE", nil
	case "PROJECT_ADMIN":
		return "PROJECT_ADMIN", nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "permission must be one of PROJECT_READ, PROJECT_WRITE, PROJECT_ADMIN", nil)
	}
}
