package project

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type RestrictionListOptions struct {
	Limit       int
	Type        string
	MatcherType string
	MatcherID   string
}

type RestrictionUpsertInput struct {
	Type           string
	MatcherID      string
	MatcherType    string
	MatcherDisplay string
	Users          []string
	Groups         []string
	AccessKeyIDs   []int32
}

func (service *Service) ListRestrictions(ctx context.Context, projectKey string, options RestrictionListOptions) ([]openapigenerated.RestRefRestriction, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return nil, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	if options.Limit <= 0 {
		options.Limit = 1000
	}

	start := float32(0)
	pageLimit := float32(25)
	results := make([]openapigenerated.RestRefRestriction, 0)

	for {
		params := &openapigenerated.GetRestrictionsParams{Start: &start, Limit: &pageLimit}

		if options.Type != "" {
			t, err := normalizeProjectRestrictionType(options.Type)
			if err != nil {
				return nil, err
			}
			params.Type = &t
		}

		if options.MatcherType != "" {
			m, err := normalizeProjectRestrictionMatcherType(options.MatcherType)
			if err != nil {
				return nil, err
			}
			params.MatcherType = &m
		}

		if options.MatcherID != "" {
			params.MatcherId = &options.MatcherID
		}

		response, err := service.client.GetRestrictionsWithResponse(ctx, trimmedProject, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list project branch restrictions", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}

		if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		results = append(results, (*response.ApplicationjsonCharsetUTF8200.Values)...)

		if len(results) >= options.Limit {
			results = results[:options.Limit]
			break
		}

		if response.ApplicationjsonCharsetUTF8200.IsLastPage != nil && *response.ApplicationjsonCharsetUTF8200.IsLastPage {
			break
		}
		if response.ApplicationjsonCharsetUTF8200.NextPageStart == nil {
			break
		}
		start = float32(*response.ApplicationjsonCharsetUTF8200.NextPageStart)
	}

	return results, nil
}

func (service *Service) GetRestriction(ctx context.Context, projectKey string, id string) (openapigenerated.RestRefRestriction, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindValidation, "restriction id is required", nil)
	}

	response, err := service.client.GetRestrictionWithResponse(ctx, trimmedProject, trimmedID)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindTransient, "failed to get project branch restriction", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestRefRestriction{}, nil
}

func (service *Service) CreateRestriction(ctx context.Context, projectKey string, input RestrictionUpsertInput) (openapigenerated.RestRefRestriction, error) {
	return service.upsertRestriction(ctx, projectKey, "", input)
}

func (service *Service) UpdateRestriction(ctx context.Context, projectKey string, id string, input RestrictionUpsertInput) (openapigenerated.RestRefRestriction, error) {
	return service.upsertRestriction(ctx, projectKey, id, input)
}

