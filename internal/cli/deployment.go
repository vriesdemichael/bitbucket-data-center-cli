package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

func newDeploymentCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	depCmd := &cobra.Command{
		Use:   "deployment",
		Short: "Manage repository-scoped deployments for commits",
	}

	depCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	var seqNum int64
	var description string
	var displayName string
	var key string
	var state string
	var url string
	var envKey string
	var envName string
	var envType string
	var envUrl string

	createCmd := &cobra.Command{
		Use:   "create <commit>",
		Short: "Create or update a repository-scoped deployment for a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := loadQualityRepoServiceAndClient(repositorySelector)
			if err != nil {
				return err
			}

			request := openapigenerated.RestDeploymentSetRequest{
				DeploymentSequenceNumber: seqNum,
				Description:              description,
				DisplayName:              displayName,
				Key:                      key,
				State:                    openapigenerated.RestDeploymentSetRequestState(state),
				Url:                      url,
				Environment: openapigenerated.RestDeploymentEnvironment{
					DisplayName: &envName,
					Key:         &envKey,
				},
			}
			if envType != "" {
				request.Environment.Type = &envType
			}
			if envUrl != "" {
				request.Environment.Url = &envUrl
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				gotDep, err := service.GetDeployment(cmd.Context(), repo, args[0], openapigenerated.Get1Params{
					Key: &key,
				})
				predicted := "create"
				reason := "deployment status will be created"
				if err == nil && gotDep.Key != nil {
					predicted = "update"
					reason = "deployment status will be updated"
				} else if err != nil && apperrors.ExitCode(err) != 4 {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "deployment.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": key, "state": state},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"deployment get"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "create" {
					preview.Summary.CreateCount = 1
				} else {
					preview.Summary.UpdateCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			created, err := service.CreateOrUpdateDeployment(cmd.Context(), repo, args[0], request)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), created)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deployment %s (%s) set on %s/%s at %s\n", key, displayName, repo.ProjectKey, repo.Slug, args[0])
			return nil
		},
	}
	createCmd.Flags().Int64Var(&seqNum, "deployment-sequence-number", 0, "Sequence number of the deployment")
	createCmd.Flags().StringVar(&description, "description", "", "Description of the deployment")
	createCmd.Flags().StringVar(&displayName, "display-name", "", "Display name of the deployment")
	createCmd.Flags().StringVar(&key, "key", "", "Deployment key")
	createCmd.Flags().StringVar(&state, "state", "", "Deployment state (SUCCESSFUL, FAILED, IN_PROGRESS, PENDING, CANCELLED, ROLLED_BACK, UNKNOWN)")
	createCmd.Flags().StringVar(&url, "url", "", "Deployment URL")
	createCmd.Flags().StringVar(&envKey, "env-key", "", "Environment key")
	createCmd.Flags().StringVar(&envName, "env-name", "", "Environment display name")
	createCmd.Flags().StringVar(&envType, "env-type", "", "Environment type (DEVELOPMENT, TESTING, STAGING, PRODUCTION)")
	createCmd.Flags().StringVar(&envUrl, "env-url", "", "Environment URL")

	_ = createCmd.MarkFlagRequired("deployment-sequence-number")
	_ = createCmd.MarkFlagRequired("display-name")
	_ = createCmd.MarkFlagRequired("key")
	_ = createCmd.MarkFlagRequired("state")
	_ = createCmd.MarkFlagRequired("url")
	_ = createCmd.MarkFlagRequired("env-key")
	_ = createCmd.MarkFlagRequired("env-name")

	depCmd.AddCommand(createCmd)

	var getSeqNum string
	var getKey string
	var getEnvKey string

	getCmd := &cobra.Command{
		Use:   "get <commit>",
		Short: "Get repository-scoped deployment details for a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, err := loadQualityRepoAndService(repositorySelector)
			if err != nil {
				return err
			}

			params := openapigenerated.Get1Params{}
			if getSeqNum != "" {
				params.DeploymentSequenceNumber = &getSeqNum
			}
			if getKey != "" {
				params.Key = &getKey
			}
			if getEnvKey != "" {
				params.EnvironmentKey = &getEnvKey
			}

			dep, err := service.GetDeployment(cmd.Context(), repo, args[0], params)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), dep)
			}

			stateStr := "UNKNOWN"
			if dep.State != nil {
				stateStr = string(*dep.State)
			}

			displayNameStr := ""
			if dep.DisplayName != nil {
				displayNameStr = *dep.DisplayName
			}

			urlStr := ""
			if dep.Url != nil {
				urlStr = *dep.Url
			}

			rows := [][]string{
				{
					style.Resource.Render(safeString(dep.Key)),
					displayNameStr,
					style.ActionStyle(stateStr).Render(stateStr),
					style.Secondary.Render(urlStr),
				},
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	getCmd.Flags().StringVar(&getSeqNum, "deployment-sequence-number", "", "Filter by deployment sequence number")
	getCmd.Flags().StringVar(&getKey, "key", "", "Filter by deployment key")
	getCmd.Flags().StringVar(&getEnvKey, "env-key", "", "Filter by environment key")
	depCmd.AddCommand(getCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <commit>",
		Short: "Delete repository-scoped deployment for a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := loadQualityRepoServiceAndClient(repositorySelector)
			if err != nil {
				return err
			}

			params := openapigenerated.Delete1Params{}
			if getSeqNum != "" {
				params.DeploymentSequenceNumber = &getSeqNum
			}
			if getKey != "" {
				params.Key = &getKey
			}
			if getEnvKey != "" {
				params.EnvironmentKey = &getEnvKey
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				getParams := openapigenerated.Get1Params{}
				if getSeqNum != "" {
					getParams.DeploymentSequenceNumber = &getSeqNum
				}
				if getKey != "" {
					getParams.Key = &getKey
				}
				if getEnvKey != "" {
					getParams.EnvironmentKey = &getEnvKey
				}
				_, err = service.GetDeployment(cmd.Context(), repo, args[0], getParams)
				predicted := "delete"
				reason := "deployment will be deleted"
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						predicted = "no-op"
						reason = "deployment was not found"
					} else {
						return err
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "deployment.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": getKey, "env_key": getEnvKey, "sequence": getSeqNum},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"deployment get"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "delete" {
					preview.Summary.DeleteCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			err = service.DeleteDeployment(cmd.Context(), repo, args[0], params)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": getKey})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted deployment on %s/%s at %s\n", repo.ProjectKey, repo.Slug, args[0])
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&getSeqNum, "deployment-sequence-number", "", "Identify by deployment sequence number")
	deleteCmd.Flags().StringVar(&getKey, "key", "", "Identify by deployment key")
	deleteCmd.Flags().StringVar(&getEnvKey, "env-key", "", "Identify by environment key")
	depCmd.AddCommand(deleteCmd)

	return depCmd
}
