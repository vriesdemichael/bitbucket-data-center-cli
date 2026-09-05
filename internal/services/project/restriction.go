package project

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// AllResults asks a project listing for everything rather than a page of it. A
// dry-run existence check needs the complete set (#470): it is looking for one
// entry, and a cap can stop just short of the entry it is looking for.
const AllResults = 1_000_000

type RestrictionListOptions struct {
	MaxResults  int
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

	if options.MaxResults <= 0 {
		options.MaxResults = 1000
	}

	// Normalised once. These ran inside the loop, so a listing that needed four
	// requests re-validated the same two flags four times -- and could return a
	// validation error from the middle of a walk it had already half finished.
	params := &openapigenerated.GetRestrictionsParams{}
	if options.Type != "" {
		restrictionType, err := normalizeProjectRestrictionType(options.Type)
		if err != nil {
			return nil, err
		}
		params.Type = &restrictionType
	}
	if options.MatcherType != "" {
		matcherType, err := normalizeProjectRestrictionMatcherType(options.MatcherType)
		if err != nil {
			return nil, err
		}
		params.MatcherType = &matcherType
	}
	if options.MatcherID != "" {
		params.MatcherId = &options.MatcherID
	}

	return openapi.PageThrough(ctx, 0, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestRefRestriction], error) {
			// The page size stays what it was. The cap here defaults to a
			// thousand, and asking for a thousand at once is a different request
			// than this endpoint has ever been sent.
			if limit > restrictionPageSize {
				limit = restrictionPageSize
			}

			startValue, limitValue := float32(start), float32(limit)
			pageParams := *params
			pageParams.Start = &startValue
			pageParams.Limit = &limitValue

			response, err := service.client.GetRestrictionsWithResponse(ctx, trimmedProject, &pageParams)
			if err != nil {
				return openapi.Page[openapigenerated.RestRefRestriction]{}, apperrors.New(apperrors.KindTransient, "failed to list project branch restrictions", err)
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return openapi.Page[openapigenerated.RestRefRestriction]{}, err
			}

			page := response.ApplicationjsonCharsetUTF8200
			if page == nil || page.Values == nil {
				return openapi.Page[openapigenerated.RestRefRestriction]{}, nil
			}

			return openapi.Page[openapigenerated.RestRefRestriction]{
				Values:        *page.Values,
				IsLastPage:    page.IsLastPage,
				NextPageStart: openapi.Offset(page.NextPageStart),
			}, nil
		})
}

// restrictionPageSize is the window this endpoint has always been asked for.
const restrictionPageSize = 25

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
			Id   openapigenerated.RestRestrictionRequestMatcherTypeId `json:"id"`
			Name string                                               `json:"name"`
		} `json:"type,omitempty"`
	}{
		Id: &trimmedMatcherID,
		Type: &struct {
			Id   openapigenerated.RestRestrictionRequestMatcherTypeId `json:"id"`
			Name string                                               `json:"name"`
		}{Id: matcherType},
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
				AlgorithmType     *string "json:\"algorithmType,omitempty\""
				BitLength         *int32  "json:\"bitLength,omitempty\""
				CreatedDate       *int64  "json:\"createdDate,omitempty\""
				ExpiryDays        *int32  "json:\"expiryDays,omitempty\""
				Fingerprint       *string "json:\"fingerprint,omitempty\""
				Id                *int32  "json:\"id,omitempty\""
				Label             *string "json:\"label,omitempty\""
				LastAuthenticated *string "json:\"lastAuthenticated,omitempty\""
				Text              *string "json:\"text,omitempty\""
				Warning           *string "json:\"warning,omitempty\""
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
	defer func() { _ = rawResponse.Body.Close() }()

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
