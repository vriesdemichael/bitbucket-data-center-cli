package branch

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	"io"
	"strconv"
	"strings"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

type ListOptions struct {
	Limit      int
	Start      int
	OrderBy    string
	FilterText string
	Base       string
	Details    *bool
}

type RestrictionListOptions struct {
	Limit       int
	Type        string
	MatcherType string
	MatcherID   string
}

type RestrictionUpsertInput struct {
	Type           string
	MatcherType    string
	MatcherID      string
	MatcherDisplay string
	Users          []string
	Groups         []string
	AccessKeyIDs   []int32
}

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func (service *Service) List(ctx context.Context, repo RepositoryRef, options ListOptions) ([]openapigenerated.RestBranch, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	if options.Limit <= 0 {
		options.Limit = 25
	}

	if options.Start < 0 {
		options.Start = 0
	}
	start := float32(options.Start)
	results := make([]openapigenerated.RestBranch, 0)

	for {
		remaining := options.Limit - len(results)
		if remaining <= 0 {
			break
		}

		pageLimit := float32(remaining)
		params := &openapigenerated.GetBranchesParams{Start: &start, Limit: &pageLimit}
		if strings.TrimSpace(options.OrderBy) != "" {
			orderBy, err := normalizeBranchOrderBy(options.OrderBy)
			if err != nil {
				return nil, err
			}
			params.OrderBy = &orderBy
		}
		if strings.TrimSpace(options.FilterText) != "" {
			filterText := strings.TrimSpace(options.FilterText)
			params.FilterText = &filterText
		}
		if strings.TrimSpace(options.Base) != "" {
			base := strings.TrimSpace(options.Base)
			params.Base = &base
		}
		if options.Details != nil {
			details := *options.Details
			params.Details = &details
		}

		response, err := service.client.GetBranchesWithResponse(ctx, repo.ProjectKey, repo.Slug, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list repository branches", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}
		if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		results = append(results, (*response.ApplicationjsonCharsetUTF8200.Values)...)

		if len(results) >= options.Limit {
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

	if len(results) > options.Limit {
		results = results[:options.Limit]
	}
	return results, nil
}

func (service *Service) Create(ctx context.Context, repo RepositoryRef, name string, startPoint string) (openapigenerated.RestBranch, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestBranch{}, err
	}

	trimmedName := strings.TrimSpace(name)
	trimmedStartPoint := strings.TrimSpace(startPoint)
	if trimmedName == "" {
		return openapigenerated.RestBranch{}, apperrors.New(apperrors.KindValidation, "branch name is required", nil)
	}
	if trimmedStartPoint == "" {
		return openapigenerated.RestBranch{}, apperrors.New(apperrors.KindValidation, "branch start-point is required", nil)
	}

	body := openapigenerated.CreateBranchJSONRequestBody{
		Name:       &trimmedName,
		StartPoint: &trimmedStartPoint,
	}

	response, err := service.client.CreateBranchWithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return openapigenerated.RestBranch{}, apperrors.New(apperrors.KindTransient, "failed to create repository branch", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestBranch{}, err
	}

	if response.ApplicationjsonCharsetUTF8201 != nil {
		return *response.ApplicationjsonCharsetUTF8201, nil
	}
	if len(response.Body) > 0 && json.Valid(response.Body) {
		decoded := openapigenerated.RestBranch{}
		if err := json.Unmarshal(response.Body, &decoded); err == nil {
			return decoded, nil
		}
	}

	return openapigenerated.RestBranch{}, nil
}

func (service *Service) Delete(ctx context.Context, repo RepositoryRef, name string, endPoint string, dryRun bool) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}

	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return apperrors.New(apperrors.KindValidation, "branch name is required", nil)
	}

	body := openapigenerated.DeleteBranchJSONRequestBody{Name: &trimmedName}
	if strings.TrimSpace(endPoint) != "" {
		trimmedEndPoint := strings.TrimSpace(endPoint)
		body.EndPoint = &trimmedEndPoint
	}
	body.DryRun = &dryRun

	response, err := service.client.DeleteBranchWithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete repository branch", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) GetDefault(ctx context.Context, repo RepositoryRef) (openapigenerated.RestMinimalRef, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestMinimalRef{}, err
	}

	response, err := service.client.GetDefaultBranch2WithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return openapigenerated.RestMinimalRef{}, apperrors.New(apperrors.KindTransient, "failed to get repository default branch", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestMinimalRef{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestMinimalRef{}, nil
}

func (service *Service) SetDefault(ctx context.Context, repo RepositoryRef, branch string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}

	ref := normalizeBranchRef(branch)
	if ref == "" {
		return apperrors.New(apperrors.KindValidation, "default branch name is required", nil)
	}

	body := openapigenerated.SetDefaultBranch2JSONRequestBody{Id: &ref}
	response, err := service.client.SetDefaultBranch2WithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to set repository default branch", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) FindByCommit(ctx context.Context, repo RepositoryRef, commitID string, limit int) ([]openapigenerated.RestMinimalRef, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	trimmedCommitID := strings.TrimSpace(commitID)
	if trimmedCommitID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "commit id is required", nil)
	}

	if limit <= 0 {
		limit = 25
	}

	start := float32(0)
	pageLimit := float32(limit)
	results := make([]openapigenerated.RestMinimalRef, 0)

	for {
		response, err := service.client.FindByCommitWithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedCommitID, &openapigenerated.FindByCommitParams{Start: &start, Limit: &pageLimit})
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to inspect branch model details", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}
		if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		results = append(results, (*response.ApplicationjsonCharsetUTF8200.Values)...)

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

