package pullrequest

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

type RepositoryRef struct {
	ProjectKey string `json:"project_key"`
	Slug       string `json:"slug"`
}

type ListOptions struct {
	State        string `json:"state"`
	Limit        int    `json:"limit"`
	Start        int    `json:"start"`
	SourceBranch string `json:"source_branch,omitempty"`
	TargetBranch string `json:"target_branch,omitempty"`
}

type PullRequest struct {
	ID           int64          `json:"id"`
	Title        string         `json:"title"`
	Description  string         `json:"description,omitempty"`
	State        string         `json:"state"`
	Open         bool           `json:"open"`
	Closed       bool           `json:"closed"`
	Draft        bool           `json:"draft,omitempty"`
	Repository   *RepositoryRef `json:"repository,omitempty"`
	Version      int            `json:"version,omitempty"`
	Author       string         `json:"author,omitempty"`
	SourceBranch string         `json:"source_branch,omitempty"`
	TargetBranch string         `json:"target_branch,omitempty"`
	SourceCommit string         `json:"source_commit,omitempty"`
	CreatedDate  int64          `json:"created_date,omitempty"`
	UpdatedDate  int64          `json:"updated_date,omitempty"`
	Reviewers    []Reviewer     `json:"reviewers,omitempty"`
	Mergeability *Mergeability  `json:"mergeability,omitempty"`
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
}

type UpdateInput struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Version     int    `json:"version"`
	// Draft, when non-nil, sets or clears the draft flag on the pull request.
	Draft *bool `json:"draft,omitempty"`
}

// AutoMerge represents the auto-merge configuration for a pull request.
// Enabled is false when no auto-merge is configured (the API returns 404).
type AutoMerge struct {
	Enabled    bool   `json:"enabled"`
	StrategyID string `json:"strategy_id,omitempty"`
}

type TaskListOptions struct {
	State string `json:"state"`
	Limit int    `json:"limit"`
	Start int    `json:"start"`
}

type Task struct {
	ID          int64  `json:"id"`
	Text        string `json:"text"`
	State       string `json:"state,omitempty"`
	Resolved    bool   `json:"resolved"`
	Version     int    `json:"version,omitempty"`
	CreatedDate int64  `json:"created_date,omitempty"`
	UpdatedDate int64  `json:"updated_date,omitempty"`
	Author      string `json:"author,omitempty"`
	Assignee    string `json:"assignee,omitempty"`
}

type Service struct {
	client *httpclient.Client
}

func NewService(client *httpclient.Client) *Service {
	return &Service{client: client}
}

type DashboardListOptions struct {
	State string
	Role  string
	Limit int
	Start int
}

