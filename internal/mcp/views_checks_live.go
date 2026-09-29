//go:build live

package mcp

import (
	"context"

	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// RequiredChecksForLiveSuite is requiredChecksForView for the live suite, which
// holds it against Bitbucket's own merge veto. It is built only with the live
// tag, so bb itself exports nothing: the card is the function's one caller.
func RequiredChecksForLiveSuite(ctx context.Context, c Clients, project, repo string, pr pullrequestservice.PullRequest) ([]viewRequiredCheck, bool) {
	return requiredChecksForView(ctx, c, project, repo, pr)
}
