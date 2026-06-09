package token

import (
	"context"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type ScopeType string

const (
	ScopeUser    ScopeType = "user"
	ScopeProject ScopeType = "project"
	ScopeRepo    ScopeType = "repo"
)

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func parseRepoTarget(target string) (string, string, error) {
	parts := strings.Split(target, "/")
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", apperrors.New(apperrors.KindValidation, "repository target must be in format 'projectKey/repositorySlug'", nil)
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), nil
}

func (s *Service) List(ctx context.Context, scope ScopeType, target string, limit int) ([]openapigenerated.RestAccessToken, error) {
	if limit <= 0 {
		limit = 25
	}
	start := float32(0)
	pageLimit := float32(limit)
	results := make([]openapigenerated.RestAccessToken, 0)

	for {
		var values *[]openapigenerated.RestAccessToken
		var isLastPage *bool
		var nextPageStart *int32

		switch scope {
		case ScopeUser:
			trimmedUser := strings.TrimSpace(target)
			if trimmedUser == "" {
				return nil, apperrors.New(apperrors.KindValidation, "user slug is required for user scope", nil)
			}
			params := &openapigenerated.GetAllAccessTokens2Params{
				Start: &start,
				Limit: &pageLimit,
			}
			resp, err := s.client.GetAllAccessTokens2WithResponse(ctx, trimmedUser, params)
			if err != nil {
				return nil, apperrors.New(apperrors.KindTransient, "failed to list user tokens", err)
			}
			if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
				return nil, err
			}
			if resp.ApplicationjsonCharsetUTF8200 != nil {
				values = resp.ApplicationjsonCharsetUTF8200.Values
				isLastPage = resp.ApplicationjsonCharsetUTF8200.IsLastPage
				nextPageStart = resp.ApplicationjsonCharsetUTF8200.NextPageStart
			}

		case ScopeProject:
			trimmedProj := strings.TrimSpace(target)
			if trimmedProj == "" {
				return nil, apperrors.New(apperrors.KindValidation, "project key is required for project scope", nil)
			}
			params := &openapigenerated.GetAllAccessTokensParams{
				Start: &start,
				Limit: &pageLimit,
			}
			resp, err := s.client.GetAllAccessTokensWithResponse(ctx, trimmedProj, params)
			if err != nil {
				return nil, apperrors.New(apperrors.KindTransient, "failed to list project tokens", err)
			}
			if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
				return nil, err
			}
			if resp.ApplicationjsonCharsetUTF8200 != nil {
				values = resp.ApplicationjsonCharsetUTF8200.Values
				isLastPage = resp.ApplicationjsonCharsetUTF8200.IsLastPage
				nextPageStart = resp.ApplicationjsonCharsetUTF8200.NextPageStart
			}

		case ScopeRepo:
			proj, repo, err := parseRepoTarget(target)
			if err != nil {
				return nil, err
			}
			params := &openapigenerated.GetAllAccessTokens1Params{
				Start: &start,
				Limit: &pageLimit,
			}
			resp, err := s.client.GetAllAccessTokens1WithResponse(ctx, proj, repo, params)
			if err != nil {
				return nil, apperrors.New(apperrors.KindTransient, "failed to list repository tokens", err)
			}
			if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
				return nil, err
			}
			if resp.ApplicationjsonCharsetUTF8200 != nil {
				values = resp.ApplicationjsonCharsetUTF8200.Values
				isLastPage = resp.ApplicationjsonCharsetUTF8200.IsLastPage
				nextPageStart = resp.ApplicationjsonCharsetUTF8200.NextPageStart
			}

		default:
			return nil, apperrors.New(apperrors.KindValidation, "invalid scope type", nil)
		}

		if values != nil {
			results = append(results, *values...)
		}

		if len(results) >= limit || isLastPage == nil || *isLastPage || nextPageStart == nil {
			break
		}
		start = float32(*nextPageStart)
	}

	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

