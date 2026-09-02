package deploymentcmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
)

type PermissionChecker interface {
	CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error
}

type Dependencies struct {
	JSONEnabled         func() bool
	DryRunEnabled       func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	PermissionChecker   func(*openapigenerated.ClientWithResponses) PermissionChecker
}

func (deps *Dependencies) withDefaults() Dependencies {
	d := *deps
	if d.JSONEnabled == nil {
		d.JSONEnabled = func() bool { return false }
	}
	if d.DryRunEnabled == nil {
		d.DryRunEnabled = func() bool { return false }
	}
	if d.LoadConfig == nil {
		d.LoadConfig = func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		}
	}
	if d.LoadConfigAndClient == nil {
		d.LoadConfigAndClient = func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := d.LoadConfig()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			return cfg, client, nil
		}
	}
	if d.WriteJSON == nil {
		d.WriteJSON = jsonoutput.Write
	}
	return d
}

func resolveQualityRepoServiceAndClient(selector string, deps Dependencies) (qualityservice.RepositoryRef, *qualityservice.Service, *openapigenerated.ClientWithResponses, error) {
	cfg, client, err := deps.LoadConfigAndClient()
	if err != nil {
		return qualityservice.RepositoryRef{}, nil, nil, err
	}
	projectKey, slug, err := reposel.Resolve(selector, cfg)
	if err != nil {
		return qualityservice.RepositoryRef{}, nil, nil, err
	}
	service := qualityservice.NewService(client)
	return qualityservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}, service, client, nil
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

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
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
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

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
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

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "deployment.create",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": key, "state": state},
					Action:          "create",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"deployment get"},
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			created, err := service.CreateOrUpdateDeployment(cmd.Context(), repo, args[0], request)
			if err != nil {
				return err
			}

			reported := deploymentFrom(created)

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), reported)
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
			repo, service, _, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
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

			reported := deploymentFrom(dep)

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), reported)
			}

			stateStr := reported.State
			if stateStr == "" {
				stateStr = "UNKNOWN"
			}
			displayNameStr := reported.DisplayName
			urlStr := reported.URL

			rows := [][]string{
				{
					style.Resource.Render(reported.Key),
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
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
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

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
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

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "deployment.delete",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": getKey, "envKey": getEnvKey, "sequence": getSeqNum},
					Action:          "delete",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"deployment get"},
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			err = service.DeleteDeployment(cmd.Context(), repo, args[0], params)
			if err != nil {
				return err
			}

			reported := Deletion{
				Status:     result.OK(),
				Repository: result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				Commit:     args[0],
				Key:        getKey,
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), reported)
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
