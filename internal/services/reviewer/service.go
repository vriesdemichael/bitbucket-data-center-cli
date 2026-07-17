package reviewer

import (
	"context"
	"encoding/json"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func (service *Service) ListProjectConditions(ctx context.Context, projectKey string) ([]openapigenerated.RestPullRequestCondition, error) {
	if strings.TrimSpace(projectKey) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	response, err := service.client.GetPullRequestConditionsWithResponse(ctx, projectKey)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list project reviewer conditions", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	if response.JSON200 == nil {
		return []openapigenerated.RestPullRequestCondition{}, nil
	}

	return *response.JSON200, nil
}

func (service *Service) ListRepositoryConditions(ctx context.Context, projectKey, repositorySlug string) ([]openapigenerated.RestPullRequestCondition, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key and repository slug are required", nil)
	}

	response, err := service.client.GetPullRequestConditions1WithResponse(ctx, projectKey, repositorySlug)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list repository reviewer conditions", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	if response.JSON200 == nil {
		return []openapigenerated.RestPullRequestCondition{}, nil
	}

	return *response.JSON200, nil
}

func (service *Service) DeleteProjectCondition(ctx context.Context, projectKey string, conditionID string) error {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(conditionID) == "" {
		return apperrors.New(apperrors.KindValidation, "project key and condition ID are required", nil)
	}

	response, err := service.client.DeletePullRequestConditionWithResponse(ctx, projectKey, conditionID)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete project reviewer condition", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) DeleteRepositoryCondition(ctx context.Context, projectKey, repositorySlug string, conditionID string) error {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" || strings.TrimSpace(conditionID) == "" {
		return apperrors.New(apperrors.KindValidation, "project key, repository slug, and condition ID are required", nil)
	}

	id, err := strconv.ParseInt(conditionID, 10, 32)
	if err != nil {
		return apperrors.New(apperrors.KindValidation, "condition ID must be an integer", err)
	}

	response, err := service.client.DeletePullRequestCondition1WithResponse(ctx, projectKey, repositorySlug, int32(id))
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete repository reviewer condition", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) CreateProjectCondition(ctx context.Context, projectKey string, condition openapigenerated.RestDefaultReviewersRequest) (openapigenerated.RestPullRequestCondition, error) {
	if strings.TrimSpace(projectKey) == "" {
		return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	response, err := service.client.CreatePullRequestConditionWithResponse(ctx, projectKey, condition)
	if err != nil {
		return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindTransient, "failed to create project reviewer condition", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestPullRequestCondition{}, err
	}

	if response.JSON200 != nil {
		return *response.JSON200, nil
	}

	if response.StatusCode() == 201 {
		var created openapigenerated.RestPullRequestCondition
		if err := json.Unmarshal(response.Body, &created); err != nil {
			return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindPermanent, "failed to decode created condition", err)
		}
		return created, nil
	}

	return openapigenerated.RestPullRequestCondition{}, nil
}

func (service *Service) CreateRepositoryCondition(ctx context.Context, projectKey, repositorySlug string, condition openapigenerated.RestDefaultReviewersRequest) (openapigenerated.RestPullRequestCondition, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" {
		return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindValidation, "project key and repository slug are required", nil)
	}

	response, err := service.client.CreatePullRequestCondition1WithResponse(ctx, projectKey, repositorySlug, condition)
	if err != nil {
		return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindTransient, "failed to create repository reviewer condition", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestPullRequestCondition{}, err
	}

	if response.JSON200 != nil {
		return *response.JSON200, nil
	}

	if response.StatusCode() == 201 {
		var created openapigenerated.RestPullRequestCondition
		if err := json.Unmarshal(response.Body, &created); err != nil {
			return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindPermanent, "failed to decode created condition", err)
		}
		return created, nil
	}

	return openapigenerated.RestPullRequestCondition{}, nil
}

func (service *Service) UpdateProjectCondition(ctx context.Context, projectKey string, conditionID string, condition openapigenerated.UpdatePullRequestConditionJSONRequestBody) (openapigenerated.RestPullRequestCondition, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(conditionID) == "" {
		return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindValidation, "project key and condition ID are required", nil)
	}

	response, err := service.client.UpdatePullRequestConditionWithResponse(ctx, projectKey, conditionID, condition)
	if err != nil {
		return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindTransient, "failed to update project reviewer condition", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestPullRequestCondition{}, err
	}

	if response.JSON200 != nil {
		return *response.JSON200, nil
	}

	return openapigenerated.RestPullRequestCondition{}, nil
}

