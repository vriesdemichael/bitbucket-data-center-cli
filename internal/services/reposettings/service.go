package reposettings

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"io"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type RepositoryRef struct {
	ProjectKey string
	Slug       string
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

type WebhookList struct {
	Count   int `json:"count"`
	Payload any `json:"payload"`
}

type WebhookCreateInput struct {
	Name   string
	URL    string
	Events []string
	Active bool
}

type DefaultTask struct {
	Id            *int64              `json:"id,omitempty"`
	Description   *string             `json:"description,omitempty"`
	SourceMatcher *DefaultTaskMatcher `json:"sourceMatcher,omitempty"`
	TargetMatcher *DefaultTaskMatcher `json:"targetMatcher,omitempty"`
}

type DefaultTaskMatcher struct {
	Id        *string `json:"id,omitempty"`
	DisplayId *string `json:"displayId,omitempty"`
}

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func (service *Service) ListRepositoryPermissionUsers(ctx context.Context, repo RepositoryRef, limit int) ([]PermissionUser, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}

	start := float32(0)
	pageLimit := float32(limit)
	results := make([]PermissionUser, 0)

	for {
		response, err := service.client.GetUsersWithAnyPermission2WithResponse(ctx, repo.ProjectKey, repo.Slug, &openapigenerated.GetUsersWithAnyPermission2Params{
			Start: &start,
			Limit: &pageLimit,
		})
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list repository permissions", err)
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

func (service *Service) ListRepositoryPermissionGroups(ctx context.Context, repo RepositoryRef, limit int) ([]PermissionGroup, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}

	start := float32(0)
	pageLimit := float32(limit)
	results := make([]PermissionGroup, 0)

	for {
		response, err := service.client.GetGroupsWithAnyPermission2WithResponse(ctx, repo.ProjectKey, repo.Slug, &openapigenerated.GetGroupsWithAnyPermission2Params{
			Start: &start,
			Limit: &pageLimit,
		})
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list repository group permissions", err)
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

func (service *Service) GrantRepositoryUserPermission(ctx context.Context, repo RepositoryRef, username string, permission string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	trimmedUser := strings.TrimSpace(username)
	if trimmedUser == "" {
		return apperrors.New(apperrors.KindValidation, "username is required", nil)
	}

	normalizedPermission, err := normalizeRepositoryPermission(permission)
	if err != nil {
		return err
	}

	response, err := service.client.SetPermissionForUserWithResponse(ctx, repo.ProjectKey, repo.Slug, &openapigenerated.SetPermissionForUserParams{
		Name:       []string{trimmedUser},
		Permission: openapigenerated.SetPermissionForUserParamsPermission(normalizedPermission),
	})
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to grant repository permission", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) RevokeRepositoryUserPermission(ctx context.Context, repo RepositoryRef, username string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	trimmedUser := strings.TrimSpace(username)
	if trimmedUser == "" {
		return apperrors.New(apperrors.KindValidation, "username is required", nil)
	}

	response, err := service.client.RevokePermissionsForUser2WithResponse(ctx, repo.ProjectKey, repo.Slug, &openapigenerated.RevokePermissionsForUser2Params{
		Name: trimmedUser,
	})
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to revoke repository user permission", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) GrantRepositoryGroupPermission(ctx context.Context, repo RepositoryRef, group string, permission string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	trimmedGroup := strings.TrimSpace(group)
	if trimmedGroup == "" {
		return apperrors.New(apperrors.KindValidation, "group name is required", nil)
	}

	normalizedPermission, err := normalizeRepositoryPermission(permission)
	if err != nil {
		return err
	}

	response, err := service.client.SetPermissionForGroupWithResponse(ctx, repo.ProjectKey, repo.Slug, &openapigenerated.SetPermissionForGroupParams{
		Name:       []string{trimmedGroup},
		Permission: openapigenerated.SetPermissionForGroupParamsPermission(normalizedPermission),
	})
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to grant repository group permission", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) RevokeRepositoryGroupPermission(ctx context.Context, repo RepositoryRef, group string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	trimmedGroup := strings.TrimSpace(group)
	if trimmedGroup == "" {
		return apperrors.New(apperrors.KindValidation, "group name is required", nil)
	}

	response, err := service.client.RevokePermissionsForGroup2WithResponse(ctx, repo.ProjectKey, repo.Slug, &openapigenerated.RevokePermissionsForGroup2Params{
		Name: trimmedGroup,
	})
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to revoke repository group permission", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) ListRepositoryWebhooks(ctx context.Context, repo RepositoryRef) (WebhookList, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return WebhookList{}, err
	}

	response, err := service.client.FindWebhooks1WithResponse(ctx, repo.ProjectKey, repo.Slug, nil)
	if err != nil {
		return WebhookList{}, apperrors.New(apperrors.KindTransient, "failed to list repository webhooks", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return WebhookList{}, err
	}

	if !json.Valid(response.Body) {
		return WebhookList{}, apperrors.New(apperrors.KindPermanent, "invalid JSON payload from webhooks endpoint", nil)
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return WebhookList{}, apperrors.New(apperrors.KindPermanent, "failed to decode webhooks payload", err)
	}

	count := 0
	switch typed := payload.(type) {
	case []any:
		count = len(typed)
	case map[string]any:
		if values, ok := typed["values"].([]any); ok {
			count = len(values)
		}
	}

	return WebhookList{Count: count, Payload: payload}, nil
}

func (service *Service) CreateRepositoryWebhook(ctx context.Context, repo RepositoryRef, input WebhookCreateInput) (any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	trimmedName := strings.TrimSpace(input.Name)
	trimmedURL := strings.TrimSpace(input.URL)
	if trimmedName == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook name is required", nil)
	}
	if trimmedURL == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook url is required", nil)
	}

	events := make([]string, 0, len(input.Events))
	for _, event := range input.Events {
		if trimmedEvent := strings.TrimSpace(event); trimmedEvent != "" {
			events = append(events, trimmedEvent)
		}
	}
	if len(events) == 0 {
		events = []string{"repo:refs_changed"}
	}

	body := openapigenerated.CreateWebhook1JSONRequestBody{
		Name:   &trimmedName,
		Url:    &trimmedURL,
		Events: &events,
		Active: &input.Active,
	}

	response, err := service.client.CreateWebhook1WithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to create repository webhook", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	if !json.Valid(response.Body) {
		return nil, nil
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode created webhook payload", err)
	}

	return payload, nil
}

