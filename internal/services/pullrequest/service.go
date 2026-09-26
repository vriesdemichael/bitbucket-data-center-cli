package pullrequest

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
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

// pullRequestOutOfDateException is what Bitbucket calls a rejected optimistic
// lock on a pull request: the version sent is not the version the server holds.
const pullRequestOutOfDateException = "com.atlassian.bitbucket.pull.PullRequestOutOfDateException"

type ListOptions struct {
	State        string `json:"state"`
	MaxResults   int    `json:"limit"`
	Start        int    `json:"start"`
	SourceBranch string `json:"source_branch,omitempty"`
	TargetBranch string `json:"target_branch,omitempty"`
	// No Role. The repository listing has no role parameter -- Bitbucket
	// declares none and drops the one we used to send, so the caller got every
	// open pull request while believing it had asked for its own. Role belongs
	// to DashboardListOptions, where the endpoint honours it.
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
	// Version is the optimistic lock. Nil means bb reads the current version
	// and sends that, the way merge, decline, reopen and rebase do (#532); a
	// value asserts a specific version, and a conflict is reported rather than
	// retried.
	Version *int `json:"version,omitempty"`
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
	// ProjectKey narrows the dashboard to one project. Bitbucket has no such
	// parameter, so each page is filtered as it arrives, inside the walk, and
	// MaxResults counts pull requests in the project rather than dashboard
	// rows (#576).
	ProjectKey string
	MaxResults int
	Start      int
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

	// MaxResults caps the dashboard, which it did not until now: the walk ran to
	// the last page and nothing trimmed it, so `bb pr status --limit 5` listed
	// every open pull request the caller authors and every one awaiting their
	// review. The field was already called MaxResults, which is why neither
	// ADR-074 nor its guard could see this one -- converting the loop is what
	// found it.
	path := "/rest/api/1.0/dashboard/pull-requests"

	return openapi.PageThrough(ctx, options.Start, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[PullRequest], error) {
			query := map[string]string{
				"limit": strconv.Itoa(limit),
				"start": strconv.Itoa(start),
			}
			if normalizedState != "" {
				query["state"] = bitbucketState(normalizedState)
			}
			if options.Role != "" {
				query["role"] = strings.ToUpper(options.Role)
			}
			if strings.TrimSpace(options.ParticipantStatus) != "" {
				query["participantStatus"] = strings.ToUpper(strings.TrimSpace(options.ParticipantStatus))
			}

			var response pagedPullRequestResponse
			if err := service.client.GetJSON(ctx, path, query, &response); err != nil {
				return openapi.Page[PullRequest]{}, err
			}

			mapped := make([]PullRequest, 0, len(response.Values))
			for _, value := range response.Values {
				pullRequest := mapPullRequest(value)
				if !inProject(pullRequest, options.ProjectKey) {
					continue
				}
				mapped = append(mapped, pullRequest)
			}

			return pullRequestPage(response, mapped), nil
		})
}

// inProject reports whether a pull request targets a repository in the
// project, and says yes to every pull request when no project is named.
func inProject(pullRequest PullRequest, projectKey string) bool {
	projectKey = strings.TrimSpace(projectKey)
	if projectKey == "" {
		return true
	}

	return pullRequest.Repository != nil && strings.EqualFold(pullRequest.Repository.ProjectKey, projectKey)
}

