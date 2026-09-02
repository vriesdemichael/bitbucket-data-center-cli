package prcmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/giturl"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestStatusCommand(deps Dependencies, repositorySelector *string) *cobra.Command {
	var listPaging paging.Options

	command := &cobra.Command{
		Use:   "status",
		Short: "Show pull requests waiting on you",
		Long: "Show pull requests waiting on you, in three sections: the pull requests for the current " +
			"branch, the ones you opened, and the ones asking for your review.\n\n" +
			"The last two are cross-repository and need no repository context. The current-branch section " +
			"needs a git checkout with a Bitbucket remote, and is reported as unavailable rather than as " +
			"an error when there is not one.",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			payload := Status{}

			payload.CurrentBranch = collectCurrentBranchPullRequests(cmd.Context(), deps, service, cfg, *repositorySelector, listPaging.ServiceLimit())

			created, err := service.ListDashboard(cmd.Context(), pullrequestservice.DashboardListOptions{
				State:      "open",
				Role:       "author",
				MaxResults: listPaging.ServiceLimit(),
			})
			if err != nil {
				return err
			}
			payload.CreatedByYou = StatusSection{PullRequests: result.PullRequestsFrom(created)}

			reviewing, err := service.ListDashboard(cmd.Context(), pullrequestservice.DashboardListOptions{
				State:             "open",
				Role:              "reviewer",
				ParticipantStatus: "UNAPPROVED",
				MaxResults:        listPaging.ServiceLimit(),
			})
			if err != nil {
				return err
			}
			payload.RequestingYourReview = StatusSection{PullRequests: result.PullRequestsFrom(reviewing)}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), payload)
			}

			writePullRequestStatus(cmd, payload)

			return nil
		},
	}
	listPaging.Register(command, paging.DefaultLimit)

	return command
}

func collectCurrentBranchPullRequests(
	ctx context.Context,
	deps Dependencies,
	service *pullrequestservice.Service,
	cfg config.AppConfig,
	repositorySelector string,
	limit int,
) CurrentBranchSection {
	empty := func(note string) CurrentBranchSection {
		return CurrentBranchSection{
			StatusSection: StatusSection{PullRequests: []result.PullRequest{}, Note: note},
		}
	}

	repoProj, repoSlug, err := reposel.Resolve(repositorySelector, cfg)
	if err != nil {
		return empty("no repository context (use --repo PROJECT/slug, or run inside a Bitbucket checkout)")
	}
	repo := pullrequestservice.RepositoryRef{ProjectKey: repoProj, Slug: repoSlug}

	branch, err := currentGitBranch(ctx, deps)
	if err != nil {
		return empty("could not read the current branch: " + err.Error())
	}
	if branch == "" {
		return empty("not on a branch (no git repository, or HEAD is detached)")
	}

	pullRequests, err := service.List(ctx, repo, pullrequestservice.ListOptions{
		State:        "open",
		MaxResults:   limit,
		SourceBranch: branch,
	})
	if err != nil {
		return empty(fmt.Sprintf("could not list pull requests for %s: %s", branch, err.Error()))
	}

	return CurrentBranchSection{
		StatusSection: StatusSection{PullRequests: result.PullRequestsFrom(pullRequests)},
		Branch:        branch,
		Repository:    fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
	}
}

func currentGitBranch(ctx context.Context, deps Dependencies) (string, error) {
	backend := deps.GitBackend()
	if backend == nil {
		return "", nil
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", nil
	}

	repositoryRoot, err := backend.RepositoryRoot(ctx, workingDirectory)
	if err != nil {
		if giturl.IsNonRepositoryError(err) {
			return "", nil
		}
		return "", err
	}

	branch, err := backend.CurrentBranch(ctx, repositoryRoot)
	if err != nil {
		if giturl.IsNonRepositoryError(err) {
			return "", nil
		}
		return "", err
	}

	return branch, nil
}

func writePullRequestStatus(cmd *cobra.Command, payload Status) {
	writer := cmd.OutOrStdout()

	heading := "Current branch"
	if payload.CurrentBranch.Branch != "" {
		heading = fmt.Sprintf("Current branch (%s)", payload.CurrentBranch.Branch)
	}
	writePullRequestStatusSection(cmd, heading, payload.CurrentBranch.StatusSection, "No pull request for the current branch")
	fmt.Fprintln(writer)
	writePullRequestStatusSection(cmd, "Created by you", payload.CreatedByYou, "You have no open pull requests")
	fmt.Fprintln(writer)
	writePullRequestStatusSection(cmd, "Requesting a code review from you", payload.RequestingYourReview, "You have no pull requests to review")
}

func writePullRequestStatusSection(cmd *cobra.Command, heading string, section StatusSection, emptyMessage string) {
	writer := cmd.OutOrStdout()
	fmt.Fprintln(writer, style.Label.Render(heading))

	if len(section.PullRequests) == 0 {
		message := emptyMessage
		if section.Note != "" {
			message = section.Note
		}
		fmt.Fprintf(writer, "  %s\n", style.Empty.Render(message))
		return
	}

	if section.Note != "" {
		fmt.Fprintf(writer, "  %s\n", style.Empty.Render(section.Note))
	}

	for _, pullRequest := range section.PullRequests {
		repositoryPrefix := ""
		if pullRequest.Repository.ProjectKey != "" {
			repositoryPrefix = fmt.Sprintf("[%s/%s] ", pullRequest.Repository.ProjectKey, pullRequest.Repository.Slug)
		}

		indicator := formatPullRequestCounts(pullRequest)
		if indicator != "" {
			indicator = "\t" + indicator
		}

		fmt.Fprintf(
			writer,
			"  %s\t%s\t%s%s\n",
			style.Resource.Render(fmt.Sprintf("#%d", pullRequest.ID)),
			pullRequest.State,
			repositoryPrefix+pullRequest.Title,
			indicator,
		)
	}
}
