package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	reviewerservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reviewer"
)

func newReviewerCommand(options *rootOptions) *cobra.Command {
	var projectKey string
	var repositorySelector string
	var configFile string

	reviewerCmd := &cobra.Command{
		Use:   "reviewer",
		Short: "Manage default reviewers",
	}

	reviewerCmd.PersistentFlags().StringVar(&projectKey, "project", "", "Project key")
	reviewerCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug")
	reviewerCmd.PersistentFlags().StringVar(&configFile, "config-file", "", "JSON file containing condition settings")

	conditionCmd := &cobra.Command{
		Use:   "condition",
		Short: "Manage default reviewer conditions",
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List default reviewer conditions",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)

			if repositorySelector != "" {
				repo, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}
				conditions, err := service.ListRepositoryConditions(cmd.Context(), repo.ProjectKey, repo.Slug)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), map[string]any{"conditions": conditions})
				}
				printReviewerConditions(cmd, conditions)
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}

			conditions, err := service.ListProjectConditions(cmd.Context(), projectKey)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"conditions": conditions})
			}
			printReviewerConditions(cmd, conditions)
			return nil
		},
	}
	conditionCmd.AddCommand(listCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a default reviewer condition",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)
			id := args[0]

			if repositorySelector != "" {
				repo, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}
				if options.DryRun {
					checker := options.permissionCheckerFor(client)
					if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
						return err
					}

					conditions, err := service.ListRepositoryConditions(cmd.Context(), repo.ProjectKey, repo.Slug)
					if err != nil {
						return err
					}
					predicted := "no-op"
					reason := "reviewer condition not found in repository"
					if reviewerConditionExists(conditions, id) {
						predicted = "delete"
						reason = "reviewer condition will be deleted"
					}
					preview := dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent:          "reviewer.condition.delete",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": id},
							Action:          "delete",
							PredictedAction: predicted,
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   []string{"repository reviewer conditions"},
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
				if err := service.DeleteRepositoryCondition(cmd.Context(), repo.ProjectKey, repo.Slug, id); err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "id": id})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Deleted.Render("Deleted condition"), style.Resource.Render(id), style.Resource.Render(repo.ProjectKey+"/"+repo.Slug))
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
					return err
				}

				conditions, err := service.ListProjectConditions(cmd.Context(), projectKey)
				if err != nil {
					return err
				}
				predicted := "no-op"
				reason := "reviewer condition not found in project"
				if reviewerConditionExists(conditions, id) {
					predicted = "delete"
					reason = "reviewer condition will be deleted"
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "reviewer.condition.delete",
						Target:          map[string]any{"project": projectKey, "id": id},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project reviewer conditions"},
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

			if err := service.DeleteProjectCondition(cmd.Context(), projectKey, id); err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]string{"status": "ok", "id": id})
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s for project %s\n", style.Deleted.Render("Deleted condition"), style.Resource.Render(id), projectKey)
			return nil
		},
	}
	conditionCmd.AddCommand(deleteCmd)

	createCmd := &cobra.Command{
		Use:   "create [json-config]",
		Short: "Create a default reviewer condition",
		Long:  "Create a default reviewer condition using JSON from argument, file (--config-file), or stdin (-)",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			var configData []byte
			hasArgConfig := len(args) > 0
			hasConfigFile := configFile != ""

			if hasArgConfig && hasConfigFile {
				return fmt.Errorf("cannot provide condition config as both an argument and via --config-file; please use only one")
			}

			if hasArgConfig {
				if args[0] == "-" {
					configData, err = io.ReadAll(cmd.InOrStdin())
					if err != nil {
						return fmt.Errorf("failed to read stdin: %w", err)
					}
				} else {
					configData = []byte(args[0])
				}
			} else if hasConfigFile {
				configData, err = os.ReadFile(configFile)
				if err != nil {
					return fmt.Errorf("failed to read config file: %w", err)
				}
			} else {
				return fmt.Errorf("condition configuration is required (as JSON argument, file via --config-file, or stdin '-')")
			}

			service := reviewerservice.NewService(client)
			var condition openapigenerated.RestDefaultReviewersRequest
			if err := json.Unmarshal(configData, &condition); err != nil {
				return fmt.Errorf("invalid condition JSON: %w", err)
			}

			if repositorySelector != "" {
				repo, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}
				if options.DryRun {
					checker := options.permissionCheckerFor(client)
					if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
						return err
					}

					conditions, err := service.ListRepositoryConditions(cmd.Context(), repo.ProjectKey, repo.Slug)
					if err != nil {
						return err
					}
					predicted := "create"
					reason := "reviewer condition will be created"
					if reviewerConditionEquivalentExists(conditions, condition) {
						predicted = "conflict"
						reason = "equivalent reviewer condition already exists"
					}
					preview := dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent:          "reviewer.condition.create",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
							Action:          "create",
							PredictedAction: predicted,
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   []string{"repository reviewer conditions"},
							BlockingReasons: func() []string {
								if predicted == "conflict" {
									return []string{"equivalent condition exists"}
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
				created, err := service.CreateRepositoryCondition(cmd.Context(), repo.ProjectKey, repo.Slug, condition)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), created)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %d for repository %s\n", style.Success.Render("Created reviewer condition"), created.Id, style.Resource.Render(repo.ProjectKey+"/"+repo.Slug))
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
					return err
				}

				conditions, err := service.ListProjectConditions(cmd.Context(), projectKey)
				if err != nil {
					return err
				}
				predicted := "create"
				reason := "reviewer condition will be created"
				if reviewerConditionEquivalentExists(conditions, condition) {
					predicted = "conflict"
					reason = "equivalent reviewer condition already exists"
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "reviewer.condition.create",
						Target:          map[string]any{"project": projectKey},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project reviewer conditions"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"equivalent condition exists"}
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
			created, err := service.CreateProjectCondition(cmd.Context(), projectKey, condition)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), created)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %d for project %s\n", style.Success.Render("Created reviewer condition"), created.Id, projectKey)
			return nil
		},
	}
	conditionCmd.AddCommand(createCmd)

	updateCmd := &cobra.Command{
		Use:   "update <id> [json-config]",
		Short: "Update a default reviewer condition",
		Long:  "Update a default reviewer condition using JSON from argument, file (--config-file), or stdin (-)",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			id := args[0]
			var configData []byte
			hasArgConfig := len(args) > 1
			hasConfigFile := configFile != ""

			if hasArgConfig && hasConfigFile {
				return fmt.Errorf("cannot provide condition config as both an argument and via --config-file; please use only one")
			}

			if hasArgConfig {
				if args[1] == "-" {
					configData, err = io.ReadAll(cmd.InOrStdin())
					if err != nil {
						return fmt.Errorf("failed to read stdin: %w", err)
					}
				} else {
					configData = []byte(args[1])
				}
			} else if hasConfigFile {
				configData, err = os.ReadFile(configFile)
				if err != nil {
					return fmt.Errorf("failed to read config file: %w", err)
				}
			} else {
				return fmt.Errorf("condition configuration is required for update (as JSON argument, file via --config-file, or stdin '-')")
			}

			service := reviewerservice.NewService(client)
			if repositorySelector != "" {
				repo, err := resolveRepositoryReference(repositorySelector, cfg)
				if err != nil {
					return err
				}
				var condition openapigenerated.UpdatePullRequestCondition1JSONRequestBody
				if err := json.Unmarshal(configData, &condition); err != nil {
					return fmt.Errorf("invalid condition JSON: %w", err)
				}
				if options.DryRun {
					checker := options.permissionCheckerFor(client)
					if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOADMIN); err != nil {
						return err
					}

					conditions, err := service.ListRepositoryConditions(cmd.Context(), repo.ProjectKey, repo.Slug)
					if err != nil {
						return err
					}
					predicted := "blocked"
					reason := "reviewer condition not found in repository"
					blocking := []string{"reviewer condition not found"}
					if existing, found := findReviewerCondition(conditions, id); found {
						blocking = nil
						predicted = "update"
						reason = "reviewer condition will be updated"
						if reviewerConditionUpdateEquivalent(existing, condition) {
							predicted = "no-op"
							reason = "reviewer condition already matches requested update"
						}
					}
					preview := dryRunPreview{
						DryRun:       true,
						PlanningMode: planningModeStateful,
						Capability:   capabilityFull,
						Items: []dryRunItem{{
							Intent:          "reviewer.condition.update",
							Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": id},
							Action:          "update",
							PredictedAction: predicted,
							Supported:       true,
							Reason:          reason,
							Confidence:      capabilityFull,
							RequiredState:   []string{"repository reviewer conditions"},
							BlockingReasons: blocking,
						}},
						Summary: dryRunSummary{Total: 1, Supported: 1},
					}
					if predicted == "update" {
						preview.Summary.UpdateCount = 1
					} else if predicted == "no-op" {
						preview.Summary.NoopCount = 1
					} else {
						preview.Summary.UnknownCount = 1
					}
					return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
				}
				updated, err := service.UpdateRepositoryCondition(cmd.Context(), repo.ProjectKey, repo.Slug, id, condition)
				if err != nil {
					return err
				}
				if options.JSON {
					return writeJSON(cmd.OutOrStdout(), updated)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Updated.Render("Updated reviewer condition"), style.Resource.Render(id), style.Resource.Render(repo.ProjectKey+"/"+repo.Slug))
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}
			var condition openapigenerated.UpdatePullRequestConditionJSONRequestBody
			if err := json.Unmarshal(configData, &condition); err != nil {
				return fmt.Errorf("invalid condition JSON: %w", err)
			}
			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckProjectAdmin(cmd.Context(), projectKey); err != nil {
					return err
				}

				conditions, err := service.ListProjectConditions(cmd.Context(), projectKey)
				if err != nil {
					return err
				}
				predicted := "blocked"
				reason := "reviewer condition not found in project"
				blocking := []string{"reviewer condition not found"}
				if existing, found := findReviewerCondition(conditions, id); found {
					blocking = nil
					predicted = "update"
					reason = "reviewer condition will be updated"
					if reviewerConditionUpdateEquivalent(existing, condition) {
						predicted = "no-op"
						reason = "reviewer condition already matches requested update"
					}
				}
				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "reviewer.condition.update",
						Target:          map[string]any{"project": projectKey, "id": id},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"project reviewer conditions"},
						BlockingReasons: blocking,
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else if predicted == "no-op" {
					preview.Summary.NoopCount = 1
				} else {
					preview.Summary.UnknownCount = 1
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			updated, err := service.UpdateProjectCondition(cmd.Context(), projectKey, id, condition)
			if err != nil {
				return err
			}
			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), updated)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s for project %s\n", style.Updated.Render("Updated reviewer condition"), style.Resource.Render(id), projectKey)
			return nil
		},
	}
	conditionCmd.AddCommand(updateCmd)

	reviewerCmd.AddCommand(conditionCmd)

	return reviewerCmd
}

