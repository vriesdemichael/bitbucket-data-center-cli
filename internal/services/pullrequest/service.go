package pullrequest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commentanchor"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

type RepositoryRef struct {
	ProjectKey string `json:"project_key"`
	Slug       string `json:"slug"`
}

// AllResults asks for every matching pull request rather than a page of them.
// A dry-run duplicate check needs the complete set, and the server-side branch
// filter keeps that cheap (#470).
const AllResults = 1_000_000

type ListOptions struct {
	State        string `json:"state"`
	MaxResults   int    `json:"limit"`
	Start        int    `json:"start"`
	SourceBranch string `json:"source_branch,omitempty"`
	TargetBranch string `json:"target_branch,omitempty"`
	Role         string `json:"role,omitempty"`
}

type PullRequest struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	State       string `json:"state"`
	Open        bool   `json:"open"`
	Closed      bool   `json:"closed"`
	Draft       bool   `json:"draft,omitempty"`
	// Repository is where the pull request targets — the repository it will
	// merge into.
	Repository *RepositoryRef `json:"repository,omitempty"`
	// SourceRepository is where the source branch lives. It differs from
	// Repository exactly when the pull request comes from a fork, which is the
	// only way to tell a fork pull request from a same-repository one, and the
	// thing that decides where a checkout has to fetch from.
	SourceRepository *RepositoryRef `json:"source_repository,omitempty"`
	Version          int            `json:"version,omitempty"`
	Author           string         `json:"author,omitempty"`
	AuthorUsername   string         `json:"author_username,omitempty"`
	SourceBranch     string         `json:"source_branch,omitempty"`
	TargetBranch     string         `json:"target_branch,omitempty"`
	SourceCommit     string         `json:"source_commit,omitempty"`
	CreatedDate      int64          `json:"created_date,omitempty"`
	UpdatedDate      int64          `json:"updated_date,omitempty"`
	Reviewers        []Reviewer     `json:"reviewers,omitempty"`
	Mergeability     *Mergeability  `json:"mergeability,omitempty"`

	// CommentCount, OpenTaskCount and ResolvedTaskCount come from the
	// "properties" object Bitbucket returns alongside every pull request. The
	// field is undocumented in the published OpenAPI spec, so it is decoded
	// defensively: the counts stay nil rather than reporting a wrong zero when
	// the server does not send them.
	CommentCount      *int `json:"comment_count,omitempty"`
	OpenTaskCount     *int `json:"open_task_count,omitempty"`
	ResolvedTaskCount *int `json:"resolved_task_count,omitempty"`
}

type Mergeability struct {
	Mergeable  bool           `json:"mergeable"`
	Outcome    string         `json:"outcome,omitempty"`
	Conflicted bool           `json:"conflicted"`
	Blockers   []MergeBlocker `json:"blockers,omitempty"`
}

type MergeBlocker struct {
	Summary string `json:"summary,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

type Reviewer struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Email       string `json:"email,omitempty"`
	Role        string `json:"role,omitempty"`
	Status      string `json:"status,omitempty"`
	Approved    bool   `json:"approved"`
}

type CreateInput struct {
	FromRef     string `json:"from_ref"`
	ToRef       string `json:"to_ref"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Draft       bool   `json:"draft,omitempty"`
	// Reviewers lists usernames to add as PR reviewers on creation. Blank and
	// whitespace-only entries are ignored; an empty result omits reviewers from
	// the request payload.
	Reviewers []string `json:"reviewers,omitempty"`
	// FromRepository, when set, is the repository the source branch lives in.
	// Empty means the branch is in the repository the pull request targets,
	// which is the same-repository case. Naming it is what makes a fork to
	// upstream pull request expressible at all (#506).
	FromRepository *RepositoryRef `json:"from_repository,omitempty"`
}

type UpdateInput struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Version     int    `json:"version"`
	// Draft, when non-nil, sets or clears the draft flag on the pull request.
	Draft *bool `json:"draft,omitempty"`
	// Reviewers, when non-nil, replaces the reviewer set. Nil means "leave it
	// alone", which the caller achieves by echoing the current set back --
	// see Update, and #511 for what omitting it did instead.
	Reviewers *[]string `json:"reviewers,omitempty"`
}

// AutoMerge represents the auto-merge configuration for a pull request.
// Enabled is false when no auto-merge is configured (the API returns 404).
type AutoMerge struct {
	Enabled    bool   `json:"enabled"`
	StrategyID string `json:"strategy_id,omitempty"`
	// MergedImmediately reports that arming merged the pull request rather than
	// queueing it, because its checks already passed. Enabled is false then:
	// there is no pending auto-merge, and saying otherwise would describe a
	// state that will never fire.
	MergedImmediately bool `json:"merged_immediately,omitempty"`
}

type Service struct {
	client    *httpclient.Client
	apiClient *openapigenerated.ClientWithResponses
}

func NewService(client *httpclient.Client) *Service {
	return &Service{client: client}
}

func (service *Service) WithAPIClient(apiClient *openapigenerated.ClientWithResponses) *Service {
	service.apiClient = apiClient
	return service
}

type DashboardListOptions struct {
	State string
	Role  string
	// ParticipantStatus narrows the result to how the authenticated user has
	// responded so far: a comma-separated list of UNAPPROVED, NEEDS_WORK and
	// APPROVED. Combined with Role=REVIEWER, UNAPPROVED is "asked to review and
	// has not acted yet", which is what Bitbucket's own dashboard shows.
	ParticipantStatus string
	MaxResults        int
	Start             int
}