func (service *Service) DeleteRepositoryWebhook(ctx context.Context, repo RepositoryRef, webhookID string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	trimmedWebhookID := strings.TrimSpace(webhookID)
	if trimmedWebhookID == "" {
		return apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}

	response, err := service.client.DeleteWebhook1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedWebhookID)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete repository webhook", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) GetRepositoryPullRequestSettings(ctx context.Context, repo RepositoryRef) (map[string]any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	response, err := service.client.GetPullRequestSettings1(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get pull request settings", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to read pull request settings response", readErr)
	}

	if err := openapi.MapStatusError(response.StatusCode, body); err != nil {
		return nil, err
	}
	if !json.Valid(body) {
		return nil, apperrors.New(apperrors.KindPermanent, "invalid JSON payload from pull request settings endpoint", nil)
	}

	payload := map[string]any{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode pull request settings payload", err)
	}

	return payload, nil
}

func (service *Service) UpdateRepositoryPullRequestSettings(ctx context.Context, repo RepositoryRef, settings map[string]any) (map[string]any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	rawPayload, err := json.Marshal(settings)
	if err != nil {
		return nil, apperrors.New(apperrors.KindInternal, "failed to encode pull request settings update", err)
	}

	response, err := service.client.UpdatePullRequestSettings1WithBody(ctx, repo.ProjectKey, repo.Slug, "application/json", bytes.NewReader(rawPayload))
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to update pull request settings", err)
	}
	body, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to read pull request settings update response", readErr)
	}
	if err := openapi.MapStatusError(response.StatusCode, body); err != nil {
		return nil, err
	}

	if len(bytes.TrimSpace(body)) == 0 || !json.Valid(body) {
		// Fallback for non-JSON response from some Bitbucket versions/endpoints
		return settings, nil
	}

	updated := map[string]any{}
	if err := json.Unmarshal(body, &updated); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode pull request settings update response", err)
	}

	return updated, nil
}

