package repository

import (
	"context"
	"net/http"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// noSuchPathException is how Bitbucket answers a README request for a
// repository that has none.
const noSuchPathException = "com.atlassian.bitbucket.content.NoSuchPathException"

// Get reads one repository.
func (service *AdminService) Get(ctx context.Context, repo RepositoryRef) (openapigenerated.RestRepository, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return openapigenerated.RestRepository{}, err
	}

	response, err := service.client.GetRepositoryWithResponse(ctx, repo.ProjectKey, repo.Slug)
	if err != nil {
		return openapigenerated.RestRepository{}, apperrors.Transport("failed to get repository", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return openapigenerated.RestRepository{}, err
	}
	if response.ApplicationjsonCharsetUTF8200 == nil {
		return openapigenerated.RestRepository{}, openapi.MissingPayload(response.StatusCode(), response.Body, "reading the repository")
	}

	return *response.ApplicationjsonCharsetUTF8200, nil
}

// Readme returns a repository's README as it is stored, unrendered, and
// whether there is one.
//
// Bitbucket picks the file, from the default branch. A repository without a
// README answers 404 naming NoSuchPathException -- an empty repository answers
// the same -- and that is an answer rather than a failure. No other 404 is: the
// same endpoint asked at a ref that does not exist answers
// NoSuchObjectException, and reading every 404 as "no README" would report a
// broken request as a repository with nothing to say.
func (service *AdminService) Readme(ctx context.Context, repo RepositoryRef) ([]byte, bool, error) {
	if err := validateRepositoryRef(repo); err != nil {
		return nil, false, err
	}

	response, err := service.client.StreamReadmeWithResponse(ctx, repo.ProjectKey, repo.Slug, nil)
	if err != nil {
		return nil, false, apperrors.Transport("failed to read the repository README", err)
	}
	if response.StatusCode() == http.StatusNotFound && openapi.NamesException(response.Body, noSuchPathException) {
		return nil, false, nil
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return nil, false, err
	}

	return response.Body, true, nil
}