func (service *Service) ListDashboard(ctx context.Context, options DashboardListOptions) ([]PullRequest, error) {
	normalizedState, err := normalizeState(options.State)
	if err != nil {
		return nil, err
	}

	if options.MaxResults <= 0 {
		options.MaxResults = 25
	}
	if options.Start < 0 {
		return nil, apperrors.New(apperrors.KindValidation, "start must be greater than or equal to 0", nil)
	}

	path := "/rest/api/1.0/dashboard/pull-requests"
	results := make([]PullRequest, 0)
	start := options.Start

	for {
		query := map[string]string{
			"limit": strconv.Itoa(options.MaxResults),
			"start": strconv.Itoa(start),
		}
		if normalizedState != "" {
			if normalizedState == "open" {
				query["state"] = "OPEN"
			} else if normalizedState != "all" {
				query["state"] = strings.ToUpper(normalizedState)
			}
		}

		if options.Role != "" {
			query["role"] = strings.ToUpper(options.Role)
		}

		if strings.TrimSpace(options.ParticipantStatus) != "" {
			query["participantStatus"] = strings.ToUpper(strings.TrimSpace(options.ParticipantStatus))
		}

		var response pagedPullRequestResponse
		if err := service.client.GetJSON(ctx, path, query, &response); err != nil {
			return nil, err
		}

		for _, value := range response.Values {
			mapped := mapPullRequest(value)
			results = append(results, mapped)
		}

		if response.IsLastPage {
			break
		}

		if response.NextPageStart == start {
			break
		}

		start = response.NextPageStart
	}

	return results, nil
}

func (service *Service) List(ctx context.Context, repository RepositoryRef, options ListOptions) ([]PullRequest, error) {
	if strings.TrimSpace(repository.ProjectKey) == "" || strings.TrimSpace(repository.Slug) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}

	normalizedState, err := normalizeState(options.State)
	if err != nil {
		return nil, err
	}

	if options.MaxResults <= 0 {
		options.MaxResults = 25
	}
	if options.Start < 0 {
		return nil, apperrors.New(apperrors.KindValidation, "start must be greater than or equal to 0", nil)
	}

	path := pullRequestPath(repository)
	results := make([]PullRequest, 0)
	start := options.Start

	for {
		query := map[string]string{
			"limit": strconv.Itoa(options.MaxResults),
			"start": strconv.Itoa(start),
		}
		if normalizedState == "open" {
			query["state"] = "OPEN"
		} else if normalizedState != "all" {
			query["state"] = strings.ToUpper(normalizedState)
		} else {
			query["state"] = "ALL"
		}
		if options.Role != "" {
			query["role"] = strings.ToUpper(options.Role)
		}

		var response pagedPullRequestResponse
		if err := service.client.GetJSON(ctx, path, query, &response); err != nil {
			return nil, err
		}

		for _, value := range response.Values {
			mapped := mapPullRequest(value)
			if matchesFilters(mapped, normalizedState, options.SourceBranch, options.TargetBranch) {
				results = append(results, mapped)
			}
		}

		// Limit caps the results returned, as it does in every other list
		// service. This loop used to run to the last page and return everything,
		// treating Limit purely as a page size — so `bb pr list --limit 10`
		// against a repository with 500 open pull requests returned all 500.
		//
		// The pages still have to be walked, because matchesFilters runs after
		// the fetch and a page can contribute nothing; the cap belongs on the
		// results, not on the requests.
		if len(results) >= options.MaxResults {
			break
		}

		if response.IsLastPage {
			break
		}

		if response.NextPageStart == start {
			break
		}

		start = response.NextPageStart
	}

	if len(results) > options.MaxResults {
		results = results[:options.MaxResults]
	}

	return results, nil
}

func (service *Service) Get(ctx context.Context, repository RepositoryRef, pullRequestID string) (PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return PullRequest{}, err
	}

	var response pullRequestValue
	if err := service.client.GetJSON(ctx, fmt.Sprintf("%s/%s", pullRequestPath(repository), resolvedID), nil, &response); err != nil {
		return PullRequest{}, err
	}

	pullRequest := mapPullRequest(response)
	if pullRequest.Open {
		mergeability, err := service.GetMergeability(ctx, repository, resolvedID)
		switch {
		case err == nil:
			pullRequest.Mergeability = &mergeability
		case apperrors.IsKind(err, apperrors.KindNotFound), apperrors.IsKind(err, apperrors.KindConflict):
			// Older Bitbucket variants or non-open PR states can omit mergeability details.
		default:
			return PullRequest{}, err
		}
	}

	return pullRequest, nil
}

func (service *Service) GetMergeability(ctx context.Context, repository RepositoryRef, pullRequestID string) (Mergeability, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return Mergeability{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return Mergeability{}, err
	}

	var response mergeabilityValue
	if err := service.client.GetJSON(ctx, fmt.Sprintf("%s/%s/merge", pullRequestPath(repository), resolvedID), nil, &response); err != nil {
		return Mergeability{}, err
	}

	return mapMergeability(response), nil
}

func (service *Service) Create(ctx context.Context, repository RepositoryRef, input CreateInput) (PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, err
	}

	payload, err := buildCreatePayload(input)
	if err != nil {
		return PullRequest{}, err
	}

	var response pullRequestValue
	if err := service.client.PostJSON(ctx, pullRequestPath(repository), nil, payload, &response); err != nil {
		return PullRequest{}, err
	}

	return mapPullRequest(response), nil
}

func (service *Service) Update(ctx context.Context, repository RepositoryRef, pullRequestID string, input UpdateInput) (PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return PullRequest{}, err
	}

	// Validate before fetching. The reviewer echo below costs a request, and a
	// caller who named no field or a negative version should hear that
	// immediately rather than after a round trip -- ADR-054, and the reason
	// this is not simply folded into the block that follows.
	payload, err := buildUpdatePayload(input)
	if err != nil {
		return PullRequest{}, err
	}

	// Read the pull request when the caller did not name a reviewer set, so the
	// existing one can be echoed back. Without it the PUT drops every reviewer,
	// which is what #511 reported: an update to the description silently
	// emptied a set of six.
	if input.Reviewers == nil {
		current, err := service.Get(ctx, repository, resolvedID)
		if err != nil {
			return PullRequest{}, err
		}

		existing := make([]map[string]any, 0, len(current.Reviewers))
		for _, reviewer := range current.Reviewers {
			if name := strings.TrimSpace(reviewer.Name); name != "" {
				existing = append(existing, map[string]any{"user": map[string]any{"name": name}})
			}
		}
		payload["reviewers"] = existing
	}

	var response pullRequestValue
	if err := service.client.PutJSON(ctx, fmt.Sprintf("%s/%s", pullRequestPath(repository), resolvedID), nil, payload, &response); err != nil {
		return PullRequest{}, err
	}

	return mapPullRequest(response), nil
}

