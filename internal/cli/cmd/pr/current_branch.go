package prcmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// commandsReadingTheCurrentPullRequest are the pr commands that only read one
// pull request, by their path under pr. Without a number they read the open
// pull request from the checked-out branch, as gh pr view and gh pr checks
// do (#766).
var commandsReadingTheCurrentPullRequest = []string{
	"get", "checks", "commits", "files", "merge-base", "diff", "jira",
	"comment list", "activity list", "review get", "auto-merge get", "build status",
}

// openPullRequestsShown is how many open pull requests from one branch are
// listed when more than one could be meant.
const openPullRequestsShown = 10

// takeTheCurrentPullRequest makes the pull request a command reads optional:
// given, it is used as before; left out, it is the open pull request whose
// source is the checked-out branch, found the way pr status finds it, and
// named on stderr as pr create names what it inferred (ADR-102).
//
// Panics on a path that is not a command taking exactly <pr-id>, a wiring
// error the pr command tests reach.
func takeTheCurrentPullRequest(prCmd *cobra.Command, path string, deps Dependencies, repositorySelector *string) {
	command, _, err := prCmd.Find(strings.Fields(path))
	if err != nil || command == prCmd || !strings.HasSuffix(command.Use, " <pr-id>") || command.RunE == nil {
		panic(fmt.Sprintf("pr %s is not a command that takes <pr-id> alone", path))
	}

	command.Use = strings.TrimSuffix(command.Use, "<pr-id>") + "[pr-id]"
	command.Args = cobra.MaximumNArgs(1)
	run := command.RunE
	command.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			id, err := currentBranchPullRequest(cmd, deps, *repositorySelector)
			if err != nil {
				return err
			}
			args = []string{id}
		}

		return run(cmd, args)
	}
}

// currentBranchPullRequest is the open pull request whose source is the
// checked-out branch, of the repository the checkout was inferred to be.
//
// Only a repository taken from this checkout: with --repo naming another, the
// branch underfoot says nothing about it, as pr create holds for its source
// branch. None, or more than one, is refused, naming what was found.
func currentBranchPullRequest(cmd *cobra.Command, deps Dependencies, repositorySelector string) (string, error) {
	called := calledPath(cmd)
	if deps.RepositoryWasInferred == nil || !deps.RepositoryWasInferred() {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf(
			"%s takes [pr-id]; without one it reads the pull request of the checked-out branch, and the repository was not taken from a checkout. Pass the pull request's number", called), nil)
	}

	branch, err := currentGitBranch(cmd.Context(), deps)
	if err != nil || branch == "" {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf(
			"%s takes [pr-id]; without one it reads the pull request of the checked-out branch, and no branch is checked out. Pass the pull request's number", called), nil)
	}

	cfg, err := deps.LoadConfig()
	if err != nil {
		return "", err
	}
	projectKey, slug, err := reposel.Resolve(repositorySelector, cfg)
	if err != nil {
		return "", err
	}

	// The pull requests from this repository's branch, wherever they go: one
	// from a fork's branch of the same name is not this checkout's, and one
	// from a fork's checkout goes into its upstream.
	open, err := pullrequestservice.NewService(httpclient.NewFromConfig(cfg)).List(cmd.Context(),
		pullrequestservice.RepositoryRef{ProjectKey: projectKey, Slug: slug},
		pullrequestservice.ListOptions{State: "open", MaxResults: openPullRequestsShown, SourceBranch: branch, Outgoing: true})
	if err != nil {
		return "", err
	}
	name := func(pullRequest pullrequestservice.PullRequest) string {
		if target := pullRequest.Repository; target != nil && !(strings.EqualFold(target.ProjectKey, projectKey) && strings.EqualFold(target.Slug, slug)) {
			return fmt.Sprintf("%s/%s#%d", target.ProjectKey, target.Slug, pullRequest.ID)
		}
		return fmt.Sprintf("#%d", pullRequest.ID)
	}

	switch len(open) {
	case 0:
		return "", apperrors.New(apperrors.KindNotFound, fmt.Sprintf(
			"no open pull request comes from the checked-out branch %s in %s/%s. Pass the pull request's number", branch, projectKey, slug), nil)
	case 1:
		if !deps.JSONEnabled() {
			fmt.Fprintf(cmd.ErrOrStderr(), "Using pull request %s (the open pull request from the checked-out branch, %s)\n", name(open[0]), branch)
		}
		// A pull request into another repository is named by its address,
		// which says the repository the number belongs to.
		if name(open[0]) != fmt.Sprintf("#%d", open[0].ID) {
			if open[0].URL == "" {
				return "", apperrors.New(apperrors.KindNotFound, fmt.Sprintf(
					"the open pull request from the checked-out branch %s is %s, and Bitbucket gave no address for it. Pass its address", branch, name(open[0])), nil)
			}
			return open[0].URL, nil
		}

		return strconv.FormatInt(open[0].ID, 10), nil
	}

	named := make([]string, 0, len(open))
	for _, pullRequest := range open {
		named = append(named, fmt.Sprintf("%s into %s (%s)", name(pullRequest), pullRequest.TargetBranch, pullRequest.Title))
	}
	many := strconv.Itoa(len(open))
	if len(open) == openPullRequestsShown {
		many = "at least " + many
	}

	return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf(
		"%s open pull requests come from the checked-out branch %s in %s/%s: %s. Pass the number of the one you mean",
		many, branch, projectKey, slug, strings.Join(named, ", ")), nil)
}

// calledPath is the command as the person typed it, alias and all.
func calledPath(cmd *cobra.Command) string {
	path := cmd.CommandPath()
	if called := cmd.CalledAs(); called != "" && called != cmd.Name() {
		path = strings.TrimSuffix(path, cmd.Name()) + called
	}

	return path
}
