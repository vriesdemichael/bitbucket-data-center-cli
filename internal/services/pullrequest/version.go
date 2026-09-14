package pullrequest

import (
	"context"
	"net/http"
	"strconv"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// writeAtCurrentVersion reads the pull request and hands it to write, which
// sends a change carrying the version it was given. When Bitbucket refuses that
// version as out of date, the pull request is read again and write is called
// once more with what the second read found.
//
// The version is an optimistic lock the caller has no reason to know, so bb
// reads it rather than demanding it (#532). A version read a moment ago can
// already be stale by the time the write lands: advancing the target branch
// makes Bitbucket rescope the pull request and bump its version asynchronously
// (#598). Reading the version and then handing back a 409 that names a version
// the caller never supplied would only half keep that.
//
// Only for a version bb resolved itself. A caller who passed --version asserted
// a specific lock, and the conflict is the answer they asked for.
//
// The retry is safe because of what the refusal is: Bitbucket checks the
// version before it changes anything, so a stale-version 409 applied nothing.
// Nothing else is retried. A write whose outcome is unknown -- a timeout, a lost
// connection -- may have landed.
//
// Once rather than in a loop: a second staleness in the same instant means
// something is writing continuously, which is worth reporting rather than
// racing.
func (service *Service) writeAtCurrentVersion(ctx context.Context, repository RepositoryRef, resolvedID string, write func(current PullRequest) error) error {
	current, err := service.Get(ctx, repository, resolvedID)
	if err != nil {
		return err
	}

	return retryOnceWhenStale(current, func() (PullRequest, error) {
		return service.Get(ctx, repository, resolvedID)
	}, write)
}

// retryOnceWhenStale is the decision writeAtCurrentVersion makes, apart from
// the reads, so it can be exercised without a server.
func retryOnceWhenStale(current PullRequest, reread func() (PullRequest, error), write func(current PullRequest) error) error {
	err := write(current)
	if !isStaleVersion(err) {
		return err
	}

	fresh, readErr := reread()
	if readErr != nil {
		// Report the conflict, not the failure to investigate it.
		return err
	}

	return write(fresh)
}

// isStaleVersion reports whether err is Bitbucket refusing a pull request
// change because the version it carried is not the one the server holds.
//
// The exception name is the only part of a 409 that tells a stale optimistic
// lock from every other conflict. A declined pull request refusing a draft
// change answers 409 as well, and retrying that would only be refused again.
func isStaleVersion(err error) bool {
	if err == nil {
		return false
	}

	details := apperrors.DetailsOf(err)

	return details["upstreamStatus"] == strconv.Itoa(http.StatusConflict) &&
		details["upstreamException"] == pullRequestOutOfDateException
}
