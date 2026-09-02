package comment

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/commentanchor"
)

type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

type Context struct {
	Type           string `json:"type"`
	ProjectKey     string `json:"project_key"`
	RepositorySlug string `json:"repository_slug"`
	CommitID       string `json:"commit_id,omitempty"`
	PullRequestID  string `json:"pull_request_id,omitempty"`
}

type Target struct {
	Repository    RepositoryRef
	CommitID      string
	PullRequestID string
	Blocker       bool
	Pending       bool
	Path          string
	Line          int
	LineType      string
	ParentID      int64
}

func (target Target) anchorOptions() commentanchor.Options {
	return commentanchor.Options{
		Path:     target.Path,
		Line:     target.Line,
		LineType: target.LineType,
		ParentID: target.ParentID,
		Blocker:  target.Blocker,
	}
}

func (target Target) Context() Context {
	ctx := Context{
		ProjectKey:     target.Repository.ProjectKey,
		RepositorySlug: target.Repository.Slug,
	}

	if strings.TrimSpace(target.CommitID) != "" {
		ctx.Type = "commit"
		ctx.CommitID = target.CommitID
		return ctx
	}

	ctx.Type = "pull_request"
	ctx.PullRequestID = target.PullRequestID
	return ctx
}

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func (service *Service) List(ctx context.Context, target Target, path string, limit int) ([]openapigenerated.RestComment, error) {
	if err := validateTarget(target); err != nil {
		return nil, err
	}
	trimmedPath := strings.TrimSpace(path)
	// Every listing but the blocker one needs a path (ADR-077).
	//
	// The vendored spec types the commit endpoint's path as optional and it is
	// wrong: a real Bitbucket answers 400 "The path query parameter is required
	// when retrieving comments." A comment must therefore be anchored to a file
	// to be listable, which is why `repo comment create` takes --path, --line
	// and --line-type.
	if !target.Blocker && trimmedPath == "" {
		return nil, apperrors.New(apperrors.KindValidation, "comment path is required for list operations", nil)
	}
	if limit <= 0 {
		limit = 25
	}

	start := float32(0)
	pageLimit := float32(limit)
	results := make([]openapigenerated.RestComment, 0)

	for {
		page, err := service.commentPage(ctx, target, trimmedPath, start, pageLimit)
		if err != nil {
			return nil, err
		}

		results = append(results, page.Values...)
		if page.IsLastPage != nil && *page.IsLastPage {
			break
		}
		if page.NextPageStart == nil {
			break
		}
		start = *page.NextPageStart
	}

	return results, nil
}

// commentPage fetches one page from whichever endpoint the target names.
//
// Each branch issues the request and decodes it in one expression, rather than
// assigning the response into a variable the switch shares: the raw request
// methods hand back a body the caller must close, and passing it straight to
// decodeCommentPage is what keeps that provable.
//
// The raw methods rather than their WithResponse wrappers, for the reason
// decodeCommentPage gives: the wrappers decode into RestComment, whose
// anchor.path is an object, and Bitbucket sends a plain string here.
func (service *Service) commentPage(ctx context.Context, target Target, path string, start float32, limit float32) (commentPage, error) {
	if strings.TrimSpace(target.CommitID) != "" {
		response, err := service.client.GetComments(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.CommitID, &openapigenerated.GetCommentsParams{Path: &path, Start: &start, Limit: &limit})
		return decodeCommentPage(response, err, "failed to list commit comments")
	}

	if target.Blocker {
		response, err := service.client.GetComments1(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, &openapigenerated.GetComments1Params{Start: &start, Limit: &limit})
		return decodeCommentPage(response, err, "failed to list pull request blocker comments")
	}

	response, err := service.client.GetComments2(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, &openapigenerated.GetComments2Params{Path: path, Start: &start, Limit: &limit})

	return decodeCommentPage(response, err, "failed to list pull request comments")
}

// TaskCounts is the per-state tally of a pull request's blocker comments, which
// is what Bitbucket Data Center 8+ calls tasks.
type TaskCounts struct {
	Open     int `json:"open"`
	Resolved int `json:"resolved"`
}