func (s *Service) Get(ctx context.Context, scope ScopeType, target string, id string) (openapigenerated.RestAccessToken, error) {
	trimmedId := strings.TrimSpace(id)
	if trimmedId == "" {
		return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindValidation, "token ID is required", nil)
	}

	switch scope {
	case ScopeUser:
		trimmedUser := strings.TrimSpace(target)
		if trimmedUser == "" {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindValidation, "user slug is required for user scope", nil)
		}
		resp, err := s.client.GetById2WithResponse(ctx, trimmedUser, trimmedId)
		if err != nil {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindTransient, "failed to get user token", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return openapigenerated.RestAccessToken{}, err
		}
		if resp.ApplicationjsonCharsetUTF8200 != nil {
			return *resp.ApplicationjsonCharsetUTF8200, nil
		}

	case ScopeProject:
		trimmedProj := strings.TrimSpace(target)
		if trimmedProj == "" {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindValidation, "project key is required for project scope", nil)
		}
		resp, err := s.client.GetByIdWithResponse(ctx, trimmedProj, trimmedId)
		if err != nil {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindTransient, "failed to get project token", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return openapigenerated.RestAccessToken{}, err
		}
		if resp.ApplicationjsonCharsetUTF8200 != nil {
			return *resp.ApplicationjsonCharsetUTF8200, nil
		}

	case ScopeRepo:
		proj, repo, err := parseRepoTarget(target)
		if err != nil {
			return openapigenerated.RestAccessToken{}, err
		}
		resp, err := s.client.GetById1WithResponse(ctx, proj, repo, trimmedId)
		if err != nil {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindTransient, "failed to get repository token", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return openapigenerated.RestAccessToken{}, err
		}
		if resp.ApplicationjsonCharsetUTF8200 != nil {
			return *resp.ApplicationjsonCharsetUTF8200, nil
		}

	default:
		return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindValidation, "invalid scope type", nil)
	}

	return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindNotFound, "token not found", nil)
}

func (s *Service) Create(ctx context.Context, scope ScopeType, target string, name string, permissions []string, expiryDays int) (openapigenerated.RestRawAccessToken, error) {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return openapigenerated.RestRawAccessToken{}, apperrors.New(apperrors.KindValidation, "token name is required", nil)
	}

	body := openapigenerated.RestAccessTokenRequest{
		Name: &trimmedName,
	}
	if len(permissions) > 0 {
		body.Permissions = &permissions
	}
	if expiryDays > 0 {
		exp32 := int32(expiryDays)
		body.ExpiryDays = &exp32
	}

	switch scope {
	case ScopeUser:
		trimmedUser := strings.TrimSpace(target)
		if trimmedUser == "" {
			return openapigenerated.RestRawAccessToken{}, apperrors.New(apperrors.KindValidation, "user slug is required for user scope", nil)
		}
		resp, err := s.client.CreateAccessToken2WithResponse(ctx, trimmedUser, body)
		if err != nil {
			return openapigenerated.RestRawAccessToken{}, apperrors.New(apperrors.KindTransient, "failed to create user token", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return openapigenerated.RestRawAccessToken{}, err
		}
		if resp.ApplicationjsonCharsetUTF8200 != nil {
			return *resp.ApplicationjsonCharsetUTF8200, nil
		}

	case ScopeProject:
		trimmedProj := strings.TrimSpace(target)
		if trimmedProj == "" {
			return openapigenerated.RestRawAccessToken{}, apperrors.New(apperrors.KindValidation, "project key is required for project scope", nil)
		}
		resp, err := s.client.CreateAccessTokenWithResponse(ctx, trimmedProj, body)
		if err != nil {
			return openapigenerated.RestRawAccessToken{}, apperrors.New(apperrors.KindTransient, "failed to create project token", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return openapigenerated.RestRawAccessToken{}, err
		}
		if resp.ApplicationjsonCharsetUTF8200 != nil {
			return *resp.ApplicationjsonCharsetUTF8200, nil
		}

	case ScopeRepo:
		proj, repo, err := parseRepoTarget(target)
		if err != nil {
			return openapigenerated.RestRawAccessToken{}, err
		}
		resp, err := s.client.CreateAccessToken1WithResponse(ctx, proj, repo, body)
		if err != nil {
			return openapigenerated.RestRawAccessToken{}, apperrors.New(apperrors.KindTransient, "failed to create repository token", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return openapigenerated.RestRawAccessToken{}, err
		}
		if resp.ApplicationjsonCharsetUTF8200 != nil {
			return *resp.ApplicationjsonCharsetUTF8200, nil
		}

	default:
		return openapigenerated.RestRawAccessToken{}, apperrors.New(apperrors.KindValidation, "invalid scope type", nil)
	}

	return openapigenerated.RestRawAccessToken{}, apperrors.New(apperrors.KindInternal, "unexpected empty response during token creation", nil)
}

func (s *Service) Update(ctx context.Context, scope ScopeType, target string, id string, name string, permissions []string) (openapigenerated.RestAccessToken, error) {
	trimmedId := strings.TrimSpace(id)
	if trimmedId == "" {
		return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindValidation, "token ID is required", nil)
	}

	body := openapigenerated.RestAccessTokenRequest{}
	if trimmedName := strings.TrimSpace(name); trimmedName != "" {
		body.Name = &trimmedName
	}
	if len(permissions) > 0 {
		body.Permissions = &permissions
	}

	switch scope {
	case ScopeUser:
		trimmedUser := strings.TrimSpace(target)
		if trimmedUser == "" {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindValidation, "user slug is required for user scope", nil)
		}
		resp, err := s.client.UpdateAccessToken2WithResponse(ctx, trimmedUser, trimmedId, body)
		if err != nil {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindTransient, "failed to update user token", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return openapigenerated.RestAccessToken{}, err
		}
		if resp.ApplicationjsonCharsetUTF8200 != nil {
			return *resp.ApplicationjsonCharsetUTF8200, nil
		}

	case ScopeProject:
		trimmedProj := strings.TrimSpace(target)
		if trimmedProj == "" {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindValidation, "project key is required for project scope", nil)
		}
		resp, err := s.client.UpdateAccessTokenWithResponse(ctx, trimmedProj, trimmedId, body)
		if err != nil {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindTransient, "failed to update project token", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return openapigenerated.RestAccessToken{}, err
		}
		if resp.ApplicationjsonCharsetUTF8200 != nil {
			return *resp.ApplicationjsonCharsetUTF8200, nil
		}

	case ScopeRepo:
		proj, repo, err := parseRepoTarget(target)
		if err != nil {
			return openapigenerated.RestAccessToken{}, err
		}
		resp, err := s.client.UpdateAccessToken1WithResponse(ctx, proj, repo, trimmedId, body)
		if err != nil {
			return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindTransient, "failed to update repository token", err)
		}
		if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
			return openapigenerated.RestAccessToken{}, err
		}
		if resp.ApplicationjsonCharsetUTF8200 != nil {
			return *resp.ApplicationjsonCharsetUTF8200, nil
		}

	default:
		return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindValidation, "invalid scope type", nil)
	}

	return openapigenerated.RestAccessToken{}, apperrors.New(apperrors.KindInternal, "unexpected empty response during token update", nil)
}

