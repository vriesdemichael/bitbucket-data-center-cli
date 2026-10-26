package prcmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

func newPullRequestAutoMergeCommand(deps Dependencies, repository *string) *cobra.Command {
	autoMergeCmd := &cobra.Command{
		Use:   "auto-merge",
		Short: "Turn a pull request's auto-merge on or off, or read it",
	}

	autoMergeGetCmd := &cobra.Command{
		Use:   "get <pr-id>",
		Short: "Get auto-merge configuration for a pull request",
		Example: `  # Whether a pull request merges on its own, and how
  bb pr auto-merge get 42 --repo PROJ/repo`,
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

			autoMerge, err := service.GetAutoMerge(cmd.Context(), repo, target.PullRequestID)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), AutoMergeState{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, AutoMerge: autoMergeFrom(autoMerge)})
			}

			if !autoMerge.Enabled {
				fmt.Fprintln(cmd.OutOrStdout(), "Auto-merge: disabled")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Auto-merge: enabled (strategy=%s)\n", autoMerge.StrategyID)
			return nil
		},
	}
	autoMergeCmd.AddCommand(autoMergeGetCmd)

	var autoMergeStrategy string
	autoMergeEnableCmd := &cobra.Command{
		Use:   "enable <pr-id>",
		Short: "Enable auto-merge on a pull request",
		Example: "  # Enable auto-merge with the default strategy (no-ff)\n" +
			"  bb pr auto-merge enable 42 --repo PROJ/repo\n\n" +
			"  # Enable auto-merge with a specific strategy\n" +
			"  bb pr auto-merge enable 42 --repo PROJ/repo --strategy rebase-ff-only",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				current, err := service.GetAutoMerge(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "auto-merge will be enabled"
				if current.Enabled && strings.EqualFold(strings.TrimSpace(current.StrategyID), strings.TrimSpace(autoMergeStrategy)) {
					predicted = "no-op"
					reason = "auto-merge is already enabled with the same strategy"
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.auto-merge.enable",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID, "strategy": autoMergeStrategy},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			autoMerge, err := service.EnableAutoMerge(cmd.Context(), repo, target.PullRequestID, autoMergeStrategy, nil)
			if err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), AutoMergeState{Repository: repositoryOf(repo), PullRequestID: target.PullRequestID, AutoMerge: autoMergeFrom(autoMerge)})
			}

			if autoMerge.MergedImmediately {
				fmt.Fprintf(cmd.OutOrStdout(), "Merged pull request #%s immediately (strategy=%s): its checks already passed, so there was nothing to wait for\n", target.PullRequestID, autoMerge.StrategyID)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Enabled auto-merge on pull request #%s (strategy=%s)\n", target.PullRequestID, autoMerge.StrategyID)
			return nil
		},
	}
	enumflag.Register(autoMergeEnableCmd.Flags(), &autoMergeStrategy, "strategy", "no-ff", openapi.MergeStrategies, "Merge strategy")
	autoMergeCmd.AddCommand(autoMergeEnableCmd)

	autoMergeDisableCmd := &cobra.Command{
		Use:   "disable <pr-id>",
		Short: "Disable auto-merge on a pull request",
		Example: `  # Stop a pull request from merging on its own
  bb pr auto-merge disable 42 --repo PROJ/repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := deps.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			target, err := prsel.Resolve(cmd.Context(), args[0], *repository, cfg, service)
			if err != nil {
				return err
			}
			repo := target.RepositoryRef()

			if deps.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), deps.PermissionChecker, apiClient, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				current, err := service.GetAutoMerge(cmd.Context(), repo, target.PullRequestID)
				if err != nil {
					return err
				}

				predicted := "delete"
				reason := "auto-merge will be disabled"
				if !current.Enabled {
					predicted = "no-op"
					reason = "auto-merge is not enabled"
				}

				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "pr.auto-merge.disable",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": target.PullRequestID},
					Action:          "delete",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), deps.JSONEnabled(), preview)
			}

			if err := service.DisableAutoMerge(cmd.Context(), repo, target.PullRequestID); err != nil {
				return err
			}

			if deps.JSONEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), AutoMergeCancellation{Status: result.OK(), Repository: repositoryOf(repo), PullRequestID: target.PullRequestID})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Disabled auto-merge on pull request #%s\n", target.PullRequestID)
			return nil
		},
	}
	autoMergeCmd.AddCommand(autoMergeDisableCmd)
	return autoMergeCmd
}