func (service *Service) ListDashboard(ctx context.Context, options DashboardListOptions) ([]PullRequest, error) {
	normalizedState, err := normalizeState(options.State)
	if err != nil {
		return nil, err
	}

	if options.Limit <= 0 {
		options.Limit = 25
	}
	if options.Start < 0 {
		return nil, apperrors.New(apperrors.KindValidation, "start must be greater than or equal to 0", nil)
	}

	path := "/rest/api/1.0/dashboard/pull-requests"
	results := make([]PullRequest, 0)
	start := options.Start

	for {
		query := map[string]string{
			"limit": strconv.Itoa(options.Limit),
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

	if options.Limit <= 0 {
		options.Limit = 25
	}
	if options.Start < 0 {
		return nil, apperrors.New(apperrors.KindValidation, "start must be greater than or equal to 0", nil)
	}

	path := pullRequestPath(repository)
	results := make([]PullRequest, 0)
	start := options.Start

	for {
		query := map[string]string{
			"limit": strconv.Itoa(options.Limit),
			"start": strconv.Itoa(start),
		}
		if normalizedState == "open" {
			query["state"] = "OPEN"
		} else if normalizedState != "all" {
			query["state"] = strings.ToUpper(normalizedState)
		} else {
			query["state"] = "ALL"
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

	payload, err := buildUpdatePayload(input)
	if err != nil {
		return PullRequest{}, err
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

func (service *Service) AddReviewer(ctx context.Context, repository RepositoryRef, pullRequestID string, username string) (PullRequest, error) {
	return service.updateReviewer(ctx, repository, pullRequestID, username, true)
}

func (service *Service) RemoveReviewer(ctx context.Context, repository RepositoryRef, pullRequestID string, username string) (PullRequest, error) {
	return service.updateReviewer(ctx, repository, pullRequestID, username, false)
}

func (service *Service) ListTasks(ctx context.Context, repository RepositoryRef, pullRequestID string, options TaskListOptions) ([]Task, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return nil, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return nil, err
	}

	normalizedState, err := normalizeTaskState(options.State)
	if err != nil {
		return nil, err
	}

	if options.Limit <= 0 {
		options.Limit = 25
	}
	if options.Start < 0 {
		return nil, apperrors.New(apperrors.KindValidation, "start must be greater than or equal to 0", nil)
	}

	path := fmt.Sprintf("%s/%s/tasks", pullRequestPath(repository), resolvedID)
	results := make([]Task, 0)
	start := options.Start

	for {
		query := map[string]string{
			"limit": strconv.Itoa(options.Limit),
			"start": strconv.Itoa(start),
		}

		var response pagedTaskResponse
		if err := service.client.GetJSON(ctx, path, query, &response); err != nil {
			return nil, err
		}

		for _, value := range response.Values {
			mapped := mapTask(value)
			if taskMatchesState(mapped, normalizedState) {
				results = append(results, mapped)
			}
		}

		if response.IsLastPage || response.NextPageStart == start {
			break
		}

		start = response.NextPageStart
	}

	return results, nil
}

func (service *Service) CreateTask(ctx context.Context, repository RepositoryRef, pullRequestID string, text string) (Task, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return Task{}, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return Task{}, err
	}

	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return Task{}, apperrors.New(apperrors.KindValidation, "task text is required", nil)
	}

	var response taskValue
	if err := service.client.PostJSON(ctx, fmt.Sprintf("%s/%s/tasks", pullRequestPath(repository), resolvedID), nil, map[string]any{"text": trimmedText}, &response); err != nil {
		return Task{}, err
	}

	return mapTask(response), nil
}

func (service *Service) UpdateTask(ctx context.Context, repository RepositoryRef, pullRequestID string, taskID string, text string, resolved *bool, version *int) (Task, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return Task{}, err
	}

	resolvedPRID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return Task{}, err
	}

	resolvedTaskID, err := normalizeTaskID(taskID)
	if err != nil {
		return Task{}, err
	}

	payload := map[string]any{}
	if strings.TrimSpace(text) != "" {
		payload["text"] = strings.TrimSpace(text)
	}
	if resolved != nil {
		payload["state"] = resolveTaskStateValue(*resolved)
	}
	if version != nil {
		payload["version"] = *version
	}

	if len(payload) == 0 {
		return Task{}, apperrors.New(apperrors.KindValidation, "at least one of text, resolved, or version is required", nil)
	}

	var response taskValue
	if err := service.client.PutJSON(ctx, fmt.Sprintf("%s/%s/tasks/%s", pullRequestPath(repository), resolvedPRID, resolvedTaskID), nil, payload, &response); err != nil {
		return Task{}, err
	}

	return mapTask(response), nil
}

func (service *Service) DeleteTask(ctx context.Context, repository RepositoryRef, pullRequestID string, taskID string, version *int) error {
	if err := validateRepositoryRef(repository); err != nil {
		return err
	}

	resolvedPRID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return err
	}

	resolvedTaskID, err := normalizeTaskID(taskID)
	if err != nil {
		return err
	}

	query := map[string]string{}
	if version != nil {
		query["version"] = strconv.Itoa(*version)
	}

	return service.client.DeleteJSON(ctx, fmt.Sprintf("%s/%s/tasks/%s", pullRequestPath(repository), resolvedPRID, resolvedTaskID), query, nil, nil)
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
			results = append(results, BuildStatus{
				Key:   v.Key,
				State: v.State,
				URL:   v.URL,
				Name:  v.Name,
			})
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
// Valid strategy values: no-ff, ff-only, rebase-no-ff, rebase-ff-only, squash, squash-ff-only.
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

	payload := map[string]any{"strategyId": strategy}
	var response autoMergeValue
	if err := service.client.PostJSON(ctx, fmt.Sprintf("%s/%s/auto-merge", pullRequestPath(repository), resolvedID), nil, payload, &response); err != nil {
		return AutoMerge{}, err
	}

	responseStrategy := strategy
	if response.StrategyId != nil {
		responseStrategy = *response.StrategyId
	}
	return AutoMerge{Enabled: true, StrategyID: responseStrategy}, nil
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

func normalizeTaskState(state string) (string, error) {
	resolved := strings.ToLower(strings.TrimSpace(state))
	if resolved == "" {
		return "open", nil
	}

	switch resolved {
	case "open", "resolved", "all":
		return resolved, nil
	default:
		return "", apperrors.New(apperrors.KindValidation, "--state must be one of: open, resolved, all", nil)
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
	if raw.Author != nil && raw.Author.User != nil {
		author = strings.TrimSpace(raw.Author.User.DisplayName)
		if author == "" {
			author = strings.TrimSpace(raw.Author.User.Name)
		}
	}

	pr := PullRequest{
		ID:           raw.ID,
		Title:        raw.Title,
		Description:  strings.TrimSpace(raw.Description),
		State:        strings.TrimSpace(raw.State),
		Open:         raw.Open,
		Closed:       raw.Closed,
		Draft:        raw.Draft,
		Version:      raw.Version,
		Author:       author,
		SourceBranch: branchDisplayName(raw.FromRef),
		TargetBranch: branchDisplayName(raw.ToRef),
		SourceCommit: sourceCommit(raw.FromRef),
		CreatedDate:  raw.CreatedDate,
		UpdatedDate:  raw.UpdatedDate,
		Reviewers:    mapReviewers(raw.Participants),
	}

	if raw.ToRef != nil && raw.ToRef.Repository != nil {
		pr.Repository = &RepositoryRef{
			ProjectKey: raw.ToRef.Repository.Project.Key,
			Slug:       raw.ToRef.Repository.Slug,
		}
	}

	return pr
}

func mapReviewers(participants []pullRequestParticipant) []Reviewer {
	if len(participants) == 0 {
		return nil
	}

	reviewers := make([]Reviewer, 0, len(participants))
	for _, participant := range participants {
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

		reviewers = append(reviewers, reviewer)
	}

	if len(reviewers) == 0 {
		return nil
	}

	return reviewers
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

func mapTask(raw taskValue) Task {
	author := ""
	if raw.Author != nil {
		author = resolveUserName(raw.Author)
	}

	assignee := ""
	if raw.Assignee != nil {
		assignee = resolveUserName(raw.Assignee)
	}

	state := strings.TrimSpace(raw.State)
	if state == "" {
		if raw.Resolved {
			state = "RESOLVED"
		} else {
			state = "OPEN"
		}
	}

	return Task{
		ID:          raw.ID,
		Text:        strings.TrimSpace(raw.Text),
		State:       state,
		Resolved:    raw.Resolved,
		Version:     raw.Version,
		CreatedDate: raw.CreatedDate,
		UpdatedDate: raw.UpdatedDate,
		Author:      author,
		Assignee:    assignee,
	}
}

func resolveUserName(user *pullRequestUserIdentity) string {
	if user == nil {
		return ""
	}

	if displayName := strings.TrimSpace(user.DisplayName); displayName != "" {
		return displayName
	}

	return strings.TrimSpace(user.Name)
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

func normalizeTaskID(taskID string) (string, error) {
	resolved := strings.TrimSpace(taskID)
	if resolved == "" {
		return "", apperrors.New(apperrors.KindValidation, "task id is required", nil)
	}

	if _, err := strconv.ParseInt(resolved, 10, 64); err != nil {
		return "", apperrors.New(apperrors.KindValidation, "task id must be a valid integer", nil)
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

	payload := map[string]any{
		"title":   title,
		"fromRef": map[string]any{"id": normalizeBranchRef(fromRef)},
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

	if len(payload) == 1 {
		return nil, apperrors.New(apperrors.KindValidation, "at least one of title, description, or draft is required", nil)
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

	query := map[string]string{}
	if version != nil {
		query["version"] = strconv.Itoa(*version)
	}

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

func taskMatchesState(task Task, state string) bool {
	switch state {
	case "open":
		return !task.Resolved
	case "resolved":
		return task.Resolved
	default:
		return true
	}
}

func resolveTaskStateValue(resolved bool) string {
	if resolved {
		return "RESOLVED"
	}

	return "OPEN"
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
	FromRef      *pullRequestRef          `json:"fromRef"`
	ToRef        *pullRequestRef          `json:"toRef"`
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

type pagedTaskResponse struct {
	Values        []taskValue `json:"values"`
	IsLastPage    bool        `json:"isLastPage"`
	NextPageStart int         `json:"nextPageStart"`
}

type taskValue struct {
	ID          int64                    `json:"id"`
	Text        string                   `json:"text"`
	State       string                   `json:"state"`
	Resolved    bool                     `json:"resolved"`
	Version     int                      `json:"version"`
	CreatedDate int64                    `json:"createdDate"`
	UpdatedDate int64                    `json:"updatedDate"`
	Author      *pullRequestUserIdentity `json:"author"`
	Assignee    *pullRequestUserIdentity `json:"assignee"`
}
