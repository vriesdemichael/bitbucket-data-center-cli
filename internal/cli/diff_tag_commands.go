package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	diffservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/diff"
	tagservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/tag"
)

func newDiffCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	diffCmd := &cobra.Command{
		Use:   "diff",
		Short: "Diff and patch commands",
	}

	diffCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	var refsPath string
	var refsPatch bool
	var refsStat bool
	var refsNameOnly bool

	refsCmd := &cobra.Command{
		Use:   "refs <from> <to>",
		Short: "Diff two refs or commits",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := diffservice.NewService(client)
			outputMode, err := resolveDiffOutputMode(refsPatch, refsStat, refsNameOnly)
			if err != nil {
				return err
			}

			result, err := service.DiffRefs(cmd.Context(), diffservice.DiffRefsInput{
				Repository: repo,
				From:       args[0],
				To:         args[1],
				Path:       refsPath,
				Output:     outputMode,
			})
			if err != nil {
				return err
			}

			return writeDiffResult(cmd.OutOrStdout(), options.JSON, outputMode, result)
		},
	}
	refsCmd.Flags().StringVar(&refsPath, "path", "", "Optional file path for file-scoped diff")
	refsCmd.Flags().BoolVar(&refsPatch, "patch", false, "Output unified patch stream")
	refsCmd.Flags().BoolVar(&refsStat, "stat", false, "Output structured diff stats")
	refsCmd.Flags().BoolVar(&refsNameOnly, "name-only", false, "Output only changed file names")
	diffCmd.AddCommand(refsCmd)

	var prPatch bool
	var prStat bool
	var prNameOnly bool

	prCmd := &cobra.Command{
		Use:   "pr <id>",
		Short: "Diff a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := diffservice.NewService(client)
			outputMode, err := resolveDiffOutputMode(prPatch, prStat, prNameOnly)
			if err != nil {
				return err
			}

			result, err := service.DiffPR(cmd.Context(), diffservice.DiffPRInput{
				Repository:    repo,
				PullRequestID: args[0],
				Output:        outputMode,
			})
			if err != nil {
				return err
			}

			return writeDiffResult(cmd.OutOrStdout(), options.JSON, outputMode, result)
		},
	}
	prCmd.Flags().BoolVar(&prPatch, "patch", false, "Output unified patch stream")
	prCmd.Flags().BoolVar(&prStat, "stat", false, "Output structured diff stats")
	prCmd.Flags().BoolVar(&prNameOnly, "name-only", false, "Output only changed file names")
	diffCmd.AddCommand(prCmd)

	var commitPath string

	commitCmd := &cobra.Command{
		Use:   "commit <sha>",
		Short: "Diff a commit against its parent",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := diffservice.NewService(client)
			result, err := service.DiffCommit(cmd.Context(), diffservice.DiffCommitInput{
				Repository: repo,
				CommitID:   args[0],
				Path:       commitPath,
			})
			if err != nil {
				return err
			}

			return writeDiffResult(cmd.OutOrStdout(), options.JSON, diffservice.OutputKindRaw, result)
		},
	}
	commitCmd.Flags().StringVar(&commitPath, "path", "", "Optional file path for file-scoped diff")
	diffCmd.AddCommand(commitCmd)

	return diffCmd
}

func newTagCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var limit int
	var start int
	var orderBy string
	var filterText string

	tagCmd := &cobra.Command{
		Use:   "tag",
		Short: "Repository tag lifecycle commands",
	}

	tagCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	tagCmd.PersistentFlags().IntVar(&limit, "limit", 25, "Page size for list operations")
	tagCmd.PersistentFlags().IntVar(&start, "start", 0, "Start offset for list operations")
	tagCmd.PersistentFlags().StringVar(&orderBy, "order-by", "", "Tag ordering: ALPHABETICAL or MODIFICATION")
	tagCmd.PersistentFlags().StringVar(&filterText, "filter", "", "Filter text for tag names")

	tagCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List repository tags",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveTagRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := tagservice.NewService(client)
			tags, err := service.List(cmd.Context(), repo, tagservice.ListOptions{Limit: limit, Start: start, OrderBy: orderBy, FilterText: filterText})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), tags)
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
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveTagRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := tagservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
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

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "tag.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0], "start_point": startPoint, "message": message},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"tag list (filtered by name)"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"tag already exists"}
							}
							return nil
						}(),
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "create" {
					preview.Summary.CreateCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			createdTag, err := service.Create(cmd.Context(), repo, args[0], startPoint, message)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), createdTag)
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
			cfg, client, err := loadConfigAndClient()
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

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), tag)
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
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveTagRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := tagservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
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

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "tag.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"tag get"},
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

			if err := service.Delete(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "tag": args[0]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted tag %s\n", args[0])
			return nil
		},
	})

	return tagCmd
}