func (service *Service) UpdateRepositoryCondition(ctx context.Context, projectKey, repositorySlug string, conditionID string, condition openapigenerated.UpdatePullRequestCondition1JSONRequestBody) (openapigenerated.RestPullRequestCondition, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" || strings.TrimSpace(conditionID) == "" {
		return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindValidation, "project key, repository slug, and condition ID are required", nil)
	}

	response, err := service.client.UpdatePullRequestCondition1WithResponse(ctx, projectKey, repositorySlug, conditionID, condition)
	if err != nil {
		return openapigenerated.RestPullRequestCondition{}, apperrors.New(apperrors.KindTransient, "failed to update repository reviewer condition", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestPullRequestCondition{}, err
	}

	if response.JSON200 != nil {
		return *response.JSON200, nil
	}

	return openapigenerated.RestPullRequestCondition{}, nil
}

func (service *Service) ListRepositoryReviewerGroups(ctx context.Context, projectKey, repositorySlug string) ([]openapigenerated.RestReviewerGroup, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key and repository slug are required", nil)
	}

	response, err := service.client.GetReviewerGroups1WithResponse(ctx, projectKey, repositorySlug, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list repository reviewer groups", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil && response.ApplicationjsonCharsetUTF8200.Values != nil {
		return *response.ApplicationjsonCharsetUTF8200.Values, nil
	}

	return []openapigenerated.RestReviewerGroup{}, nil
}

func (service *Service) CreateRepositoryReviewerGroup(ctx context.Context, projectKey, repositorySlug string, name, description string) (openapigenerated.RestReviewerGroup, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" || strings.TrimSpace(name) == "" {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindValidation, "project key, repository slug, and name are required", nil)
	}

	body := openapigenerated.RestReviewerGroup{
		Name: &name,
	}
	if description != "" {
		body.Description = &description
	}

	response, err := service.client.Create2WithResponse(ctx, projectKey, repositorySlug, body)
	if err != nil {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindTransient, "failed to create repository reviewer group", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestReviewerGroup{}, err
	}

	if response.ApplicationjsonCharsetUTF8201 != nil {
		return *response.ApplicationjsonCharsetUTF8201, nil
	}

	if response.StatusCode() == 200 || response.StatusCode() == 201 {
		var group openapigenerated.RestReviewerGroup
		if err := json.Unmarshal(response.Body, &group); err == nil {
			return group, nil
		}
	}

	return openapigenerated.RestReviewerGroup{}, nil
}

func (service *Service) GetRepositoryReviewerGroup(ctx context.Context, projectKey, repositorySlug string, id string) (openapigenerated.RestReviewerGroup, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" || strings.TrimSpace(id) == "" {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindValidation, "project key, repository slug, and ID are required", nil)
	}

	response, err := service.client.GetReviewerGroup1WithResponse(ctx, projectKey, repositorySlug, id)
	if err != nil {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindTransient, "failed to get repository reviewer group", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestReviewerGroup{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestReviewerGroup{}, nil
}

func (service *Service) UpdateRepositoryReviewerGroup(ctx context.Context, projectKey, repositorySlug string, id string, name, description string) (openapigenerated.RestReviewerGroup, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" || strings.TrimSpace(id) == "" {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindValidation, "project key, repository slug, and ID are required", nil)
	}

	body := openapigenerated.RestReviewerGroup{}
	if name != "" {
		body.Name = &name
	}
	if description != "" {
		body.Description = &description
	}

	response, err := service.client.Update2WithResponse(ctx, projectKey, repositorySlug, id, body)
	if err != nil {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindTransient, "failed to update repository reviewer group", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestReviewerGroup{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestReviewerGroup{}, nil
}

func (service *Service) DeleteRepositoryReviewerGroup(ctx context.Context, projectKey, repositorySlug string, id string) error {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" || strings.TrimSpace(id) == "" {
		return apperrors.New(apperrors.KindValidation, "project key, repository slug, and ID are required", nil)
	}

	response, err := service.client.Delete7WithResponse(ctx, projectKey, repositorySlug, id)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete repository reviewer group", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) ListRepositoryReviewerGroupUsers(ctx context.Context, projectKey, repositorySlug string, id string) ([]openapigenerated.RestApplicationUser, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" || strings.TrimSpace(id) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key, repository slug, and ID are required", nil)
	}

	response, err := service.client.GetUsersWithResponse(ctx, projectKey, repositorySlug, id)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list repository reviewer group users", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	if response.ApplicationjsonCharsetUTF8200 == nil {
		return []openapigenerated.RestApplicationUser{}, nil
	}

	return *response.ApplicationjsonCharsetUTF8200, nil
}