// pullRequestPage adapts a hand-decoded page for the shared walk. isLastPage is
// a plain bool here and the next start a plain int, so both are addressed.
//
// The values are passed in rather than read off the response, because one
// caller filters them and a page that filters to nothing still has to carry the
// offsets that say what comes after it.
func pullRequestPage(response pagedPullRequestResponse, values []PullRequest) openapi.Page[PullRequest] {
	isLastPage := response.IsLastPage
	next := response.NextPageStart

	return openapi.Page[PullRequest]{Values: values, IsLastPage: &isLastPage, NextPageStart: &next}
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

	// MaxResults caps the results, as it does in every other list service. This
	// loop used to run to the last page and return everything, treating the
	// value purely as a page size — so `bb pr list --limit 10` against a
	// repository with 500 open pull requests returned all 500.
	//
	// The filter runs inside the fetch, which is why this was the last loop
	// converted: matchesFilters removes entries after the server has sized the
	// page, so a page can contribute nothing while there is more behind it. A
	// walk that reads an empty page as the end would return nothing for a
	// repository full of pull requests. openapi.PageThrough looks past one, and
	// the cap counts what survived the filter rather than what came back.
	return openapi.PageThrough(ctx, options.Start, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[PullRequest], error) {
			query := map[string]string{
				"limit": strconv.Itoa(limit),
				"start": strconv.Itoa(start),
				"state": bitbucketState(normalizedState),
			}

			var response pagedPullRequestResponse
			if err := service.client.GetJSON(ctx, path, query, &response); err != nil {
				return openapi.Page[PullRequest]{}, err
			}

			matched := make([]PullRequest, 0, len(response.Values))
			for _, value := range response.Values {
				mapped := mapPullRequest(value)
				if matchesFilters(mapped, normalizedState, options.SourceBranch, options.TargetBranch) {
					matched = append(matched, mapped)
				}
			}

			return pullRequestPage(response, matched), nil
		})
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

	// Validate before fetching. The reads below cost a request, and a caller
	// who named no field or a negative version should hear that immediately
	// rather than after a round trip -- ADR-054, and the reason this is not
	// simply folded into the block that follows.
	if err := validateUpdateInput(input); err != nil {
		return PullRequest{}, err
	}

	send := func(current PullRequest) (PullRequest, error) {
		version := current.Version
		if input.Version != nil {
			version = *input.Version
		}

		payload := buildUpdatePayload(input, version)

		// The existing reviewer set is echoed back when the caller did not name
		// one. Without it the PUT drops every reviewer, which is what #511
		// reported: an update to the description silently emptied a set of six.
		if input.Reviewers == nil {
			payload["reviewers"] = reviewerEcho(current)
		}

		var response pullRequestValue
		if err := service.client.PutJSON(ctx, fmt.Sprintf("%s/%s", pullRequestPath(repository), resolvedID), nil, payload, &response); err != nil {
			return PullRequest{}, err
		}

		return mapPullRequest(response), nil
	}

	// The version is an optimistic lock the caller has no reason to know, so bb
	// reads it rather than demanding it -- #532, applied to merge, decline,
	// reopen and rebase, and missed here. It left --version as the one required
	// flag on the command that edits a title, so changing a title took a
	// get_pull_request first, and a stale read turned an edit into a 409.
	//
	// Read here it can also go stale between the read and the write (#598), so
	// this is the same retry those transitions get. A version the caller
	// asserted is not retried: the conflict is the answer they asked for.
	if input.Version == nil {
		var updated PullRequest
		err := service.writeAtCurrentVersion(ctx, repository, resolvedID, func(current PullRequest) error {
			var sendErr error
			updated, sendErr = send(current)

			return sendErr
		})
		if err != nil {
			return PullRequest{}, err
		}

		return updated, nil
	}

	if input.Reviewers != nil {
		// The caller supplied both halves; there is nothing to read.
		return send(PullRequest{})
	}

	current, err := service.Get(ctx, repository, resolvedID)
	if err != nil {
		return PullRequest{}, err
	}

	return send(current)
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
	return service.review(ctx, repository, pullRequestID, func(resolvedID string) (pullRequestParticipant, error) {
		var participant pullRequestParticipant
		err := service.client.PostJSON(ctx, fmt.Sprintf("%s/%s/approve", pullRequestPath(repository), resolvedID), nil, map[string]any{}, &participant)

		return participant, err
	})
}

func (service *Service) Unapprove(ctx context.Context, repository RepositoryRef, pullRequestID string) (PullRequest, error) {
	return service.review(ctx, repository, pullRequestID, func(resolvedID string) (pullRequestParticipant, error) {
		var participant pullRequestParticipant
		err := service.client.DeleteJSON(ctx, fmt.Sprintf("%s/%s/approve", pullRequestPath(repository), resolvedID), nil, nil, &participant)

		return participant, err
	})
}

