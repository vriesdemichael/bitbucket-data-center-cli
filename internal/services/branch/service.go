package branch

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/compat"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
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

	params, err := branchListParams(options)
	if err != nil {
		return nil, err
	}

	return openapi.PageThrough(ctx, options.Start, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestBranch], error) {
			startValue, limitValue := float32(start), float32(limit)
			pageParams := *params
			pageParams.Start = &startValue
			pageParams.Limit = &limitValue

			response, err := service.client.GetBranchesWithResponse(ctx, repo.ProjectKey, repo.Slug, &pageParams)
			if err != nil {
				return openapi.Page[openapigenerated.RestBranch]{}, apperrors.Transport("failed to list repository branches", err)
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

// branchListParams turns the options into the request's parameters.
//
// Built once per listing. normalizeBranchOrderBy ran on every page, so a walk
// that needed four requests validated the same flag four times and could fail
// halfway through one it had already started.
func branchListParams(options ListOptions) (*openapigenerated.GetBranchesParams, error) {
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

	return params, nil
}

// DetailedBranch is a branch with what Bitbucket's own branch list shows
// beside it.
type DetailedBranch struct {
	openapigenerated.RestBranch
	Details BranchDetails
}

// BranchDetails is what Bitbucket attaches to a branch when details are asked
// for.
//
// Each part comes from a provider of its own, and a provider with nothing to
// say about a branch leaves its part out: the base branch has no ahead and
// behind, a branch whose commit has no builds has no tally, and one nobody
// opened a pull request from has no pull requests.
type BranchDetails struct {
	AheadBehind  *AheadBehind
	LatestCommit *openapigenerated.RestCommit
	Builds       *openapigenerated.RestBuildStats
	PullRequests *BranchPullRequests
}

// AheadBehind is how far a branch has moved from the base: the commits it has
// that the base lacks, and the commits the base has that it lacks.
type AheadBehind struct {
	Ahead  int32 `json:"ahead"`
	Behind int32 `json:"behind"`
}

// BranchPullRequests is the pull requests opened from a branch.
//
// Bitbucket answers in one of two shapes. A branch with exactly one pull
// request gets that pull request, whatever its state; a branch with more gets
// a count by state and no pull request. Only says which it was, and the counts
// are filled in either way, so a caller asking how many are open reads one
// field.
type BranchPullRequests struct {
	Open     int32
	Merged   int32
	Declined int32
	Only     *BranchPullRequest
}

// BranchPullRequest is the one pull request a branch has.
type BranchPullRequest struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	State string `json:"state"`
}

// The providers whose answers bb reads, by the key Bitbucket files each under.
// Bitbucket 9.2 and 10.4 file them under the same keys.
const (
	aheadBehindProvider  = "com.atlassian.bitbucket.server.bitbucket-branch:ahead-behind-metadata-provider"
	latestCommitProvider = "com.atlassian.bitbucket.server.bitbucket-branch:latest-commit-metadata"
	buildStatusProvider  = "com.atlassian.bitbucket.server.bitbucket-build:build-status-metadata"
	pullRequestProvider  = "com.atlassian.bitbucket.server.bitbucket-ref-metadata:outgoing-pull-request-metadata"
)

// ListDetailed lists branches with what Bitbucket's own branch list shows
// beside each: how far it is ahead of and behind options.Base, or the default
// branch when none is named, its latest commit, the builds of that commit and
// the pull requests opened from it.
//
// The details are not in the API's description of a branch, so the generated
// client drops them. They are read from the same response, beside the branches
// it did decode.
func (service *Service) ListDetailed(ctx context.Context, repo RepositoryRef, options ListOptions) ([]DetailedBranch, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}
	if options.MaxResults <= 0 {
		options.MaxResults = 25
	}

	details := true
	options.Details = &details
	params, err := branchListParams(options)
	if err != nil {
		return nil, err
	}

	return openapi.PageThrough(ctx, options.Start, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[DetailedBranch], error) {
			startValue, limitValue := float32(start), float32(limit)
			pageParams := *params
			pageParams.Start = &startValue
			pageParams.Limit = &limitValue

			response, err := service.client.GetBranchesWithResponse(ctx, repo.ProjectKey, repo.Slug, &pageParams)
			if err != nil {
				return openapi.Page[DetailedBranch]{}, apperrors.Transport("failed to list repository branches", err)
			}
			if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
				return openapi.Page[DetailedBranch]{}, err
			}

			page := response.ApplicationjsonCharsetUTF8200
			if page == nil || page.Values == nil {
				return openapi.Page[DetailedBranch]{}, nil
			}

			values, err := detailedBranches(*page.Values, response.Body)
			if err != nil {
				return openapi.Page[DetailedBranch]{}, err
			}

			return openapi.Page[DetailedBranch]{
				Values:        values,
				IsLastPage:    page.IsLastPage,
				NextPageStart: openapi.Offset(page.NextPageStart),
			}, nil
		})
}