func (service *Service) Merge(ctx context.Context, repository RepositoryRef, pullRequestID string, version *int) (PullRequest, error) {
	return service.transition(ctx, repository, pullRequestID, "merge", version)
}

func (service *Service) Decline(ctx context.Context, repository RepositoryRef, pullRequestID string, version *int) (PullRequest, error) {
	return service.transition(ctx, repository, pullRequestID, "decline", version)
}

func (service *Service) Reopen(ctx context.Context, repository RepositoryRef, pullRequestID string, version *int) (PullRequest, error) {
	return service.transition(ctx, repository, pullRequestID, "reopen", version)
}

func (service *Service) Approve(ctx context.Context, repository RepositoryRef, pullRequestID string) (PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return PullRequest{}, err
	}

	var response pullRequestValue
	if err := service.client.PostJSON(ctx, fmt.Sprintf("%s/%s/approve", pullRequestPath(repository), resolvedID), nil, map[string]any{}, &response); err != nil {
		return PullRequest{}, err
	}

	return mapPullRequest(response), nil
}

func (service *Service) Unapprove(ctx context.Context, repository RepositoryRef, pullRequestID string) (PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return PullRequest{}, err
	}

	var response pullRequestValue
	if err := service.client.DeleteJSON(ctx, fmt.Sprintf("%s/%s/approve", pullRequestPath(repository), resolvedID), nil, nil, &response); err != nil {
		return PullRequest{}, err
	}

	return mapPullRequest(response), nil
}

// NeedsWork sets the current user's review status to NEEDS_WORK on a pull
// request, the equivalent of "request changes". Unlike approve and unapprove
// there is no dedicated endpoint, so this updates the participant record
// directly via PUT .../participants/{userSlug}.
func (service *Service) NeedsWork(ctx context.Context, repository RepositoryRef, pullRequestID string) (PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return PullRequest{}, err
	}

	userSlug, err := service.client.CurrentUserSlug(ctx)
	if err != nil {
		return PullRequest{}, err
	}

	path := fmt.Sprintf("%s/%s/participants/%s", pullRequestPath(repository), resolvedID, url.PathEscape(userSlug))
	payload := map[string]any{"status": "NEEDS_WORK"}

	var response pullRequestValue
	if err := service.client.PutJSON(ctx, path, nil, payload, &response); err != nil {
		return PullRequest{}, err
	}

	return mapPullRequest(response), nil
}

// InlineCommentAnchor specifies the file location for an inline PR comment.
type InlineCommentAnchor struct {
	Line     int    `json:"line"`
	Path     string `json:"path"`
	LineType string `json:"lineType"` // ADDED, REMOVED, or CONTEXT
}

// Comment represents a pull request comment returned by the API.
type Comment struct {
	ID      int64  `json:"id"`
	Version int    `json:"version"`
	Text    string `json:"text"`
	Author  struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
		Slug        string `json:"slug"`
	} `json:"author"`
}

// AddComment adds a general comment to a pull request.
// If parentID is greater than zero the comment is posted as a reply to it.
func (service *Service) AddComment(ctx context.Context, repository RepositoryRef, pullRequestID string, text string, parentID int64) (Comment, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return Comment{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return Comment{}, err
	}

	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return Comment{}, apperrors.New(apperrors.KindValidation, "comment text is required", nil)
	}

	payload := map[string]any{"text": trimmedText}
	if parentID > 0 {
		payload["parent"] = map[string]any{"id": parentID}
	}

	path := fmt.Sprintf("%s/%s/comments", pullRequestPath(repository), resolvedID)

	var result Comment
	if err := service.client.PostJSON(ctx, path, nil, payload, &result); err != nil {
		return Comment{}, err
	}

	return result, nil
}

// AddInlineComment adds a comment on a specific line of a file in a pull
// request diff.
func (service *Service) AddInlineComment(ctx context.Context, repository RepositoryRef, pullRequestID string, text string, anchor InlineCommentAnchor) (Comment, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return Comment{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return Comment{}, err
	}

	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return Comment{}, apperrors.New(apperrors.KindValidation, "comment text is required", nil)
	}

	trimmedPath := strings.TrimSpace(anchor.Path)
	if trimmedPath == "" {
		return Comment{}, apperrors.New(apperrors.KindValidation, "file path is required for inline comments", nil)
	}

	if anchor.Line <= 0 {
		return Comment{}, apperrors.New(apperrors.KindValidation, "line must be a positive integer", nil)
	}

	anchorFields, err := commentanchor.Payload(commentanchor.Options{
		Path:     trimmedPath,
		Line:     anchor.Line,
		LineType: anchor.LineType,
	}, commentanchor.APINames)
	if err != nil {
		return Comment{}, err
	}

	payload := map[string]any{"text": trimmedText}
	for key, value := range anchorFields {
		payload[key] = value
	}

	path := fmt.Sprintf("%s/%s/comments", pullRequestPath(repository), resolvedID)

	var result Comment
	if err := service.client.PostJSON(ctx, path, nil, payload, &result); err != nil {
		return Comment{}, commentanchor.ExplainRejection(err, commentanchor.Options{
			Path:     trimmedPath,
			Line:     anchor.Line,
			LineType: anchor.LineType,
		})
	}

	return result, nil
}

