package project

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/webhookfields"
)

func (service *Service) ListProjectWebhooks(ctx context.Context, projectKey string) (any, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	response, err := service.client.FindWebhooksWithResponse(ctx, trimmedProject, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list project webhooks", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	if len(response.Body) == 0 {
		return nil, nil
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode project webhooks payload", err)
	}

	return payload, nil
}

// WebhookCreateInput and WebhookUpdateInput are the shared shapes: a project
// webhook and a repository webhook are one object behind two routes.
type (
	WebhookCreateInput = webhookfields.CreateInput
	WebhookUpdateInput = webhookfields.UpdateInput
)

func (service *Service) CreateProjectWebhook(ctx context.Context, projectKey string, input WebhookCreateInput) (any, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	body, err := webhookfields.NewCreateBody(input)
	if err != nil {
		return nil, err
	}

	response, err := service.client.CreateWebhookWithResponse(ctx, trimmedProject, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to create project webhook", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	if len(response.Body) == 0 {
		return nil, nil
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode created project webhook payload", err)
	}

	return payload, nil
}

func (service *Service) GetProjectWebhook(ctx context.Context, projectKey string, id string) (any, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}

	response, err := service.client.GetWebhookWithResponse(ctx, trimmedProject, trimmedID, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get project webhook", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode project webhook payload", err)
	}

	return payload, nil
}

// projectWebhookForUpdate reads the current webhook as an update request body.
//
// The update endpoint replaces the webhook rather than patching it, so every
// field has to be sent back. Reading first is what lets --name on its own mean
// "change the name" instead of "clear the url and the events".
func (service *Service) projectWebhookForUpdate(ctx context.Context, projectKey string, id string) (openapigenerated.RestWebhook, error) {
	response, err := service.client.GetWebhookWithResponse(ctx, projectKey, id, nil)
	if err != nil {
		return openapigenerated.RestWebhook{}, apperrors.New(apperrors.KindTransient, "failed to read the project webhook before updating it", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestWebhook{}, err
	}

	var current openapigenerated.RestWebhook
	if err := json.Unmarshal(response.Body, &current); err != nil {
		return openapigenerated.RestWebhook{}, apperrors.New(apperrors.KindPermanent, "failed to decode the project webhook being updated", err)
	}

	current.Statistics = nil
	current.ScopeType = nil

	return current, nil
}

func (service *Service) UpdateProjectWebhook(ctx context.Context, projectKey string, id string, input WebhookUpdateInput) (any, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}

	body, err := service.projectWebhookForUpdate(ctx, trimmedProject, trimmedID)
	if err != nil {
		return nil, err
	}
	webhookfields.ApplyUpdate(&body, input)

	response, err := service.client.UpdateWebhookWithResponse(ctx, trimmedProject, trimmedID, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to update project webhook", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode project webhook payload", err)
	}

	return payload, nil
}

func (service *Service) DeleteProjectWebhook(ctx context.Context, projectKey string, id string) error {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}

	response, err := service.client.DeleteWebhookWithResponse(ctx, trimmedProject, trimmedID)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete project webhook", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

// TestProjectWebhook asks the server to deliver a test payload.
//
// url is required in practice even though the specification marks it optional:
// sending only webhookId makes the server throw an unhandled exception and
// answer 500. Same defect and same fix as the repository variant — see #383.
func (service *Service) TestProjectWebhook(ctx context.Context, projectKey string, id string, overrideURL string) (any, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
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

	targetURL := strings.TrimSpace(overrideURL)
	if targetURL == "" {
		targetURL, err = service.projectWebhookURL(ctx, trimmedProject, trimmedID)
		if err != nil {
			return nil, err
		}
	}

	params := &openapigenerated.TestWebhookParams{
		WebhookId: &webhookID32,
		Url:       &targetURL,
	}
	body := openapigenerated.TestWebhookJSONRequestBody{}

	response, err := service.client.TestWebhookWithResponse(ctx, trimmedProject, params, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to test project webhook", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode test project webhook response", err)
	}

	return payload, nil
}

func (service *Service) GetProjectWebhookStatistics(ctx context.Context, projectKey string, id string) (any, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}

	response, err := service.client.GetStatisticsWithResponse(ctx, trimmedProject, trimmedID, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get project webhook statistics", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode project webhook statistics response", err)
	}

	return payload, nil
}

func (service *Service) GetProjectWebhookStatisticsSummary(ctx context.Context, projectKey string, id string) (any, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}

	response, err := service.client.GetStatisticsSummaryWithResponse(ctx, trimmedProject, trimmedID)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get project webhook statistics summary", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	var payload any
	if err := json.Unmarshal(response.Body, &payload); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode project webhook statistics summary response", err)
	}

	return payload, nil
}

// projectWebhookURL reads a project webhook's stored url, so testing one does
// not require the caller to repeat what the server already holds.
func (service *Service) projectWebhookURL(ctx context.Context, projectKey string, id string) (string, error) {
	payload, err := service.GetProjectWebhook(ctx, projectKey, id)
	if err != nil {
		return "", err
	}

	webhook, ok := payload.(map[string]any)
	if !ok {
		return "", apperrors.New(apperrors.KindPermanent, "unexpected webhook payload shape", nil)
	}

	url, ok := webhook["url"].(string)
	if !ok || strings.TrimSpace(url) == "" {
		return "", apperrors.New(apperrors.KindNotFound, "webhook has no url to test; pass --url to test a specific endpoint", nil)
	}

	return url, nil
}
