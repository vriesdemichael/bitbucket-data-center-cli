package browse

import (
	"bytes"
	"context"
	"fmt"
	"mime/multipart"
	"net/url"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

type TreeOptions struct {
	At    string
	Limit int
}

type FileOptions struct {
	At    string
	Blame bool
}

type EditInput struct {
	Branch         string
	Message        string
	Content        string
	SourceBranch   string
	SourceCommitId string
}

type Service struct {
	client *openapigenerated.ClientWithResponses
	http   *httpclient.Client
}

func NewService(client *openapigenerated.ClientWithResponses, http *httpclient.Client) *Service {
	return &Service{client: client, http: http}
}

func (service *Service) Tree(ctx context.Context, repo RepositoryRef, path string, options TreeOptions) ([]string, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	trimmedPath := strings.TrimSpace(path)

	if options.Limit <= 0 {
		options.Limit = 1000
	}

	start := float32(0)
	pageLimit := float32(options.Limit)
	var at *string
	if strings.TrimSpace(options.At) != "" {
		a := strings.TrimSpace(options.At)
		at = &a
	}

	results := make([]string, 0)

	for {
		var responseStatus int
		var responseBody []byte
		var responseValues *[]openapigenerated.FileListResource
		var responseIsLastPage *bool
		var responseNextPageStart *int32

		if trimmedPath == "" {
			params := &openapigenerated.StreamFilesParams{Start: &start, Limit: &pageLimit, At: at}
			resp, err := service.client.StreamFilesWithResponse(ctx, repo.ProjectKey, repo.Slug, params)
			if err != nil {
				return nil, apperrors.New(apperrors.KindTransient, "failed to stream repository files", err)
			}
			responseStatus = resp.StatusCode()
			responseBody = resp.Body
			if resp.ApplicationjsonCharsetUTF8200 != nil {
				responseValues = resp.ApplicationjsonCharsetUTF8200.Values
				responseIsLastPage = resp.ApplicationjsonCharsetUTF8200.IsLastPage
				responseNextPageStart = resp.ApplicationjsonCharsetUTF8200.NextPageStart
			}
		} else {
			params := &openapigenerated.StreamFiles1Params{Start: &start, Limit: &pageLimit, At: at}
			resp, err := service.client.StreamFiles1WithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedPath, params)
			if err != nil {
				return nil, apperrors.New(apperrors.KindTransient, "failed to stream repository files", err)
			}
			responseStatus = resp.StatusCode()
			responseBody = resp.Body
			if resp.ApplicationjsonCharsetUTF8200 != nil {
				responseValues = resp.ApplicationjsonCharsetUTF8200.Values
				responseIsLastPage = resp.ApplicationjsonCharsetUTF8200.IsLastPage
				responseNextPageStart = resp.ApplicationjsonCharsetUTF8200.NextPageStart
			}
		}

		if err := openapi.MapStatusError(responseStatus, responseBody); err != nil {
			return nil, err
		}

		if responseValues == nil {
			break
		}

		for _, val := range *responseValues {
			if strVal, ok := val.(string); ok {
				results = append(results, strVal)
			}
		}

		if responseIsLastPage != nil && *responseIsLastPage {
			break
		}
		if responseNextPageStart == nil {
			break
		}

		start = float32(*responseNextPageStart)
	}

	return results, nil
}

func (service *Service) Raw(ctx context.Context, repo RepositoryRef, path string, at string) ([]byte, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	encodedPath, err := encodeFilePath(path)
	if err != nil {
		return nil, err
	}

	query := map[string]string{}
	if strings.TrimSpace(at) != "" {
		query["at"] = strings.TrimSpace(at)
	}

	return service.http.GetRaw(ctx, repositoryAPIPath(repo, "raw", encodedPath), query)
}

func (service *Service) File(ctx context.Context, repo RepositoryRef, path string, options FileOptions) ([]byte, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	encodedPath, err := encodeFilePath(path)
	if err != nil {
		return nil, err
	}

	query := map[string]string{}
	if strings.TrimSpace(options.At) != "" {
		query["at"] = strings.TrimSpace(options.At)
	}
	if options.Blame {
		query["blame"] = "true"
	}

	return service.http.GetRaw(ctx, repositoryAPIPath(repo, "browse", encodedPath), query)
}