func (service *Service) ListRestrictions(ctx context.Context, repo RepositoryRef, options RestrictionListOptions) ([]openapigenerated.RestRefRestriction, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	if options.Limit <= 0 {
		options.Limit = 25
	}

	start := float32(0)
	pageLimit := float32(options.Limit)
	results := make([]openapigenerated.RestRefRestriction, 0)

	for {
		params := &openapigenerated.GetRestrictions1Params{Start: &start, Limit: &pageLimit}
		if strings.TrimSpace(options.Type) != "" {
			restrictionType, err := normalizeRestrictionType(options.Type)
			if err != nil {
				return nil, err
			}
			params.Type = &restrictionType
		}
		if strings.TrimSpace(options.MatcherType) != "" {
			matcherType, err := normalizeRestrictionMatcherType(options.MatcherType)
			if err != nil {
				return nil, err
			}
			params.MatcherType = &matcherType
		}
		if strings.TrimSpace(options.MatcherID) != "" {
			matcherID := strings.TrimSpace(options.MatcherID)
			params.MatcherId = &matcherID
		}

		response, err := service.client.GetRestrictions1WithResponse(ctx, repo.ProjectKey, repo.Slug, params)
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list branch restrictions", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}
		if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
			break
		}

		results = append(results, (*response.ApplicationjsonCharsetUTF8200.Values)...)

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

func (service *Service) GetRestriction(ctx context.Context, repo RepositoryRef, id string) (openapigenerated.RestRefRestriction, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindValidation, "restriction id is required", nil)
	}

	response, err := service.client.GetRestriction1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindTransient, "failed to get branch restriction", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestRefRestriction{}, nil
}

func (service *Service) CreateRestriction(ctx context.Context, repo RepositoryRef, input RestrictionUpsertInput) (openapigenerated.RestRefRestriction, error) {
	return service.upsertRestriction(ctx, repo, "", input)
}

func (service *Service) UpdateRestriction(ctx context.Context, repo RepositoryRef, id string, input RestrictionUpsertInput) (openapigenerated.RestRefRestriction, error) {
	return service.upsertRestriction(ctx, repo, id, input)
}

func (service *Service) upsertRestriction(ctx context.Context, repo RepositoryRef, id string, input RestrictionUpsertInput) (openapigenerated.RestRefRestriction, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	trimmedUpdateID := strings.TrimSpace(id)
	if trimmedUpdateID != "" {
		// Bitbucket REST API does not have a PUT endpoint for updating a single restriction.
		// We implement update as a Delete followed by a Create (bulk POST).
		if err := service.DeleteRestriction(ctx, repo, trimmedUpdateID); err != nil {
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

	matcherType, err := normalizeRestrictionRequestMatcherType(input.MatcherType)
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
		groups := cleanedStrings(input.Groups)
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

	requestBody := openapigenerated.CreateRestrictions1ApplicationVndAtlBitbucketBulkPlusJSONBody{bodyEntry}

	// Use the direct client to avoid generated response parsing errors for this array endpoint
	client, ok := service.client.ClientInterface.(*openapigenerated.Client)
	if !ok {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindInternal, "failed to initialize branch restriction request client", nil)
	}

	rawResponse, err := client.CreateRestrictions1WithApplicationVndAtlBitbucketBulkPlusJSONBody(ctx, repo.ProjectKey, repo.Slug, requestBody)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindTransient, "failed to upsert branch restriction", err)
	}
	defer rawResponse.Body.Close()

	responseBody, readErr := io.ReadAll(rawResponse.Body)
	if readErr != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindTransient, "failed to read branch restriction response", readErr)
	}

	if err := openapi.MapStatusError(rawResponse.StatusCode, responseBody); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	// The API returns an array of restrictions for this bulk endpoint.
	// We only sent one, so we take the first one from the array.
	var results []openapigenerated.RestRefRestriction
	if err := json.Unmarshal(responseBody, &results); err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindPermanent, "failed to decode branch restriction response", err)
	}

	if len(results) > 0 {
		return results[0], nil
	}

	return openapigenerated.RestRefRestriction{}, nil
}