// NeedsWork sets the current user's review status to NEEDS_WORK on a pull
// request, the equivalent of "request changes". Unlike approve and unapprove
// there is no dedicated endpoint, so this updates the participant record
// directly via PUT .../participants/{userSlug}.
func (service *Service) NeedsWork(ctx context.Context, repository RepositoryRef, pullRequestID string) (PullRequest, error) {
	return service.review(ctx, repository, pullRequestID, func(resolvedID string) (pullRequestParticipant, error) {
		userSlug, err := service.client.CurrentUserSlug(ctx)
		if err != nil {
			return pullRequestParticipant{}, err
		}

		var participant pullRequestParticipant
		path := fmt.Sprintf("%s/%s/participants/%s", pullRequestPath(repository), resolvedID, url.PathEscape(userSlug))
		err = service.client.PutJSON(ctx, path, nil, map[string]any{"status": "NEEDS_WORK"}, &participant)

		return participant, err
	})
}

// review changes the caller's own review and returns the pull request it
// leaves behind.
//
// The participant endpoints answer with the participant, not the pull request
// -- the spec calls it "Details of the new participant" -- so decoding the
// reply as a pull request left a zero in every field, and `bb pr review
// approve` reported "pull request #0" while succeeding against the right one
// (#587).
//
// So the pull request is read, before the change and after it. Before, so a
// pull request that cannot be read fails the command while nothing has been
// applied. After, so the result shows what the change did. The read after can
// fail too, and by then the review has landed: reporting a failure would send a
// caller to repeat an approval it had already given. The first read stands in,
// with the participant Bitbucket confirmed applied to it.
func (service *Service) review(ctx context.Context, repository RepositoryRef, pullRequestID string, change func(resolvedID string) (pullRequestParticipant, error)) (PullRequest, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return PullRequest{}, err
	}

	before, err := service.Get(ctx, repository, resolvedID)
	if err != nil {
		return PullRequest{}, err
	}

	participant, err := change(resolvedID)
	if err != nil {
		return PullRequest{}, err
	}

	return service.readBack(ctx, repository, resolvedID, before, func(reviewers []Reviewer) []Reviewer {
		return withParticipant(reviewers, participant)
	}), nil
}

// readBack reads a pull request after a change that has already been applied.
// When that read fails it returns before, with the change applied to its
// reviewers by apply -- see review for why a failed read is not a failure.
func (service *Service) readBack(ctx context.Context, repository RepositoryRef, resolvedID string, before PullRequest, apply func([]Reviewer) []Reviewer) PullRequest {
	if after, err := service.Get(ctx, repository, resolvedID); err == nil {
		return after
	}

	before.Reviewers = apply(before.Reviewers)

	return before
}

// withParticipant returns reviewers with a participant's record in place of
// the one with the same name, or added where there was none. A reply that
// names nobody, or names the author, leaves the list as it was.
func withParticipant(reviewers []Reviewer, participant pullRequestParticipant) []Reviewer {
	mapped := mapReviewers([]pullRequestParticipant{participant}, nil)
	if len(mapped) == 0 {
		return reviewers
	}

	updated := make([]Reviewer, 0, len(reviewers)+1)
	replaced := false
	for _, reviewer := range reviewers {
		if strings.EqualFold(reviewer.Name, mapped[0].Name) {
			updated = append(updated, mapped[0])
			replaced = true

			continue
		}
		updated = append(updated, reviewer)
	}
	if !replaced {
		updated = append(updated, mapped[0])
	}

	return updated
}

