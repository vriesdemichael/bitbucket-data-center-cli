package forksync

import (
	"context"
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

func (s *Service) GetSyncStatus(ctx context.Context, projectKey, repoSlug string) (*openapigenerated.RestRefSyncStatus, error) {
	proj := strings.TrimSpace(projectKey)
	slug := strings.TrimSpace(repoSlug)
	if proj == "" || slug == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key and repository slug are required", nil)
	}

	// GetStatus, not GetStatus2: the Bitbucket 10.2 spec reorders the two
	// colliding getStatus operations, so the fork-sync endpoint
	// (/sync/latest/projects/{key}/repos/{slug}) is now the unsuffixed one.
	// GetStatus2 is /tsv/latest/status.
	resp, err := s.client.GetStatusWithResponse(ctx, proj, slug, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to get fork synchronization status", err)
	}
	if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	if resp.ApplicationjsonCharsetUTF8200 != nil {
		return resp.ApplicationjsonCharsetUTF8200, nil
	}

	return nil, apperrors.New(apperrors.KindInternal, "unexpected empty response getting synchronization status", nil)
}

func (s *Service) SetEnabled(ctx context.Context, projectKey, repoSlug string, enabled bool) (*openapigenerated.RestRefSyncStatus, error) {
	proj := strings.TrimSpace(projectKey)
	slug := strings.TrimSpace(repoSlug)
	if proj == "" || slug == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key and repository slug are required", nil)
	}

	body := openapigenerated.SetEnabledJSONRequestBody{
		Enabled: &enabled,
	}

	resp, err := s.client.SetEnabledWithResponse(ctx, proj, slug, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to update fork synchronization settings", err)
	}
	if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	if resp.ApplicationjsonCharsetUTF8200 != nil {
		return resp.ApplicationjsonCharsetUTF8200, nil
	}

	return nil, apperrors.New(apperrors.KindInternal, "unexpected empty response setting synchronization status", nil)
}

// Synchronize triggers a manual synchronization of one ref.
//
// Both the ref and the action are required by the server even though the schema
// marks them optional: omitting either answers 500 with an unhandled exception
// rather than a validation error, so they are checked here instead.
func (s *Service) Synchronize(ctx context.Context, projectKey, repoSlug, refID, action string) error {
	proj := strings.TrimSpace(projectKey)
	slug := strings.TrimSpace(repoSlug)
	if proj == "" || slug == "" {
		return apperrors.New(apperrors.KindValidation, "project key and repository slug are required", nil)
	}
	trimmedRef := strings.TrimSpace(refID)
	if trimmedRef == "" {
		return apperrors.New(apperrors.KindValidation, "ref is required", nil)
	}
	normalizedAction, err := normalizeSyncAction(action)
	if err != nil {
		return err
	}

	body := openapigenerated.SynchronizeJSONRequestBody{
		RefId:  &trimmedRef,
		Action: &normalizedAction,
	}

	resp, err := s.client.SynchronizeWithResponse(ctx, proj, slug, body)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to trigger fork synchronization", err)
	}
	return openapi.MapStatusError(resp.StatusCode(), resp.Body)
}

func normalizeSyncAction(action string) (openapigenerated.RestRefSyncRequestAction, error) {
	switch strings.ToUpper(strings.TrimSpace(action)) {
	case "", "MERGE":
		return openapigenerated.RestRefSyncRequestAction("MERGE"), nil
	case "DISCARD":
		return openapigenerated.RestRefSyncRequestAction("DISCARD"), nil
	case "REBASE":
		return openapigenerated.RestRefSyncRequestAction("REBASE"), nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "action must be one of MERGE, DISCARD, REBASE", nil)
	}
}