// normalizeLineType validates the diff side an inline comment anchors to,
// defaulting to ADDED when unset.
func normalizeLineType(lineType string) (string, error) {
	return commentanchor.NormalizeLineType(lineType, commentanchor.APINames)
}

func (service *Service) AddReviewer(ctx context.Context, repository RepositoryRef, pullRequestID string, username string) (PullRequest, error) {
	return service.updateReviewer(ctx, repository, pullRequestID, username, true)
}

func (service *Service) RemoveReviewer(ctx context.Context, repository RepositoryRef, pullRequestID string, username string) (PullRequest, error) {
	return service.updateReviewer(ctx, repository, pullRequestID, username, false)
}

// BuildStatus represents a single CI build status associated with a pull request.
type BuildStatus struct {
	Key   string `json:"key,omitempty"`
	State string `json:"state,omitempty"`
	URL   string `json:"url,omitempty"`
	Name  string `json:"name,omitempty"`
}

type pagedBuildStatusResponse struct {
	Values        []buildStatusValue `json:"values"`
	IsLastPage    bool               `json:"isLastPage"`
	NextPageStart int                `json:"nextPageStart"`
}

type buildStatusValue struct {
	Key   string `json:"key"`
	State string `json:"state"`
	URL   string `json:"url"`
	Name  string `json:"name"`
}

type mergeabilityValue struct {
	Conflicted bool             `json:"conflicted"`
	Outcome    string           `json:"outcome"`
	Vetoes     []mergeVetoValue `json:"vetoes"`
}

type mergeVetoValue struct {
	DetailedMessage string `json:"detailedMessage"`
	SummaryMessage  string `json:"summaryMessage"`
}

// GetBuildStatuses retrieves build statuses for the source commit of the given pull request.
func (service *Service) GetBuildStatuses(ctx context.Context, repository RepositoryRef, pullRequestID string, limit int) ([]BuildStatus, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return nil, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return nil, err
	}

	if limit <= 0 {
		limit = 25
	}

	// Fetch the PR to get the source commit hash.
	var pr pullRequestValue
	if err := service.client.GetJSON(ctx, fmt.Sprintf("%s/%s", pullRequestPath(repository), resolvedID), nil, &pr); err != nil {
		return nil, err
	}

	commitID := sourceCommit(pr.FromRef)
	if commitID == "" {
		return nil, apperrors.New(apperrors.KindInternal, "pull request has no resolvable source commit", nil)
	}

	// Fetch build statuses for the source commit via the build-status API.
	path := fmt.Sprintf("/rest/build-status/latest/commits/%s", url.PathEscape(commitID))
	results := make([]BuildStatus, 0)
	start := 0

	for {
		query := map[string]string{
			"limit": strconv.Itoa(limit),
			"start": strconv.Itoa(start),
		}

		var response pagedBuildStatusResponse
		if err := service.client.GetJSON(ctx, path, query, &response); err != nil {
			return nil, err
		}

		for _, v := range response.Values {
			// A conversion rather than a literal: the two structs must stay
			// field-for-field identical, and a conversion stops compiling if
			// they diverge. The literal would silently drop a field added to
			// only one of them.
			results = append(results, BuildStatus(v))
		}

		if response.IsLastPage {
			break
		}

		if response.NextPageStart == start {
			break
		}

		start = response.NextPageStart
	}

	return results, nil
}

// GetAutoMerge returns the auto-merge configuration for a pull request.
// When auto-merge is not configured, Enabled is false and no error is returned.
func (service *Service) GetAutoMerge(ctx context.Context, repository RepositoryRef, pullRequestID string) (AutoMerge, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return AutoMerge{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return AutoMerge{}, err
	}

	var response autoMergeValue
	err = service.client.GetJSON(ctx, fmt.Sprintf("%s/%s/auto-merge", pullRequestPath(repository), resolvedID), nil, &response)
	if err != nil {
		if apperrors.IsKind(err, apperrors.KindNotFound) {
			return AutoMerge{Enabled: false}, nil
		}
		return AutoMerge{}, err
	}

	strategyID := ""
	if response.StrategyId != nil {
		strategyID = *response.StrategyId
	}
	return AutoMerge{Enabled: true, StrategyID: strategyID}, nil
}

// EnableAutoMerge enables auto-merge on a pull request using the given merge strategy.
// Valid strategy values: no-ff, ff, ff-only, rebase-no-ff, rebase-ff-only, squash,
// squash-ff-only -- openapi.MergeStrategies is the one declaration.
// EnableAutoMerge arms auto-merge so the pull request merges once its checks
// pass.
//
// This goes through the merge endpoint, not POST .../auto-merge, and the
// difference is the whole bug in #378. The auto-merge endpoint is documented as
// "requests the system to try merging the pull request *if auto-merge was
// requested on it*" — it retries an existing request. Asking it to arm one
// produces AutoMergeNotRequestedException, which reads like a server quirk and
// is in fact the server correctly reporting that nothing was ever armed.
//
// Arming is RestPullRequestMergeRequest.autoMerge on POST .../merge, a body
// field rather than a query parameter.
//
// The merge endpoint enforces optimistic locking, so a version is required. It
// is resolved here rather than pushed onto callers: every caller would
// otherwise have to fetch the pull request first to do the same thing.
func (service *Service) EnableAutoMerge(ctx context.Context, repository RepositoryRef, pullRequestID string, strategyID string) (AutoMerge, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return AutoMerge{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return AutoMerge{}, err
	}

	strategy := strings.TrimSpace(strategyID)
	if strategy == "" {
		strategy = "no-ff"
	}

	current, err := service.Get(ctx, repository, resolvedID)
	if err != nil {
		return AutoMerge{}, err
	}

	payload := map[string]any{
		"autoMerge":  true,
		"strategyId": strategy,
		"version":    current.Version,
	}

	var response pullRequestValue
	if err := service.client.PostJSON(ctx, fmt.Sprintf("%s/%s/merge", pullRequestPath(repository), resolvedID), nil, payload, &response); err != nil {
		return AutoMerge{}, err
	}

	// Arming a pull request that already satisfies its checks merges it there
	// and then — that is what auto-merge means, and the endpoint returns the
	// merged pull request. Reporting "auto-merge enabled" in that case would
	// describe a pending state that does not exist and will never fire, so the
	// two outcomes are distinguished rather than blurred.
	merged := mapPullRequest(response)
	if strings.EqualFold(strings.TrimSpace(merged.State), "MERGED") || merged.Closed {
		return AutoMerge{Enabled: false, StrategyID: strategy, MergedImmediately: true}, nil
	}

	return AutoMerge{Enabled: true, StrategyID: strategy}, nil
}

