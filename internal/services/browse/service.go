package browse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/url"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

type TreeOptions struct {
	At       string
	PageSize int
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

	// The /files endpoint takes the directory as a trailing wildcard, so it
	// needs the same per-segment escaping as /raw and /browse.
	encodedPath, err := encodeOptionalFilePath(path)
	if err != nil {
		return nil, err
	}

	if options.PageSize <= 0 {
		options.PageSize = 1000
	}

	apiPath := repositoryRootPath(repo, "files")
	if encodedPath != "" {
		apiPath = repositoryAPIPath(repo, "files", encodedPath)
	}

	results := make([]string, 0)
	start := 0

	for {
		query := map[string]string{
			"start": strconv.Itoa(start),
			"limit": strconv.Itoa(options.PageSize),
		}
		if strings.TrimSpace(options.At) != "" {
			query["at"] = strings.TrimSpace(options.At)
		}

		var response fileListResponse
		if err := service.http.GetJSON(ctx, apiPath, query, &response); err != nil {
			return nil, err
		}

		for _, value := range response.Values {
			if name, ok := value.(string); ok {
				results = append(results, name)
			}
		}

		if response.IsLastPage || response.NextPageStart == nil {
			break
		}
		if *response.NextPageStart == start {
			break
		}

		start = *response.NextPageStart
	}

	return results, nil
}

type fileListResponse struct {
	Values        []any `json:"values"`
	IsLastPage    bool  `json:"isLastPage"`
	NextPageStart *int  `json:"nextPageStart"`
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

	// Unlike /raw, the /browse endpoint answers with JSON, so it is requested
	// as JSON and handed back undecoded for the caller to render.
	var content json.RawMessage
	if err := service.http.GetJSON(ctx, repositoryAPIPath(repo, "browse", encodedPath), query, &content); err != nil {
		return nil, err
	}

	return content, nil
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
	return openapi.ValidateRepository(repo.ProjectKey, repo.Slug)
}

// repositoryAPIPath builds a REST path for a repository endpoint that takes a
// file path as a trailing wildcard, such as /raw/{path} or /browse/{path}.
// encodedPath must already be escaped by encodeFilePath.
//
// Kept as a single fmt.Sprintf return so tools/quality-report can statically
// resolve the endpoints reached through the raw httpclient; see
// repositoryRootPath for the variant without a trailing path.
func repositoryAPIPath(repo RepositoryRef, endpoint string, encodedPath string) string {
	return fmt.Sprintf(
		"/rest/api/latest/projects/%s/repos/%s/%s/%s",
		url.PathEscape(strings.TrimSpace(repo.ProjectKey)),
		url.PathEscape(strings.TrimSpace(repo.Slug)),
		endpoint,
		encodedPath,
	)
}

// repositoryRootPath builds the same REST path without a trailing file path,
// for endpoints where it is optional (listing the repository root).
func repositoryRootPath(repo RepositoryRef, endpoint string) string {
	return fmt.Sprintf(
		"/rest/api/latest/projects/%s/repos/%s/%s",
		url.PathEscape(strings.TrimSpace(repo.ProjectKey)),
		url.PathEscape(strings.TrimSpace(repo.Slug)),
		endpoint,
	)
}

// encodeOptionalFilePath behaves like encodeFilePath but allows an empty path,
// for endpoints that treat it as "the repository root".
func encodeOptionalFilePath(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	return encodeFilePath(path)
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
