package project

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
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
		Description: trimmedDesc,
	}

	// Both matchers are mandatory: the API rejects a default task that leaves
	// either one out, so an unset flag becomes an any-ref matcher.
	body.SourceMatcher = openapi.NewDefaultTaskSourceMatcher(sourceRef)
	body.TargetMatcher = openapi.NewDefaultTaskTargetMatcher(targetRef)

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

	// Both matchers are mandatory: the API rejects a default task that leaves
	// either one out. On create an unset flag rightly becomes an any-ref
	// matcher, and that reasoning was carried into update, where it is wrong:
	// there an unset flag means "leave it alone", not "widen it to everything".
	//
	// The effect was silent. `default-task update --description` on a task
	// scoped to release/* -> main reset both matchers to the any-ref matcher,
	// so a checklist meant for one branch pair started applying to every pull
	// request in the project.
	if sourceRef == nil || targetRef == nil {
		current, err := service.findDefaultTask(ctx, trimmedProject, trimmedTaskID)
		if err != nil {
			return nil, err
		}
		if sourceRef == nil {
			sourceRef = matcherRef(current.SourceMatcher)
		}
		if targetRef == nil {
			targetRef = matcherRef(current.TargetMatcher)
		}
	}

	body := openapigenerated.UpdateDefaultTaskJSONRequestBody{
		Description: trimmedDesc,
	}

	body.SourceMatcher = openapi.NewDefaultTaskSourceMatcher(sourceRef)
	body.TargetMatcher = openapi.NewDefaultTaskTargetMatcher(targetRef)

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

// findDefaultTask returns one project default task by id. There is no
// single-task endpoint, so the listing is the only way to read what a task
// currently says.
func (service *Service) findDefaultTask(ctx context.Context, projectKey, taskID string) (DefaultTask, error) {
	tasks, err := service.ListDefaultTasks(ctx, projectKey)
	if err != nil {
		return DefaultTask{}, err
	}

	for _, task := range tasks {
		if task.Id != nil && strconv.FormatInt(*task.Id, 10) == taskID {
			return task, nil
		}
	}

	return DefaultTask{}, apperrors.New(apperrors.KindNotFound,
		"default task "+taskID+" does not exist on project "+projectKey, nil)
}

// matcherRef turns a matcher read back from the server into the ref a caller
// would have typed, so it can be sent again unchanged. Nil means any ref, which
// is what the matcher constructors already take.
func matcherRef(matcher *DefaultTaskMatcher) *string {
	if matcher == nil || matcher.Id == nil || openapi.IsAnyRefMatcherID(*matcher.Id) {
		return nil
	}

	return matcher.Id
}