// DisableAutoMerge removes the auto-merge configuration from a pull request.
func (service *Service) DisableAutoMerge(ctx context.Context, repository RepositoryRef, pullRequestID string) error {
	if err := validateRepositoryRef(repository); err != nil {
		return err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return err
	}

	return service.client.DeleteJSON(ctx, fmt.Sprintf("%s/%s/auto-merge", pullRequestPath(repository), resolvedID), nil, nil, nil)
}

func normalizeState(state string) (string, error) {
	resolved := strings.ToLower(strings.TrimSpace(state))
	if resolved == "" {
		return "open", nil
	}

	switch resolved {
	case "open", "closed", "all":
		return resolved, nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "--state must be one of: open, closed, all", nil)
	}
}

func matchesFilters(pullRequest PullRequest, state string, sourceBranch string, targetBranch string) bool {
	switch state {
	case "open":
		if !pullRequest.Open {
			return false
		}
	case "closed":
		if pullRequest.Open && !pullRequest.Closed {
			return false
		}
	}

	if !branchMatches(sourceBranch, pullRequest.SourceBranch) {
		return false
	}

	if !branchMatches(targetBranch, pullRequest.TargetBranch) {
		return false
	}

	return true
}

func branchMatches(filter string, actual string) bool {
	trimmedFilter := strings.TrimSpace(filter)
	if trimmedFilter == "" {
		return true
	}

	return normalizeBranch(trimmedFilter) == normalizeBranch(actual)
}

func normalizeBranch(branch string) string {
	trimmed := strings.TrimSpace(branch)
	trimmed = strings.TrimPrefix(trimmed, "refs/heads/")
	return strings.ToLower(trimmed)
}

func mapPullRequest(raw pullRequestValue) PullRequest {
	author := ""
	authorUsername := ""
	if raw.Author != nil && raw.Author.User != nil {
		authorUsername = strings.TrimSpace(raw.Author.User.Name)
		author = strings.TrimSpace(raw.Author.User.DisplayName)
		if author == "" {
			author = authorUsername
		}
	}

	pr := PullRequest{
		ID:             raw.ID,
		Title:          raw.Title,
		Description:    strings.TrimSpace(raw.Description),
		State:          strings.TrimSpace(raw.State),
		Open:           raw.Open,
		Closed:         raw.Closed,
		Draft:          raw.Draft,
		Version:        raw.Version,
		Author:         author,
		AuthorUsername: authorUsername,
		SourceBranch:   branchDisplayName(raw.FromRef),
		TargetBranch:   branchDisplayName(raw.ToRef),
		SourceCommit:   sourceCommit(raw.FromRef),
		CreatedDate:    raw.CreatedDate,
		UpdatedDate:    raw.UpdatedDate,
		Reviewers:      mapReviewers(raw.Participants, raw.Reviewers),
	}

	if raw.ToRef != nil && raw.ToRef.Repository != nil {
		pr.Repository = &RepositoryRef{
			ProjectKey: raw.ToRef.Repository.Project.Key,
			Slug:       raw.ToRef.Repository.Slug,
		}
	}

	if raw.FromRef != nil && raw.FromRef.Repository != nil {
		pr.SourceRepository = &RepositoryRef{
			ProjectKey: raw.FromRef.Repository.Project.Key,
			Slug:       raw.FromRef.Repository.Slug,
		}
	}

	if raw.Properties != nil {
		pr.CommentCount = raw.Properties.CommentCount
		pr.OpenTaskCount = raw.Properties.OpenTaskCount
		pr.ResolvedTaskCount = raw.Properties.ResolvedTaskCount
	}

	return pr
}

// mapReviewers maps PR participants to reviewers. On Bitbucket Data Center 9.4.16,
// the PR response "participants" field is always empty; only "reviewers" contains
// the assigned reviewers with their approval status. Falls back to "reviewers"
// when "participants" is empty. Filters out role=="author" as a safety net.
func mapReviewers(participants []pullRequestParticipant, reviewers []pullRequestParticipant) []Reviewer {
	raw := participants
	if len(raw) == 0 {
		raw = reviewers
	}
	if len(raw) == 0 {
		return nil
	}

	result := make([]Reviewer, 0, len(raw))
	for _, participant := range raw {
		if participant.User == nil {
			continue
		}

		reviewer := Reviewer{
			Name:        strings.TrimSpace(participant.User.Name),
			DisplayName: strings.TrimSpace(participant.User.DisplayName),
			Email:       strings.TrimSpace(participant.User.EmailAddress),
			Role:        strings.TrimSpace(participant.Role),
			Status:      strings.TrimSpace(participant.Status),
			Approved:    participant.Approved,
		}

		if strings.ToLower(reviewer.Role) == "author" {
			continue
		}

		result = append(result, reviewer)
	}

	if len(result) == 0 {
		return nil
	}

	return result
}

