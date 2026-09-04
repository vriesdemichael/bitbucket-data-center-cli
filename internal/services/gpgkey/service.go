package gpgkey

import (
	"context"
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

func (s *Service) ListGpgKeys(ctx context.Context, limit int) ([]openapigenerated.RestGpgKey, error) {
	if limit <= 0 {
		limit = 25
	}

	return openapi.PageThrough(ctx, 0, limit,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestGpgKey], error) {
			startValue, limitValue := float32(start), float32(limit)
			resp, err := s.client.GetKeysForUserWithResponse(ctx, &openapigenerated.GetKeysForUserParams{
				Start: &startValue,
				Limit: &limitValue,
			})
			if err != nil {
				return openapi.Page[openapigenerated.RestGpgKey]{}, apperrors.New(apperrors.KindTransient, "failed to list user GPG keys", err)
			}
			if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
				return openapi.Page[openapigenerated.RestGpgKey]{}, err
			}

			page := resp.JSON200
			if page == nil || page.Values == nil {
				return openapi.Page[openapigenerated.RestGpgKey]{}, nil
			}

			return openapi.Page[openapigenerated.RestGpgKey]{
				Values:        *page.Values,
				IsLastPage:    page.IsLastPage,
				NextPageStart: page.NextPageStart,
			}, nil
		})
}

// AddGpgKey submits an armored public key block and returns every key the server
// took from it.
//
// The result is a list because a block can carry more than one key, and the
// server answers with all of them even in the ordinary single-key case.
func (s *Service) AddGpgKey(ctx context.Context, keyText string) ([]openapigenerated.RestGpgKey, error) {
	trimmedKey := strings.TrimSpace(keyText)
	if trimmedKey == "" {
		return nil, apperrors.New(apperrors.KindValidation, "GPG public key text is required", nil)
	}

	body := openapigenerated.AddKeyJSONRequestBody{
		Text: &trimmedKey,
	}

	resp, err := s.client.AddKeyWithResponse(ctx, nil, body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to add GPG key", err)
	}
	if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}
	if resp.JSON200 != nil {
		return *resp.JSON200, nil
	}

	return nil, apperrors.New(apperrors.KindInternal, "unexpected empty response adding GPG key", nil)
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
