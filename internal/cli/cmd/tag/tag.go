package tagcmd

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	tagservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/tag"
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
	WriteJSONList       func(io.Writer, any, bool) error
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
	if d.WriteJSONList == nil {
		d.WriteJSONList = jsonoutput.WriteList
	}
	return d
}

func resolveTagRepositoryReference(selector string, cfg config.AppConfig) (tagservice.RepositoryRef, error) {
	projectKey, slug, err := reposel.Resolve(selector, cfg)
	if err != nil {
		return tagservice.RepositoryRef{}, err
	}
	return tagservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}, nil
}

func safeString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func safeStringFromTagType(tagType *openapigenerated.RestTagType) string {
	if tagType == nil {
		return ""
	}
	return string(*tagType)
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	var repositorySelector string
	var listPaging paging.Options
	var start int
	var orderBy string
	var filterText string

	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Repository tag lifecycle commands",
	}

	tagCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	listPaging.RegisterPersistent(tagCmd, 25)
	tagCmd.PersistentFlags().IntVar(&start, "start", 0, "Start offset for list operations")
	tagCmd.PersistentFlags().StringVar(&orderBy, "order-by", "", "Tag ordering: ALPHABETICAL or MODIFICATION")
	tagCmd.PersistentFlags().StringVar(&filterText, "filter", "", "Filter text for tag names")

	tagCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List repository tags",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveTagRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := tagservice.NewService(client)
			tags, err := service.List(cmd.Context(), repo, tagservice.ListOptions{Limit: listPaging.ServiceLimit(), Start: start, OrderBy: orderBy, FilterText: filterText})
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSONList(cmd.OutOrStdout(), tags, paging.LimitReached(listPaging, len(tags)))
			}

			if len(tags) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tags found")
				return nil
			}

			for _, tag := range tags {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", safeString(tag.DisplayId), safeStringFromTagType(tag.Type), safeString(tag.LatestCommit))
			}

			return nil
		},
	})

	var startPoint string
	var message string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create repository tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveTagRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := tagservice.NewService(client)
			if d.DryRunEnabled() {
				if d.PermissionChecker != nil {
					checker := d.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
							return err
						}
					}
				}

				tags, err := service.List(cmd.Context(), repo, tagservice.ListOptions{Limit: 200, FilterText: args[0]})
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "tag will be created"
				for _, tag := range tags {
					if strings.EqualFold(strings.TrimSpace(safeString(tag.DisplayId)), strings.TrimSpace(args[0])) || strings.EqualFold(strings.TrimSpace(safeString(tag.Id)), strings.TrimSpace(args[0])) {
						predicted = "conflict"
						reason = "tag already exists"
						break
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "tag.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0], "start_point": startPoint, "message": message},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"tag list (filtered by name)"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"tag already exists"}
							}
							return nil
						}(),
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "create" {
					preview.Summary.CreateCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			createdTag, err := service.Create(cmd.Context(), repo, args[0], startPoint, message)
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), createdTag)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created tag %s (%s)\n", safeString(createdTag.DisplayId), safeString(createdTag.LatestCommit))
			return nil
		},
	}
	createCmd.Flags().StringVar(&startPoint, "start-point", "", "Commit ID or ref to tag")
	createCmd.Flags().StringVar(&message, "message", "", "Optional annotated tag message")
	_ = createCmd.MarkFlagRequired("start-point")
	tagCmd.AddCommand(createCmd)

	tagCmd.AddCommand(&cobra.Command{
		Use:   "view <name>",
		Short: "View repository tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveTagRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := tagservice.NewService(client)
			tag, err := service.Get(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), tag)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Tag: %s\n", safeString(tag.DisplayId))
			fmt.Fprintf(cmd.OutOrStdout(), "Type: %s\n", safeStringFromTagType(tag.Type))
			fmt.Fprintf(cmd.OutOrStdout(), "Commit: %s\n", safeString(tag.LatestCommit))
			return nil
		},
	})

	tagCmd.AddCommand(&cobra.Command{
		Use:   "delete <name>",
		Short: "Delete repository tag",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveTagRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := tagservice.NewService(client)
			if d.DryRunEnabled() {
				if d.PermissionChecker != nil {
					checker := d.PermissionChecker(client)
					if checker != nil {
						if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
							return err
						}
					}
				}

				_, err := service.Get(cmd.Context(), repo, args[0])
				predicted := "delete"
				reason := "tag will be deleted"
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						predicted = "no-op"
						reason = "tag was not found"
					} else {
						return err
					}
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "tag.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"tag get"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "delete" {
					preview.Summary.DeleteCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.Delete(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "tag": args[0]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted tag %s\n", args[0])
			return nil
		},
	})

	return tagCmd
}
