package prcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestListCommand(deps Dependencies, repository *string) *cobra.Command {
	var state string
	var listPaging paging.Options
	var start int
	var sourceBranch string
	var targetBranch string
	var listWithReviewStatus bool

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		Example: `  # The open pull requests of a repository
  bb pr list --repo PROJ/repo

  # Merged into main
  bb pr list --repo PROJ/repo --state merged --target-branch main

  # Whatever was opened from a branch, open or not
  bb pr list --repo PROJ/repo --source-branch feature/x --state all

  # Open ones, with the unresolved threads of each counted
  bb pr list --repo PROJ/repo --with-review-status`,
		Long: "List pull requests. Each entry carries the open task and comment counters Bitbucket reports " +
			"with the pull request, so pull requests with outstanding feedback stand out. Pass --with-review-status " +
			"to additionally resolve unresolved comment threads per pull request. That walks each pull request activity " +
			"timeline, so it is markedly slower on a long listing.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repoProj, repoSlug, err := reposel.Resolve(*repository, cfg)
			if err != nil {
				return err
			}
			repo := pullrequestservice.RepositoryRef{ProjectKey: repoProj, Slug: repoSlug}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			pullRequests, err := service.List(cmd.Context(), repo, pullrequestservice.ListOptions{
				State:        state,
				MaxResults:   listPaging.ServiceLimit(),
				Start:        start,
				SourceBranch: sourceBranch,
				TargetBranch: targetBranch,
			})
			if err != nil {
				return err
			}

			var reviewSummaries []pullrequestservice.ReviewSummary
			if listWithReviewStatus {
				reviewSummaries, err = collectReviewSummaries(cmd.Context(), client, repo, pullRequests)
				if err != nil {
					return err
				}
			}

			if deps.JSONEnabled() {
				payload := PullRequests{
					Repository: repositoryOf(repo),
					Filters: ListFilters{
						State:        strings.ToLower(strings.TrimSpace(state)),
						Start:        start,
						Limit:        listPaging.ServiceLimit(),
						SourceBranch: sourceBranch,
						TargetBranch: targetBranch,
					},
					PullRequests: result.PullRequestsFrom(pullRequests),
				}
				if reviewSummaries != nil {
					payload.ReviewSummaries = reviewSummariesFrom(reviewSummaries)
				}

				return deps.WriteJSONList(cmd.OutOrStdout(), payload, paging.LimitReached(listPaging, len(pullRequests)))
			}

			if len(pullRequests) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No pull requests found")
				return nil
			}

			for index, pullRequest := range result.PullRequestsFrom(pullRequests) {
				indicator := formatPullRequestCounts(pullRequest)
				if reviewSummaries != nil {
					indicator = formatReviewStatusIndicator(reviewSummaries[index])
				}
				if indicator != "" {
					indicator = "\t" + indicator
				}

				fmt.Fprintf(
					cmd.OutOrStdout(),
					"#%d\t%s\t%s -> %s\t%s%s\n",
					pullRequest.ID,
					pullRequest.State,
					pullRequest.SourceBranch,
					pullRequest.TargetBranch,
					pullRequest.Title,
					indicator,
				)
			}
			paging.Hint(cmd.ErrOrStderr(), listPaging, len(pullRequests))

			return nil
		},
	}
	listCmd.Flags().BoolVar(&listWithReviewStatus, "with-review-status", false, "Resolve unresolved comment threads per pull request (walks each activity timeline; slower)")
	enumflag.Register(listCmd.Flags(), &state, "state", "open", openapi.PullRequestStateFilters, "Pull request state filter")
	listPaging.Register(listCmd, 25)
	paging.RegisterStart(listCmd, &start)
	listCmd.Flags().StringVar(&sourceBranch, "source-branch", "", "Optional source branch filter")
	listCmd.Flags().StringVar(&targetBranch, "target-branch", "", "Optional target branch filter")
	return listCmd
}