func (service *Service) UpdateRepositoryPullRequestRequiredAllTasks(ctx context.Context, repo RepositoryRef, required bool) (map[string]any, error) {
	return service.UpdateRepositoryPullRequestSettings(ctx, repo, map[string]any{"requiredAllTasksComplete": required})
}

func (service *Service) UpdateRepositoryPullRequestRequiredApproversCount(ctx context.Context, repo RepositoryRef, count int) (map[string]any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, apperrors.New(apperrors.KindValidation, "required approvers count must be >= 0", nil)
	}

	// Try object structure first (modern Bitbucket)
	settings := map[string]any{
		"requiredApprovers": map[string]any{
			"enabled": count > 0,
			"count":   count,
		},
	}
	if count == 0 {
		settings["requiredApprovers"] = map[string]any{
			"enabled": false,
		}
	}

	result, err := service.UpdateRepositoryPullRequestSettings(ctx, repo, settings)
	if err != nil {
		// Only fallback if it's a validation error AND it likely relates to the payload structure.
		// Our MapStatusError includes the body in the message.
		if apperrors.IsKind(err, apperrors.KindValidation) &&
			(strings.Contains(strings.ToLower(err.Error()), "invalid") || strings.Contains(strings.ToLower(err.Error()), "payload")) {
			return service.UpdateRepositoryPullRequestSettings(ctx, repo, map[string]any{"requiredApprovers": count})
		}
		return nil, err
	}

	return result, nil
}

func (service *Service) ListRequiredBuildsMergeChecks(ctx context.Context, repo RepositoryRef) (any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	response, err := service.client.GetPageOfRequiredBuildsMergeChecksWithResponse(ctx, repo.ProjectKey, repo.Slug, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list required builds merge checks", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode merge checks payload", err)
	}

	return payload, nil
}

func validateRepositoryRef(repo RepositoryRef) error {
	if strings.TrimSpace(repo.ProjectKey) == "" || strings.TrimSpace(repo.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}

	return nil
}

func normalizeRepositoryPermission(permission string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(permission)) {
	case "REPO_READ":
		return "REPO_READ", nil
	case "REPO_WRITE":
		return "REPO_WRITE", nil
	case "REPO_ADMIN":
		return "REPO_ADMIN", nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "permission must be one of REPO_READ, REPO_WRITE, REPO_ADMIN", nil)
	}
}

func (service *Service) GetRepositoryAutoMergeSettings(ctx context.Context, repo RepositoryRef) (*openapigenerated.RestAutoMergeRestrictedSettings, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	response, err := service.client.Get5WithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get auto-merge settings", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	return response.ApplicationjsonCharsetUTF8200, nil
}

func (service *Service) UpdateRepositoryAutoMergeSettings(ctx context.Context, repo RepositoryRef, enabled bool) (*openapigenerated.RestAutoMergeRestrictedSettings, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	body := openapigenerated.Set1JSONRequestBody{
		Enabled: &enabled,
	}
	response, err := service.client.Set1WithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to update auto-merge settings", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	return response.ApplicationjsonCharsetUTF8200, nil
}

func (service *Service) DeleteRepositoryAutoMergeSettings(ctx context.Context, repo RepositoryRef) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	response, err := service.client.Delete5WithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete auto-merge settings", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) GetRepositoryAutoDeclineSettings(ctx context.Context, repo RepositoryRef) (*openapigenerated.RestAutoDeclineSettings, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	response, err := service.client.GetAutoDeclineSettings1WithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get auto-decline settings", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	return response.ApplicationjsonCharsetUTF8200, nil
}

