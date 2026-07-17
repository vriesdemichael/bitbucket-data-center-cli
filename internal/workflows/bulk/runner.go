package bulk

import (
	"context"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	qualityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/quality"
	reposettings "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reposettings"
)

type ServiceRunner struct {
	repoSettings *reposettings.Service
	quality      *qualityservice.Service
}

func NewServiceRunner(repoSettings *reposettings.Service, quality *qualityservice.Service) *ServiceRunner {
	return &ServiceRunner{repoSettings: repoSettings, quality: quality}
}

func (runner *ServiceRunner) Run(ctx context.Context, repo RepositoryTarget, operation OperationSpec) (any, error) {
	if runner == nil {
		return nil, apperrors.New(apperrors.KindInternal, "bulk runner is not configured", nil)
	}

	repoRef := reposettings.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}
	qualityRepoRef := qualityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}

	switch operation.Type {
	case OperationRepoPermissionUserGrant:
		if runner.repoSettings == nil {
			return nil, apperrors.New(apperrors.KindInternal, "repo settings service is not configured", nil)
		}
		if err := runner.repoSettings.GrantRepositoryUserPermission(ctx, repoRef, operation.Username, operation.Permission); err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "username": operation.Username, "permission": operation.Permission}, nil
	case OperationRepoPermissionGroupGrant:
		if runner.repoSettings == nil {
			return nil, apperrors.New(apperrors.KindInternal, "repo settings service is not configured", nil)
		}
		if err := runner.repoSettings.GrantRepositoryGroupPermission(ctx, repoRef, operation.Group, operation.Permission); err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "group": operation.Group, "permission": operation.Permission}, nil
	case OperationRepoWebhookCreate:
		if runner.repoSettings == nil {
			return nil, apperrors.New(apperrors.KindInternal, "repo settings service is not configured", nil)
		}
		active := true
		if operation.Active != nil {
			active = *operation.Active
		}
		payload, err := runner.repoSettings.CreateRepositoryWebhook(ctx, repoRef, reposettings.WebhookCreateInput{
			Name:   operation.Name,
			URL:    operation.URL,
			Events: operation.Events,
			Active: active,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"status": "ok", "webhook": payload}, nil
	case OperationRepoPullRequestRequiredAllTasksComplete:
		if runner.repoSettings == nil {
			return nil, apperrors.New(apperrors.KindInternal, "repo settings service is not configured", nil)
		}
		if operation.RequiredAllTasksComplete == nil {
			return nil, apperrors.New(apperrors.KindValidation, "requiredAllTasksComplete is required", nil)
		}
		return runner.repoSettings.UpdateRepositoryPullRequestRequiredAllTasks(ctx, repoRef, *operation.RequiredAllTasksComplete)
	case OperationRepoPullRequestRequiredApproversCount:
		if runner.repoSettings == nil {
			return nil, apperrors.New(apperrors.KindInternal, "repo settings service is not configured", nil)
		}
		if operation.Count == nil {
			return nil, apperrors.New(apperrors.KindValidation, "count is required", nil)
		}
		return runner.repoSettings.UpdateRepositoryPullRequestRequiredApproversCount(ctx, repoRef, *operation.Count)
	case OperationBuildRequiredCreate:
		if runner.quality == nil {
			return nil, apperrors.New(apperrors.KindInternal, "quality service is not configured", nil)
		}
		return runner.quality.CreateRequiredBuildCheck(ctx, qualityRepoRef, operation.Payload)
	case OperationRepoSettingsAutoMerge:
		if runner.repoSettings == nil {
			return nil, apperrors.New(apperrors.KindInternal, "repo settings service is not configured", nil)
		}
		if operation.Enabled == nil {
			return nil, apperrors.New(apperrors.KindValidation, "enabled is required", nil)
		}
		return runner.repoSettings.UpdateRepositoryAutoMergeSettings(ctx, repoRef, *operation.Enabled)
	case OperationRepoSettingsAutoDecline:
		if runner.repoSettings == nil {
			return nil, apperrors.New(apperrors.KindInternal, "repo settings service is not configured", nil)
		}
		if operation.Enabled == nil {
			return nil, apperrors.New(apperrors.KindValidation, "enabled is required", nil)
		}
		var inactivityWeeks int32
		if *operation.Enabled {
			if operation.InactivityWeeks == nil {
				return nil, apperrors.New(apperrors.KindValidation, "inactivityWeeks is required when enabled is true", nil)
			}
			inactivityWeeks = int32(*operation.InactivityWeeks)
		}
		return runner.repoSettings.UpdateRepositoryAutoDeclineSettings(ctx, repoRef, *operation.Enabled, inactivityWeeks)
	case OperationRepoDefaultTaskCreate:
		if runner.repoSettings == nil {
			return nil, apperrors.New(apperrors.KindInternal, "repo settings service is not configured", nil)
		}
		if operation.Description == nil {
			return nil, apperrors.New(apperrors.KindValidation, "description is required", nil)
		}
		return runner.repoSettings.AddDefaultTask(ctx, repoRef, *operation.Description, operation.SourceRef, operation.TargetRef)
	default:
		return nil, apperrors.New(apperrors.KindNotImplemented, "bulk operation type is not implemented", nil)
	}
}
