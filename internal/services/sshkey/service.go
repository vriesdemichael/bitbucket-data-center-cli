package sshkey

import (
	"context"
	"fmt"
	"strings"
	"time"

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

func (s *Service) ListUserKeys(ctx context.Context, limit int) ([]openapigenerated.RestSshKey, error) {
	if limit <= 0 {
		limit = 25
	}
	start := float32(0)
	pageLimit := float32(limit)
	results := make([]openapigenerated.RestSshKey, 0)

	for {
		params := &openapigenerated.GetSshKeysParams{
			Start: &start,
			Limit: &pageLimit,
		}
		resp, err := s.client.GetSshKeysWithResponse(ctx, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list user SSH keys", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return nil, err
		}
		if resp.ApplicationjsonCharsetUTF8200 == nil || resp.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		results = append(results, *resp.ApplicationjsonCharsetUTF8200.Values...)

		if len(results) >= limit || resp.ApplicationjsonCharsetUTF8200.IsLastPage == nil || *resp.ApplicationjsonCharsetUTF8200.IsLastPage || resp.ApplicationjsonCharsetUTF8200.NextPageStart == nil {
			break
		}
		start = float32(*resp.ApplicationjsonCharsetUTF8200.NextPageStart)
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *Service) AddUserKey(ctx context.Context, label string, publicKeyText string) (openapigenerated.RestSshKey, error) {
	trimmedKey := strings.TrimSpace(publicKeyText)
	if trimmedKey == "" {
		return openapigenerated.RestSshKey{}, apperrors.New(apperrors.KindValidation, "SSH public key text is required", nil)
	}
	trimmedLabel := strings.TrimSpace(label)

	body := openapigenerated.AddSshKeyJSONRequestBody{
		Text: &trimmedKey,
	}
	if trimmedLabel != "" {
		body.Label = &trimmedLabel
	}

	resp, err := s.client.AddSshKeyWithResponse(ctx, nil, body)
	if err != nil {
		return openapigenerated.RestSshKey{}, apperrors.New(apperrors.KindTransient, "failed to add user SSH key", err)
	}
	if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
		return openapigenerated.RestSshKey{}, err
	}
	if resp.ApplicationjsonCharsetUTF8201 != nil {
		return *resp.ApplicationjsonCharsetUTF8201, nil
	}

	return openapigenerated.RestSshKey{}, apperrors.New(apperrors.KindInternal, "unexpected empty response adding user SSH key", nil)
}

func (s *Service) RemoveUserKey(ctx context.Context, keyId string) error {
	trimmedId := strings.TrimSpace(keyId)
	if trimmedId == "" {
		return apperrors.New(apperrors.KindValidation, "SSH key ID is required", nil)
	}

	resp, err := s.client.DeleteSshKeyWithResponse(ctx, trimmedId)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to remove user SSH key", err)
	}
	return openapi.MapStatusError(resp.StatusCode(), resp.Body)
}