func (service *Service) UpdateRepositoryAutoDeclineSettings(ctx context.Context, repo RepositoryRef, enabled bool, inactivityWeeks int32) (*openapigenerated.RestAutoDeclineSettings, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	body := openapigenerated.SetAutoDeclineSettings1JSONRequestBody{
		Enabled: &enabled,
	}
	if enabled {
		body.InactivityWeeks = &inactivityWeeks
	}
	response, err := service.client.SetAutoDeclineSettings1WithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to update auto-decline settings", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	return response.ApplicationjsonCharsetUTF8200, nil
}

func (service *Service) DeleteRepositoryAutoDeclineSettings(ctx context.Context, repo RepositoryRef) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	response, err := service.client.DeleteAutoDeclineSettings1WithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete auto-decline settings", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) ListRepositoryLabels(ctx context.Context, repo RepositoryRef) ([]string, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	response, err := service.client.GetAllLabelsForRepositoryWithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list labels", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var page struct {
		Values []struct {
			Name string `json:"name"`
		} `json:"values"`
	}
	if len(response.Body) > 0 {
		if err := json.Unmarshal(response.Body, &page); err != nil {
			return nil, apperrors.New(apperrors.KindPermanent, "failed to decode labels list", err)
		}
	}
	labels := make([]string, len(page.Values))
	for i, v := range page.Values {
		labels[i] = v.Name
	}
	return labels, nil
}

func (service *Service) AddRepositoryLabel(ctx context.Context, repo RepositoryRef, labelName string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(labelName)
	if trimmed == "" {
		return apperrors.New(apperrors.KindValidation, "label name is required", nil)
	}
	body := openapigenerated.AddLabelJSONRequestBody{
		Name: &trimmed,
	}
	response, err := service.client.AddLabelWithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to add repository label", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) RemoveRepositoryLabel(ctx context.Context, repo RepositoryRef, labelName string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	trimmed := strings.TrimSpace(labelName)
	if trimmed == "" {
		return apperrors.New(apperrors.KindValidation, "label name is required", nil)
	}
	response, err := service.client.RemoveLabelWithResponse(ctx, repo.ProjectKey, repo.Slug, trimmed)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to remove repository label", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) WatchRepository(ctx context.Context, repo RepositoryRef) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	body := openapigenerated.Watch2JSONRequestBody{}
	response, err := service.client.Watch2WithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to watch repository", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) UnwatchRepository(ctx context.Context, repo RepositoryRef) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	response, err := service.client.Unwatch2WithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to unwatch repository", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) ListDefaultTasks(ctx context.Context, repo RepositoryRef) ([]DefaultTask, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	response, err := service.client.GetDefaultTasks1WithResponse(ctx, repo.ProjectKey, repo.Slug, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list default tasks", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var page struct {
		Values []DefaultTask `json:"values"`
	}
	if len(response.Body) > 0 {
		if err := json.Unmarshal(response.Body, &page); err != nil {
			return nil, apperrors.New(apperrors.KindPermanent, "failed to decode default tasks list", err)
		}
	}
	return page.Values, nil
}

