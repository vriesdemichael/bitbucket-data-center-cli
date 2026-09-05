package branch

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	"io"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

// AllResults asks for every matching branch rather than a page of them.
// A dry-run existence check needs the complete set: MaxResults caps the total,
// so a scan bounded by it can miss the very branch it is looking for (#470).
const AllResults = 1_000_000

type ListOptions struct {
	MaxResults int
	Start      int
	OrderBy    string
	FilterText string
	Base       string
	Details    *bool
}

type RestrictionListOptions struct {
	MaxResults  int
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
	if options.MaxResults <= 0 {
		options.MaxResults = 25
	}

	// Built once. normalizeBranchOrderBy ran on every page, so a walk that
	// needed four requests validated the same flag four times and could fail
	// halfway through one it had already started.
	params := &openapigenerated.GetBranchesParams{}
	if strings.TrimSpace(options.OrderBy) != "" {
		orderBy, err := normalizeBranchOrderBy(options.OrderBy)
		if err != nil {
			return nil, err
		}
		params.OrderBy = &orderBy
	}
	if filterText := strings.TrimSpace(options.FilterText); filterText != "" {
		params.FilterText = &filterText
	}
	if base := strings.TrimSpace(options.Base); base != "" {
		params.Base = &base
	}
	if options.Details != nil {
		details := *options.Details
		params.Details = &details
	}

	return openapi.PageThrough(ctx, options.Start, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestBranch], error) {
			startValue, limitValue := float32(start), float32(limit)
			pageParams := *params
			pageParams.Start = &startValue
			pageParams.Limit = &limitValue

			response, err := service.client.GetBranchesWithResponse(ctx, repo.ProjectKey, repo.Slug, &pageParams)
			if err != nil {
				return openapi.Page[openapigenerated.RestBranch]{}, apperrors.New(apperrors.KindTransient, "failed to list repository branches", err)
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return openapi.Page[openapigenerated.RestBranch]{}, err
			}

			page := response.ApplicationjsonCharsetUTF8200
			if page == nil || page.Values == nil {
				return openapi.Page[openapigenerated.RestBranch]{}, nil
			}

			return openapi.Page[openapigenerated.RestBranch]{
				Values:        *page.Values,
				IsLastPage:    page.IsLastPage,
				NextPageStart: openapi.Offset(page.NextPageStart),
			}, nil
		})
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

	// Bitbucket accepts a ref that does not exist -- 204, and the repository is
	// left pointing at nothing. Its own UI only offers real branches, so a
	// typo here is silent and the repository's default branch is broken until
	// somebody notices. Refuse it (ADR-054).
	//
	// An empty repository is the exception and a real use: setting the default
	// before the first push is how a repository gets `main` instead of
	// `master`. There is nothing to check against there, so it is allowed.
	if err := service.assertBranchExists(ctx, repo, ref, branch); err != nil {
		return err
	}

	body := openapigenerated.SetDefaultBranch2JSONRequestBody{Id: &ref}
	response, err := service.client.SetDefaultBranch2WithResponse(ctx, repo.ProjectKey, repo.Slug, body)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to set repository default branch", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

// assertBranchExists refuses a default branch that names no branch in a
// repository that has some.
func (service *Service) assertBranchExists(ctx context.Context, repo RepositoryRef, ref, requested string) error {
	display := strings.TrimPrefix(ref, "refs/heads/")

	// AllResults, not a page.
	//
	// filterText is a substring match, so a repository with hundreds of
	// branches sharing a prefix -- release/2026-* and the like -- can push the
	// exact one past any fixed cap. A capped scan would then report a branch
	// that exists as missing and refuse the operation, which is the worse of
	// the two failures: it blocks work that should succeed, where the typo this
	// guard catches only lets through work that should not.
	//
	// This is the same trap AllResults was added for in #470, and the same
	// answer. List pages internally and stops at the last page, so the cost is
	// bounded by how many branches actually share the name.
	matches, err := service.List(ctx, repo, ListOptions{FilterText: display, MaxResults: AllResults})
	if err != nil {
		return err
	}

	for _, candidate := range matches {
		if candidate.Id != nil && *candidate.Id == ref {
			return nil
		}
		if candidate.DisplayId != nil && *candidate.DisplayId == display {
			return nil
		}
	}

	// Nothing matched, which is either "no such branch" or "no branches at
	// all". Only the first is an error: an empty repository legitimately takes
	// a default branch that does not exist yet.
	any, err := service.List(ctx, repo, ListOptions{MaxResults: 1})
	if err != nil {
		return err
	}
	if len(any) == 0 {
		return nil
	}

	return apperrors.New(apperrors.KindValidation,
		fmt.Sprintf("branch %q does not exist in %s/%s; Bitbucket would accept it and leave the repository pointing at nothing",
			requested, repo.ProjectKey, repo.Slug), nil)
}