// detailedBranches pairs each branch of a page with the details the same
// response carries for it.
func detailedBranches(branches []openapigenerated.RestBranch, body []byte) ([]DetailedBranch, error) {
	var page struct {
		Values []struct {
			Metadata map[string]json.RawMessage `json:"metadata"`
		} `json:"values"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, apperrors.New(apperrors.KindPermanent, "failed to decode branch details", err)
	}
	if len(page.Values) != len(branches) {
		return nil, apperrors.New(apperrors.KindPermanent,
			fmt.Sprintf("failed to decode branch details: %d branches and details for %d", len(branches), len(page.Values)), nil)
	}

	detailed := make([]DetailedBranch, len(branches))
	for index, branch := range branches {
		details, err := branchDetails(page.Values[index].Metadata)
		if err != nil {
			return nil, apperrors.New(apperrors.KindPermanent,
				fmt.Sprintf("failed to decode the details of branch %s", safederef.String(branch.DisplayId)), err)
		}
		detailed[index] = DetailedBranch{RestBranch: branch, Details: details}
	}

	return detailed, nil
}

// branchDetails reads the parts bb knows out of what the providers attached.
// A part that does not decode is an error rather than a part left out: a
// listing that silently lost its counts would read as a branch that has none.
func branchDetails(metadata map[string]json.RawMessage) (BranchDetails, error) {
	var details BranchDetails

	if raw, present := metadata[aheadBehindProvider]; present {
		details.AheadBehind = &AheadBehind{}
		if err := json.Unmarshal(raw, details.AheadBehind); err != nil {
			return BranchDetails{}, fmt.Errorf("ahead and behind: %w", err)
		}
	}
	if raw, present := metadata[latestCommitProvider]; present {
		details.LatestCommit = &openapigenerated.RestCommit{}
		if err := json.Unmarshal(raw, details.LatestCommit); err != nil {
			return BranchDetails{}, fmt.Errorf("latest commit: %w", err)
		}
	}
	if raw, present := metadata[buildStatusProvider]; present {
		details.Builds = &openapigenerated.RestBuildStats{}
		if err := json.Unmarshal(raw, details.Builds); err != nil {
			return BranchDetails{}, fmt.Errorf("builds: %w", err)
		}
	}
	if raw, present := metadata[pullRequestProvider]; present {
		pullRequests, err := branchPullRequests(raw)
		if err != nil {
			return BranchDetails{}, fmt.Errorf("pull requests: %w", err)
		}
		details.PullRequests = pullRequests
	}

	return details, nil
}

// branchPullRequests reads either shape Bitbucket answers in: the one pull
// request, or a count by state.
func branchPullRequests(raw json.RawMessage) (*BranchPullRequests, error) {
	var answer struct {
		PullRequest *BranchPullRequest `json:"pullRequest"`
		Open        int32              `json:"open"`
		Merged      int32              `json:"merged"`
		Declined    int32              `json:"declined"`
	}
	if err := json.Unmarshal(raw, &answer); err != nil {
		return nil, err
	}

	pullRequests := &BranchPullRequests{Open: answer.Open, Merged: answer.Merged, Declined: answer.Declined, Only: answer.PullRequest}
	if only := answer.PullRequest; only != nil {
		switch only.State {
		case "OPEN":
			pullRequests.Open = 1
		case "MERGED":
			pullRequests.Merged = 1
		case "DECLINED":
			pullRequests.Declined = 1
		}
	}

	return pullRequests, nil
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
		return openapigenerated.RestBranch{}, apperrors.Transport("failed to create repository branch", err)
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
		return apperrors.Transport("failed to delete repository branch", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) GetDefault(ctx context.Context, repo RepositoryRef) (openapigenerated.RestMinimalRef, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestMinimalRef{}, err
	}

	response, err := service.client.GetDefaultBranch2WithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return openapigenerated.RestMinimalRef{}, apperrors.Transport("failed to get repository default branch", err)
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
		return apperrors.Transport("failed to set repository default branch", err)
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
				return openapi.Page[openapigenerated.RestMinimalRef]{}, apperrors.Transport("failed to inspect branch model details", err)
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

	// A release without no-creates refuses the filter rather than applying it,
	// and holds no restriction of the type. So the request goes out without the
	// filter and the answer is narrowed here instead: it comes back empty, which
	// is what such a release holds, and a repository the caller cannot read
	// still answers as it would for any other listing.
	narrowToNoCreates := false
	if compat.IsNoCreates(options.Type) {
		lacks, err := compat.NoCreatesRestriction.LackedBy(ctx, service.client)
		if err != nil {
			return nil, err
		}
		if lacks {
			params.Type, narrowToNoCreates = nil, true
		}
	}

	// MaxResults now caps the results, which is what it is named for and what
	// every other listing does with it. It was the page size, and nothing
	// capped anything: `bb branch restriction list --limit 5` walked to the
	// last page and returned all of them. The CLI does not truncate afterwards,
	// so the flag did nothing at all.
	restrictions, err := openapi.PageThrough(ctx, 0, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestRefRestriction], error) {
			startValue, limitValue := float32(start), float32(limit)
			pageParams := *params
			pageParams.Start = &startValue
			pageParams.Limit = &limitValue

			response, err := service.client.GetRestrictions1WithResponse(ctx, repo.ProjectKey, repo.Slug, &pageParams)
			if err != nil {
				return openapi.Page[openapigenerated.RestRefRestriction]{}, apperrors.Transport("failed to list branch restrictions", err)
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
	if err != nil {
		return nil, err
	}
	if narrowToNoCreates {
		return compat.OnlyNoCreates(restrictions), nil
	}

	return restrictions, nil
}

func (service *Service) GetRestriction(ctx context.Context, repo RepositoryRef, id string) (openapigenerated.RestRefRestriction, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindValidation, "restriction id is required", nil)
	}
	if err := checkRestrictionID(trimmedID); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	response, err := service.client.GetRestriction1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.Transport("failed to get branch restriction", err)
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
		// Refused before anything is sent. `restriction update bad` reached the
		// server and came back as a not-found, which reads like the restriction
		// is gone rather than like the id was never an id.
		if err := checkRestrictionID(trimmedUpdateID); err != nil {
			return openapigenerated.RestRefRestriction{}, err
		}
	}

	bodyEntry, err := mapRestrictionInput(input)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}
	if err := service.RefuseRestrictionType(ctx, input.Type); err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}

	if trimmedUpdateID == "" {
		return service.createRestriction(ctx, repo, bodyEntry)
	}

	// Bitbucket has no endpoint that updates one restriction, and its create is
	// an upsert keyed by type and matcher: the same pair again replaces that
	// restriction's exemptions and answers with its id. So an update creates
	// first, and removes the old restriction only when a different one came
	// back. Deleting first, as this used to, left the branch unprotected whenever
	// the create was refused -- a user name that does not exist was enough.
	//
	// The old restriction is read first so that updating an id that is not there
	// reports that, rather than creating a restriction nobody asked to add.
	current, err := service.GetRestriction(ctx, repo, trimmedUpdateID)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, err
	}
	// The repository's route answers for a restriction the repository inherits
	// from its project as well, and the delete below would remove that one from
	// the project, and so from every repository in it (#657).
	if current.Scope != nil && strings.EqualFold(string(current.Scope.Type), "PROJECT") {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindValidation, fmt.Sprintf(
			"branch restriction %s is inherited from project %s, so it cannot be updated through %s/%s",
			trimmedUpdateID, repo.ProjectKey, repo.ProjectKey, repo.Slug), nil)
	}

	created, err := service.createRestriction(ctx, repo, bodyEntry)
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
	if err := service.DeleteRestriction(ctx, repo, trimmedUpdateID); err != nil {
		return created, fmt.Errorf("created restriction %d, but removing restriction %s, which it replaces, failed: %w", createdID, trimmedUpdateID, err)
	}

	return created, nil
}

// RefuseRestrictionType refuses a restriction type the instance's release does
// not have (compat.NoCreatesRestriction), before anything is sent. Only a
// no-creates restriction asks for the release.
func (service *Service) RefuseRestrictionType(ctx context.Context, restrictionType string) error {
	if !compat.IsNoCreates(restrictionType) {
		return nil
	}

	return compat.NoCreatesRestriction.Require(ctx, service.client)
}

// createRestriction sends one restriction to the bulk create.
func (service *Service) createRestriction(ctx context.Context, repo RepositoryRef, bodyEntry openapigenerated.RestRestrictionRequest) (openapigenerated.RestRefRestriction, error) {
	requestBody := openapigenerated.CreateRestrictions1ApplicationVndAtlBitbucketBulkPlusJSONBody{bodyEntry}

	// Use the direct client to avoid generated response parsing errors for this array endpoint
	client, ok := service.client.ClientInterface.(*openapigenerated.Client)
	if !ok {
		return openapigenerated.RestRefRestriction{}, apperrors.New(apperrors.KindInternal, "failed to initialize branch restriction request client", nil)
	}

	rawResponse, err := client.CreateRestrictions1WithApplicationVndAtlBitbucketBulkPlusJSONBody(ctx, repo.ProjectKey, repo.Slug, requestBody)
	if err != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.Transport("failed to upsert branch restriction", err)
	}
	defer func() { _ = rawResponse.Body.Close() }()

	responseBody, readErr := io.ReadAll(rawResponse.Body)
	if readErr != nil {
		return openapigenerated.RestRefRestriction{}, apperrors.Transport("failed to read branch restriction response", readErr)
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
	if err := checkRestrictionID(trimmedID); err != nil {
		return err
	}

	response, err := service.client.DeleteRestriction1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedID)
	if err != nil {
		return apperrors.Transport("failed to delete branch restriction", err)
	}

	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

// checkRestrictionID refuses an id Bitbucket cannot route, before anything is
// sent (ADR-054).
//
// The id is a path segment, and one that is not a 32-bit integer never reaches
// the restriction resource: Bitbucket answers 404 with an empty body under a
// JSON content type. The generated client cannot decode that, so `restriction
// get abc` reported a transient failure -- exit 10, try again -- for an id that
// can never name a restriction.
func checkRestrictionID(id string) error {
	if _, err := strconv.ParseInt(id, 10, 32); err != nil {
		return apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("restriction id must be a number no larger than %d, got %q", math.MaxInt32, id), nil)
	}

	return nil
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

	// Exemptions go by name and by access key id: Bitbucket refuses users sent as
	// objects and fails on access keys sent as objects (OPENAPI-031).
	if users := cleanedStrings(input.Users); len(users) > 0 {
		bodyEntry.Users = &users
	}

	if groups := cleanedStrings(input.Groups); len(groups) > 0 {
		bodyEntry.Groups = &groups
	}

	if len(input.AccessKeyIDs) > 0 {
		keys := append([]int32(nil), input.AccessKeyIDs...)
		bodyEntry.AccessKeys = &keys
	}

	return bodyEntry, nil
}
