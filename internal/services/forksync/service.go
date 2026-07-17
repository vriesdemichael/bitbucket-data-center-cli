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

	resp, err := s.client.GetStatus2WithResponse(ctx, proj, slug, nil)
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

func (s *Service) Synchronize(ctx context.Context, projectKey, repoSlug string) error {
	proj := strings.TrimSpace(projectKey)
	slug := strings.TrimSpace(repoSlug)
	if proj == "" || slug == "" {
		return apperrors.New(apperrors.KindValidation, "project key and repository slug are required", nil)
	}

	body := openapigenerated.SynchronizeJSONRequestBody{}

	resp, err := s.client.SynchronizeWithResponse(ctx, proj, slug, body)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to trigger fork synchronization", err)
	}
	return openapi.MapStatusError(resp.StatusCode(), resp.Body)
}
