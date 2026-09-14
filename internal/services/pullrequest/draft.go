package pullrequest

import (
	"context"
	"fmt"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// SetDraft marks a pull request a draft, or ready for review, reading its
// version itself rather than asking the caller for it.
//
// It reports whether anything was sent. An open pull request already in the
// requested state comes back as it is: the change is already true, and writing
// it again would only bump the version under anyone else holding it.
//
// A pull request that is not open is refused before anything is sent; see
// DraftChangeRefusal.
func (service *Service) SetDraft(ctx context.Context, repository RepositoryRef, pullRequestID string, draft bool) (PullRequest, bool, error) {
	if err := validateRepositoryRef(repository); err != nil {
		return PullRequest{}, false, err
	}

	resolvedID, err := normalizePullRequestID(pullRequestID)
	if err != nil {
		return PullRequest{}, false, err
	}

	var updated PullRequest
	changed := false

	err = service.writeAtCurrentVersion(ctx, repository, resolvedID, func(current PullRequest) error {
		if err := DraftChangeRefusal(current); err != nil {
			return err
		}
		if current.Draft == draft {
			updated, changed = current, false

			return nil
		}

		// The reviewers travel with it, because a PUT without them empties the
		// list (#511). The title and description do not need to: Bitbucket
		// keeps both when they are absent.
		payload := map[string]any{
			"version":   current.Version,
			"draft":     draft,
			"reviewers": reviewerEcho(current),
		}

		var response pullRequestValue
		if err := service.client.PutJSON(ctx, fmt.Sprintf("%s/%s", pullRequestPath(repository), resolvedID), nil, payload, &response); err != nil {
			return err
		}
		updated, changed = mapPullRequest(response), true

		return nil
	})
	if err != nil {
		return PullRequest{}, false, err
	}

	return updated, changed, nil
}

// DraftChangeRefusal says why a pull request's draft state cannot be changed,
// or returns nil when it can: only an open pull request can be marked ready for
// review or turned into a draft.
//
// Bitbucket does not answer this consistently enough to leave to it. A merged
// pull request refuses any draft value, and a declined one refuses a change --
// but a declined pull request sent the draft flag it already holds answers 200
// and bumps its version. Sending that would report a declined pull request as
// marked ready for review. gh refuses a closed pull request for the same
// command, before it looks at the draft flag at all.
func DraftChangeRefusal(current PullRequest) error {
	if current.Open {
		return nil
	}

	return apperrors.New(apperrors.KindConflict, fmt.Sprintf(
		"pull request #%d is %s; only an open pull request can be marked ready for review or turned into a draft",
		current.ID, current.State), nil)
}

// reviewerEcho is a pull request's current reviewers in the shape an update
// sends, so a change to something else leaves them where they are.
func reviewerEcho(current PullRequest) []map[string]any {
	existing := make([]map[string]any, 0, len(current.Reviewers))
	for _, reviewer := range current.Reviewers {
		if name := strings.TrimSpace(reviewer.Name); name != "" {
			existing = append(existing, map[string]any{"user": map[string]any{"name": name}})
		}
	}

	return existing
}
