package completion

import (
	"context"
	"strconv"
	"strings"

	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

func init() {
	register(KindPullRequest, pullRequestSource)
}

// pullRequestSource offers the pull requests of the repository in scope.
//
// Which ones depends on the verb. Offering a merged pull request to `bb pr
// merge` is offering a value the command will refuse, and the set that can be
// reopened is exactly the declined ones -- so the filter is part of the
// answer rather than something the user applies afterwards.
//
// The order is the server's, newest first, and it is kept: a list of numbers
// sorted as text puts #10 above #9, and the one you want is nearly always the
// most recent.
func pullRequestSource(ctx context.Context, environment *Environment, request Request) (Result, error) {
	repository, err := environment.Repository(ctx)
	if err != nil {
		return Result{}, err
	}

	client, err := environment.HTTPClient(ctx)
	if err != nil {
		return Result{}, err
	}

	state, keep := stateFor(commandPath(request.Command))

	pullRequests, err := pullrequestservice.NewService(client).List(
		ctx,
		pullrequestservice.RepositoryRef{ProjectKey: repository.ProjectKey, Slug: repository.Slug},
		pullrequestservice.ListOptions{State: state, MaxResults: maxCandidates},
	)
	if err != nil {
		return Result{}, err
	}

	candidates := make([]Candidate, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		if keep != nil && !keep(pullRequest) {
			continue
		}

		candidates = append(candidates, Candidate{
			Value:       strconv.FormatInt(pullRequest.ID, 10),
			Description: describePullRequest(pullRequest),
		})
	}

	return Result{Candidates: candidates, KeepOrder: true}, nil
}

// stateFor is the listing a verb can act on.
func stateFor(path string) (string, func(pullrequestservice.PullRequest) bool) {
	switch path {
	case "pr reopen":
		return "closed", func(pullRequest pullrequestservice.PullRequest) bool {
			return strings.EqualFold(pullRequest.State, "DECLINED")
		}
	case "pr ready":
		return "open", func(pullRequest pullrequestservice.PullRequest) bool {
			return pullRequest.Draft
		}
	case "pr get", "pr diff", "pr files", "pr commits", "pr activity list", "pr comment list":
		// Reading one says nothing about its state, and the one being looked
		// up is often the one that just merged.
		return "all", nil
	default:
		return "open", nil
	}
}

func describePullRequest(pullRequest pullrequestservice.PullRequest) string {
	description := pullRequest.Title

	author := pullRequest.Author
	if strings.TrimSpace(author) == "" {
		author = pullRequest.AuthorUsername
	}
	if strings.TrimSpace(author) != "" {
		description += " (" + author + ")"
	}

	if pullRequest.Draft {
		description = "draft: " + description
	}

	return description
}