func (s *Service) Revoke(ctx context.Context, scope ScopeType, target string, id string) error {
	trimmedId := strings.TrimSpace(id)
	if trimmedId == "" {
		return apperrors.New(apperrors.KindValidation, "token ID is required", nil)
	}

	switch scope {
	case ScopeUser:
		trimmedUser := strings.TrimSpace(target)
		if trimmedUser == "" {
			return apperrors.New(apperrors.KindValidation, "user slug is required for user scope", nil)
		}
		resp, err := s.client.DeleteById2WithResponse(ctx, trimmedUser, trimmedId)
		if err != nil {
			return apperrors.New(apperrors.KindTransient, "failed to revoke user token", err)
		}
		return openapi.MapStatusError(resp.StatusCode(), resp.Body)

	case ScopeProject:
		trimmedProj := strings.TrimSpace(target)
		if trimmedProj == "" {
			return apperrors.New(apperrors.KindValidation, "project key is required for project scope", nil)
		}
		resp, err := s.client.DeleteByIdWithResponse(ctx, trimmedProj, trimmedId)
		if err != nil {
			return apperrors.New(apperrors.KindTransient, "failed to revoke project token", err)
		}
		return openapi.MapStatusError(resp.StatusCode(), resp.Body)

	case ScopeRepo:
		proj, repo, err := parseRepoTarget(target)
		if err != nil {
			return err
		}
		resp, err := s.client.DeleteById1WithResponse(ctx, proj, repo, trimmedId)
		if err != nil {
			return apperrors.New(apperrors.KindTransient, "failed to revoke repository token", err)
		}
		return openapi.MapStatusError(resp.StatusCode(), resp.Body)

	default:
		return apperrors.New(apperrors.KindValidation, "invalid scope type", nil)
	}
}