func (service *Service) upsertRestriction(ctx context.Context, projectKey string, id string, input RestrictionUpsertInput) (openapigenerated.RestRefRestriction, error) {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedUpdateID := strings.TrimSpace(id)
	if trimmedUpdateID != "" {
		// Like repositories, project-level restriction updates delete the existing restriction first
		if err := service.DeleteRestriction(ctx, trimmedProject, trimmedUpdateID); err != nil {
			return openapigenerated.RestRefRestriction{}, fmt.Errorf("failed to delete existing restriction for update: %w", err)
		}
	}

	trimmedType := strings.TrimSpace(input.Type)
	if trimmedType == "" {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindValidation, "restriction type is required", nil)
	}

	trimmedMatcherID := strings.TrimSpace(input.MatcherID)
	if trimmedMatcherID == "" {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindValidation, "matcher id is required", nil)
	}

	matcherType, err := normalizeProjectRestrictionRequestMatcherType(input.MatcherType)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	bodyEntry := openapigenerated.RestRestrictionRequest{Type: &trimmedType}
	bodyEntry.Matcher = &struct {
		DisplayId *string `json:"displayId,omitempty"`
		Id        *string `json:"id,omitempty"`
		Type      *struct {
			Id   *openapigenerated.RestRestrictionRequestMatcherTypeId `json:"id,omitempty"`
			Name *string                                               `json:"name,omitempty"`
		} `json:"type,omitempty"`
	}{
		Id: &trimmedMatcherID,
		Type: &struct {
			Id   *openapigenerated.RestRestrictionRequestMatcherTypeId `json:"id,omitempty"`
			Name *string                                               `json:"name,omitempty"`
		}{Id: &matcherType},
	}

	if trimmedMatcherID != "" && input.MatcherDisplay != "" {
		bodyEntry.Matcher.DisplayId = &input.MatcherDisplay
	}

	if len(input.Users) > 0 {
		users := make([]openapigenerated.RestApplicationUser, 0, len(input.Users))
		for _, name := range input.Users {
			if strings.TrimSpace(name) != "" {
				nameCopy := strings.TrimSpace(name)
				users = append(users, openapigenerated.RestApplicationUser{Name: &nameCopy})
			}
		}
		if len(users) > 0 {
			bodyEntry.Users = &users
		}
	}

	if len(input.Groups) > 0 {
		groups := make([]string, 0, len(input.Groups))
		for _, group := range input.Groups {
			if trimmed := strings.TrimSpace(group); trimmed != "" {
				groups = append(groups, trimmed)
			}
		}
		if len(groups) > 0 {
			bodyEntry.Groups = &groups
		}
	}

	if len(input.AccessKeyIDs) > 0 {
		keys := make([]openapigenerated.RestSshAccessKey, 0, len(input.AccessKeyIDs))
		for _, kid := range input.AccessKeyIDs {
			kidCopy := kid
			keys = append(keys, openapigenerated.RestSshAccessKey{Key: &struct {
				AlgorithmType     *string    "json:\"algorithmType,omitempty\""
				BitLength         *int32     "json:\"bitLength,omitempty\""
				CreatedDate       *time.Time "json:\"createdDate,omitempty\""
				ExpiryDays        *int32     "json:\"expiryDays,omitempty\""
				Fingerprint       *string    "json:\"fingerprint,omitempty\""
				Id                *int32     "json:\"id,omitempty\""
				Label             *string    "json:\"label,omitempty\""
				LastAuthenticated *string    "json:\"lastAuthenticated,omitempty\""
				Text              *string    "json:\"text,omitempty\""
				Warning           *string    "json:\"warning,omitempty\""
			}{Id: &kidCopy}})
		}
		if len(keys) > 0 {
			bodyEntry.AccessKeys = &keys
		}
	}

	requestBody := openapigenerated.CreateRestrictionsApplicationVndAtlBitbucketBulkPlusJSONRequestBody{bodyEntry}

	client, ok := service.client.ClientInterface.(*openapigenerated.Client)
	if !ok {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindInternal, "failed to initialize project branch restriction request client", nil)
	}

	rawResponse, err := client.CreateRestrictionsWithApplicationVndAtlBitbucketBulkPlusJSONBody(ctx, trimmedProject, requestBody)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindTransient, "failed to upsert project branch restriction", err)
	}
	defer rawResponse.Body.Close()

	responseBody, readErr := io.ReadAll(rawResponse.Body)
	if readErr != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindTransient, "failed to read project branch restriction response", readErr)
	}

	if err := openapi.MapStatusError(rawResponse.StatusCode, responseBody); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	var results []openapigenerated.RestRefRestriction
	if err := json.Unmarshal(responseBody, &results); err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindPermanent, "failed to decode project branch restriction response", err)
	}

	if len(results) > 0 {
		return results[0], nil
	}

	return openapigenerated.RestRefRestriction{}, nil
}

func (service *Service) DeleteRestriction(ctx context.Context, projectKey string, id string) error {
	trimmedProject := strings.TrimSpace(projectKey)
	if trimmedProject == "" {
		return apperrors.New(apperrors.KindValidation, "project key is required", nil)
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return apperrors.New(apperrors.KindValidation, "restriction id is required", nil)
	}

	response, err := service.client.DeleteRestrictionWithResponse(ctx, trimmedProject, trimmedID)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete project branch restriction", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func normalizeProjectRestrictionType(value string) (openapigenerated.GetRestrictionsParamsType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "read-only":
		return openapigenerated.GetRestrictionsParamsType("read-only"), nil
	case "no-deletes":
		return openapigenerated.GetRestrictionsParamsType("no-deletes"), nil
	case "fast-forward-only":
		return openapigenerated.GetRestrictionsParamsType("fast-forward-only"), nil
	case "pull-request-only":
		return openapigenerated.GetRestrictionsParamsType("pull-request-only"), nil
	case "no-creates":
		return openapigenerated.GetRestrictionsParamsType("no-creates"), nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "restriction type must be one of read-only, no-deletes, fast-forward-only, pull-request-only, no-creates", nil)
	}
}

func normalizeProjectRestrictionMatcherType(value string) (openapigenerated.GetRestrictionsParamsMatcherType, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "BRANCH":
		return openapigenerated.GetRestrictionsParamsMatcherType("BRANCH"), nil
	case "MODEL_BRANCH":
		return openapigenerated.GetRestrictionsParamsMatcherType("MODEL_BRANCH"), nil
	case "MODEL_CATEGORY":
		return openapigenerated.GetRestrictionsParamsMatcherType("MODEL_CATEGORY"), nil
	case "PATTERN":
		return openapigenerated.GetRestrictionsParamsMatcherType("PATTERN"), nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "matcher type must be one of BRANCH, MODEL_BRANCH, MODEL_CATEGORY, PATTERN", nil)
	}
}

func normalizeProjectRestrictionRequestMatcherType(value string) (openapigenerated.RestRestrictionRequestMatcherTypeId, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	if trimmed == "" {
		trimmed = "BRANCH"
	}

	switch trimmed {
	case "BRANCH", "MODEL_BRANCH", "MODEL_CATEGORY", "PATTERN":
		return openapigenerated.RestRestrictionRequestMatcherTypeId(trimmed), nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "matcher type must be one of BRANCH, MODEL_BRANCH, MODEL_CATEGORY, PATTERN", nil)
	}
}