// CountTasks returns the number of open and resolved tasks on a pull request in
// a single request, without fetching any comment bodies. It is the cheap,
// exact alternative to walking the activity timeline when only the task tally
// is needed.
func (service *Service) CountTasks(ctx context.Context, repository RepositoryRef, pullRequestID string) (TaskCounts, error) {
	target := Target{Repository: repository, PullRequestID: pullRequestID, Blocker: true}
	if err := validateTarget(target); err != nil {
		return TaskCounts{}, err
	}

	count := "true"
	response, err := service.client.GetComments1WithResponse(ctx, repository.ProjectKey, repository.Slug, pullRequestID, &openapigenerated.GetComments1Params{Count: &count})
	if err != nil {
		return TaskCounts{}, apperrors.New(apperrors.KindTransient, "failed to count pull request tasks", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return TaskCounts{}, err
	}

	// With count=true Bitbucket replaces the usual page with a state->count map,
	// which the generated page model cannot represent.
	var states map[string]int
	if err := json.Unmarshal(response.Body, &states); err != nil {
		return TaskCounts{}, apperrors.New(apperrors.KindInternal, "failed to decode pull request task counts", err)
	}

	return TaskCounts{Open: states["OPEN"], Resolved: states["RESOLVED"]}, nil
}

func (service *Service) Create(ctx context.Context, target Target, text string) (openapigenerated.RestComment, error) {
	if err := validateTarget(target); err != nil {
		return openapigenerated.RestComment{}, err
	}

	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindValidation, "comment text is required", nil)
	}

	// A draft comment is created by asking for the PENDING state, not by the
	// pending flag: the schema marks pending readOnly and the server ignores it,
	// publishing the comment the caller wanted kept private.
	var pendingState *string
	if target.Pending {
		state := "PENDING"
		pendingState = &state
	}
	echo := openapigenerated.RestComment{
		Text:  &trimmedText,
		State: pendingState,
	}

	// The body goes out as a map rather than the generated struct because an
	// anchor's path has to be a plain string on the way in, and the generated
	// RestComment models it as the object Bitbucket sends back. Round-tripping
	// through the struct would rewrite the path into a shape the create
	// endpoint does not accept.
	fields := map[string]any{"text": trimmedText}
	if pendingState != nil {
		fields["state"] = *pendingState
	}
	anchorFields, err := commentanchor.Payload(target.anchorOptions(), commentanchor.APINames)
	if err != nil {
		return openapigenerated.RestComment{}, err
	}
	for key, value := range anchorFields {
		fields[key] = value
	}

	encoded, err := json.Marshal(fields)
	if err != nil {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindInternal, "failed to encode comment payload", err)
	}

	// The raw request methods are used rather than their WithResponse wrappers
	// because those decode a 201 straight into the generated RestComment, and
	// an inline comment comes back with a string anchor path the model cannot
	// hold — so a comment that was created successfully would surface as a
	// transient failure. decodeCreatedComment does the decode after repairing
	// the shape.
	if strings.TrimSpace(target.CommitID) != "" {
		response, err := service.client.CreateCommentWithBody(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.CommitID, nil, jsonContentType, bytes.NewReader(encoded))
		return decodeCreatedComment(response, err, "failed to create commit comment", echo)
	}

	if target.Blocker {
		response, err := service.client.CreateComment1WithBody(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, jsonContentType, bytes.NewReader(encoded))
		return decodeCreatedComment(response, err, "failed to create pull request blocker comment", echo)
	}

	response, err := service.client.CreateComment2WithBody(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, jsonContentType, bytes.NewReader(encoded))

	created, createErr := decodeCreatedComment(response, err, "failed to create pull request comment", echo)

	return created, commentanchor.ExplainRejection(createErr, target.anchorOptions())
}

const jsonContentType = "application/json"

// decodeCreatedComment turns a create-comment response into the generated
// model, falling back to the request echo when the body cannot be read as one.
//
// The comment has already been created by the time this runs, so a body that
// will not decode must not be reported as a failed create — the caller would
// retry and post a duplicate. Anchor paths are normalised first because
// Bitbucket sends them as strings while the generated model expects objects.
func decodeCreatedComment(response *http.Response, requestErr error, failureMessage string, echo openapigenerated.RestComment) (openapigenerated.RestComment, error) {
	if requestErr != nil {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, failureMessage, requestErr)
	}

	raw, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, failureMessage, readErr)
	}

	if err := openapi.MapStatusError(response.StatusCode, raw); err != nil {
		return openapigenerated.RestComment{}, err
	}
	if !json.Valid(raw) {
		return echo, nil
	}

	normalized, err := commentanchor.NormalizeResponsePaths(raw)
	if err != nil {
		return echo, nil
	}

	var created openapigenerated.RestComment
	if err := json.Unmarshal(normalized, &created); err != nil {
		return echo, nil
	}

	return created, nil
}

