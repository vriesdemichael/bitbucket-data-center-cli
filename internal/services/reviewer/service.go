package reviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
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

// ResolveDefaultReviewers queries matching default reviewer conditions for a source and target ref
// and resolves both individual reviewers and constituent reviewer group members into usernames.
func (service *Service) ResolveDefaultReviewers(ctx context.Context, projectKey, repositorySlug, sourceRef, targetRef string) ([]string, error) {
	var sourceRefPtr, targetRefPtr *string
	if strings.TrimSpace(sourceRef) != "" {
		s := strings.TrimSpace(sourceRef)
		sourceRefPtr = &s
	}
	if strings.TrimSpace(targetRef) != "" {
		t := strings.TrimSpace(targetRef)
		targetRefPtr = &t
	}

	conditions, err := service.GetDefaultReviewers(ctx, projectKey, repositorySlug, nil, nil, sourceRefPtr, targetRefPtr)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	var resolved []string

	for _, cond := range conditions {
		if cond.Reviewers != nil {
			for _, r := range *cond.Reviewers {
				name := ""
				if r.Name != nil {
					name = *r.Name
				}
				if name != "" && !seen[strings.ToLower(name)] {
					seen[strings.ToLower(name)] = true
					resolved = append(resolved, name)
				}
			}
		}
		if cond.ReviewerGroups != nil {
			for _, g := range *cond.ReviewerGroups {
				groupName := ""
				if g.Name != nil {
					groupName = *g.Name
				}
				if groupName != "" {
					members, err := service.ResolveReviewerGroupUsers(ctx, projectKey, repositorySlug, groupName)
					if err != nil {
						return nil, err
					}
					for _, m := range members {
						if m != "" && !seen[strings.ToLower(m)] {
							seen[strings.ToLower(m)] = true
							resolved = append(resolved, m)
						}
					}
				}
			}
		}
	}

	return resolved, nil
}

// ResolveReviewerGroupUsers resolves a reviewer group name to its constituent user usernames.
// It searches repository-level reviewer groups first (if repositorySlug is non-empty),
// falling back to project-level reviewer groups. If the server does not support reviewer
// groups (e.g. Bitbucket Data Center < 7.13), a descriptive error indicating that
// Bitbucket Data Center 7.13+ is required is returned.
func (service *Service) ResolveReviewerGroupUsers(ctx context.Context, projectKey, repositorySlug, groupName string) ([]string, error) {
	trimmedGroup := strings.TrimPrefix(strings.TrimSpace(groupName), "@")
	if trimmedGroup == "" {
		return nil, apperrors.New(apperrors.KindValidation, "reviewer group name is required", nil)
	}
	if strings.TrimSpace(projectKey) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	// 1. If repository slug is provided, search repository-level reviewer groups first.
	if strings.TrimSpace(repositorySlug) != "" {
		repoGroups, err := service.ListRepositoryReviewerGroups(ctx, projectKey, repositorySlug)
		if err != nil {
			if openapi.IsRouteMissing(err) || (apperrors.IsKind(err, apperrors.KindNotFound) && strings.Contains(err.Error(), "404")) {
				return nil, apperrors.New(apperrors.KindPermanent, "reviewer groups require Bitbucket Data Center 7.13 or later (server returned 404 for reviewer groups endpoint)", err)
			}
			return nil, err
		}
		for _, g := range repoGroups {
			if g.Name != nil && strings.EqualFold(*g.Name, trimmedGroup) {
				if g.Id != nil {
					idStr := fmt.Sprintf("%d", *g.Id)
					users, err := service.ListRepositoryReviewerGroupUsers(ctx, projectKey, repositorySlug, idStr)
					if err == nil && len(users) > 0 {
						names := make([]string, 0, len(users))
						for _, u := range users {
							if u.Name != nil && strings.TrimSpace(*u.Name) != "" {
								names = append(names, strings.TrimSpace(*u.Name))
							}
						}
						return names, nil
					}
					if g.Users != nil && len(*g.Users) > 0 {
						names := make([]string, 0, len(*g.Users))
						for _, u := range *g.Users {
							if u.Name != nil && strings.TrimSpace(*u.Name) != "" {
								names = append(names, strings.TrimSpace(*u.Name))
							}
						}
						return names, nil
					}
				}
				return []string{}, nil
			}
		}
	}

	// 2. Search project-level reviewer groups.
	projGroups, err := service.ListProjectReviewerGroups(ctx, projectKey)
	if err != nil {
		if openapi.IsRouteMissing(err) || (apperrors.IsKind(err, apperrors.KindNotFound) && strings.Contains(err.Error(), "404")) {
			return nil, apperrors.New(apperrors.KindPermanent, "reviewer groups require Bitbucket Data Center 7.13 or later (server returned 404 for reviewer groups endpoint)", err)
		}
		return nil, err
	}
	for _, g := range projGroups {
		if g.Name != nil && strings.EqualFold(*g.Name, trimmedGroup) {
			if g.Id != nil {
				idStr := fmt.Sprintf("%d", *g.Id)
				groupDetail, err := service.GetProjectReviewerGroup(ctx, projectKey, idStr)
				if err == nil && groupDetail.Users != nil && len(*groupDetail.Users) > 0 {
					names := make([]string, 0, len(*groupDetail.Users))
					for _, u := range *groupDetail.Users {
						if u.Name != nil && strings.TrimSpace(*u.Name) != "" {
							names = append(names, strings.TrimSpace(*u.Name))
						}
					}
					return names, nil
				}
			}
			if g.Users != nil && len(*g.Users) > 0 {
				names := make([]string, 0, len(*g.Users))
				for _, u := range *g.Users {
					if u.Name != nil && strings.TrimSpace(*u.Name) != "" {
						names = append(names, strings.TrimSpace(*u.Name))
					}
				}
				return names, nil
			}
			return []string{}, nil
		}
	}

	if strings.TrimSpace(repositorySlug) != "" {
		return nil, apperrors.New(apperrors.KindNotFound, fmt.Sprintf("reviewer group %q not found in repository %s/%s or project %s", trimmedGroup, projectKey, repositorySlug, projectKey), nil)
	}
	return nil, apperrors.New(apperrors.KindNotFound, fmt.Sprintf("reviewer group %q not found in project %s", trimmedGroup, projectKey), nil)
}

// SelectMembers applies reviewer group selection strategies (:all, :random(N), :least_busy(N)),
// excluding the PR author from selection.
func SelectMembers(
	members []string,
	author string,
	strategy string,
	count int,
	busyCounts map[string]int,
) []string {
	var eligible []string
	for _, m := range members {
		trimmed := strings.TrimSpace(m)
		if trimmed == "" {
			continue
		}
		if author != "" && strings.EqualFold(trimmed, strings.TrimSpace(author)) {
			continue
		}
		eligible = append(eligible, trimmed)
	}

	if count <= 0 || len(eligible) <= count {
		return eligible
	}

	switch strategy {
	case "random":
		shuffled := make([]string, len(eligible))
		copy(shuffled, eligible)
		rand.Shuffle(len(shuffled), func(i, j int) {
			shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
		})
		return shuffled[:count]
	case "least_busy":
		sorted := make([]string, len(eligible))
		copy(sorted, eligible)
		sort.SliceStable(sorted, func(i, j int) bool {
			countI := 0
			countJ := 0
			if busyCounts != nil {
				countI = busyCounts[strings.ToLower(sorted[i])]
				countJ = busyCounts[strings.ToLower(sorted[j])]
			}
			return countI < countJ
		})
		return sorted[:count]
	default:
		return eligible
	}
}