func (s *Service) ListProjectKeys(ctx context.Context, projectKey string, limit int) ([]openapigenerated.RestSshAccessKey, error) {
	trimmedProj := strings.TrimSpace(projectKey)
	if trimmedProj == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	if limit <= 0 {
		limit = 25
	}
	start := float32(0)
	pageLimit := float32(limit)
	results := make([]openapigenerated.RestSshAccessKey, 0)

	for {
		params := &openapigenerated.GetSshKeysForProjectParams{
			Start: &start,
			Limit: &pageLimit,
		}
		resp, err := s.client.GetSshKeysForProjectWithResponse(ctx, trimmedProj, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list project SSH keys", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return nil, err
		}
		if resp.ApplicationjsonCharsetUTF8200 == nil || resp.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		results = append(results, *resp.ApplicationjsonCharsetUTF8200.Values...)

		if len(results) >= limit || resp.ApplicationjsonCharsetUTF8200.IsLastPage == nil || *resp.ApplicationjsonCharsetUTF8200.IsLastPage || resp.ApplicationjsonCharsetUTF8200.NextPageStart == nil {
			break
		}
		start = float32(*resp.ApplicationjsonCharsetUTF8200.NextPageStart)
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *Service) AddProjectKey(ctx context.Context, projectKey string, label string, publicKeyText string, permission string) (openapigenerated.RestSshAccessKey, error) {
	trimmedProj := strings.TrimSpace(projectKey)
	if trimmedProj == "" {
		return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	trimmedKey := strings.TrimSpace(publicKeyText)
	if trimmedKey == "" {
		return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindValidation, "SSH public key text is required", nil)
	}
	trimmedLabel := strings.TrimSpace(label)

	permVal := openapigenerated.RestSshAccessKeyPermission(strings.ToUpper(strings.TrimSpace(permission)))
	if permVal == "" {
		permVal = openapigenerated.RestSshAccessKeyPermissionPROJECTREAD
	}
	if permVal != openapigenerated.RestSshAccessKeyPermissionPROJECTREAD && permVal != openapigenerated.RestSshAccessKeyPermissionPROJECTWRITE {
		return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid project SSH key permission %q, must be PROJECT_READ or PROJECT_WRITE", permVal), nil)
	}

	body := openapigenerated.RestSshAccessKey{
		Permission: &permVal,
		Key: &struct {
			AlgorithmType     *string    `json:"algorithmType,omitempty"`
			BitLength         *int32     `json:"bitLength,omitempty"`
			CreatedDate       *time.Time `json:"createdDate,omitempty"`
			ExpiryDays        *int32     `json:"expiryDays,omitempty"`
			Fingerprint       *string    `json:"fingerprint,omitempty"`
			Id                *int32     `json:"id,omitempty"`
			Label             *string    `json:"label,omitempty"`
			LastAuthenticated *string    `json:"lastAuthenticated,omitempty"`
			Text              *string    `json:"text,omitempty"`
			Warning           *string    `json:"warning,omitempty"`
		}{
			Text: &trimmedKey,
		},
	}
	if trimmedLabel != "" {
		body.Key.Label = &trimmedLabel
	}

	resp, err := s.client.AddForProjectWithResponse(ctx, trimmedProj, body)
	if err != nil {
		return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindTransient, "failed to add project SSH key", err)
	}
	if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
		return openapigenerated.RestSshAccessKey{}, err
	}
	if resp.ApplicationjsonCharsetUTF8201 != nil {
		return *resp.ApplicationjsonCharsetUTF8201, nil
	}

	return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindInternal, "unexpected empty response adding project SSH key", nil)
}

func (s *Service) RemoveProjectKey(ctx context.Context, projectKey string, keyId string) error {
	trimmedProj := strings.TrimSpace(projectKey)
	if trimmedProj == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	trimmedId := strings.TrimSpace(keyId)
	if trimmedId == "" {
		return apperrors.New(apperrors.KindValidation, "SSH key ID is required", nil)
	}

	resp, err := s.client.RevokeForProjectWithResponse(ctx, trimmedProj, trimmedId)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to remove project SSH key", err)
	}
	return openapi.MapStatusError(resp.StatusCode(), resp.Body)
}

