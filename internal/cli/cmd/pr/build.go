package prcmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestBuildCommands(deps Dependencies, repository *string) []*cobra.Command {
	// bb pr build status and bb pr checks are the same command registered twice:
	// once on the canonical path that names its subject, and once under the gh
	// spelling a reader arrives with (ADR-050). Built from a constructor rather
	// than by adding one *cobra.Command to two parents, so each registration
	// gets its own paging flags and resolves --repo from the tree it sits in.
	newBuildStatusCmd := func(use string, short string, path string) *cobra.Command {
		var statusPaging paging.Options
		cmd := &cobra.Command{
			Use:   use,
			Short: short,
			Example: "  # The builds of a pull request's latest commit\n" +
				"  " + path + " 42 --repo PROJ/repo\n\n" +
				"  # Merge only when none failed and none is still running\n" +
				"  " + path + " 42 --repo PROJ/repo && bb pr merge 42 --repo PROJ/repo",
			Long: short + `.

Without --json, the exit status reports the builds, as gh pr checks does: 1
when one failed, 8 when none failed and one is still in progress or has no
result, and 0 otherwise, a cancelled build included. It counts every build,
not only the --limit shown. With --json the exit status is 0 and each build's
state is in the output.`,
			Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cfg, err := deps.LoadConfig()
				if err != nil {
					return err
				}

				service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
				target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
				if err != nil {
					return err
				}
				repo := target.RepositoryRef()

				// Text output ends in an exit status that reports the builds
				// (ADR-091), so it reads all of them: a failed build past --limit
				// would otherwise pass for green. --limit still decides what is
				// shown.
				limit := statusPaging.ServiceLimit()
				if !deps.JSONEnabled() {
					limit = pullrequestservice.AllResults
				}
				statuses, err := service.GetBuildStatuses(cmd.Context(), repo, target.PullRequestID, limit)
				if err != nil {
					return err
				}

				// The service already stops at the cap (ADR-074); this keeps --limit
				// honest if one ever does not. A no-op under --all.
				shown := paging.Truncate(statusPaging, statuses)

				if deps.JSONEnabled() {
					return deps.WriteJSONList(cmd.OutOrStdout(), BuildStatuses{
						Repository:    repositoryOf(repo),
						PullRequestID: target.PullRequestID,
						Statuses:      buildStatusesFrom(shown),
					}, paging.LimitReached(statusPaging, len(shown)))
				}

				if len(shown) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No build statuses found")
					return nil
				}

				for _, s := range shown {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", s.Key, s.State, s.URL)
				}
				paging.Hint(cmd.ErrOrStderr(), statusPaging, len(statuses))

				return buildsExitStatus(statuses)
			},
		}
		statusPaging.Register(cmd, 25)

		return cmd
	}

	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Show the builds of a pull request",
	}
	// Each half names the other (ADR-050): a reader who found one has no way to
	// learn the other exists, and `bb pr checks` is the spelling a gh user
	// reaches for.
	buildCmd.AddCommand(newBuildStatusCmd("status <pr-id>", "Show build statuses for a pull request's source commit (also available as bb pr checks)", "bb pr build status"))
	return []*cobra.Command{buildCmd, newBuildStatusCmd("checks <pr-id>", "Show build statuses for a pull request's source commit (alias for bb pr build status)", "bb pr checks")}
}
