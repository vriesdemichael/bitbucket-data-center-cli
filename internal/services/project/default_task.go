package project

import (
	"context"
	"encoding/json"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

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

func (service *Service) ListDefaultTasks(ctx context.Context, projectKey string) ([]DefaultTask, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	response, err := service.client.GetDefaultTasksWithResponse(ctx, trimmedProject, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list project default tasks", err)
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

func (service *Service) AddDefaultTask(ctx context.Context, projectKey string, description string, sourceRef *string, targetRef *string) (*DefaultTask, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedDesc := strings.TrimSpace(description)
	if trimmedDesc == "" {
		return nil, apperrors.New(apperrors.KindValidation, "description is required", nil)
	}

	body := openapigenerated.AddDefaultTaskJSONRequestBody{
		Description: &trimmedDesc,
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

	response, err := service.client.AddDefaultTaskWithResponse(ctx, trimmedProject, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to add project default task", err)
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

func (service *Service) UpdateDefaultTask(ctx context.Context, projectKey string, taskId string, description string, sourceRef *string, targetRef *string) (*DefaultTask, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedTaskID := strings.TrimSpace(taskId)
	if trimmedTaskID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "task id is required", nil)
	}

	trimmedDesc := strings.TrimSpace(description)
	if trimmedDesc == "" {
		return nil, apperrors.New(apperrors.KindValidation, "description is required", nil)
	}

	body := openapigenerated.UpdateDefaultTaskJSONRequestBody{
		Description: &trimmedDesc,
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

	response, err := service.client.UpdateDefaultTaskWithResponse(ctx, trimmedProject, trimmedTaskID, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to update project default task", err)
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

func (service *Service) DeleteDefaultTask(ctx context.Context, projectKey string, taskId string) error {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedTaskID := strings.TrimSpace(taskId)
	if trimmedTaskID == "" {
		return apperrors.New(apperrors.KindValidation, "task id is required", nil)
	}

	response, err := service.client.DeleteDefaultTaskWithResponse(ctx, trimmedProject, trimmedTaskID)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete project default task", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}