func (s *Service) ListRepoKeys(ctx context.Context, projectKey string, repoSlug string, limit int) ([]openapigenerated.RestSshAccessKey, error) {
	trimmedProj := strings.TrimSpace(projectKey)
	if trimmedProj == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	trimmedRepo := strings.TrimSpace(repoSlug)
	if trimmedRepo == "" {
		return nil, apperrors.New(apperrors.KindValidation, "repository slug is required", nil)
	}
	if limit <= 0 {
		limit = 25
	}
	start := float32(0)
	pageLimit := float32(limit)
	results := make([]openapigenerated.RestSshAccessKey, 0)

	for {
		params := &openapigenerated.GetForRepository1Params{
			Start: &start,
			Limit: &pageLimit,
		}
		resp, err := s.client.GetForRepository1WithResponse(ctx, trimmedProj, trimmedRepo, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list repository SSH keys", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return nil, err
		}
		if resp.ApplicationjsonCharsetUTF8200 == nil || resp.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		results = append(results, *resp.ApplicationjsonCharsetUTF8200.Values...)

		if len(results) >= limit || resp.ApplicationjsonCharsetUTF8200.IsLastPage == nil || *resp.ApplicationjsonCharsetUTF8200.IsLastPage || resp.ApplicationjsonCharsetUTF8200.NextPageStart == nil {
			break
		}
		start = float32(*resp.ApplicationjsonCharsetUTF8200.NextPageStart)
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *Service) AddRepoKey(ctx context.Context, projectKey string, repoSlug string, label string, publicKeyText string, permission string) (openapigenerated.RestSshAccessKey, error) {
	trimmedProj := strings.TrimSpace(projectKey)
	if trimmedProj == "" {
		return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	trimmedRepo := strings.TrimSpace(repoSlug)
	if trimmedRepo == "" {
		return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindValidation, "repository slug is required", nil)
	}
	trimmedKey := strings.TrimSpace(publicKeyText)
	if trimmedKey == "" {
		return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindValidation, "SSH public key text is required", nil)
	}
	trimmedLabel := strings.TrimSpace(label)

	permVal := openapigenerated.RestSshAccessKeyPermission(strings.ToUpper(strings.TrimSpace(permission)))
	if permVal == "" {
		permVal = openapigenerated.RestSshAccessKeyPermissionREPOREAD
	}
	if permVal != openapigenerated.RestSshAccessKeyPermissionREPOREAD && permVal != openapigenerated.RestSshAccessKeyPermissionREPOWRITE {
		return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid repository SSH key permission %q, must be REPO_READ or REPO_WRITE", permVal), nil)
	}

	body := openapigenerated.RestSshAccessKey{
		Permission: &permVal,
		Key: &struct {
			AlgorithmType     *string    `json:"algorithmType,omitempty"`
			BitLength         *int32     `json:"bitLength,omitempty"`
			CreatedDate       *time.Time `json:"createdDate,omitempty"`
			ExpiryDays        *int32     `json:"expiryDays,omitempty"`
			Fingerprint       *string    `json:"fingerprint,omitempty"`
			Id                *int32     `json:"id,omitempty"`
			Label             *string    `json:"label,omitempty"`
			LastAuthenticated *string    `json:"lastAuthenticated,omitempty"`
			Text              *string    `json:"text,omitempty"`
			Warning           *string    `json:"warning,omitempty"`
		}{
			Text: &trimmedKey,
		},
	}
	if trimmedLabel != "" {
		body.Key.Label = &trimmedLabel
	}

	resp, err := s.client.AddForRepositoryWithResponse(ctx, trimmedProj, trimmedRepo, body)
	if err != nil {
		return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindTransient, "failed to add repository SSH key", err)
	}
	if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
		return openapigenerated.RestSshAccessKey{}, err
	}
	if resp.ApplicationjsonCharsetUTF8201 != nil {
		return *resp.ApplicationjsonCharsetUTF8201, nil
	}

	return openapigenerated.RestSshAccessKey{}, apperrors.New(apperrors.KindInternal, "unexpected empty response adding repository SSH key", nil)
}

func (s *Service) RemoveRepoKey(ctx context.Context, projectKey string, repoSlug string, keyId string) error {
	trimmedProj := strings.TrimSpace(projectKey)
	if trimmedProj == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}
	trimmedRepo := strings.TrimSpace(repoSlug)
	if trimmedRepo == "" {
		return apperrors.New(apperrors.KindValidation, "repository slug is required", nil)
	}
	trimmedId := strings.TrimSpace(keyId)
	if trimmedId == "" {
		return apperrors.New(apperrors.KindValidation, "SSH key ID is required", nil)
	}

	resp, err := s.client.RevokeForRepositoryWithResponse(ctx, trimmedProj, trimmedRepo, trimmedId)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to remove repository SSH key", err)
	}
	return openapi.MapStatusError(resp.StatusCode(), resp.Body)
}