func mapMergeability(raw mergeabilityValue) Mergeability {
	blockers := make([]MergeBlocker, 0, len(raw.Vetoes))
	for _, veto := range raw.Vetoes {
		summary := strings.TrimSpace(veto.SummaryMessage)
		detail := strings.TrimSpace(veto.DetailedMessage)
		if summary == "" && detail == "" {
			continue
		}

		blockers = append(blockers, MergeBlocker{
			Summary: summary,
			Detail:  detail,
		})
	}

	mergedOutcome := strings.TrimSpace(raw.Outcome)
	mergeable := !raw.Conflicted && len(blockers) == 0
	if mergedOutcome != "" {
		mergeable = mergeable && strings.EqualFold(mergedOutcome, "CLEAN")
	}

	return Mergeability{
		Mergeable:  mergeable,
		Outcome:    mergedOutcome,
		Conflicted: raw.Conflicted,
		Blockers:   blockers,
	}
}

func validateRepositoryRef(repository RepositoryRef) error {
	if strings.TrimSpace(repository.ProjectKey) == "" || strings.TrimSpace(repository.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}

	return nil
}

func normalizePullRequestID(pullRequestID string) (string, error) {
	resolved := strings.TrimSpace(pullRequestID)
	if resolved == "" {
		return "", apperrors.New(apperrors.KindValidation, "pull request id is required", nil)
	}

	if _, err := strconv.ParseInt(resolved, 10, 64); err != nil {
		return "", apperrors.New(apperrors.KindValidation, "pull request id must be a valid integer", nil)
	}

	return resolved, nil
}

func pullRequestPath(repository RepositoryRef) string {
	return fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", repository.ProjectKey, repository.Slug)
}

func buildCreatePayload(input CreateInput) (map[string]any, error) {
	fromRef := strings.TrimSpace(input.FromRef)
	toRef := strings.TrimSpace(input.ToRef)
	title := strings.TrimSpace(input.Title)

	if fromRef == "" {
		return nil, apperrors.New(apperrors.KindValidation, "from ref is required", nil)
	}
	if toRef == "" {
		return nil, apperrors.New(apperrors.KindValidation, "to ref is required", nil)
	}
	if title == "" {
		return nil, apperrors.New(apperrors.KindValidation, "title is required", nil)
	}

	from := map[string]any{"id": normalizeBranchRef(fromRef)}
	if input.FromRepository != nil {
		if err := validateRepositoryRef(*input.FromRepository); err != nil {
			return nil, err
		}
		// Bitbucket takes the source repository on fromRef. Without it every
		// pull request is same-repository, which is why a fork could not open
		// one upstream however the permissions were set (#506).
		from["repository"] = map[string]any{
			"slug":    input.FromRepository.Slug,
			"project": map[string]any{"key": input.FromRepository.ProjectKey},
		}
	}

	payload := map[string]any{
		"title":   title,
		"fromRef": from,
		"toRef":   map[string]any{"id": normalizeBranchRef(toRef)},
	}

	if input.Draft {
		payload["draft"] = true
	}

	if description := strings.TrimSpace(input.Description); description != "" {
		payload["description"] = description
	}

	if len(input.Reviewers) > 0 {
		reviewers := make([]map[string]any, 0, len(input.Reviewers))
		for _, name := range input.Reviewers {
			if n := strings.TrimSpace(name); n != "" {
				reviewers = append(reviewers, map[string]any{
					"user": map[string]any{"name": n},
					"role": "REVIEWER",
				})
			}
		}
		if len(reviewers) > 0 {
			payload["reviewers"] = reviewers
		}
	}

	return payload, nil
}

// hasUpdatableField reports whether the caller asked for a change, ignoring
// the keys that always travel: version, and the reviewer set that has to be
// echoed back so a PUT does not clear it.
// hasUpdatableField reports whether the caller named anything to change.
//
// "version" never counts: it is the precondition, not a change. "reviewers"
// counts only when the caller asked for it. The service also writes that key on
// its own, echoing the current list back so an update does not clear it (#511),
// and an echo is not a request to change anything -- treating it as one would
// let `bb pr update --version 3` through as a no-op write.
func hasUpdatableField(payload map[string]any, reviewersRequested bool) bool {
	for key := range payload {
		switch key {
		case "version":
		case "reviewers":
			if reviewersRequested {
				return true
			}
		default:
			return true
		}
	}

	return false
}

func buildUpdatePayload(input UpdateInput) (map[string]any, error) {
	payload := map[string]any{}

	if input.Version < 0 {
		return nil, apperrors.New(apperrors.KindValidation, "version must be greater than or equal to 0", nil)
	}
	payload["version"] = input.Version

	if title := strings.TrimSpace(input.Title); title != "" {
		payload["title"] = title
	}
	if description := strings.TrimSpace(input.Description); description != "" {
		payload["description"] = description
	}
	if input.Draft != nil {
		payload["draft"] = *input.Draft
	}

	// A PUT replaces the pull request, and Bitbucket reads an absent reviewers
	// key as "no reviewers" rather than "unchanged" -- confirmed against a
	// running Data Center, where a body of version plus description emptied a
	// reviewer set (#511). So the key always travels, carrying the set the
	// caller wants: the current one, echoed back by Update, unless they asked
	// for a different one.
	if input.Reviewers != nil {
		reviewers := make([]map[string]any, 0, len(*input.Reviewers))
		for _, name := range *input.Reviewers {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				continue
			}
			reviewers = append(reviewers, map[string]any{"user": map[string]any{"name": trimmed}})
		}
		payload["reviewers"] = reviewers
	}

	if !hasUpdatableField(payload, input.Reviewers != nil) {
		return nil, apperrors.New(apperrors.KindValidation, "at least one of title, description, draft, or reviewers is required", nil)
	}

	return payload, nil
}

func normalizeBranchRef(branch string) string {
	trimmed := strings.TrimSpace(branch)
	if strings.HasPrefix(trimmed, "refs/") {
		return trimmed
	}

	return "refs/heads/" + trimmed
}