func (service *Service) Update(ctx context.Context, target Target, commentID string, text string, version *int32) (openapigenerated.RestComment, error) {
	if err := validateTarget(target); err != nil {
		return openapigenerated.RestComment{}, err
	}

	trimmedCommentID := strings.TrimSpace(commentID)
	if trimmedCommentID == "" {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindValidation, "comment id is required", nil)
	}

	trimmedText := strings.TrimSpace(text)
	if trimmedText == "" {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindValidation, "comment text is required", nil)
	}

	resolvedVersion := version
	if resolvedVersion == nil {
		current, err := service.Get(ctx, target, trimmedCommentID)
		if err != nil {
			return openapigenerated.RestComment{}, err
		}
		resolvedVersion = current.Version
	}

	body := openapigenerated.RestComment{Text: &trimmedText, Version: resolvedVersion}

	// Raw requests here too, for the reason decodeCreatedComment gives: the
	// generated wrappers decode into RestComment, and Bitbucket sends an
	// anchored comment's path as a plain string. An update to a comment on a
	// file therefore failed to decode -- reporting a write that had already
	// happened as a transient failure, which invites a retry.
	if strings.TrimSpace(target.CommitID) != "" {
		response, err := service.client.UpdateComment(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.CommitID, trimmedCommentID, body)
		return decodeCreatedComment(response, err, "failed to update commit comment", body)
	}

	if target.Blocker {
		response, err := service.client.UpdateComment1(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID, body)
		return decodeCreatedComment(response, err, "failed to update pull request blocker comment", body)
	}

	response, err := service.client.UpdateComment2(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID, body)

	return decodeCreatedComment(response, err, "failed to update pull request comment", body)
}

func (service *Service) Delete(ctx context.Context, target Target, commentID string, version *int32) (*int32, error) {
	if err := validateTarget(target); err != nil {
		return nil, err
	}

	trimmedCommentID := strings.TrimSpace(commentID)
	if trimmedCommentID == "" {
		return nil, apperrors.New(apperrors.KindValidation, "comment id is required", nil)
	}

	resolvedVersion := version
	if resolvedVersion == nil {
		current, err := service.Get(ctx, target, trimmedCommentID)
		if err != nil {
			return nil, err
		}
		resolvedVersion = current.Version
	}

	var versionParam *string
	if resolvedVersion != nil {
		value := strconv.Itoa(int(*resolvedVersion))
		versionParam = &value
	}

	if strings.TrimSpace(target.CommitID) != "" {
		response, err := service.client.DeleteCommentWithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.CommitID, trimmedCommentID, &openapigenerated.DeleteCommentParams{Version: versionParam})
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to delete commit comment", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}
		return resolvedVersion, nil
	}

	if target.Blocker {
		response, err := service.client.DeleteComment1WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID, &openapigenerated.DeleteComment1Params{Version: versionParam})
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to delete pull request blocker comment", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return nil, err
		}
		return resolvedVersion, nil
	}

	response, err := service.client.DeleteComment2WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID, &openapigenerated.DeleteComment2Params{Version: versionParam})
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to delete pull request comment", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, err
	}

	return resolvedVersion, nil
}

func (service *Service) Get(ctx context.Context, target Target, commentID string) (openapigenerated.RestComment, error) {
	if err := validateTarget(target); err != nil {
		return openapigenerated.RestComment{}, err
	}

	trimmedCommentID := strings.TrimSpace(commentID)
	if trimmedCommentID == "" {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindValidation, "comment id is required", nil)
	}

	// And here, the same repair. Get is also how Update and Delete resolve the
	// version when the caller did not supply one, so an anchored comment could
	// not be edited or removed either -- the read they depend on failed first.
	if strings.TrimSpace(target.CommitID) != "" {
		response, err := service.client.GetComment(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.CommitID, trimmedCommentID)
		return decodeCreatedComment(response, err, "failed to get commit comment", openapigenerated.RestComment{})
	}

	if target.Blocker {
		response, err := service.client.GetComment1(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID)
		return decodeCreatedComment(response, err, "failed to get pull request blocker comment", openapigenerated.RestComment{})
	}

	response, err := service.client.GetComment2(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID)

	return decodeCreatedComment(response, err, "failed to get pull request comment", openapigenerated.RestComment{})
}

