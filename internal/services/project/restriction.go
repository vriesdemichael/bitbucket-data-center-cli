package project

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
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
				return openapi.Page[openapigenerated.RestRefRestriction]{}, apperrors.Transport("failed to list project branch restrictions", err)
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
	if err := checkRestrictionID(trimmedID); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	response, err := service.client.GetRestrictionWithResponse(ctx, trimmedProject, trimmedID)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.Transport("failed to get project branch restriction", err)
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

	bodyEntry, err := mapRestrictionInput(input)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	trimmedUpdateID := strings.TrimSpace(id)
	if trimmedUpdateID == "" {
		return service.createRestriction(ctx, trimmedProject, bodyEntry)
	}

	// Refused before anything is sent, as for a repository restriction: a value
	// that cannot name a restriction came back from the server as a not-found.
	if err := checkRestrictionID(trimmedUpdateID); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	// The same order as a repository restriction's update, for the same reason:
	// the create is an upsert keyed by type and matcher, so it goes first, and the
	// old restriction is removed only when a different one came back. Deleting
	// first left the branches unprotected whenever the create was refused.
	if _, err := service.GetRestriction(ctx, trimmedProject, trimmedUpdateID); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	created, err := service.createRestriction(ctx, trimmedProject, bodyEntry)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	createdID := int32(0)
	if created.Id != nil {
		createdID = *created.Id
	}
	if strconv.Itoa(int(createdID)) == trimmedUpdateID {
		return created, nil
	}
	if err := service.DeleteRestriction(ctx, trimmedProject, trimmedUpdateID); err != nil {
		return created, fmt.Errorf("created restriction %d, but removing restriction %s, which it replaces, failed: %w", createdID, trimmedUpdateID, err)
	}

	return created, nil
}

// mapRestrictionInput validates a restriction and builds the request for it,
// before anything is sent.
func mapRestrictionInput(input RestrictionUpsertInput) (openapigenerated.RestRestrictionRequest, error) {
	trimmedType := strings.TrimSpace(input.Type)
	if trimmedType == "" {
		return openapigenerated.RestRestrictionRequest{}, apperrors.New(apperrors.KindValidation, "restriction type is required", nil)
	}

	trimmedMatcherID := strings.TrimSpace(input.MatcherID)
	if trimmedMatcherID == "" {
		return openapigenerated.RestRestrictionRequest{}, apperrors.New(apperrors.KindValidation, "matcher id is required", nil)
	}

	matcherType, err := normalizeProjectRestrictionRequestMatcherType(input.MatcherType)
	if err != nil {
		return openapigenerated.RestRestrictionRequest{}, err
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

	// Exemptions go by name and by access key id: Bitbucket refuses users sent as
	// objects and fails on access keys sent as objects (OPENAPI-031).
	if users := trimmedNonEmpty(input.Users); len(users) > 0 {
		bodyEntry.Users = &users
	}

	if groups := trimmedNonEmpty(input.Groups); len(groups) > 0 {
		bodyEntry.Groups = &groups
	}

	if len(input.AccessKeyIDs) > 0 {
		keys := append([]int32(nil), input.AccessKeyIDs...)
		bodyEntry.AccessKeys = &keys
	}

	return bodyEntry, nil
}

// trimmedNonEmpty trims each value and drops the empty ones.
func trimmedNonEmpty(values []string) []string {
	kept := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			kept = append(kept, trimmed)
		}
	}
	return kept
}

// createRestriction sends one restriction to the bulk create.
func (service *Service) createRestriction(ctx context.Context, trimmedProject string, bodyEntry openapigenerated.RestRestrictionRequest) (openapigenerated.RestRefRestriction, error) {
	requestBody := openapigenerated.CreateRestrictionsApplicationVndAtlBitbucketBulkPlusJSONRequestBody{bodyEntry}

	client, ok := service.client.ClientInterface.(*openapigenerated.Client)
	if !ok {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindInternal, "failed to initialize project branch restriction request client", nil)
	}

	rawResponse, err := client.CreateRestrictionsWithApplicationVndAtlBitbucketBulkPlusJSONBody(ctx, trimmedProject, requestBody)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.Transport("failed to upsert project branch restriction", err)
	}
	defer func() { _ = rawResponse.Body.Close() }()

	responseBody, readErr := io.ReadAll(rawResponse.Body)
	if readErr != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.Transport("failed to read project branch restriction response", readErr)
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
	if err := checkRestrictionID(trimmedID); err != nil {
		return err
	}

	response, err := service.client.DeleteRestrictionWithResponse(ctx, trimmedProject, trimmedID)
	if err != nil {
		return apperrors.Transport("failed to delete project branch restriction", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

// checkRestrictionID refuses an id Bitbucket cannot route, before anything is
// sent (ADR-054).
//
// The id is a path segment, and one that is not a 32-bit integer never reaches
// the restriction resource: Bitbucket answers 404 with an empty body under a
// JSON content type. The generated client cannot decode that, so
// `branch-restriction get PROJ abc` reported a transient failure -- exit 10, try
// again -- for an id that can never name a restriction.
func checkRestrictionID(id string) error {
	if _, err := strconv.ParseInt(id, 10, 32); err != nil {
		return apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("restriction id must be a number no larger than %d, got %q", math.MaxInt32, id), nil)
	}

	return nil
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
