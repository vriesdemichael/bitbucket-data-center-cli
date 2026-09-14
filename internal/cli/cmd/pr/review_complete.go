package prcmd

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// noSuchPullRequestReviewException is Bitbucket's answer to completing a review
// the caller never started.
const noSuchPullRequestReviewException = "com.atlassian.bitbucket.pull.NoSuchPullRequestReviewException"

// isNoDraftReview reports whether a finish-review answer says there was no
// draft review to finish.
//
// Read from the exception name rather than the message: the name is the part
// of a Bitbucket error that is not reworded between releases, and it is what
// separates this 404 from the one for a pull request that does not exist.
func isNoDraftReview(status int, body []byte) bool {
	return status == http.StatusNotFound && openapi.NamesException(body, noSuchPullRequestReviewException)
}

// hasDraftReview reports whether the caller has a draft review on the pull
// request, which is what completing one needs.
//
// Bitbucket reads a review back as its pending comment threads and answers an
// empty page when there is none, which is exactly when finishing it answers
// 404: deleting the last draft comment leaves nothing to finish either. A
// pending reply comes back as the published thread it belongs to, so one thread
// is enough to know, and one is all this asks for.
func hasDraftReview(ctx context.Context, client *openapigenerated.ClientWithResponses, target prsel.Target) (bool, error) {
	limit := float32(1)
	response, err := client.GetReviewWithResponse(ctx, target.ProjectKey, target.RepoSlug, target.PullRequestID,
		&openapigenerated.GetReviewParams{Limit: &limit})
	if err != nil {
		return false, err
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		return false, err
	}
	if response.ApplicationjsonCharsetUTF8200 == nil {
		return false, openapi.MissingPayload(response.StatusCode(), response.Body, "reading your draft review")
	}

	values := response.ApplicationjsonCharsetUTF8200.Values

	return values != nil && len(*values) > 0, nil
}

// noDraftReviewError is the failure of completing a review that was never
// started.
//
// It stays not_found, the kind Bitbucket's 404 maps to, because that is what
// happened: the draft review the command acts on does not exist, and nothing
// was changed. conflict would say the pull request is in a state that refuses
// the request, and validation that the input is wrong; the same input
// completes a review that was started.
//
// What it adds is the command for each part of what was asked, because the
// usual way here is posting comments directly and then asking this command for
// a status it only submits as part of a review.
func noDraftReviewError(target prsel.Target, status, comment string) error {
	message := fmt.Sprintf("you have no draft review on pull request #%s to complete, so nothing was changed; %s",
		target.PullRequestID, noDraftReviewRemedy(target, status, comment))

	err := apperrors.WithDetail(apperrors.New(apperrors.KindNotFound, message, nil),
		"upstreamStatus", strconv.Itoa(http.StatusNotFound))

	return apperrors.WithDetail(err, "upstreamException", noSuchPullRequestReviewException)
}

// noDraftReviewReason is what a preview says about the same case.
func noDraftReviewReason(target prsel.Target, status, comment string) string {
	return fmt.Sprintf("you have no draft review on pull request #%s to complete; %s",
		target.PullRequestID, noDraftReviewRemedy(target, status, comment))
}

// noDraftReviewRemedy names the command for each part of what was asked,
// spelled with the values given so it runs as it stands.
//
// With neither a status nor a comment there is nothing left undone, only
// nothing to publish, so it says how a draft review starts rather than
// suggesting a change nobody asked for.
func noDraftReviewRemedy(target prsel.Target, status, comment string) string {
	repoFlag := "--repo " + target.ProjectKey + "/" + target.RepoSlug

	var remedies []string
	if status != "" {
		remedies = append(remedies, fmt.Sprintf("set the status with: bb pr review set %s %s %s",
			target.PullRequestID, status, repoFlag))
	}
	if comment != "" {
		remedies = append(remedies, fmt.Sprintf("post the comment with: bb pr comment add %s --text %s %s",
			target.PullRequestID, shellQuote(comment), repoFlag))
	}
	if len(remedies) == 0 {
		return "a draft review starts with a comment added by bb pr comment add --pending"
	}

	return strings.Join(remedies, "; ")
}

// shellQuote quotes text as one shell word, so a comment echoed back into a
// command is neither split nor expanded when that command is run.
func shellQuote(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}