func (service *Service) FindByCommit(ctx context.Context, repo RepositoryRef, commitID string, maxResults int) ([]openapigenerated.RestMinimalRef, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	trimmedCommitID := strings.TrimSpace(commitID)
	if trimmedCommitID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "commit id is required", nil)
	}
	if maxResults <= 0 {
		maxResults = 25
	}

	return openapi.PageThrough(ctx, 0, maxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestMinimalRef], error) {
			startValue, limitValue := float32(start), float32(limit)
			response, err := service.client.FindByCommitWithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedCommitID,
				&openapigenerated.FindByCommitParams{Start: &startValue, Limit: &limitValue})
			if err != nil {
				return openapi.Page[openapigenerated.RestMinimalRef]{}, apperrors.New(apperrors.KindTransient, "failed to inspect branch model details", err)
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return openapi.Page[openapigenerated.RestMinimalRef]{}, err
			}

			page := response.ApplicationjsonCharsetUTF8200
			if page == nil || page.Values == nil {
				return openapi.Page[openapigenerated.RestMinimalRef]{}, nil
			}

			return openapi.Page[openapigenerated.RestMinimalRef]{
				Values:        *page.Values,
				IsLastPage:    page.IsLastPage,
				NextPageStart: openapi.Offset(page.NextPageStart),
			}, nil
		})
}

func (service *Service) ListRestrictions(ctx context.Context, repo RepositoryRef, options RestrictionListOptions) ([]openapigenerated.RestRefRestriction, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	if options.MaxResults <= 0 {
		options.MaxResults = 25
	}

	// Normalised once rather than on every page.
	params := &openapigenerated.GetRestrictions1Params{}
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
	if matcherID := strings.TrimSpace(options.MatcherID); matcherID != "" {
		params.MatcherId = &matcherID
	}

	// MaxResults now caps the results, which is what it is named for and what
	// every other listing does with it. It was the page size, and nothing
	// capped anything: `bb branch restriction list --limit 5` walked to the
	// last page and returned all of them. The CLI does not truncate afterwards,
	// so the flag did nothing at all.
	return openapi.PageThrough(ctx, 0, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestRefRestriction], error) {
			startValue, limitValue := float32(start), float32(limit)
			pageParams := *params
			pageParams.Start = &startValue
			pageParams.Limit = &limitValue

			response, err := service.client.GetRestrictions1WithResponse(ctx, repo.ProjectKey, repo.Slug, &pageParams)
			if err != nil {
				return openapi.Page[openapigenerated.RestRefRestriction]{}, apperrors.New(apperrors.KindTransient, "failed to list branch restrictions", err)
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
		// The id is a path segment on a DELETE, and update is delete followed
		// by create, so a value that cannot name a restriction is worth
		// refusing before anything is sent rather than after something is
		// removed (ADR-054). `restriction update bad` reached the server and
		// came back as a not-found, which reads like the restriction is gone
		// rather than like the id was never an id.
		if _, err := strconv.Atoi(trimmedUpdateID); err != nil {
			return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindValidation,
				fmt.Sprintf("restriction id must be a number, got %q", trimmedUpdateID), nil)
		}

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
	defer func() { _ = rawResponse.Body.Close() }()

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
	return openapi.ValidateRepository(repo.ProjectKey, repo.Slug)
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

	return bodyEntry, nil
}