func (service *Service) DeleteRestriction(ctx context.Context, repo RepositoryRef, id string) error {
	if err := validateRepositoryRef(repo); err != nil {
		return err
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return apperrors.New(apperrors.KindValidation, "restriction id is required", nil)
	}

	response, err := service.client.DeleteRestriction1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to delete branch restriction", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func validateRepositoryRef(repo RepositoryRef) error {
	if strings.TrimSpace(repo.ProjectKey) == "" || strings.TrimSpace(repo.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}

	return nil
}

func normalizeBranchOrderBy(value string) (openapigenerated.GetBranchesParamsOrderBy, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "ALPHABETICAL":
		return openapigenerated.GetBranchesParamsOrderBy("ALPHABETICAL"), nil
	case "MODIFICATION":
		return openapigenerated.GetBranchesParamsOrderBy("MODIFICATION"), nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "order-by must be ALPHABETICAL or MODIFICATION", nil)
	}
}

func normalizeRestrictionType(value string) (openapigenerated.GetRestrictions1ParamsType, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "read-only":
		return openapigenerated.GetRestrictions1ParamsType("read-only"), nil
	case "no-deletes":
		return openapigenerated.GetRestrictions1ParamsType("no-deletes"), nil
	case "fast-forward-only":
		return openapigenerated.GetRestrictions1ParamsType("fast-forward-only"), nil
	case "pull-request-only":
		return openapigenerated.GetRestrictions1ParamsType("pull-request-only"), nil
	case "no-creates":
		return openapigenerated.GetRestrictions1ParamsType("no-creates"), nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "restriction type must be one of read-only, no-deletes, fast-forward-only, pull-request-only, no-creates", nil)
	}
}

func normalizeRestrictionMatcherType(value string) (openapigenerated.GetRestrictions1ParamsMatcherType, error) {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "BRANCH":
		return openapigenerated.GetRestrictions1ParamsMatcherType("BRANCH"), nil
	case "MODEL_BRANCH":
		return openapigenerated.GetRestrictions1ParamsMatcherType("MODEL_BRANCH"), nil
	case "MODEL_CATEGORY":
		return openapigenerated.GetRestrictions1ParamsMatcherType("MODEL_CATEGORY"), nil
	case "PATTERN":
		return openapigenerated.GetRestrictions1ParamsMatcherType("PATTERN"), nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "matcher type must be one of BRANCH, MODEL_BRANCH, MODEL_CATEGORY, PATTERN", nil)
	}
}

func normalizeRestrictionRequestMatcherType(value string) (openapigenerated.RestRestrictionRequestMatcherTypeId, error) {
	trimmed := strings.ToUpper(strings.TrimSpace(value))
	if trimmed == "" {
		trimmed = "BRANCH"
	}

	switch trimmed {
	case "BRANCH", "MODEL_BRANCH", "MODEL_CATEGORY", "PATTERN":
		matcherType := openapigenerated.RestRestrictionRequestMatcherTypeId(trimmed)
		return matcherType, nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "matcher type must be one of BRANCH, MODEL_BRANCH, MODEL_CATEGORY, PATTERN", nil)
	}
}

func parseRestrictionID(value string) (int32, error) {
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return 0, apperrors.New(apperrors.KindValidation, "restriction id must be numeric", nil)
	}
	if parsed <= 0 {
		return 0, apperrors.New(apperrors.KindValidation, "restriction id must be > 0", nil)
	}

	return int32(parsed), nil
}

func cleanedStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			cleaned = append(cleaned, trimmed)
		}
	}

	return cleaned
}

func normalizeBranchRef(branch string) string {
	trimmed := strings.TrimSpace(branch)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "refs/heads/") {
		return trimmed
	}

	return "refs/heads/" + trimmed
}

func mapRestrictionInput(input RestrictionUpsertInput) (openapigenerated.RestRestrictionRequest, error) {
	trimmedType := strings.TrimSpace(input.Type)
	if trimmedType == "" {
		return openapigenerated.RestRestrictionRequest{}, apperrors.New(apperrors.KindValidation, "restriction type is required", nil)
	}

	trimmedMatcherID := strings.TrimSpace(input.MatcherID)
	if trimmedMatcherID == "" {
		return openapigenerated.RestRestrictionRequest{}, apperrors.New(apperrors.KindValidation, "matcher id is required", nil)
	}

	matcherType, err := normalizeRestrictionRequestMatcherType(input.MatcherType)
	if err != nil {
		return openapigenerated.RestRestrictionRequest{}, err
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
		groups := cleanedStrings(input.Groups)
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

	return bodyEntry, nil
}
