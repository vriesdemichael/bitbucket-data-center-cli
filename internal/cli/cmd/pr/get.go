package prcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestGetCommand(deps Dependencies, repository *string) *cobra.Command {
	var noReviewSummary bool
	getCmd := &cobra.Command{
		Use:     "get <pr-id>",
		Aliases: []string{"view"},
		Short:   "Get pull request details, including outstanding review feedback",
		Example: `  # A pull request, with what is still outstanding on it
  bb pr get 42 --repo PROJ/repo

  # Inside a checkout: the open pull request of the checked-out branch
  bb pr view

  # Without counting the unresolved threads, which is quicker
  bb pr get 42 --repo PROJ/repo --no-review-summary`,
		Long: "Get pull request details. The output carries a review summary describing unresolved comment " +
			"threads, open tasks and reviewers who requested changes, so outstanding feedback is visible without " +
			"a separate lookup.\n\n" +
			"The unresolved thread counts come from the activity timeline, which is paged through; pass " +
			"--no-review-summary to skip it. When the timeline is unavailable the summary falls back to the " +
			"blocker-comment tally, then to the counters Bitbucket ships with the pull request. " +
			"reviewSummary.countsSource reports which was used.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			pullRequest, err := service.Get(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			counts, err := resolveReviewCounts(cmd.Context(), client, repo, target.PullRequestID, !noReviewSummary)
			if err != nil {
				return err
			}
			reviewSummary := pullrequestservice.BuildReviewSummary(pullRequest, counts)

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SinglePullRequest{
					Repository:    repositoryOf(repo),
					PullRequest:   result.PullRequestFrom(pullRequest),
					ReviewSummary: reviewSummaryFrom(reviewSummary),
				})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "#%d\t%s\t%s -> %s\t%s\n", pullRequest.ID, pullRequest.State, pullRequest.SourceBranch, pullRequest.TargetBranch, pullRequest.Title)
			if pullRequest.URL != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "URL: %s\n", pullRequest.URL)
			}
			if len(pullRequest.Reviewers) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Reviewers: %d\n", len(pullRequest.Reviewers))
			}
			for _, line := range formatReviewSummaryLines(reviewSummary) {
				fmt.Fprintln(cmd.OutOrStdout(), line)
			}
			if pullRequest.Mergeability != nil {
				mergeability := pullRequest.Mergeability
				mergeableLabel := "no"
				if mergeability.Mergeable {
					mergeableLabel = "yes"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Mergeable: %s\n", mergeableLabel)
				if mergeability.Outcome != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Merge outcome: %s\n", mergeability.Outcome)
				}
				if mergeability.Conflicted {
					fmt.Fprintln(cmd.OutOrStdout(), "Merge conflicts: yes")
				}
				if lines := mergeBlockerLines(mergeability.Blockers); len(lines) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Merge blockers:")
					for _, line := range lines {
						fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", line)
					}
				}
			}

			return nil
		},
	}
	getCmd.Flags().BoolVar(&noReviewSummary, "no-review-summary", false, "Skip the paged activity timeline walk; the summary still carries the open task tally, which costs one request")
	return getCmd
}

// mergeBlockerLines renders one line per veto for `bb pr get`.
//
// A veto carries a summary and a detail and either can be empty, so the line has
// to be built rather than read off one field: a veto with only a detail used to
// print an empty bullet. The two are joined only when they say different things,
// because Bitbucket repeats the summary as the detail for several of its own
// merge checks.
//
// It is deliberately not mergeBlockingReasons. That one feeds the dry-run
// preview's blockingReasons and joins the pair as "summary: detail" rather than
// "summary (detail)", and it adds a line for a conflict and a fallback for a
// veto-less refusal, neither of which belongs in a display list that only runs
// when there are vetoes. The two formats are a divergence worth removing, but
// blockingReasons is machine output and changing its shape is a contract change.
func mergeBlockerLines(blockers []pullrequestservice.MergeBlocker) []string {
	lines := make([]string, 0, len(blockers))

	for _, blocker := range blockers {
		summary := strings.TrimSpace(blocker.Summary)
		detail := strings.TrimSpace(blocker.Detail)

		switch {
		case summary != "" && detail != "" && !strings.EqualFold(summary, detail):
			lines = append(lines, fmt.Sprintf("%s (%s)", summary, detail))
		case summary != "":
			lines = append(lines, summary)
		case detail != "":
			lines = append(lines, detail)
		}
	}

	return lines
}
