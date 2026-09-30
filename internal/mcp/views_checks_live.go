//go:build live

package mcp

import (
	"context"

	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// RequiredChecksForLiveSuite is requiredChecksForView for the live suite, which
// holds it against Bitbucket's own merge veto, with the listing read as the
// card reads it. It is built only with the live tag, so bb itself exports
// nothing: the card is the function's one caller.
func RequiredChecksForLiveSuite(ctx context.Context, c Clients, project, repo string, pr pullrequestservice.PullRequest) ([]viewRequiredCheck, bool) {
	listing, err := pullRequestBuilds(ctx, c, project, repo, pr)
	if err != nil {
		return requiredChecksForView(ctx, c, project, repo, pr, nil)
	}
	return requiredChecksForView(ctx, c, project, repo, pr, &listing)
}
