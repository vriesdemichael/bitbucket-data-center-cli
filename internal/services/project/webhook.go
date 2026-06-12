package project

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
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

func (service *Service) CreateProjectWebhook(ctx context.Context, projectKey string, name string, url string, events []string, active bool) (any, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedName := strings.TrimSpace(name)
	trimmedURL := strings.TrimSpace(url)
	if trimmedName == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook name is required", nil)
	}
	if trimmedURL == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook url is required", nil)
	}

	cleanedEvents := make([]string, 0, len(events))
	for _, event := range events {
		if trimmedEvent := strings.TrimSpace(event); trimmedEvent != "" {
			cleanedEvents = append(cleanedEvents, trimmedEvent)
		}
	}
	if len(cleanedEvents) == 0 {
		cleanedEvents = []string{"repo:refs_changed"}
	}

	body := openapigenerated.CreateWebhookJSONRequestBody{
		Name:   &trimmedName,
		Url:    &trimmedURL,
		Events: &cleanedEvents,
		Active: &active,
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

func (service *Service) UpdateProjectWebhook(ctx context.Context, projectKey string, id string, name string, url string, events []string, active *bool) (any, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "webhook id is required", nil)
	}

	body := openapigenerated.UpdateWebhookJSONRequestBody{}
	if strings.TrimSpace(name) != "" {
		n := strings.TrimSpace(name)
		body.Name = &n
	}
	if strings.TrimSpace(url) != "" {
		u := strings.TrimSpace(url)
		body.Url = &u
	}
	if len(events) > 0 {
		cleanedEvents := make([]string, 0, len(events))
		for _, event := range events {
			if trimmedEvent := strings.TrimSpace(event); trimmedEvent != "" {
				cleanedEvents = append(cleanedEvents, trimmedEvent)
			}
		}
		if len(cleanedEvents) > 0 {
			body.Events = &cleanedEvents
		}
	}
	if active != nil {
		body.Active = active
	}

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

func (service *Service) TestProjectWebhook(ctx context.Context, projectKey string, id string) (any, error) {
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

	params := &openapigenerated.TestWebhookParams{
		WebhookId: &webhookID32,
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