// withoutReviewer returns reviewers without the one named.
func withoutReviewer(reviewers []Reviewer, name string) []Reviewer {
	remaining := make([]Reviewer, 0, len(reviewers))
	for _, reviewer := range reviewers {
		if !strings.EqualFold(reviewer.Name, name) {
			remaining = append(remaining, reviewer)
		}
	}

	return remaining
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
func (service *Service) GetBuildStatuses(ctx context.Context, repository RepositoryRef, pullRequestID string, maxResults int) ([]BuildStatus, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return nil, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return nil, err
	}

	if maxResults <= 0 {
		maxResults = 25
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

	return openapi.PageThrough(ctx, 0, maxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[BuildStatus], error) {
			query := map[string]string{
				"limit": strconv.Itoa(limit),
				"start": strconv.Itoa(start),
			}

			var response pagedBuildStatusResponse
			if err := service.client.GetJSON(ctx, path, query, &response); err != nil {
				return openapi.Page[BuildStatus]{}, err
			}

			statuses := make([]BuildStatus, 0, len(response.Values))
			for _, value := range response.Values {
				// A conversion rather than a literal: the two structs must stay
				// field-for-field identical, and a conversion stops compiling if
				// they diverge. The literal would silently drop a field added to
				// only one of them.
				statuses = append(statuses, BuildStatus(value))
			}

			return openapi.Page[BuildStatus]{
				Values:        statuses,
				IsLastPage:    &response.IsLastPage,
				NextPageStart: &response.NextPageStart,
			}, nil
		})
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
// otherwise have to fetch the pull request first to do the same thing. A
// caller that names one is held to it, as the MCP server holds a person's
// confirmation to the pull request they were shown: arming may merge at once,
// and a pull request that changed since then is refused rather than merged.
func (service *Service) EnableAutoMerge(ctx context.Context, repository RepositoryRef, pullRequestID string, strategyID string, version *int) (AutoMerge, error) {
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

	if version == nil {
		current, err := service.Get(ctx, repository, resolvedID)
		if err != nil {
			return AutoMerge{}, err
		}
		version = &current.Version
	}

	payload := map[string]any{
		"autoMerge":  true,
		"strategyId": strategy,
		"version":    *version,
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

// bitbucketState turns the state a caller asked for into one the server will
// accept.
//
// Bitbucket takes OPEN, DECLINED, MERGED and ALL. It does not take CLOSED:
//
//	400 {"errors":[{"message":"Pull request state should be one of:
//	     [DECLINED, MERGED, OPEN]", ...}]}
//
// "closed" is bb's own idea -- declined and merged together -- and there is no
// single value for it, so the request asks for everything and matchesFilters
// narrows the answer. It already did that; the state was being sent as well,
// which made `bb pr list --state closed` fail outright on a value the CLI's own
// help offers.
func bitbucketState(normalized string) string {
	switch normalized {
	case "open":
		return "OPEN"
	case "closed":
		return "ALL"
	default:
		return "ALL"
	}
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
	return openapi.ValidateRepository(repository.ProjectKey, repository.Slug)
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
	return fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/pull-requests", url.PathEscape(repository.ProjectKey), url.PathEscape(repository.Slug))
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

// validateUpdateInput refuses an update that cannot be sent, before anything is
// read.
//
// A version is the precondition rather than a change, and the reviewer set the
// service echoes back is not one either (#511), so neither on its own is an
// update: `bb pr update 42 --version 3` names nothing to do.
func validateUpdateInput(input UpdateInput) error {
	if input.Version != nil && *input.Version < 0 {
		return apperrors.New(apperrors.KindValidation, "version must be greater than or equal to 0", nil)
	}

	if strings.TrimSpace(input.Title) == "" &&
		strings.TrimSpace(input.Description) == "" &&
		input.Draft == nil &&
		input.Reviewers == nil {
		return apperrors.New(apperrors.KindValidation, "at least one of title, description, draft, or reviewers is required", nil)
	}

	return nil
}

// buildUpdatePayload is the body of the PUT, at the version the caller asserted
// or the one bb read.
func buildUpdatePayload(input UpdateInput, version int) map[string]any {
	payload := map[string]any{}

	payload["version"] = version

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

	return payload
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

	// Read before and after, as review does and for the same reasons. Adding a
	// participant answers with the participant, and removing one answers with
	// nothing at all -- which decoded as a pull request was #0 again (#587).
	before, err := service.Get(ctx, repository, resolvedID)
	if err != nil {
		return PullRequest{}, err
	}

	if add {
		var participant pullRequestParticipant
		path := fmt.Sprintf("%s/%s/participants", pullRequestPath(repository), resolvedID)
		payload := map[string]any{
			"user": map[string]any{
				"name": trimmedUsername,
			},
			"role": "REVIEWER",
		}
		if err := service.client.PostJSON(ctx, path, nil, payload, &participant); err != nil {
			return PullRequest{}, err
		}

		return service.readBack(ctx, repository, resolvedID, before, func(reviewers []Reviewer) []Reviewer {
			return withParticipant(reviewers, participant)
		}), nil
	}

	path := fmt.Sprintf("%s/%s/participants/%s", pullRequestPath(repository), resolvedID, url.PathEscape(trimmedUsername))
	if err := service.client.DeleteJSON(ctx, path, nil, nil, nil); err != nil {
		return PullRequest{}, err
	}

	return service.readBack(ctx, repository, resolvedID, before, func(reviewers []Reviewer) []Reviewer {
		return withoutReviewer(reviewers, trimmedUsername)
	}), nil
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

	response, err := service.apiClient.Watch1WithResponse(ctx, repository.ProjectKey, repository.Slug, resolvedID)
	if err != nil {
		return apperrors.Transport("failed to watch pull request", err)
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

	response, err := service.apiClient.Unwatch1WithResponse(ctx, repository.ProjectKey, repository.Slug, resolvedID)
	if err != nil {
		return apperrors.Transport("failed to unwatch pull request", err)
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

	response, err := service.apiClient.CanRebaseWithResponse(ctx, repository.ProjectKey, repository.Slug, resolvedID)
	if err != nil {
		return nil, apperrors.Transport("failed to check rebase status", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}
	if response.ApplicationjsonCharsetUTF8200 == nil {
		return nil, openapi.MissingPayload(response.StatusCode(), response.Body, "reading rebaseability")
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

	// Bitbucket requires the version outright here -- "The \"version\" property
	// is required to rebase a pull request", 400 -- so leaving it out when the
	// caller did could never succeed. Same shape as the transitions in #505,
	// found the same way: the version is an optimistic lock the caller has no
	// reason to know, so read it rather than demand it (#532).
	//
	// A caller who does pass --version still gets the lock, pays no extra
	// request for it, and sees its conflict: only a version bb read itself is
	// retried when it goes stale (writeAtCurrentVersion).
	var response *openapigenerated.RebaseResponse
	rebaseAt := func(at int) error {
		var request openapigenerated.RestPullRequestRebaseRequest
		// The API field is 32-bit; a version outside its range wrapped rather
		// than being rejected, which would rebase against the wrong version.
		if at < 0 || at > math.MaxInt32 {
			return apperrors.New(
				apperrors.KindValidation,
				fmt.Sprintf("pull request version must be between 0 and %d", math.MaxInt32),
				nil,
			)
		}
		v32 := int32(at)
		request.Version = &v32

		answered, err := service.apiClient.RebaseWithResponse(ctx, repository.ProjectKey, repository.Slug, resolvedID, request)
		if err != nil {
			return apperrors.Transport("failed to rebase pull request", err)
		}
		response = answered

		return openapi.MapStatusError(answered.StatusCode(), answered.Body)
	}

	if version != nil {
		err = rebaseAt(*version)
	} else {
		// Reading the version needs the REST client. Say so rather than
		// dereferencing a nil one.
		if service.client == nil {
			return nil, apperrors.New(apperrors.KindInternal, "http client is not configured on pullrequest service", nil)
		}

		err = service.writeAtCurrentVersion(ctx, repository, resolvedID, func(current PullRequest) error {
			return rebaseAt(current.Version)
		})
	}
	if err != nil {
		return nil, err
	}
	// A branch already sitting on the tip of its target answers 204 with no
	// body: there was nothing to replay, which is success. The spec documents
	// only the 200, so the generated client has nowhere to put that and leaves
	// the payload nil, which read as the server having sent something broken
	// (OPENAPI-028).
	//
	// Reporting it as an internal error told the caller bb had failed, at exit
	// 1, for a pull request that was already exactly where it was asked to be.
	if response.StatusCode() == http.StatusNoContent {
		return nil, nil
	}

	if response.ApplicationjsonCharsetUTF8200 == nil {
		return nil, openapi.MissingPayload(response.StatusCode(), response.Body, "rebasing the pull request")
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

	response, err := service.apiClient.GetPullRequestsWithResponse(ctx, repository.ProjectKey, repository.Slug, trimmedCommit, nil)
	if err != nil {
		return nil, apperrors.Transport("failed to list pull requests containing commit", err)
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

	response, err := service.apiClient.SearchWithResponse(ctx, repository.ProjectKey, repository.Slug, &openapigenerated.SearchParams{
		Filter: &trimmedFilter,
	})
	if err != nil {
		return nil, apperrors.Transport("failed to search participants", err)
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