func printReviewerConditions(cmd *cobra.Command, conditions []openapigenerated.RestPullRequestCondition) {
	if len(conditions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No conditions found"))
		return
	}
	// Basic summary for human output
	fmt.Fprintf(cmd.OutOrStdout(), "Found %s conditions\n", style.Secondary.Render(fmt.Sprintf("%d", len(conditions))))
	// We could add more details here if we cast to RestPullRequestCondition
}

func reviewerConditionExists(conditions []openapigenerated.RestPullRequestCondition, id string) bool {
	_, ok := findReviewerCondition(conditions, id)
	return ok
}

func findReviewerCondition(conditions []openapigenerated.RestPullRequestCondition, id string) (openapigenerated.RestPullRequestCondition, bool) {
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return openapigenerated.RestPullRequestCondition{}, false
	}

	for _, condition := range conditions {
		if condition.Id != nil && strings.TrimSpace(fmt.Sprintf("%d", *condition.Id)) == trimmedID {
			return condition, true
		}
	}

	return openapigenerated.RestPullRequestCondition{}, false
}

func reviewerConditionEquivalentExists(conditions []openapigenerated.RestPullRequestCondition, condition openapigenerated.RestDefaultReviewersRequest) bool {
	for _, existing := range conditions {
		if reviewerConditionEquivalent(existing, condition) {
			return true
		}
	}

	return false
}

func reviewerConditionEquivalent(existing openapigenerated.RestPullRequestCondition, desired openapigenerated.RestDefaultReviewersRequest) bool {
	existingPayload := map[string]any{
		"requiredApprovals": existing.RequiredApprovals,
		"sourceMatcher":     existing.SourceRefMatcher,
		"targetMatcher":     existing.TargetRefMatcher,
		"reviewers":         existing.Reviewers,
	}

	desiredPayload := map[string]any{
		"requiredApprovals": desired.RequiredApprovals,
		"sourceMatcher":     desired.SourceMatcher,
		"targetMatcher":     desired.TargetMatcher,
		"reviewers":         desired.Reviewers,
	}

	return reflect.DeepEqual(normalizeJSONShape(existingPayload), normalizeJSONShape(desiredPayload))
}

func reviewerConditionUpdateEquivalent(existing openapigenerated.RestPullRequestCondition, desired any) bool {
	return reflect.DeepEqual(normalizeJSONShape(existing), normalizeJSONShape(desired))
}