func (service *Service) AddDefaultTask(ctx context.Context, repo RepositoryRef, description string, sourceRef *string, targetRef *string) (*DefaultTask, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(description)
	if trimmed == "" {
		return nil, apperrors.New(apperrors.KindValidation, "description is required", nil)
	}
	body := openapigenerated.RestDefaultTaskRequest{
		Description: &trimmed,
	}
	if sourceRef != nil && *sourceRef != "" {
		ref := *sourceRef
		typeId := openapigenerated.RestDefaultTaskRequestSourceMatcherTypeId("ANY_REF_MATCHER")
		body.SourceMatcher = &struct {
			DisplayId *string                                                      `json:"displayId,omitempty"`
			Id        *string                                                      `json:"id,omitempty"`
			Type      *struct {
				Id   *openapigenerated.RestDefaultTaskRequestSourceMatcherTypeId `json:"id,omitempty"`
				Name *string                                                     `json:"name,omitempty"`
			} `json:"type,omitempty"`
		}{
			Id:        &ref,
			DisplayId: &ref,
			Type: &struct {
				Id   *openapigenerated.RestDefaultTaskRequestSourceMatcherTypeId `json:"id,omitempty"`
				Name *string                                                     `json:"name,omitempty"`
			}{
				Id: &typeId,
			},
		}
	}
	if targetRef != nil && *targetRef != "" {
		ref := *targetRef
		typeId := openapigenerated.RestDefaultTaskRequestTargetMatcherTypeId("ANY_REF_MATCHER")
		body.TargetMatcher = &struct {
			DisplayId *string                                                      `json:"displayId,omitempty"`
			Id        *string                                                      `json:"id,omitempty"`
			Type      *struct {
				Id   *openapigenerated.RestDefaultTaskRequestTargetMatcherTypeId `json:"id,omitempty"`
				Name *string                                                     `json:"name,omitempty"`
			} `json:"type,omitempty"`
		}{
			Id:        &ref,
			DisplayId: &ref,
			Type: &struct {
				Id   *openapigenerated.RestDefaultTaskRequestTargetMatcherTypeId `json:"id,omitempty"`
				Name *string                                                     `json:"name,omitempty"`
			}{
				Id: &typeId,
			},
		}
	}

	response, err := service.client.AddDefaultTask1WithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to add default task", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var task DefaultTask
	if len(response.Body) > 0 {
		if err := json.Unmarshal(response.Body, &task); err != nil {
			return nil, apperrors.New(apperrors.KindPermanent, "failed to decode default task response", err)
		}
	}
	return &task, nil
}

func (service *Service) UpdateDefaultTask(ctx context.Context, repo RepositoryRef, taskId string, description string, sourceRef *string, targetRef *string) (*DefaultTask, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(taskId)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "task id is required", nil)
	}
	trimmed := strings.TrimSpace(description)
	if trimmed == "" {
		return nil, apperrors.New(apperrors.KindValidation, "description is required", nil)
	}
	body := openapigenerated.RestDefaultTaskRequest{
		Description: &trimmed,
	}
	if sourceRef != nil && *sourceRef != "" {
		ref := *sourceRef
		typeId := openapigenerated.RestDefaultTaskRequestSourceMatcherTypeId("ANY_REF_MATCHER")
		body.SourceMatcher = &struct {
			DisplayId *string                                                      `json:"displayId,omitempty"`
			Id        *string                                                      `json:"id,omitempty"`
			Type      *struct {
				Id   *openapigenerated.RestDefaultTaskRequestSourceMatcherTypeId `json:"id,omitempty"`
				Name *string                                                     `json:"name,omitempty"`
			} `json:"type,omitempty"`
		}{
			Id:        &ref,
			DisplayId: &ref,
			Type: &struct {
				Id   *openapigenerated.RestDefaultTaskRequestSourceMatcherTypeId `json:"id,omitempty"`
				Name *string                                                     `json:"name,omitempty"`
			}{
				Id: &typeId,
			},
		}
	}
	if targetRef != nil && *targetRef != "" {
		ref := *targetRef
		typeId := openapigenerated.RestDefaultTaskRequestTargetMatcherTypeId("ANY_REF_MATCHER")
		body.TargetMatcher = &struct {
			DisplayId *string                                                      `json:"displayId,omitempty"`
			Id        *string                                                      `json:"id,omitempty"`
			Type      *struct {
				Id   *openapigenerated.RestDefaultTaskRequestTargetMatcherTypeId `json:"id,omitempty"`
				Name *string                                                     `json:"name,omitempty"`
			} `json:"type,omitempty"`
		}{
			Id:        &ref,
			DisplayId: &ref,
			Type: &struct {
				Id   *openapigenerated.RestDefaultTaskRequestTargetMatcherTypeId `json:"id,omitempty"`
				Name *string                                                     `json:"name,omitempty"`
			}{
				Id: &typeId,
			},
		}
	}

	response, err := service.client.UpdateDefaultTask1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to update default task", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var task DefaultTask
	if len(response.Body) > 0 {
		if err := json.Unmarshal(response.Body, &task); err != nil {
			return nil, apperrors.New(apperrors.KindPermanent, "failed to decode default task response", err)
		}
	}
	return &task, nil
}

