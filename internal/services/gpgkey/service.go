package gpgkey

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

func (s *Service) ListGpgKeys(ctx context.Context, limit int) ([]openapigenerated.RestGpgKey, error) {
	if limit <= 0 {
		limit = 25
	}
	start := float32(0)
	pageLimit := float32(limit)
	results := make([]openapigenerated.RestGpgKey, 0)

	for {
		params := &openapigenerated.GetKeysForUserParams{
			Start: &start,
			Limit: &pageLimit,
		}
		resp, err := s.client.GetKeysForUserWithResponse(ctx, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list user GPG keys", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return nil, err
		}
		if resp.JSON200 == nil || resp.JSON200.Values == nil {
			break
		}

		results = append(results, *resp.JSON200.Values...)

		if len(results) >= limit || resp.JSON200.IsLastPage == nil || *resp.JSON200.IsLastPage || resp.JSON200.NextPageStart == nil {
			break
		}
		start = float32(*resp.JSON200.NextPageStart)
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *Service) AddGpgKey(ctx context.Context, keyText string) (openapigenerated.RestGpgKey, error) {
	trimmedKey := strings.TrimSpace(keyText)
	if trimmedKey == "" {
		return openapigenerated.RestGpgKey{}, apperrors.New(apperrors.KindValidation, "GPG public key text is required", nil)
	}

	body := openapigenerated.AddKeyJSONRequestBody{
		Text: &trimmedKey,
	}

	resp, err := s.client.AddKeyWithResponse(ctx, nil, body)
	if err != nil {
		return openapigenerated.RestGpgKey{}, apperrors.New(apperrors.KindTransient, "failed to add GPG key", err)
	}
	if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
		return openapigenerated.RestGpgKey{}, err
	}
	if resp.JSON200 != nil {
		return *resp.JSON200, nil
	}

	return openapigenerated.RestGpgKey{}, apperrors.New(apperrors.KindInternal, "unexpected empty response adding GPG key", nil)
}

func (s *Service) RemoveGpgKey(ctx context.Context, fingerprintOrId string) error {
	trimmedId := strings.TrimSpace(fingerprintOrId)
	if trimmedId == "" {
		return apperrors.New(apperrors.KindValidation, "GPG key ID or fingerprint is required", nil)
	}

	resp, err := s.client.DeleteKeyWithResponse(ctx, trimmedId)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to remove GPG key", err)
	}
	return openapi.MapStatusError(resp.StatusCode(), resp.Body)
}

func (s *Service) ClearGpgKeys(ctx context.Context) error {
	resp, err := s.client.DeleteForUserWithResponse(ctx, nil)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to clear GPG keys", err)
	}
	return openapi.MapStatusError(resp.StatusCode(), resp.Body)
}