func (service *Service) Edit(ctx context.Context, repo RepositoryRef, path string, input EditInput) (*openapigenerated.RestCommit, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, err
	}

	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		return nil, apperrors.New(apperrors.KindValidation, "path is required", nil)
	}

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	if input.Branch != "" {
		if err := writer.WriteField("branch", input.Branch); err != nil {
			return nil, apperrors.New(apperrors.KindInternal, "failed to write branch field", err)
		}
	}
	if input.Message != "" {
		if err := writer.WriteField("message", input.Message); err != nil {
			return nil, apperrors.New(apperrors.KindInternal, "failed to write message field", err)
		}
	}
	if input.Content != "" {
		if err := writer.WriteField("content", input.Content); err != nil {
			return nil, apperrors.New(apperrors.KindInternal, "failed to write content field", err)
		}
	}
	if input.SourceBranch != "" {
		if err := writer.WriteField("sourceBranch", input.SourceBranch); err != nil {
			return nil, apperrors.New(apperrors.KindInternal, "failed to write sourceBranch field", err)
		}
	}
	if input.SourceCommitId != "" {
		if err := writer.WriteField("sourceCommitId", input.SourceCommitId); err != nil {
			return nil, apperrors.New(apperrors.KindInternal, "failed to write sourceCommitId field", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, apperrors.New(apperrors.KindInternal, "failed to close multipart writer", err)
	}

	resp, err := service.client.EditFileWithBodyWithResponse(ctx, repo.ProjectKey, repo.Slug, trimmedPath, writer.FormDataContentType(), body)
	if err != nil {
		return nil, apperrors.New(apperrors.KindTransient, "failed to edit file", err)
	}

	if err := openapi.MapStatusError(resp.StatusCode(), resp.Body); err != nil {
		return nil, err
	}

	if resp.ApplicationjsonCharsetUTF8200 == nil {
		return nil, apperrors.New(apperrors.KindPermanent, "empty commit response from server", nil)
	}

	return resp.ApplicationjsonCharsetUTF8200, nil
}

func validateRepositoryRef(repo RepositoryRef) error {
	if strings.TrimSpace(repo.ProjectKey) == "" || strings.TrimSpace(repo.Slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}
	return nil
}

// repositoryAPIPath builds a REST path for a repository endpoint that takes a
// file path as a trailing wildcard, such as /raw/{path} or /browse/{path}.
// encodedPath must already be escaped by encodeFilePath.
func repositoryAPIPath(repo RepositoryRef, endpoint string, encodedPath string) string {
	return fmt.Sprintf(
		"/rest/api/latest/projects/%s/repos/%s/%s/%s",
		url.PathEscape(strings.TrimSpace(repo.ProjectKey)),
		url.PathEscape(strings.TrimSpace(repo.Slug)),
		endpoint,
		encodedPath,
	)
}

// encodeFilePath escapes each segment of a repository file path and rejoins them
// with real "/" separators.
//
// The generated OpenAPI client escapes the whole path as a single parameter,
// turning "/" into "%2F", which the /raw/{path} and /browse/{path} endpoints do
// not accept. Escaping per segment keeps the separators intact while ensuring a
// path cannot introduce query parameters, fragments, or traversal that would
// redirect the request to a different endpoint.
func encodeFilePath(path string) (string, error) {
	segments := strings.Split(strings.TrimSpace(path), "/")
	encoded := make([]string, 0, len(segments))

	for _, segment := range segments {
		trimmed := strings.TrimSpace(segment)
		if trimmed == "" || trimmed == "." {
			continue
		}
		if trimmed == ".." {
			return "", apperrors.New(apperrors.KindValidation, `path must not contain ".." segments`, nil)
		}
		encoded = append(encoded, url.PathEscape(trimmed))
	}

	if len(encoded) == 0 {
		return "", apperrors.New(apperrors.KindValidation, "path is required", nil)
	}

	return strings.Join(encoded, "/"), nil
}