func (service *Service) React(ctx context.Context, repo RepositoryRef, prID string, commentID string, emoticon string) (openapigenerated.RestUserReaction, error) {
	trimmedPrID := strings.TrimSpace(prID)
	trimmedCommentID := strings.TrimSpace(commentID)
	trimmedEmoticon := strings.TrimSpace(emoticon)

	if strings.TrimSpace(repo.ProjectKey) == "" || strings.TrimSpace(repo.Slug) == "" {
		return openapigenerated.RestUserReaction{}, apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}
	if trimmedPrID == "" {
		return openapigenerated.RestUserReaction{}, apperrors.New(apperrors.KindValidation, "pull request id is required", nil)
	}
	if trimmedCommentID == "" {
		return openapigenerated.RestUserReaction{}, apperrors.New(apperrors.KindValidation, "comment id is required", nil)
	}
	if trimmedEmoticon == "" {
		return openapigenerated.RestUserReaction{}, apperrors.New(apperrors.KindValidation, "emoticon is required", nil)
	}

	response, err := service.client.React1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedPrID, trimmedCommentID, trimmedEmoticon)
	if err != nil {
		return openapigenerated.RestUserReaction{}, apperrors.New(apperrors.KindTransient, "failed to add reaction", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestUserReaction{}, err
	}
	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}
	return openapigenerated.RestUserReaction{}, nil
}

func (service *Service) UnReact(ctx context.Context, repo RepositoryRef, prID string, commentID string, emoticon string) error {
	trimmedPrID := strings.TrimSpace(prID)
	trimmedCommentID := strings.TrimSpace(commentID)
	trimmedEmoticon := strings.TrimSpace(emoticon)

	if strings.TrimSpace(repo.ProjectKey) == "" || strings.TrimSpace(repo.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}
	if trimmedPrID == "" {
		return apperrors.New(apperrors.KindValidation, "pull request id is required", nil)
	}
	if trimmedCommentID == "" {
		return apperrors.New(apperrors.KindValidation, "comment id is required", nil)
	}
	if trimmedEmoticon == "" {
		return apperrors.New(apperrors.KindValidation, "emoticon is required", nil)
	}

	response, err := service.client.UnReact1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedPrID, trimmedCommentID, trimmedEmoticon)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to remove reaction", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func (service *Service) ApplySuggestion(ctx context.Context, repo RepositoryRef, prID string, commentID string, req openapigenerated.RestApplySuggestionRequest) error {
	trimmedPrID := strings.TrimSpace(prID)
	trimmedCommentID := strings.TrimSpace(commentID)

	if strings.TrimSpace(repo.ProjectKey) == "" || strings.TrimSpace(repo.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}
	if trimmedPrID == "" {
		return apperrors.New(apperrors.KindValidation, "pull request id is required", nil)
	}
	if trimmedCommentID == "" {
		return apperrors.New(apperrors.KindValidation, "comment id is required", nil)
	}

	response, err := service.client.ApplySuggestionWithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedPrID, trimmedCommentID, req)
	if err != nil {
		return apperrors.New(apperrors.KindTransient, "failed to apply suggestion", err)
	}
	return openapi.MapStatusError(response.StatusCode(), response.Body)
}

func validateTarget(target Target) error {
	if strings.TrimSpace(target.Repository.ProjectKey) == "" || strings.TrimSpace(target.Repository.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}

	hasCommit := strings.TrimSpace(target.CommitID) != ""
	hasPullRequest := strings.TrimSpace(target.PullRequestID) != ""

	if hasCommit == hasPullRequest {
		return apperrors.New(apperrors.KindValidation, "exactly one of commit or pull request id is required", nil)
	}

	if target.Blocker && hasCommit {
		return apperrors.New(apperrors.KindValidation, "blocker comments are only supported for pull requests, not commits", nil)
	}

	return commentanchor.Validate(target.anchorOptions(), commentanchor.APINames)
}

