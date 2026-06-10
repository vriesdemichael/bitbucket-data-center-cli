package cli

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	branchservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/branch"
	qualityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/quality"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
)

func newBranchCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var limit int

	branchCmd := &cobra.Command{
		Use:   "branch",
		Short: "Repository branch and branch restriction commands",
	}

	branchCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	branchCmd.PersistentFlags().IntVar(&limit, "limit", 25, "Page size for list operations")

	var orderBy string
	var filterText string
	var base string
	var details bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List repository branches",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			var detailsFilter *bool
			if cmd.Flags().Changed("details") {
				detailsFilter = &details
			}

			service := branchservice.NewService(client)
			branches, err := service.List(cmd.Context(), repo, branchservice.ListOptions{
				Limit:      limit,
				OrderBy:    orderBy,
				FilterText: filterText,
				Base:       base,
				Details:    detailsFilter,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"repository": repo,
					"branches":   branches,
				})
			}

			if len(branches) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No branches found"))
				return nil
			}

			rows := make([][]string, len(branches))
			for i, branch := range branches {
				rows[i] = []string{
					style.Resource.Render(safeString(branch.DisplayId)),
					style.Secondary.Render(safeString(branch.Id)),
					style.Secondary.Render(safeString(branch.LatestCommit)),
					fmt.Sprintf("default=%t", branch.Default != nil && *branch.Default),
				}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	listCmd.Flags().StringVar(&orderBy, "order-by", "", "Branch ordering: ALPHABETICAL or MODIFICATION")
	listCmd.Flags().StringVar(&filterText, "filter", "", "Filter text for branch names")
	listCmd.Flags().StringVar(&base, "base", "", "Base ref filter")
	listCmd.Flags().BoolVar(&details, "details", false, "Include branch details from Bitbucket")
	branchCmd.AddCommand(listCmd)

	var createStartPoint string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create repository branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				branches, err := service.List(cmd.Context(), repo, branchservice.ListOptions{Limit: 1000, FilterText: args[0]})
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "branch will be created"
				normalizedRequested := normalizeBranchName(args[0])
				for _, branch := range branches {
					if strings.EqualFold(strings.TrimSpace(safeString(branch.DisplayId)), strings.TrimSpace(args[0])) ||
						strings.EqualFold(strings.TrimSpace(safeString(branch.Id)), normalizedRequested) {
						predicted = "conflict"
						reason = "branch already exists"
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "branch.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0], "start_point": createStartPoint},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"branch list (filtered by name)"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"branch already exists"}
							}
							return nil
						}(),
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "create":
					preview.Summary.CreateCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			created, err := service.Create(cmd.Context(), repo, args[0], createStartPoint)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "branch": created})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created branch"), style.Resource.Render(safeString(created.DisplayId)))
			return nil
		},
	}
	createCmd.Flags().StringVar(&createStartPoint, "start-point", "", "Commit ID or ref to branch from")
	_ = createCmd.MarkFlagRequired("start-point")
	branchCmd.AddCommand(createCmd)

	var deleteEndPoint string
	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete repository branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if err := service.Delete(cmd.Context(), repo, args[0], deleteEndPoint, options.DryRun); err != nil {
				return err
			}

			if options.JSON {
				if options.DryRun {
					reason := "validated through Bitbucket branch delete dry-run endpoint"
					if strings.TrimSpace(deleteEndPoint) != "" {
						reason = "validated through Bitbucket branch delete dry-run endpoint with end-point precondition"
					}
					return writeJSON(cmd.OutOrStdout(), dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent: "branch.delete",
							Target: map[string]any{
								"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
								"branch":     args[0],
								"end_point":  strings.TrimSpace(deleteEndPoint),
							},
							Action:          "delete",
							PredictedAction: "delete",
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   []string{"branch delete preflight validation"},
						}},
						Summary: dryRunSummary{Total: 1, Supported: 1, DeleteCount: 1},
					})
				}

				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "repository": repo, "branch": args[0]})
			}

			if options.DryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Dry-run delete completed for %s\n", args[0])
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted branch"), style.Resource.Render(args[0]))
			return nil
		},
	}
	deleteCmd.Flags().StringVar(&deleteEndPoint, "end-point", "", "Expected commit at branch tip")
	branchCmd.AddCommand(deleteCmd)

	defaultCmd := &cobra.Command{Use: "default", Short: "Get or set repository default branch"}

	defaultGetCmd := &cobra.Command{
		Use:   "get",
		Short: "Get repository default branch",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			defaultBranch, err := service.GetDefault(cmd.Context(), repo)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "default_branch": defaultBranch})
			}

			style.WriteTable(cmd.OutOrStdout(), [][]string{{style.Resource.Render(safeString(defaultBranch.DisplayId)), style.Secondary.Render(safeString(defaultBranch.Id))}})
			return nil
		},
	}
	defaultCmd.AddCommand(defaultGetCmd)

	defaultSetCmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Set repository default branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				currentDefault, err := service.GetDefault(cmd.Context(), repo)
				if err != nil {
					return err
				}
				predicted := "update"
				reason := "default branch will be updated"
				currentDefaultID := strings.TrimSpace(safeString(currentDefault.DisplayId))
				if currentDefaultID == "" {
					currentDefaultID = strings.TrimPrefix(strings.TrimSpace(safeString(currentDefault.Id)), "refs/heads/")
				}
				if strings.EqualFold(currentDefaultID, strings.TrimSpace(args[0])) {
					predicted = "no-op"
					reason = "default branch already set to requested value"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "branch.default.set",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "default_branch": args[0]},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"default branch"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.SetDefault(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "repository": repo, "default_branch": args[0]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Default branch set to"), style.Resource.Render(args[0]))
			return nil
		},
	}
	defaultCmd.AddCommand(defaultSetCmd)
	branchCmd.AddCommand(defaultCmd)

	modelCmd := &cobra.Command{Use: "model", Short: "Inspect and update branch model-related settings"}

	modelInspectCmd := &cobra.Command{
		Use:   "inspect <commit>",
		Short: "Inspect branch refs that contain a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			refs, err := service.FindByCommit(cmd.Context(), repo, args[0], limit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "commit": args[0], "refs": refs})
			}

			if len(refs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No matching refs found"))
				return nil
			}

			rows := make([][]string, len(refs))
			for i, ref := range refs {
				rows[i] = []string{style.Resource.Render(safeString(ref.DisplayId)), style.Secondary.Render(safeString(ref.Id))}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	modelCmd.AddCommand(modelInspectCmd)

	modelUpdateCmd := &cobra.Command{
		Use:   "update <default-branch>",
		Short: "Update repository default branch used by branch model settings",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				currentDefault, err := service.GetDefault(cmd.Context(), repo)
				if err != nil {
					return err
				}
				predicted := "update"
				reason := "branch model default will be updated"
				currentDefaultID := strings.TrimSpace(safeString(currentDefault.DisplayId))
				if currentDefaultID == "" {
					currentDefaultID = strings.TrimPrefix(strings.TrimSpace(safeString(currentDefault.Id)), "refs/heads/")
				}
				if strings.EqualFold(currentDefaultID, strings.TrimSpace(args[0])) {
					predicted = "no-op"
					reason = "branch model default already set to requested value"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "branch.model.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "default_branch": args[0]},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"default branch"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.SetDefault(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "repository": repo, "default_branch": args[0]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Branch model default updated to"), style.Resource.Render(args[0]))
			return nil
		},
	}
	modelCmd.AddCommand(modelUpdateCmd)
	branchCmd.AddCommand(modelCmd)

	restrictionCmd := &cobra.Command{Use: "restriction", Short: "Manage repository branch restrictions"}

	var restrictionType string
	var matcherType string
	var matcherID string
	restrictionListCmd := &cobra.Command{
		Use:   "list",
		Short: "List branch restrictions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			restrictions, err := service.ListRestrictions(cmd.Context(), repo, branchservice.RestrictionListOptions{
				Limit:       limit,
				Type:        restrictionType,
				MatcherType: matcherType,
				MatcherID:   matcherID,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "restrictions": restrictions})
			}

			if len(restrictions) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No restrictions found"))
				return nil
			}

			rows := make([][]string, len(restrictions))
			for i, restriction := range restrictions {
				matcher := ""
				if restriction.Matcher != nil && restriction.Matcher.DisplayId != nil {
					matcher = *restriction.Matcher.DisplayId
				} else if restriction.Matcher != nil && restriction.Matcher.Id != nil {
					matcher = *restriction.Matcher.Id
				}

				rows[i] = []string{
					style.Secondary.Render(fmt.Sprintf("%d", safeInt32(restriction.Id))),
					safeString(restriction.Type),
					matcher,
					fmt.Sprintf("users=%d", len(safeUsers(restriction.Users))),
					fmt.Sprintf("groups=%d", len(safeStringSlice(restriction.Groups))),
				}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	}
	restrictionListCmd.Flags().StringVar(&restrictionType, "type", "", "Restriction type (read-only, no-deletes, fast-forward-only, pull-request-only, no-creates)")
	restrictionListCmd.Flags().StringVar(&matcherType, "matcher-type", "", "Matcher type (BRANCH, MODEL_BRANCH, MODEL_CATEGORY, PATTERN)")
	restrictionListCmd.Flags().StringVar(&matcherID, "matcher-id", "", "Matcher id value")
	restrictionCmd.AddCommand(restrictionListCmd)

	restrictionGetCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get branch restriction by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			restriction, err := service.GetRestriction(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "restriction": restriction})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", style.Secondary.Render(fmt.Sprintf("id=%d", safeInt32(restriction.Id))), safeString(restriction.Type))
			return nil
		},
	}
	restrictionCmd.AddCommand(restrictionGetCmd)

	var createRestrictionType string
	var createMatcherType string
	var createMatcherID string
	var createMatcherDisplay string
	var createUsers []string
	var createGroups []string
	var createAccessKeyIDs []int
	restrictionCreateCmd := &cobra.Command{
		Use:   "create",
		Short: "Create branch restriction",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			accessKeyIDs, err := normalizeAccessKeyIDs(createAccessKeyIDs)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				restrictions, err := service.ListRestrictions(cmd.Context(), repo, branchservice.RestrictionListOptions{Limit: 1000})
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "branch restriction will be created"
				for _, restriction := range restrictions {
					if matchesRestrictionSignature(restriction, createRestrictionType, createMatcherType, createMatcherID) {
						predicted = "conflict"
						reason = "matching branch restriction already exists"
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "branch.restriction.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "type": createRestrictionType, "matcher_type": createMatcherType, "matcher_id": createMatcherID},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"branch restrictions list"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"matching restriction exists"}
							}
							return nil
						}(),
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "create":
					preview.Summary.CreateCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			created, err := service.CreateRestriction(cmd.Context(), repo, branchservice.RestrictionUpsertInput{
				Type:           createRestrictionType,
				MatcherType:    createMatcherType,
				MatcherID:      createMatcherID,
				MatcherDisplay: createMatcherDisplay,
				Users:          createUsers,
				Groups:         createGroups,
				AccessKeyIDs:   accessKeyIDs,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "restriction": created})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created restriction"), style.Secondary.Render(fmt.Sprintf("%d", safeInt32(created.Id))))
			return nil
		},
	}
	restrictionCreateCmd.Flags().StringVar(&createRestrictionType, "type", "", "Restriction type")
	restrictionCreateCmd.Flags().StringVar(&createMatcherType, "matcher-type", "BRANCH", "Matcher type")
	restrictionCreateCmd.Flags().StringVar(&createMatcherID, "matcher-id", "", "Matcher id value")
	restrictionCreateCmd.Flags().StringVar(&createMatcherDisplay, "matcher-display", "", "Matcher display value")
	restrictionCreateCmd.Flags().StringSliceVar(&createUsers, "user", nil, "User slug allowed by restriction (repeatable)")
	restrictionCreateCmd.Flags().StringSliceVar(&createGroups, "group", nil, "Group name allowed by restriction (repeatable)")
	restrictionCreateCmd.Flags().IntSliceVar(&createAccessKeyIDs, "access-key-id", nil, "SSH access key id allowed by restriction (repeatable)")
	_ = restrictionCreateCmd.MarkFlagRequired("type")
	_ = restrictionCreateCmd.MarkFlagRequired("matcher-id")
	restrictionCmd.AddCommand(restrictionCreateCmd)

	var updateRestrictionType string
	var updateMatcherType string
	var updateMatcherID string
	var updateMatcherDisplay string
	var updateUsers []string
	var updateGroups []string
	var updateAccessKeyIDs []int
	restrictionUpdateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update branch restriction",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			accessKeyIDs, err := normalizeAccessKeyIDs(updateAccessKeyIDs)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				current, err := service.GetRestriction(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}
				predicted := "update"
				reason := "branch restriction will be updated"
				if matchesRestrictionUpdate(current, updateRestrictionType, updateMatcherType, updateMatcherID, updateUsers, updateGroups, accessKeyIDs) {
					predicted = "no-op"
					reason = "branch restriction already matches requested values"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "branch.restriction.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"branch restriction"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "update":
					preview.Summary.UpdateCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			updated, err := service.UpdateRestriction(cmd.Context(), repo, args[0], branchservice.RestrictionUpsertInput{
				Type:           updateRestrictionType,
				MatcherType:    updateMatcherType,
				MatcherID:      updateMatcherID,
				MatcherDisplay: updateMatcherDisplay,
				Users:          updateUsers,
				Groups:         updateGroups,
				AccessKeyIDs:   accessKeyIDs,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "restriction": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated restriction"), style.Secondary.Render(fmt.Sprintf("%d", safeInt32(updated.Id))))
			return nil
		},
	}
	restrictionUpdateCmd.Flags().StringVar(&updateRestrictionType, "type", "", "Restriction type")
	restrictionUpdateCmd.Flags().StringVar(&updateMatcherType, "matcher-type", "BRANCH", "Matcher type")
	restrictionUpdateCmd.Flags().StringVar(&updateMatcherID, "matcher-id", "", "Matcher id value")
	restrictionUpdateCmd.Flags().StringVar(&updateMatcherDisplay, "matcher-display", "", "Matcher display value")
	restrictionUpdateCmd.Flags().StringSliceVar(&updateUsers, "user", nil, "User slug allowed by restriction (repeatable)")
	restrictionUpdateCmd.Flags().StringSliceVar(&updateGroups, "group", nil, "Group name allowed by restriction (repeatable)")
	restrictionUpdateCmd.Flags().IntSliceVar(&updateAccessKeyIDs, "access-key-id", nil, "SSH access key id allowed by restriction (repeatable)")
	_ = restrictionUpdateCmd.MarkFlagRequired("type")
	_ = restrictionUpdateCmd.MarkFlagRequired("matcher-id")
	restrictionCmd.AddCommand(restrictionUpdateCmd)

	restrictionDeleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete branch restriction",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				_, err := service.GetRestriction(cmd.Context(), repo, args[0])
				predicted := "delete"
				reason := "branch restriction will be deleted"
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						predicted = "no-op"
						reason = "branch restriction was not found"
					} else {
						return err
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "branch.restriction.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"branch restriction"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				switch predicted {
				case "delete":
					preview.Summary.DeleteCount = 1
				case "no-op":
					preview.Summary.NoopCount = 1
				default:
					preview.Summary.UnknownCount = 1
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.DeleteRestriction(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "repository": repo, "restriction_id": args[0]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted restriction"), style.Resource.Render(args[0]))
			return nil
		},
	}
	restrictionCmd.AddCommand(restrictionDeleteCmd)

	branchCmd.AddCommand(restrictionCmd)

	return branchCmd
}

func newBuildCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string

	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Build status and required merge-check commands",
	}
	buildCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Build status commands by commit",
	}

	var setKey string
	var setState string
	var setURL string
	var setName string
	var setDescription string
	var setRef string
	var setParent string
	var setBuildNumber string
	var setDuration int64
	setCmd := &cobra.Command{
		Use:   "set <commit>",
		Short: "Set build status for a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := qualityservice.NewService(client)
			if options.DryRun {
				statuses, err := service.GetBuildStatuses(cmd.Context(), args[0], 200, "")
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "build status entry will be created"
				for _, status := range statuses {
					if strings.EqualFold(strings.TrimSpace(safeString(status.Key)), strings.TrimSpace(setKey)) {
						predicted = "update"
						reason = "build status entry with this key will be updated"
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "build.status.set",
						Target:          map[string]any{"commit": args[0], "key": setKey, "state": setState, "url": setURL},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"build statuses list"},
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

			if err := service.SetBuildStatus(cmd.Context(), args[0], qualityservice.BuildStatusSetInput{
				Key:         setKey,
				State:       setState,
				URL:         setURL,
				Name:        setName,
				Description: setDescription,
				Ref:         setRef,
				Parent:      setParent,
				BuildNumber: setBuildNumber,
				DurationMS:  setDuration,
			}); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "commit": args[0], "key": setKey})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Build status %s set on %s\n", setKey, args[0])
			return nil
		},
	}
	setCmd.Flags().StringVar(&setKey, "key", "", "Build status key")
	setCmd.Flags().StringVar(&setState, "state", "", "Build state (SUCCESSFUL, FAILED, INPROGRESS, UNKNOWN)")
	setCmd.Flags().StringVar(&setURL, "url", "", "Build URL")
	setCmd.Flags().StringVar(&setName, "name", "", "Build display name")
	setCmd.Flags().StringVar(&setDescription, "description", "", "Build description")
	setCmd.Flags().StringVar(&setRef, "ref", "", "Build ref")
	setCmd.Flags().StringVar(&setParent, "parent", "", "Build parent key")
	setCmd.Flags().StringVar(&setBuildNumber, "build-number", "", "Build number")
	setCmd.Flags().Int64Var(&setDuration, "duration-ms", 0, "Duration in milliseconds")
	_ = setCmd.MarkFlagRequired("key")
	_ = setCmd.MarkFlagRequired("state")
	_ = setCmd.MarkFlagRequired("url")
	statusCmd.AddCommand(setCmd)

	var getLimit int
	var getOrderBy string
	statusCmd.AddCommand(&cobra.Command{
		Use:   "get <commit>",
		Short: "Get build statuses for a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := qualityservice.NewService(client)
			statuses, err := service.GetBuildStatuses(cmd.Context(), args[0], getLimit, getOrderBy)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), statuses)
			}

			if len(statuses) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No build statuses found"))
				return nil
			}

			rows := make([][]string, len(statuses))
			for i, status := range statuses {
				state := safeStringFromBuildState(status.State)
				rows[i] = []string{style.Resource.Render(safeString(status.Key)), style.ActionStyle(state).Render(state), style.Secondary.Render(safeString(status.Url))}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	})
	statusCmd.PersistentFlags().IntVar(&getLimit, "limit", 25, "Page size for list operations")
	statusCmd.PersistentFlags().StringVar(&getOrderBy, "order-by", "", "Order by NEWEST, OLDEST, or STATUS")

	var includeUnique bool
	statusCmd.AddCommand(&cobra.Command{
		Use:   "stats <commit>...",
		Short: "Get build status summary counts for one or more commits",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := qualityservice.NewService(client)
			if len(args) == 1 {
				stats, err := service.GetBuildStatusStats(cmd.Context(), args[0], includeUnique)
				if err != nil {
					return err
				}

				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), stats)
				}

				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Successful:"), style.Success.Render(fmt.Sprintf("%d", safeInt32(stats.Successful))))
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Label.Render("Failed:"), style.Deleted.Render(fmt.Sprintf("%d", safeInt32(stats.Failed))))
				fmt.Fprintf(cmd.OutOrStdout(), "%s %d\n", style.Label.Render("In Progress:"), safeInt32(stats.InProgress))
				fmt.Fprintf(cmd.OutOrStdout(), "%s %d\n", style.Label.Render("Unknown:"), safeInt32(stats.Unknown))
				fmt.Fprintf(cmd.OutOrStdout(), "%s %d\n", style.Label.Render("Cancelled:"), safeInt32(stats.Cancelled))
				return nil
			}

			statsMap, err := service.GetMultipleBuildStatusStats(cmd.Context(), args)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), statsMap)
			}

			rows := make([][]string, 0, len(args)+1)
			header := []string{
				style.Label.Render("COMMIT"),
				style.Label.Render("SUCCESSFUL"),
				style.Label.Render("FAILED"),
				style.Label.Render("IN PROGRESS"),
				style.Label.Render("UNKNOWN"),
				style.Label.Render("CANCELLED"),
			}
			rows = append(rows, header)
			for _, commit := range args {
				s, ok := statsMap[commit]
				if !ok {
					rows = append(rows, []string{style.Resource.Render(commit), "0", "0", "0", "0", "0"})
					continue
				}
				rows = append(rows, []string{
					style.Resource.Render(commit),
					style.Success.Render(fmt.Sprintf("%d", safeInt32(s.Successful))),
					style.Deleted.Render(fmt.Sprintf("%d", safeInt32(s.Failed))),
					fmt.Sprintf("%d", safeInt32(s.InProgress)),
					fmt.Sprintf("%d", safeInt32(s.Unknown)),
					fmt.Sprintf("%d", safeInt32(s.Cancelled)),
				})
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	})
	statusCmd.PersistentFlags().BoolVar(&includeUnique, "include-unique", false, "Include unique result details when available")

	requiredCmd := &cobra.Command{
		Use:   "required",
		Short: "Required build merge-check management",
	}

	var requiredLimit int
	requiredCmd.PersistentFlags().IntVar(&requiredLimit, "limit", 25, "Page size for list operations")

	requiredCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List required build merge checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, err := loadQualityRepoAndService(repositorySelector)
			if err != nil {
				return err
			}

			checks, err := service.ListRequiredBuildChecks(cmd.Context(), repo, requiredLimit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), checks)
			}

			if len(checks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No required build merge checks found"))
				return nil
			}

			rows := make([][]string, len(checks))
			for i, check := range checks {
				rows[i] = []string{style.Secondary.Render(fmt.Sprintf("id=%d", safeInt64(check.Id))), fmt.Sprintf("buildParentKeys=%v", safeStringSlice(check.BuildParentKeys))}
			}
			style.WriteTable(cmd.OutOrStdout(), rows)

			return nil
		},
	})

	var createBody string
	createRequiredCmd := &cobra.Command{
		Use:   "create",
		Short: "Create required build merge check",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := loadQualityRepoServiceAndClient(repositorySelector)
			if err != nil {
				return err
			}

			payload := map[string]any{}
			if err := json.Unmarshal([]byte(createBody), &payload); err != nil {
				return apperrors.New(apperrors.KindValidation, "invalid JSON for --body", err)
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "build.required.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "required build check will be created",
						Confidence:      capabilityFull,
						RequiredState:   []string{"required build checks endpoint availability"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			created, err := service.CreateRequiredBuildCheck(cmd.Context(), repo, payload)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), created)
		},
	}
	createRequiredCmd.Flags().StringVar(&createBody, "body", "", "Raw JSON payload for required build merge check")
	_ = createRequiredCmd.MarkFlagRequired("body")
	requiredCmd.AddCommand(createRequiredCmd)

	var updateBody string
	updateRequiredCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update required build merge check",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := loadQualityRepoServiceAndClient(repositorySelector)
			if err != nil {
				return err
			}

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return apperrors.New(apperrors.KindValidation, "merge check id must be a valid integer", err)
			}

			payload := map[string]any{}
			if err := json.Unmarshal([]byte(updateBody), &payload); err != nil {
				return apperrors.New(apperrors.KindValidation, "invalid JSON for --body", err)
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "build.required.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": id},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "required build check will be updated",
						Confidence:      capabilityPartial,
						RequiredState:   []string{"required build checks endpoint availability"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			updated, err := service.UpdateRequiredBuildCheck(cmd.Context(), repo, id, payload)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), updated)
		},
	}
	updateRequiredCmd.Flags().StringVar(&updateBody, "body", "", "Raw JSON payload for required build merge check")
	_ = updateRequiredCmd.MarkFlagRequired("body")
	requiredCmd.AddCommand(updateRequiredCmd)

	requiredCmd.AddCommand(&cobra.Command{
		Use:   "delete <id>",
		Short: "Delete required build merge check",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := loadQualityRepoServiceAndClient(repositorySelector)
			if err != nil {
				return err
			}

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return apperrors.New(apperrors.KindValidation, "merge check id must be a valid integer", err)
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
					return err
				}

				checks, err := service.ListRequiredBuildChecks(cmd.Context(), repo, requiredLimit)
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "required build check was not found"
				for _, check := range checks {
					if safeInt64(check.Id) == id {
						predicted = "delete"
						reason = "required build check will be deleted"
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "build.required.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": id},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityPartial,
						RequiredState:   []string{"required build checks list"},
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

			if err := service.DeleteRequiredBuildCheck(cmd.Context(), repo, id); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "id": id})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted required build merge check"), style.Secondary.Render(fmt.Sprintf("%d", id)))
			return nil
		},
	})

	var scopedSetKey string
	var scopedSetState string
	var scopedSetURL string
	var scopedSetName string
	var scopedSetDescription string
	var scopedSetRef string
	var scopedSetParent string
	var scopedSetBuildNumber string
	var scopedSetDuration int64

	scopedSetCmd := &cobra.Command{
		Use:   "set <commit>",
		Short: "Set repository-scoped build status for a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := loadQualityRepoServiceAndClient(repositorySelector)
			if err != nil {
				return err
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				_, err := service.GetScopedBuildStatus(cmd.Context(), repo, args[0], scopedSetKey)
				predicted := "create"
				reason := "repository-scoped build status will be created"
				if err == nil {
					predicted = "update"
					reason = "repository-scoped build status will be updated"
				} else if apperrors.ExitCode(err) != 4 {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "build.set",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": scopedSetKey, "state": scopedSetState},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"scoped build status get"},
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

			err = service.AddScopedBuildStatus(cmd.Context(), repo, args[0], qualityservice.BuildStatusSetInput{
				Key:         scopedSetKey,
				State:       scopedSetState,
				URL:         scopedSetURL,
				Name:        scopedSetName,
				Description: scopedSetDescription,
				Ref:         scopedSetRef,
				Parent:      scopedSetParent,
				BuildNumber: scopedSetBuildNumber,
				DurationMS:  scopedSetDuration,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": scopedSetKey})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Repository-scoped build status %s set on %s/%s at %s\n", scopedSetKey, repo.ProjectKey, repo.Slug, args[0])
			return nil
		},
	}
	scopedSetCmd.Flags().StringVar(&scopedSetKey, "key", "", "Build status key")
	scopedSetCmd.Flags().StringVar(&scopedSetState, "state", "", "Build state (SUCCESSFUL, FAILED, INPROGRESS, UNKNOWN, CANCELLED)")
	scopedSetCmd.Flags().StringVar(&scopedSetURL, "url", "", "Build URL")
	scopedSetCmd.Flags().StringVar(&scopedSetName, "name", "", "Build display name")
	scopedSetCmd.Flags().StringVar(&scopedSetDescription, "description", "", "Build description")
	scopedSetCmd.Flags().StringVar(&scopedSetRef, "ref", "", "Build ref")
	scopedSetCmd.Flags().StringVar(&scopedSetParent, "parent", "", "Build parent key")
	scopedSetCmd.Flags().StringVar(&scopedSetBuildNumber, "build-number", "", "Build number")
	scopedSetCmd.Flags().Int64Var(&scopedSetDuration, "duration-ms", 0, "Duration in milliseconds")
	_ = scopedSetCmd.MarkFlagRequired("key")
	_ = scopedSetCmd.MarkFlagRequired("state")
	_ = scopedSetCmd.MarkFlagRequired("url")

	var scopedGetKey string
	scopedGetCmd := &cobra.Command{
		Use:   "get <commit>",
		Short: "Get repository-scoped build status by key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, err := loadQualityRepoAndService(repositorySelector)
			if err != nil {
				return err
			}

			build, err := service.GetScopedBuildStatus(cmd.Context(), repo, args[0], scopedGetKey)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), build)
			}

			state := safeStringFromBuildState(build.State)
			rows := [][]string{
				{style.Resource.Render(safeString(build.Key)), style.ActionStyle(state).Render(state), style.Secondary.Render(safeString(build.Url))},
			}
			style.WriteTable(cmd.OutOrStdout(), rows)
			return nil
		},
	}
	scopedGetCmd.Flags().StringVar(&scopedGetKey, "key", "", "Build status key")
	_ = scopedGetCmd.MarkFlagRequired("key")

	var scopedDeleteKey string
	scopedDeleteCmd := &cobra.Command{
		Use:   "delete <commit>",
		Short: "Delete repository-scoped build status by key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := loadQualityRepoServiceAndClient(repositorySelector)
			if err != nil {
				return err
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				_, err := service.GetScopedBuildStatus(cmd.Context(), repo, args[0], scopedDeleteKey)
				predicted := "delete"
				reason := "repository-scoped build status will be deleted"
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						predicted = "no-op"
						reason = "repository-scoped build status was not found"
					} else {
						return err
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "build.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": scopedDeleteKey},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"scoped build status get"},
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

			err = service.DeleteScopedBuildStatus(cmd.Context(), repo, args[0], scopedDeleteKey)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": scopedDeleteKey})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted repository-scoped build status %s on %s/%s at %s\n", scopedDeleteKey, repo.ProjectKey, repo.Slug, args[0])
			return nil
		},
	}
	scopedDeleteCmd.Flags().StringVar(&scopedDeleteKey, "key", "", "Build status key")
	_ = scopedDeleteCmd.MarkFlagRequired("key")

	buildCmd.AddCommand(statusCmd)
	buildCmd.AddCommand(requiredCmd)
	buildCmd.AddCommand(scopedSetCmd)
	buildCmd.AddCommand(scopedGetCmd)
	buildCmd.AddCommand(scopedDeleteCmd)

	return buildCmd
}

