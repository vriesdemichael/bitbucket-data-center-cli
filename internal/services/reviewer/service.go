package reviewer

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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

	// Delete9, not Delete7: these names are collision suffixes assigned in spec
	// order, so upstream additions renumber them. Pinned by TestGeneratedOperationPaths.
	response, err := service.client.Delete9WithResponse(ctx, projectKey, repositorySlug, id)
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

	// Delete8, not Delete6: see the note on DeleteRepositoryReviewerGroup.
	response, err := service.client.Delete8WithResponse(ctx, projectKey, id)
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

// NormalizeRefID expands a branch name into the fully qualified ref ID Bitbucket
// matches default reviewer conditions against. Callers hand this function either
// a bare branch name ("main"), a pull request display ID ("feature/x"), or an
// already qualified ref ("refs/heads/main"); the condition matcher only ever
// understands the last form, so anything else is prefixed.
func NormalizeRefID(ref string) string {
	trimmed := strings.TrimSpace(ref)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "refs/") {
		return trimmed
	}
	return "refs/heads/" + trimmed
}

// RepositoryID resolves the numeric ID Bitbucket uses to identify a repository
// in default reviewer conditions. It is returned as a string because the
// condition query takes it as a query parameter.
func (service *Service) RepositoryID(ctx context.Context, projectKey, repositorySlug string) (string, error) {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(repositorySlug) == "" {
		return "", apperrors.New(apperrors.KindValidation, "project key and repository slug are required", nil)
	}

	response, err := service.client.GetRepositoryWithResponse(ctx, projectKey, repositorySlug)
	if err != nil {
		return "", apperrors.New(apperrors.KindTransient, "failed to look up repository", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return "", err
	}
	if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Id == nil {
		return "", apperrors.New(apperrors.KindPermanent, "repository response did not include an ID", nil)
	}

	return strconv.FormatInt(int64(*response.ApplicationjsonCharsetUTF8200.Id), 10), nil
}

// DefaultReviewerQuery describes the pull request a default reviewer lookup is
// for. SourceProjectKey and SourceSlug are only needed for a fork pull request;
// leaving them empty means the source branch lives in the target repository.
type DefaultReviewerQuery struct {
	SourceRef string
	TargetRef string

	SourceProjectKey string
	SourceSlug       string
}

// ResolveDefaultReviewers queries matching default reviewer conditions for a source and target ref
// and resolves both individual reviewers and constituent reviewer group members into usernames.
//
// Branch names are normalized to fully qualified ref IDs, because Bitbucket matches
// condition ref patterns against "refs/heads/..." and silently returns no conditions
// for a bare branch name. Repository IDs are resolved on a best-effort basis so that
// repository-scoped conditions match; when one cannot be determined the query still
// runs without it rather than failing the caller outright.
func (service *Service) ResolveDefaultReviewers(ctx context.Context, projectKey, repositorySlug string, query DefaultReviewerQuery) ([]string, error) {
	var sourceRefPtr, targetRefPtr *string
	if normalized := NormalizeRefID(query.SourceRef); normalized != "" {
		sourceRefPtr = &normalized
	}
	if normalized := NormalizeRefID(query.TargetRef); normalized != "" {
		targetRefPtr = &normalized
	}

	targetRepoIDPtr := service.optionalRepositoryID(ctx, projectKey, repositorySlug)

	// A fork pull request has its source branch in a different repository, and
	// telling Bitbucket the target repository for both would describe a pull
	// request that does not exist.
	sourceRepoIDPtr := targetRepoIDPtr
	if query.SourceProjectKey != "" && query.SourceSlug != "" &&
		!(strings.EqualFold(query.SourceProjectKey, projectKey) && strings.EqualFold(query.SourceSlug, repositorySlug)) {
		sourceRepoIDPtr = service.optionalRepositoryID(ctx, query.SourceProjectKey, query.SourceSlug)
	}

	conditions, err := service.GetDefaultReviewers(ctx, projectKey, repositorySlug, sourceRepoIDPtr, targetRepoIDPtr, sourceRefPtr, targetRefPtr)
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

// optionalRepositoryID resolves a repository ID for a condition query, yielding
// nil when it cannot be determined. The ID sharpens condition matching but is
// not required to ask the question, so a failure here must not fail the lookup.
func (service *Service) optionalRepositoryID(ctx context.Context, projectKey, repositorySlug string) *string {
	repoID, err := service.RepositoryID(ctx, projectKey, repositorySlug)
	if err != nil || repoID == "" {
		return nil
	}
	return &repoID
}

// namedUser covers the two generated user shapes reviewer groups are returned
// with: the group payload embeds ApplicationUser while the dedicated members
// endpoint returns RestApplicationUser. Both carry the username in Name.
type namedUser interface {
	openapigenerated.ApplicationUser | openapigenerated.RestApplicationUser
}

// userNames extracts non-empty usernames from a slice of generated user structs.
func userNames[T namedUser](users []T) []string {
	names := make([]string, 0, len(users))
	for _, user := range users {
		var name *string
		switch typed := any(user).(type) {
		case openapigenerated.ApplicationUser:
			name = typed.Name
		case openapigenerated.RestApplicationUser:
			name = typed.Name
		}
		if name == nil {
			continue
		}
		if trimmed := strings.TrimSpace(*name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	return names
}

// ResolveReviewerGroupUsers resolves a reviewer group name to its constituent user usernames.
// It searches repository-level reviewer groups first (if repositorySlug is non-empty),
// falling back to project-level reviewer groups. A server with no reviewer groups API at
// all yields a descriptive error rather than an opaque 404.
func (service *Service) ResolveReviewerGroupUsers(ctx context.Context, projectKey, repositorySlug, groupName string) ([]string, error) {
	trimmedGroup := strings.TrimPrefix(strings.TrimSpace(groupName), "@")

	// "reviewer-group/cog_product" names the group cog_product. Bitbucket's own
	// Code Owners syntax spells a reviewer group that way, and callers pass the
	// token through verbatim -- from CODEOWNERS, from --reviewers @<name> and
	// from --reviewer-group. Carrying the prefix into the lookup made every one
	// of them miss a group that exists (#503), so it is stripped here, at the
	// single point all three funnel through.
	trimmedGroup = strings.TrimPrefix(trimmedGroup, "reviewer-group/")

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
				return nil, apperrors.New(apperrors.KindPermanent, "this server does not provide the reviewer groups API (it returned 404 for the reviewer groups endpoint)", err)
			}
			return nil, err
		}
		for _, g := range repoGroups {
			if g.Name == nil || !strings.EqualFold(*g.Name, trimmedGroup) {
				continue
			}

			var lookupErr error
			if g.Id != nil {
				idStr := fmt.Sprintf("%d", *g.Id)
				users, err := service.ListRepositoryReviewerGroupUsers(ctx, projectKey, repositorySlug, idStr)
				if err != nil {
					lookupErr = err
				} else if names := userNames(users); len(names) > 0 {
					return names, nil
				}
			}

			// Older servers do not expose the dedicated members endpoint but do
			// embed the members in the group payload, so fall back to those.
			if g.Users != nil {
				if names := userNames(*g.Users); len(names) > 0 {
					return names, nil
				}
			}

			if lookupErr != nil {
				// The group exists but its membership could not be read and no
				// embedded members were available. Surface that instead of
				// silently reporting an empty group, which would assign nobody.
				return nil, apperrors.New(
					apperrors.KindTransient,
					fmt.Sprintf("failed to list members of reviewer group %q in repository %s/%s", trimmedGroup, projectKey, repositorySlug),
					lookupErr,
				)
			}

			return []string{}, nil
		}
	}

	// 2. Search project-level reviewer groups.
	projGroups, err := service.ListProjectReviewerGroups(ctx, projectKey)
	if err != nil {
		if openapi.IsRouteMissing(err) || (apperrors.IsKind(err, apperrors.KindNotFound) && strings.Contains(err.Error(), "404")) {
			return nil, apperrors.New(apperrors.KindPermanent, "this server does not provide the reviewer groups API (it returned 404 for the reviewer groups endpoint)", err)
		}
		return nil, err
	}
	for _, g := range projGroups {
		if g.Name == nil || !strings.EqualFold(*g.Name, trimmedGroup) {
			continue
		}

		var lookupErr error
		if g.Id != nil {
			idStr := fmt.Sprintf("%d", *g.Id)
			groupDetail, err := service.GetProjectReviewerGroup(ctx, projectKey, idStr)
			if err != nil {
				lookupErr = err
			} else if groupDetail.Users != nil {
				if names := userNames(*groupDetail.Users); len(names) > 0 {
					return names, nil
				}
			}
		}

		// Older servers embed the members in the listing payload instead.
		if g.Users != nil {
			if names := userNames(*g.Users); len(names) > 0 {
				return names, nil
			}
		}

		if lookupErr != nil {
			// The group exists but its membership could not be read and no
			// embedded members were available. Surface that instead of
			// silently reporting an empty group.
			return nil, apperrors.New(
				apperrors.KindTransient,
				fmt.Sprintf("failed to read reviewer group %q in project %s", trimmedGroup, projectKey),
				lookupErr,
			)
		}

		return []string{}, nil
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
