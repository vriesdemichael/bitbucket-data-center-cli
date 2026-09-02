package pullrequestactivity

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commentanchor"
)

type RepositoryRef struct {
	ProjectKey string `json:"project_key"`
	Slug       string `json:"slug"`
}

type ListOptions struct {
	PageSize int `json:"limit"`
	Start    int `json:"start"`
}

type Activity struct {
	ID          int64                         `json:"id,omitempty"`
	Action      string                        `json:"action,omitempty"`
	CreatedDate int64                         `json:"created_date,omitempty"`
	Comment     *openapigenerated.RestComment `json:"comment,omitempty"`
	Raw         map[string]any                `json:"raw"`
}

type Service struct {
	client *openapigenerated.ClientWithResponses
}

func NewService(client *openapigenerated.ClientWithResponses) *Service {
	return &Service{client: client}
}

func (service *Service) List(ctx context.Context, repository RepositoryRef, pullRequestID string, options ListOptions) ([]Activity, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return nil, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return nil, err
	}

	if options.PageSize <= 0 {
		options.PageSize = 25
	}
	if options.Start < 0 {
		return nil, apperrors.New(apperrors.KindValidation, "start must be greater than or equal to 0", nil)
	}

	start := float32(options.Start)
	limit := float32(options.PageSize)
	results := make([]Activity, 0)

	for {
		response, err := service.client.GetActivitiesWithResponse(ctx, repository.ProjectKey, repository.Slug, resolvedID, &openapigenerated.GetActivitiesParams{Start: &start, Limit: &limit})
		if err != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to list pull request activities", err)
		}
		if response.StatusCode() >= 400 {
			return nil, mapActivityStatusError(response.StatusCode(), response.Body)
		}

		page, err := decodeActivityPage(response.Body)
		if err != nil {
			return nil, apperrors.New(apperrors.KindInternal, "failed to decode pull request activities", err)
		}

		results = append(results, page.Values...)
		if page.IsLastPage || page.NextPageStart == nil {
			break
		}
		if *page.NextPageStart == int(start) {
			break
		}
		start = float32(*page.NextPageStart)
	}

	return results, nil
}

func ExtractComments(activities []Activity) []openapigenerated.RestComment {
	comments := make([]openapigenerated.RestComment, 0)
	seen := map[int64]struct{}{}

	for _, activity := range activities {
		if activity.Comment == nil {
			continue
		}

		comment := *activity.Comment
		if comment.Id != nil {
			if _, ok := seen[*comment.Id]; ok {
				continue
			}
			seen[*comment.Id] = struct{}{}
		}

		comments = append(comments, comment)
	}

	return comments
}

type activityPage struct {
	IsLastPage    bool       `json:"isLastPage"`
	NextPageStart *int       `json:"nextPageStart,omitempty"`
	Values        []Activity `json:"values"`
}

type rawActivity struct {
	ID          *int64                     `json:"id,omitempty"`
	Action      *string                    `json:"action,omitempty"`
	CreatedDate *int64                     `json:"createdDate,omitempty"`
	Comment     *json.RawMessage           `json:"comment,omitempty"`
	Raw         map[string]json.RawMessage `json:"-"`
}

func (activity *rawActivity) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	activity.Raw = raw

	type alias rawActivity
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	*activity = rawActivity(decoded)
	activity.Raw = raw
	return nil
}

type rawActivityPage struct {
	IsLastPage    bool          `json:"isLastPage"`
	NextPageStart *int          `json:"nextPageStart,omitempty"`
	Values        []rawActivity `json:"values"`
}

func decodeActivityPage(body []byte) (activityPage, error) {
	page := rawActivityPage{}
	if err := json.Unmarshal(body, &page); err != nil {
		return activityPage{}, err
	}

	results := make([]Activity, 0, len(page.Values))
	for _, item := range page.Values {
		mapped, err := mapActivity(item)
		if err != nil {
			return activityPage{}, err
		}
		results = append(results, mapped)
	}

	return activityPage{IsLastPage: page.IsLastPage, NextPageStart: page.NextPageStart, Values: results}, nil
}

func mapActivity(item rawActivity) (Activity, error) {
	mapped := Activity{
		ID:          safeInt64(item.ID),
		Action:      safeString(item.Action),
		CreatedDate: safeInt64(item.CreatedDate),
		Raw:         map[string]any{},
	}

	for key, value := range item.Raw {
		var decoded any
		if err := json.Unmarshal(value, &decoded); err != nil {
			return Activity{}, err
		}
		mapped.Raw[key] = decoded
	}

	if item.Comment != nil {
		normalized, err := normalizeCommentAnchorPaths(*item.Comment)
		if err != nil {
			return Activity{}, err
		}

		comment := openapigenerated.RestComment{}
		if err := json.Unmarshal(normalized, &comment); err != nil {
			return Activity{}, err
		}
		mapped.Comment = &comment
	}

	return mapped, nil
}

// normalizeCommentAnchorPaths rewrites string anchor paths into the object form
// the generated model expects.
//
// The activity timeline serialises an inline comment's anchor path as a plain
// string ("src/main.go"), while the published spec — and the path-scoped
// comments endpoint — use an object with name/parent/extension/components.
// Without this, a single inline comment makes the whole activity page fail to
// decode, which is why pull requests with inline review comments used to return
// nothing at all.
//
// The create-comment response has the same mismatch, so the rewrite lives in
// commentanchor alongside the payload that produces those string paths.
func normalizeCommentAnchorPaths(raw json.RawMessage) (json.RawMessage, error) {
	return commentanchor.NormalizeResponsePaths(raw)
}

// pathObjectFromString splits "a/b/c.go" into the name/parent/extension/
// components shape Bitbucket uses elsewhere.
func pathObjectFromString(path string) map[string]any {
	return commentanchor.PathObjectFromString(path)
}

func validateRepositoryRef(repository RepositoryRef) error {
	if strings.TrimSpace(repository.ProjectKey) == "" || strings.TrimSpace(repository.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}

	return nil
}

func normalizePullRequestID(pullRequestID string) (string, error) {
	trimmed := strings.TrimSpace(pullRequestID)
	if trimmed == "" {
		return "", apperrors.New(apperrors.KindValidation, "pull request id is required", nil)
	}
	if _, err := strconv.Atoi(trimmed); err != nil {
		return "", apperrors.New(apperrors.KindValidation, "pull request id must be a number", err)
	}

	return trimmed, nil
}

func mapActivityStatusError(statusCode int, body []byte) error {
	if statusCode == 404 {
		// Preserve the shared route-missing classification so callers can tell
		// "this server has no activity endpoint" from "this pull request does
		// not exist" and degrade only for the former.
		if openapi.IsRouteMissing(openapi.MapStatusError(statusCode, body)) {
			return apperrors.New(apperrors.KindNotFound, "pull request activity not found", openapi.ErrRouteMissing)
		}
		return apperrors.New(apperrors.KindNotFound, "pull request activity not found", nil)
	}
	message := fmt.Sprintf("pull request activity request failed with status %d", statusCode)

	switch statusCode {
	case 400:
		return apperrors.New(apperrors.KindValidation, message, nil)
	case 401:
		return apperrors.New(apperrors.KindAuthentication, message, nil)
	case 403:
		// Reported as authorization rather than internal so callers can treat a
		// token that may not read the timeline as "unavailable here" instead of
		// as a server fault.
		return apperrors.New(apperrors.KindAuthorization, message, nil)
	}

	return apperrors.New(apperrors.KindInternal, message, nil)
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func safeInt64(value *int64) int64 {
	if value == nil {
		return 0
	}

	return *value
}
