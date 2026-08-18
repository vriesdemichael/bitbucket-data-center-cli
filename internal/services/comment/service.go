package comment

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
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
		if strings.TrimSpace(target.CommitID) != "" {
			response, err := service.client.GetCommentsWithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.CommitID, &openapigenerated.GetCommentsParams{Path: &trimmedPath, Start: &start, Limit: &pageLimit})
			if err != nil {
				return nil, apperrors.New(apperrors.KindTransient, "failed to list commit comments", err)
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
			continue
		}

		if target.Blocker {
			response, err := service.client.GetComments1WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, &openapigenerated.GetComments1Params{Start: &start, Limit: &pageLimit})
			if err != nil {
				return nil, apperrors.New(apperrors.KindTransient, "failed to list pull request blocker comments", err)
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
			continue
		}

		response, err := service.client.GetComments2WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, &openapigenerated.GetComments2Params{Path: trimmedPath, Start: &start, Limit: &pageLimit})
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list pull request comments", err)
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
	body := openapigenerated.RestComment{
		Text:  &trimmedText,
		State: pendingState,
	}

	if strings.TrimSpace(target.CommitID) != "" {
		response, err := service.client.CreateCommentWithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.CommitID, nil, body)
		if err != nil {
			return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to create commit comment", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return openapigenerated.RestComment{}, err
		}
		if response.ApplicationjsonCharsetUTF8201 != nil {
			return *response.ApplicationjsonCharsetUTF8201, nil
		}
		return body, nil
	}

	if target.Blocker {
		response, err := service.client.CreateComment1WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, body)
		if err != nil {
			return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to create pull request blocker comment", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return openapigenerated.RestComment{}, err
		}
		if response.ApplicationjsonCharsetUTF8201 != nil {
			return *response.ApplicationjsonCharsetUTF8201, nil
		}
		return body, nil
	}

	response, err := service.client.CreateComment2WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, body)
	if err != nil {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to create pull request comment", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestComment{}, err
	}
	if response.ApplicationjsonCharsetUTF8201 != nil {
		return *response.ApplicationjsonCharsetUTF8201, nil
	}

	return body, nil
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

	if strings.TrimSpace(target.CommitID) != "" {
		response, err := service.client.UpdateCommentWithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.CommitID, trimmedCommentID, body)
		if err != nil {
			return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to update commit comment", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return openapigenerated.RestComment{}, err
		}
		if response.ApplicationjsonCharsetUTF8200 != nil {
			return *response.ApplicationjsonCharsetUTF8200, nil
		}
		return body, nil
	}

	if target.Blocker {
		response, err := service.client.UpdateComment1WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID, body)
		if err != nil {
			return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to update pull request blocker comment", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return openapigenerated.RestComment{}, err
		}
		if response.ApplicationjsonCharsetUTF8200 != nil {
			return *response.ApplicationjsonCharsetUTF8200, nil
		}
		return body, nil
	}

	response, err := service.client.UpdateComment2WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID, body)
	if err != nil {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to update pull request comment", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestComment{}, err
	}
	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return body, nil
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

	if strings.TrimSpace(target.CommitID) != "" {
		response, err := service.client.GetCommentWithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.CommitID, trimmedCommentID)
		if err != nil {
			return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to get commit comment", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return openapigenerated.RestComment{}, err
		}
		if response.ApplicationjsonCharsetUTF8200 != nil {
			return *response.ApplicationjsonCharsetUTF8200, nil
		}
		return openapigenerated.RestComment{}, nil
	}

	if target.Blocker {
		response, err := service.client.GetComment1WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID)
		if err != nil {
			return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to get pull request blocker comment", err)
		}
		if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
			return openapigenerated.RestComment{}, err
		}
		if response.ApplicationjsonCharsetUTF8200 != nil {
			return *response.ApplicationjsonCharsetUTF8200, nil
		}
		return openapigenerated.RestComment{}, nil
	}

	response, err := service.client.GetComment2WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID)
	if err != nil {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to get pull request comment", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestComment{}, err
	}
	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return openapigenerated.RestComment{}, nil
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

	return nil
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

	response, err := service.client.UpdateComment2WithResponse(ctx, target.Repository.ProjectKey, target.Repository.Slug, target.PullRequestID, trimmedCommentID, body)
	if err != nil {
		return openapigenerated.RestComment{}, apperrors.New(apperrors.KindTransient, "failed to update pull request comment state", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestComment{}, err
	}
	if response.ApplicationjsonCharsetUTF8200 != nil {
		return *response.ApplicationjsonCharsetUTF8200, nil
	}

	return body, nil
}
