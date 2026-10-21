package pullrequestactivity

import (
	"context"
	"encoding/json"
	"io"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commentanchor"
)

// Review reads the caller's draft review of a pull request: the threads that
// hold a comment of theirs still pending. A pending reply comes back inside the
// published thread it answers.
//
// The review endpoint serialises an inline draft's anchor path as a plain
// string, as the timeline does, so the body is decoded here with the paths
// rewritten first. The generated client decodes it as the spec says and
// refused the whole review for one inline draft.
func (service *Service) Review(ctx context.Context, repository RepositoryRef, pullRequestID string, options ListOptions) ([]openapigenerated.RestComment, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return nil, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return nil, err
	}

	return openapi.PageThrough(ctx, options.Start, options.MaxResults,
		func(ctx context.Context, start, limit int) (openapi.Page[openapigenerated.RestComment], error) {
			startValue, limitValue := float32(start), float32(limit)

			response, err := service.client.GetReview(ctx, repository.ProjectKey, repository.Slug, resolvedID,
				&openapigenerated.GetReviewParams{Start: &startValue, Limit: &limitValue})
			if err != nil {
				return openapi.Page[openapigenerated.RestComment]{}, apperrors.Transport("failed to read the draft review", err)
			}
			defer func() { _ = response.Body.Close() }()

			body, err := io.ReadAll(response.Body)
			if err != nil {
				return openapi.Page[openapigenerated.RestComment]{}, apperrors.Transport("failed to read the draft review", err)
			}
			if err := openapi.MapStatusError(response.StatusCode, body); err != nil {
				return openapi.Page[openapigenerated.RestComment]{}, err
			}

			page, err := decodeReviewPage(body)
			if err != nil {
				return openapi.Page[openapigenerated.RestComment]{}, apperrors.New(apperrors.KindInternal, "failed to decode the draft review", err)
			}

			return page, nil
		})
}

func decodeReviewPage(body []byte) (openapi.Page[openapigenerated.RestComment], error) {
	var page struct {
		IsLastPage    *bool             `json:"isLastPage"`
		NextPageStart *int              `json:"nextPageStart"`
		Values        []json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return openapi.Page[openapigenerated.RestComment]{}, err
	}

	comments := make([]openapigenerated.RestComment, 0, len(page.Values))
	for _, value := range page.Values {
		normalized, err := commentanchor.NormalizeResponsePaths(value)
		if err != nil {
			return openapi.Page[openapigenerated.RestComment]{}, err
		}
		var comment openapigenerated.RestComment
		if err := json.Unmarshal(normalized, &comment); err != nil {
			return openapi.Page[openapigenerated.RestComment]{}, err
		}
		comments = append(comments, comment)
	}

	return openapi.Page[openapigenerated.RestComment]{Values: comments, IsLastPage: page.IsLastPage, NextPageStart: page.NextPageStart}, nil
}