func (service *Service) transition(ctx context.Context, repository RepositoryRef, pullRequestID string, action string, version *int) (PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return PullRequest{}, err
	}

	// Bitbucket does not read an absent version as "whatever is current". It
	// defaults expectedVersion to -1, compares it strictly, and answers 409 --
	// so merge, decline and reopen could not succeed without the caller first
	// looking the number up by hand (#505). Read it here instead.
	//
	// This narrows nothing: the version guards against acting on a pull request
	// that moved since the caller last looked, and a caller who omitted the
	// flag was never making that claim. One who wants the guard still passes
	// --version and still gets the conflict.
	resolvedVersion := version
	if resolvedVersion == nil {
		current, err := service.Get(ctx, repository, resolvedID)
		if err != nil {
			return PullRequest{}, err
		}
		resolvedVersion = &current.Version
	}

	query := map[string]string{"version": strconv.Itoa(*resolvedVersion)}

	var response pullRequestValue
	if err := service.client.PostJSON(ctx, fmt.Sprintf("%s/%s/%s", pullRequestPath(repository), resolvedID, action), query, map[string]any{}, &response); err != nil {
		return PullRequest{}, err
	}

	return mapPullRequest(response), nil
}

func (service *Service) updateReviewer(ctx context.Context, repository RepositoryRef, pullRequestID string, username string, add bool) (PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return PullRequest{}, err
	}

	trimmedUsername := strings.TrimSpace(username)
	if trimmedUsername == "" {
		return PullRequest{}, apperrors.New(apperrors.KindValidation, "reviewer username is required", nil)
	}

	var response pullRequestValue
	if add {
		path := fmt.Sprintf("%s/%s/participants", pullRequestPath(repository), resolvedID)
		payload := map[string]any{
			"user": map[string]any{
				"name": trimmedUsername,
			},
			"role": "REVIEWER",
		}
		if err := service.client.PostJSON(ctx, path, nil, payload, &response); err != nil {
			return PullRequest{}, err
		}
	} else {
		path := fmt.Sprintf("%s/%s/participants/%s", pullRequestPath(repository), resolvedID, url.PathEscape(trimmedUsername))
		if err := service.client.DeleteJSON(ctx, path, nil, nil, &response); err != nil {
			return PullRequest{}, err
		}
	}

	return mapPullRequest(response), nil
}

func branchDisplayName(reference *pullRequestRef) string {
	if reference == nil {
		return ""
	}

	display := strings.TrimSpace(reference.DisplayID)
	if display != "" {
		return display
	}

	return strings.TrimSpace(reference.ID)
}

func sourceCommit(reference *pullRequestRef) string {
	if reference == nil {
		return ""
	}

	return strings.TrimSpace(reference.LatestCommit)
}

type pagedPullRequestResponse struct {
	Values        []pullRequestValue `json:"values"`
	IsLastPage    bool               `json:"isLastPage"`
	NextPageStart int                `json:"nextPageStart"`
}

type pullRequestValue struct {
	ID           int64                    `json:"id"`
	Title        string                   `json:"title"`
	Description  string                   `json:"description"`
	State        string                   `json:"state"`
	Open         bool                     `json:"open"`
	Closed       bool                     `json:"closed"`
	Draft        bool                     `json:"draft"`
	Version      int                      `json:"version"`
	CreatedDate  int64                    `json:"createdDate"`
	UpdatedDate  int64                    `json:"updatedDate"`
	Author       *pullRequestUser         `json:"author"`
	Participants []pullRequestParticipant `json:"participants"`
	Reviewers    []pullRequestParticipant `json:"reviewers"`
	FromRef      *pullRequestRef          `json:"fromRef"`
	ToRef        *pullRequestRef          `json:"toRef"`
	Properties   *pullRequestProperties   `json:"properties"`
}

// pullRequestProperties carries the comment and task counters Bitbucket attaches
// to pull request payloads. They are not part of the published spec, so every
// field is optional.
type pullRequestProperties struct {
	CommentCount      *int `json:"commentCount"`
	OpenTaskCount     *int `json:"openTaskCount"`
	ResolvedTaskCount *int `json:"resolvedTaskCount"`
}

type autoMergeValue struct {
	StrategyId *string `json:"strategyId"`
}

type pullRequestParticipant struct {
	User     *pullRequestUserIdentity `json:"user"`
	Role     string                   `json:"role"`
	Status   string                   `json:"status"`
	Approved bool                     `json:"approved"`
}

type pullRequestUser struct {
	User *pullRequestUserIdentity `json:"user"`
}

type pullRequestUserIdentity struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
}

type pullRequestRef struct {
	ID           string                 `json:"id"`
	DisplayID    string                 `json:"displayId"`
	LatestCommit string                 `json:"latestCommit"`
	Repository   *pullRequestRepository `json:"repository"`
}

type pullRequestRepository struct {
	Slug    string             `json:"slug"`
	Project pullRequestProject `json:"project"`
}

type pullRequestProject struct {
	Key string `json:"key"`
}