func matchesRestrictionSignature(restriction openapigenerated.RestRefRestriction, restrictionType, matcherType, matcherID string) bool {
	currentType := strings.TrimSpace(strings.ToLower(safeString(restriction.Type)))
	requestedType := strings.TrimSpace(strings.ToLower(restrictionType))
	if currentType != requestedType {
		return false
	}

	currentMatcherType := ""
	currentMatcherID := ""
	if restriction.Matcher != nil {
		if restriction.Matcher.Type != nil && restriction.Matcher.Type.Id != nil {
			currentMatcherType = strings.TrimSpace(strings.ToUpper(string(*restriction.Matcher.Type.Id)))
		}
		currentMatcherID = strings.TrimSpace(safeString(restriction.Matcher.Id))
	}

	requestedMatcherType := strings.TrimSpace(strings.ToUpper(matcherType))
	requestedMatcherID := strings.TrimSpace(matcherID)
	return currentMatcherType == requestedMatcherType && currentMatcherID == requestedMatcherID
}

func matchesRestrictionUpdate(restriction openapigenerated.RestRefRestriction, restrictionType, matcherType, matcherID string, users, groups []string, accessKeyIDs []int32) bool {
	if !matchesRestrictionSignature(restriction, restrictionType, matcherType, matcherID) {
		return false
	}

	currentUsers := make([]string, 0, len(safeUsers(restriction.Users)))
	for _, user := range safeUsers(restriction.Users) {
		currentUsers = append(currentUsers, strings.TrimSpace(safeString(user.Name)))
	}
	currentGroups := make([]string, 0, len(safeStringSlice(restriction.Groups)))
	for _, group := range safeStringSlice(restriction.Groups) {
		currentGroups = append(currentGroups, strings.TrimSpace(group))
	}
	currentAccessKeys := make([]int32, 0)
	if restriction.AccessKeys != nil {
		for _, key := range *restriction.AccessKeys {
			if key.Key != nil && key.Key.Id != nil {
				currentAccessKeys = append(currentAccessKeys, *key.Key.Id)
			}
		}
	}

	normalizeStrings := func(values []string) []string {
		normalized := make([]string, 0, len(values))
		for _, value := range values {
			if trimmed := strings.TrimSpace(value); trimmed != "" {
				normalized = append(normalized, trimmed)
			}
		}
		slices.Sort(normalized)
		return normalized
	}

	requestedUsers := normalizeStrings(users)
	requestedGroups := normalizeStrings(groups)
	normalizedCurrentUsers := normalizeStrings(currentUsers)
	normalizedCurrentGroups := normalizeStrings(currentGroups)

	requestedAccessKeys := append([]int32(nil), accessKeyIDs...)
	slices.Sort(requestedAccessKeys)
	slices.Sort(currentAccessKeys)

	return slices.Equal(normalizedCurrentUsers, requestedUsers) && slices.Equal(normalizedCurrentGroups, requestedGroups) && slices.Equal(currentAccessKeys, requestedAccessKeys)
}

func normalizeBranchName(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "refs/") {
		return trimmed
	}
	return "refs/heads/" + trimmed
}
