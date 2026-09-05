// Package codeowners asks Bitbucket who owns the code a change touches.
//
// Bitbucket resolves CODEOWNERS itself: the file format, the matching, the
// group expansion and the selection strategies all live in its bundled
// code-owners module, and the "add code owners" button in the pull request UI
// calls the endpoint this package calls.
//
// bb used to parse the file and resolve the owners on its own, and the two
// implementations did not agree. Bitbucket requires the at-sign for a user and
// the "reviewer-group/" prefix for a reviewer group; bb accepted a bare name as
// a user and read a bare "@name" as a reviewer group, so the same file assigned
// two people through the CLI and nobody through the button. Asking the server
// is the only way for those to be the same answer.
package codeowners

import (
	"context"
	"fmt"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// RepositoryRef identifies a repository.
type RepositoryRef struct {
	ProjectKey string
	Slug       string
}

// Service reads code owners from Bitbucket.
type Service struct {
	client *httpclient.Client
}

// NewService builds a Service on an authenticated client.
func NewService(client *httpclient.Client) *Service {
	return &Service{client: client}
}

// ownersResponse is the endpoint's payload: Bitbucket returns users, already
// resolved out of whatever groups and strategies the file named.
type ownersResponse struct {
	CodeOwners []struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	} `json:"codeOwners"`
}

// Owners returns the usernames Bitbucket considers responsible for the change
// between two refs.
//
// sourceRepo names the repository holding sourceRef and is only needed when it
// differs from the target -- a pull request from a fork. Passing nil means the
// two are the same repository, which is what the endpoint assumes.
//
// A repository with no CODEOWNERS file, or a change no rule matches, is not an
// error: both come back as no owners. Bitbucket also skips an entry it cannot
// resolve rather than refusing the whole file, so a name that matches nobody
// costs the caller nothing beyond that name.
func (service *Service) Owners(
	ctx context.Context,
	repository RepositoryRef,
	sourceRef, targetRef string,
	sourceRepo *RepositoryRef,
) ([]string, error) {
	if strings.TrimSpace(repository.ProjectKey) == "" || strings.TrimSpace(repository.Slug) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "repository must be specified as PROJECT/slug", nil)
	}
	if strings.TrimSpace(sourceRef) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "source ref is required to find code owners", nil)
	}
	if strings.TrimSpace(targetRef) == "" {
		return nil, apperrors.New(apperrors.KindValidation, "target ref is required to find code owners", nil)
	}

	query := map[string]string{
		"sourceRefId": sourceRef,
		"targetRefId": targetRef,
	}
	if sourceRepo != nil &&
		(!strings.EqualFold(sourceRepo.ProjectKey, repository.ProjectKey) ||
			!strings.EqualFold(sourceRepo.Slug, repository.Slug)) {
		query["sourceRepo"] = sourceRepo.ProjectKey + "/" + sourceRepo.Slug
	}

	var payload ownersResponse
	path := fmt.Sprintf("/rest/ui/latest/projects/%s/repos/%s/code-owners", repository.ProjectKey, repository.Slug)
	if err := service.client.GetJSON(ctx, path, query, &payload); err != nil {
		return nil, err
	}

	owners := make([]string, 0, len(payload.CodeOwners))
	for _, owner := range payload.CodeOwners {
		name := strings.TrimSpace(owner.Name)
		if name == "" {
			name = strings.TrimSpace(owner.Slug)
		}
		if name != "" {
			owners = append(owners, name)
		}
	}

	return owners, nil
}