func (service *Service) Watch(ctx context.Context, repository RepositoryRef, pullRequestID string) error {
	if err := validateRepositoryRef(repository); err != nil {
		return err
	}
	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return err
	}
	if service.apiClient == nil {
		return apperrors.New(apperrors.KindInternal, "openapi client is not configured on pullrequest service", nil)
	}

	var wrapper struct {
		client *openapigenerated.ClientWithResponses
	}
	wrapper.client = service.apiClient

	response, err := wrapper.client.Watch1WithResponse(ctx, repository.ProjectKey, repository.Slug, resolvedID)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to watch pull request", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) Unwatch(ctx context.Context, repository RepositoryRef, pullRequestID string) error {
	if err := validateRepositoryRef(repository); err != nil {
		return err
	}
	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return err
	}
	if service.apiClient == nil {
		return apperrors.New(apperrors.KindInternal, "openapi client is not configured on pullrequest service", nil)
	}

	var wrapper struct {
		client *openapigenerated.ClientWithResponses
	}
	wrapper.client = service.apiClient

	response, err := wrapper.client.Unwatch1WithResponse(ctx, repository.ProjectKey, repository.Slug, resolvedID)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to unwatch pull request", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) CanRebase(ctx context.Context, repository RepositoryRef, pullRequestID string) (*openapigenerated.RestPullRequestRebaseability, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return nil, err
	}
	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return nil, err
	}
	if service.apiClient == nil {
		return nil, apperrors.New(apperrors.KindInternal, "openapi client is not configured on pullrequest service", nil)
	}

	var wrapper struct {
		client *openapigenerated.ClientWithResponses
	}
	wrapper.client = service.apiClient

	response, err := wrapper.client.CanRebaseWithResponse(ctx, repository.ProjectKey, repository.Slug, resolvedID)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to check rebase status", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	if response.ApplicationjsonCharsetUTF8200 == nil {
		return nil, apperrors.New(apperrors.KindInternal, "unexpected empty rebaseability response body", nil)
	}
	return response.ApplicationjsonCharsetUTF8200, nil
}

func (service *Service) Rebase(ctx context.Context, repository RepositoryRef, pullRequestID string, version *int) (*openapigenerated.RestPullRequestRebaseResult, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return nil, err
	}
	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return nil, err
	}
	if service.apiClient == nil {
		return nil, apperrors.New(apperrors.KindInternal, "openapi client is not configured on pullrequest service", nil)
	}

	var request openapigenerated.RestPullRequestRebaseRequest
	if version != nil {
		// The API field is 32-bit; a version outside its range wrapped rather
		// than being rejected, which would rebase against the wrong version.
		if *version < 0 || *version > math.MaxInt32 {
			return nil, apperrors.New(
				apperrors.KindValidation,
				fmt.Sprintf("pull request version must be between 0 and %d", math.MaxInt32),
				nil,
			)
		}
		v32 := int32(*version)
		request.Version = &v32
	}

	var wrapper struct {
		client *openapigenerated.ClientWithResponses
	}
	wrapper.client = service.apiClient

	response, err := wrapper.client.RebaseWithResponse(ctx, repository.ProjectKey, repository.Slug, resolvedID, request)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to rebase pull request", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	if response.ApplicationjsonCharsetUTF8200 == nil {
		return nil, apperrors.New(apperrors.KindInternal, "unexpected empty rebase response body", nil)
	}
	return response.ApplicationjsonCharsetUTF8200, nil
}

type Participant struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayName"`
	EmailAddress string `json:"emailAddress"`
	Active       bool   `json:"active"`
}

func (service *Service) ListPullRequestsContainingCommit(ctx context.Context, repository RepositoryRef, commitID string) ([]PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return nil, err
	}
	trimmedCommit := strings.TrimSpace(commitID)
	if trimmedCommit == "" {
		return nil, apperrors.New(apperrors.KindValidation, "commit ID is required", nil)
	}
	if service.apiClient == nil {
		return nil, apperrors.New(apperrors.KindInternal, "openapi client is not configured on pullrequest service", nil)
	}

	var wrapper struct {
		client *openapigenerated.ClientWithResponses
	}
	wrapper.client = service.apiClient

	response, err := wrapper.client.GetPullRequestsWithResponse(ctx, repository.ProjectKey, repository.Slug, trimmedCommit, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to list pull requests containing commit", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
		return []PullRequest{}, nil
	}

	rawValues := *response.ApplicationjsonCharsetUTF8200.Values
	results := make([]PullRequest, 0, len(rawValues))
	for _, openapiPR := range rawValues {
		data, err := json.Marshal(openapiPR)
		if err != nil {
			return nil, apperrors.New(apperrors.KindInternal, "failed to marshal openapi pull request", err)
		}
		var raw pullRequestValue
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, apperrors.New(apperrors.KindInternal, "failed to unmarshal pull request value", err)
		}
		results = append(results, mapPullRequest(raw))
	}
	return results, nil
}

func (service *Service) SearchParticipants(ctx context.Context, repository RepositoryRef, filter string) ([]Participant, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return nil, err
	}
	trimmedFilter := strings.TrimSpace(filter)
	if trimmedFilter == "" {
		return nil, apperrors.New(apperrors.KindValidation, "search query/filter is required", nil)
	}
	if service.apiClient == nil {
		return nil, apperrors.New(apperrors.KindInternal, "openapi client is not configured on pullrequest service", nil)
	}

	var wrapper struct {
		client *openapigenerated.ClientWithResponses
	}
	wrapper.client = service.apiClient

	response, err := wrapper.client.SearchWithResponse(ctx, repository.ProjectKey, repository.Slug, &openapigenerated.SearchParams{
		Filter: &trimmedFilter,
	})
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to search participants", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	if response.ApplicationjsonCharsetUTF8200 == nil || response.ApplicationjsonCharsetUTF8200.Values == nil {
		return []Participant{}, nil
	}

	rawUsers := *response.ApplicationjsonCharsetUTF8200.Values
	results := make([]Participant, 0, len(rawUsers))
	for _, rawUser := range rawUsers {
		name := ""
		if rawUser.Name != nil {
			name = *rawUser.Name
		}
		displayName := ""
		if rawUser.DisplayName != nil {
			displayName = *rawUser.DisplayName
		}
		email := ""
		if rawUser.EmailAddress != nil {
			email = *rawUser.EmailAddress
		}
		active := false
		if rawUser.Active != nil {
			active = *rawUser.Active
		}
		results = append(results, Participant{
			Name:         name,
			DisplayName:  displayName,
			EmailAddress: email,
			Active:       active,
		})
	}
	return results, nil
}
