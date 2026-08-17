package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

// prStatusSection is one of the three lists bb pr status prints.
//
// Note carries why a section is empty when the reason is not "nothing
// matched" — no git repository, a detached HEAD, no repository context, no
// known username. Without it a section that found nothing and a section that
// could not be computed at all look identical, which is exactly the ambiguity
// an agent would resolve by guessing.
type prStatusSection struct {
	PullRequests []pullrequestservice.PullRequest `json:"pull_requests"`
	Note         string                           `json:"note,omitempty"`
}

type prStatusCurrentBranchSection struct {
	prStatusSection
	Branch     string `json:"branch,omitempty"`
	Repository string `json:"repository,omitempty"`
}

type prStatusPayload struct {
	CurrentBranch        prStatusCurrentBranchSection `json:"current_branch"`
	CreatedByYou         prStatusSection              `json:"created_by_you"`
	RequestingYourReview prStatusSection              `json:"requesting_your_review"`
}

// newPullRequestStatusCommand builds `bb pr status`, the "what is waiting on
// me" view.
//
// gh has had this shape for years and it is the name people reach for, but bb
// had nothing under it: `bb pr list` needs a repository, and the cross-
// repository dashboard was only reachable as `bb search prs --role`. So this is
// a genuinely new view rather than an alias — three dashboard queries and the
// current branch, assembled into the answer both humans and agents actually
// want. See issue #338.
func newPullRequestStatusCommand(options *rootOptions, repositorySelector *string) *cobra.Command {
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
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			payload := prStatusPayload{}

			payload.CurrentBranch = collectCurrentBranchPullRequests(cmd.Context(), service, cfg, *repositorySelector, listPaging.ServiceLimit())

			created, err := service.ListDashboard(cmd.Context(), pullrequestservice.DashboardListOptions{
				State: "open",
				Role:  "author",
				Limit: listPaging.ServiceLimit(),
			})
			if err != nil {
				return err
			}
			payload.CreatedByYou = prStatusSection{PullRequests: created}

			reviewing, err := service.ListDashboard(cmd.Context(), pullrequestservice.DashboardListOptions{
				State: "open",
				Role:  "reviewer",
				Limit: listPaging.ServiceLimit(),
			})
			if err != nil {
				return err
			}
			payload.RequestingYourReview = filterReviewsStillWanted(reviewing, cfg.BitbucketUsername)

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), payload)
			}

			writePullRequestStatus(cmd, payload)

			return nil
		},
	}
	listPaging.Register(command, paging.DefaultLimit)

	return command
}

// filterReviewsStillWanted drops the pull requests the caller has already
// approved.
//
// role=REVIEWER means "you are a reviewer", not "your review is outstanding",
// so without this the section keeps showing work that is finished. The filter
// needs a username to match against; when none is configured the section is
// returned unfiltered with a note saying so, because silently showing a
// superset is worse than saying which set this is.
func filterReviewsStillWanted(pullRequests []pullrequestservice.PullRequest, username string) prStatusSection {
	trimmedUsername := strings.TrimSpace(username)
	if trimmedUsername == "" {
		return prStatusSection{
			PullRequests: pullRequests,
			Note:         "includes pull requests you already approved: no username configured to filter by (set BITBUCKET_USERNAME)",
		}
	}

	outstanding := make([]pullrequestservice.PullRequest, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		if reviewerApprovedByUser(pullRequest.Reviewers, trimmedUsername) {
			continue
		}
		outstanding = append(outstanding, pullRequest)
	}

	return prStatusSection{PullRequests: outstanding}
}

// collectCurrentBranchPullRequests answers "is there a pull request for what I
// have checked out".
//
// Every way of not knowing — no git, detached HEAD, no Bitbucket repository
// context — resolves to a note rather than an error. The other two sections do
// not need a checkout, and failing the whole command because this one section
// cannot be computed would make bb pr status unusable outside a repository.
func collectCurrentBranchPullRequests(
	ctx context.Context,
	service *pullrequestservice.Service,
	cfg config.AppConfig,
	repositorySelector string,
	limit int,
) prStatusCurrentBranchSection {
	empty := func(note string) prStatusCurrentBranchSection {
		return prStatusCurrentBranchSection{
			prStatusSection: prStatusSection{PullRequests: []pullrequestservice.PullRequest{}, Note: note},
		}
	}

	repo, err := resolvePullRequestRepositoryReference(repositorySelector, cfg)
	if err != nil {
		return empty("no repository context (use --repo PROJECT/slug, or run inside a Bitbucket checkout)")
	}

	branch, err := currentGitBranch(ctx)
	if err != nil {
		return empty("could not read the current branch: " + err.Error())
	}
	if branch == "" {
		return empty("not on a branch (no git repository, or HEAD is detached)")
	}

	pullRequests, err := service.List(ctx, repo, pullrequestservice.ListOptions{
		State:        "open",
		Limit:        limit,
		SourceBranch: branch,
	})
	if err != nil {
		return empty(fmt.Sprintf("could not list pull requests for %s: %s", branch, err.Error()))
	}

	return prStatusCurrentBranchSection{
		prStatusSection: prStatusSection{PullRequests: pullRequests},
		Branch:          branch,
		Repository:      fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
	}
}

// currentGitBranch returns the checked-out branch, or an empty string when
// there is no branch to report. Not being in a repository is one of those
// cases, not an error.
func currentGitBranch(ctx context.Context) (string, error) {
	backend := gitBackendFactory()
	if backend == nil {
		return "", nil
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", nil
	}

	repositoryRoot, err := backend.RepositoryRoot(ctx, workingDirectory)
	if err != nil {
		if isNonRepositoryError(err) {
			return "", nil
		}
		return "", err
	}

	branch, err := backend.CurrentBranch(ctx, repositoryRoot)
	if err != nil {
		if isNonRepositoryError(err) {
			return "", nil
		}
		return "", err
	}

	return branch, nil
}

func writePullRequestStatus(cmd *cobra.Command, payload prStatusPayload) {
	writer := cmd.OutOrStdout()

	heading := "Current branch"
	if payload.CurrentBranch.Branch != "" {
		heading = fmt.Sprintf("Current branch (%s)", payload.CurrentBranch.Branch)
	}
	writePullRequestStatusSection(cmd, heading, payload.CurrentBranch.prStatusSection, "No pull request for the current branch")
	fmt.Fprintln(writer)
	writePullRequestStatusSection(cmd, "Created by you", payload.CreatedByYou, "You have no open pull requests")
	fmt.Fprintln(writer)
	writePullRequestStatusSection(cmd, "Requesting a code review from you", payload.RequestingYourReview, "You have no pull requests to review")
}

func writePullRequestStatusSection(cmd *cobra.Command, heading string, section prStatusSection, emptyMessage string) {
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
		if pullRequest.Repository != nil && pullRequest.Repository.ProjectKey != "" {
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