// CommentState is the resolution state of a pull request comment.
//
// Bitbucket replaced pull request tasks with blocker comments, and this is what
// took the place of marking a task done: a resolved comment carries a resolver
// and drops out of the unresolved counts.
type CommentState string

const (
	CommentStateOpen     CommentState = "OPEN"
	CommentStateResolved CommentState = "RESOLVED"
)

// SetState resolves or reopens a comment.
//
// The version is read first unless the caller supplies one. The endpoint uses it
// for optimistic concurrency and rejects a request without it -- 409
// CommentOutOfDateException, reporting expectedVersion -1 -- so leaving it out is
// not an option, and asking every caller for it would be ceremony over a value
// the server already holds.
func (service *Service) SetState(ctx context.Context, target Target, commentID string, state CommentState, version *int32) (openapigenerated.RestComment, error) {
	if err := validateTarget(target); err != nil {
		return openapigenerated.RestComment{}, err
	}

	trimmedCommentID := strings.TrimSpace(commentID)
	if trimmedCommentID == "" {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindValidation, "comment id is required", nil)
	}

	switch state {
	case CommentStateOpen, CommentStateResolved:
	default:
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindValidation, "state must be OPEN or RESOLVED", nil)
	}

	resolvedVersion := version
	if resolvedVersion == nil {
		current, err := service.Get(ctx, target, trimmedCommentID)
		if err != nil {
			return openapigenerated.RestComment{}, err
		}
		resolvedVersion = current.Version
	}

	stateValue := string(state)
	body := openapigenerated.RestComment{State: &stateValue, Version: resolvedVersion}

	// The raw method, so an anchored comment's path is repaired before decoding
	// (ADR-077). This is the one that mattered most: resolving an inline
	// blocker is how a reviewer closes out feedback on a specific line, and the
	// wrapper made it fail on every comment that had a line to point at --
	// which is every comment worth blocking on.
	response, err := service.client.UpdateComment2(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID, body)

	return decodeCreatedComment(response, err, "failed to update pull request comment state", body)
}

// commentPage is one page of a comment listing, decoded after the anchor paths
// have been repaired.
type commentPage struct {
	Values        []openapigenerated.RestComment `json:"values"`
	IsLastPage    *bool                          `json:"isLastPage,omitempty"`
	NextPageStart *float32                       `json:"nextPageStart,omitempty"`
}

// decodeCommentPage reads a listing response the way decodeCreatedComment reads
// a create response.
//
// The generated WithResponse wrappers decode straight into RestComment, whose
// anchor.path is the {components, extension, name, parent} object Bitbucket
// returns from some endpoints and a plain string from others. A listing that
// contains one anchored comment therefore failed to decode entirely -- and
// every comment written through the Bitbucket web interface on a diff is
// anchored, so `bb repo comment list` broke on exactly the comments a reviewer
// wants to read. It went unnoticed because bb could not create an anchored
// comment until now, so nothing it made ever came back with one.
func decodeCommentPage(response *http.Response, requestErr error, failureMessage string) (commentPage, error) {
	if requestErr != nil {
		return commentPage{}, apperrors.New(apperrors.KindTransient, failureMessage, requestErr)
	}

	raw, readErr := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if readErr != nil {
		return commentPage{}, apperrors.New(apperrors.KindTransient, failureMessage, readErr)
	}

	if err := openapi.MapStatusError(response.StatusCode, raw); err != nil {
		return commentPage{}, err
	}

	// A 200 with no body is how some versions say "no comments". Reporting that
	// as a decode failure would turn an empty file into an error.
	if len(bytes.TrimSpace(raw)) == 0 {
		return commentPage{}, nil
	}

	normalized, err := commentanchor.NormalizeResponsePaths(raw)
	if err != nil {
		return commentPage{}, apperrors.New(apperrors.KindPermanent, failureMessage, err)
	}

	var page commentPage
	if err := json.Unmarshal(normalized, &page); err != nil {
		return commentPage{}, apperrors.New(apperrors.KindPermanent, failureMessage, err)
	}

	return page, nil
}