func (service *Service) DeleteDefaultTask(ctx context.Context, repo RepositoryRef, taskId string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}
	trimmedID := strings.TrimSpace(taskId)
	if trimmedID == "" {
		return apperrors.New(apperrors.KindValidation, "task id is required", nil)
	}
	response, err := service.client.DeleteDefaultTask1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete default task", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) GetWebhook(ctx context.Context, repo RepositoryRef, id string) (any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}
	response, err := service.client.GetWebhook1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get webhook", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode webhook payload", err)
	}
	return payload, nil
}

func (service *Service) UpdateWebhook(ctx context.Context, repo RepositoryRef, id string, name string, url string, events []string, active *bool) (any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}

	body := openapigenerated.UpdateWebhook1JSONRequestBody{}
	if strings.TrimSpace(name) != "" {
		n := strings.TrimSpace(name)
		body.Name = &n
	}
	if strings.TrimSpace(url) != "" {
		u := strings.TrimSpace(url)
		body.Url = &u
	}
	if len(events) > 0 {
		body.Events = &events
	}
	if active != nil {
		body.Active = active
	}

	response, err := service.client.UpdateWebhook1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to update webhook", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode webhook payload", err)
	}
	return payload, nil
}

func (service *Service) SearchWebhooks(ctx context.Context, repo RepositoryRef, event *string) (any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	params := &openapigenerated.SearchWebhooksParams{}
	if event != nil && *event != "" {
		params.Event = event
	}
	response, err := service.client.SearchWebhooksWithResponse(ctx, repo.ProjectKey, repo.Slug, params)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to search webhooks", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode webhooks search payload", err)
	}
	return payload, nil
}

func (service *Service) TestWebhook(ctx context.Context, repo RepositoryRef, id string) (any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}
	webhookIDVal, err := strconv.ParseInt(trimmedID, 10, 32)
	if err != nil {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id must be an integer", err)
	}
	webhookID32 := int32(webhookIDVal)

	params := &openapigenerated.TestWebhook1Params{
		WebhookId: &webhookID32,
	}
	body := openapigenerated.TestWebhook1JSONRequestBody{}

	response, err := service.client.TestWebhook1WithResponse(ctx, repo.ProjectKey, repo.Slug, params, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to test webhook", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode test webhook response", err)
	}
	return payload, nil
}

func (service *Service) GetWebhookLatestInvocation(ctx context.Context, repo RepositoryRef, id string, event *string, outcome *string) (any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}
	var params *openapigenerated.GetLatestInvocation1Params
	if (event != nil && *event != "") || (outcome != nil && *outcome != "") {
		params = &openapigenerated.GetLatestInvocation1Params{}
		if event != nil && *event != "" {
			params.Event = event
		}
		if outcome != nil && *outcome != "" {
			params.Outcome = outcome
		}
	}
	response, err := service.client.GetLatestInvocation1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID, params)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get latest invocation", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode latest invocation response", err)
	}
	return payload, nil
}

func (service *Service) GetWebhookStatistics(ctx context.Context, repo RepositoryRef, id string) (any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}
	response, err := service.client.GetStatistics1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get statistics", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode statistics response", err)
	}
	return payload, nil
}

func (service *Service) GetWebhookStatisticsSummary(ctx context.Context, repo RepositoryRef, id string) (any, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}
	response, err := service.client.GetStatisticsSummary1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get statistics summary", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode statistics summary response", err)
	}
	return payload, nil
}