func (service *Service) ListProjectReviewerGroups(ctx context.Context, projectKey string) ([]openapigenerated.RestReviewerGroup, error) {
	if strings.TrimSpace(projectKey) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	response, err := service.client.GetReviewerGroupsWithResponse(ctx, projectKey, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list project reviewer groups", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil && response.ApplicationjsonCharsetUTF8200.Values != nil {
		return *response.ApplicationjsonCharsetUTF8200.Values, nil
	}

	return []openapigenerated.RestReviewerGroup{}, nil
}

func (service *Service) CreateProjectReviewerGroup(ctx context.Context, projectKey string, name, description string) (openapigenerated.RestReviewerGroup, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(name) == "" {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindValidation, "project key and name are required", nil)
	}

	body := openapigenerated.RestReviewerGroup{
		Name: &name,
	}
	if description != "" {
		body.Description = &description
	}

	response, err := service.client.Create1WithResponse(ctx, projectKey, body)
	if err != nil {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindTransient, "failed to create project reviewer group", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestReviewerGroup{}, err
	}

	if response.ApplicationjsonCharsetUTF8201 != nil {
		return *response.ApplicationjsonCharsetUTF8201, nil
	}

	if response.StatusCode() == 200 || response.StatusCode() == 201 {
		var group openapigenerated.RestReviewerGroup
		if err := json.Unmarshal(response.Body, &group); err == nil {
			return group, nil
		}
	}

	return openapigenerated.RestReviewerGroup{}, nil
}

func (service *Service) GetProjectReviewerGroup(ctx context.Context, projectKey string, id string) (openapigenerated.RestReviewerGroup, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(id) == "" {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindValidation, "project key and ID are required", nil)
	}

	response, err := service.client.GetReviewerGroupWithResponse(ctx, projectKey, id)
	if err != nil {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindTransient, "failed to get project reviewer group", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestReviewerGroup{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestReviewerGroup{}, nil
}

func (service *Service) UpdateProjectReviewerGroup(ctx context.Context, projectKey string, id string, name, description string) (openapigenerated.RestReviewerGroup, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(id) == "" {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindValidation, "project key and ID are required", nil)
	}

	body := openapigenerated.RestReviewerGroup{}
	if name != "" {
		body.Name = &name
	}
	if description != "" {
		body.Description = &description
	}

	response, err := service.client.Update1WithResponse(ctx, projectKey, id, body)
	if err != nil {
		return openapigenerated.RestReviewerGroup{}, apperrors.New(apperrors.KindTransient, "failed to update project reviewer group", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestReviewerGroup{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestReviewerGroup{}, nil
}

func (service *Service) DeleteProjectReviewerGroup(ctx context.Context, projectKey string, id string) error {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(id) == "" {
		return apperrors.New(apperrors.KindValidation, "project key and ID are required", nil)
	}

	response, err := service.client.Delete6WithResponse(ctx, projectKey, id)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete project reviewer group", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) GetDefaultReviewers(ctx context.Context, projectKey, repositorySlug string, sourceRepoId, targetRepoId, sourceRefId, targetRefId *string) ([]openapigenerated.RestPullRequestCondition, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key and repository slug are required", nil)
	}

	params := &openapigenerated.GetReviewersParams{
		SourceRepoId: sourceRepoId,
		TargetRepoId: targetRepoId,
		SourceRefId:  sourceRefId,
		TargetRefId:  targetRefId,
	}

	response, err := service.client.GetReviewersWithResponse(ctx, projectKey, repositorySlug, params)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get default reviewers", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	if response.JSON200 == nil {
		return []openapigenerated.RestPullRequestCondition{}, nil
	}

	return *response.JSON200, nil
}

