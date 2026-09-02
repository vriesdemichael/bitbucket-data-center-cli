package reviewercmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

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
	reviewerservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reviewer"
)

type PermissionChecker interface {
	CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error
	CheckProjectAdmin(ctx context.Context, projectKey string) error
}

type Dependencies struct {
	JSONEnabled         func() bool
	DryRunEnabled       func() bool
	LoadConfig          func() (config.AppConfig, error)
	LoadConfigAndClient func() (config.AppConfig, *openapigenerated.ClientWithResponses, error)
	WriteJSON           func(io.Writer, any) error
	PermissionChecker   func(*openapigenerated.ClientWithResponses) PermissionChecker
}

func (d Dependencies) withDefaults() Dependencies {
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
		d.WriteJSON = func(w io.Writer, v any) error {
			return jsonoutput.Write(w, v)
		}
	}
	return d
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	var projectKey string
	var repositorySelector string
	var configFile string

	reviewerCmd := &cobra.Command{
		Use:   "reviewer",
		Short: "Manage default reviewers",
		Long: "Manage default reviewer conditions.\n\n" +
			"Note on CODEOWNERS: .bitbucket/CODEOWNERS is a git-tracked file rather than a REST resource, so it is managed through repository contents and not by this command. For server-level reviewer rules use default-reviewer conditions (bb reviewer condition) and reviewer groups (bb reviewer-group); bb pr create and bb pr review reviewer add read CODEOWNERS and expand groups directly.",
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)

			if repositorySelector != "" {
				pk, slug, err := reposel.Resolve(repositorySelector, cfg)
				if err != nil {
					return err
				}
				conditions, err := service.ListRepositoryConditions(cmd.Context(), pk, slug)
				if err != nil {
					return err
				}
				if d.JSONEnabled() {
					return d.WriteJSON(cmd.OutOrStdout(), Conditions{Conditions: result.ConditionsFrom(conditions)})
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
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Conditions{Conditions: result.ConditionsFrom(conditions)})
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)
			id := args[0]

			if repositorySelector != "" {
				pk, slug, err := reposel.Resolve(repositorySelector, cfg)
				if err != nil {
					return err
				}
				if d.DryRunEnabled() {
					if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, pk, slug, openapi.RepoAdmin); err != nil {
						return err
					}

					conditions, err := service.ListRepositoryConditions(cmd.Context(), pk, slug)
					if err != nil {
						return err
					}
					predicted := "no-op"
					reason := "reviewer condition not found in repository"
					if reviewerConditionExists(conditions, id) {
						predicted = "delete"
						reason = "reviewer condition will be deleted"
					}
					preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
						Intent:          "reviewer.condition.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", pk, slug), "id": id},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"repository reviewer conditions"},
					})
					return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
				}
				if err := service.DeleteRepositoryCondition(cmd.Context(), pk, slug, id); err != nil {
					return err
				}
				if d.JSONEnabled() {
					return d.WriteJSON(cmd.OutOrStdout(), ConditionDeletion{Status: result.OK(), ID: id})
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Deleted.Render("Deleted condition"), style.Resource.Render(id), style.Resource.Render(pk+"/"+slug))
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}

			if d.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), d.PermissionChecker, client, projectKey); err != nil {
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
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "reviewer.condition.delete",
					Target:          map[string]any{"project": projectKey, "id": id},
					Action:          "delete",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"project reviewer conditions"},
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.DeleteProjectCondition(cmd.Context(), projectKey, id); err != nil {
				return err
			}
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), ConditionDeletion{Status: result.OK(), ID: id})
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
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)

			if len(args) > 0 && args[0] != "-" && configFile != "" {
				return apperrors.New(apperrors.KindValidation, "cannot provide condition config as both an argument and via --config-file", nil)
			}

			var configData []byte
			stdinRequested := len(args) > 0 && args[0] == "-"
			if len(args) > 0 && args[0] != "-" {
				configData = []byte(args[0])
			} else if configFile != "" {
				configData, err = os.ReadFile(configFile)
				if err != nil {
					return fmt.Errorf("failed to read config file: %w", err)
				}
			} else {
				// Only read stdin when the caller asked for it. An implicit
				// fallback blocks forever when stdin is an open pipe with
				// nothing coming, which is the shape a CI runner provides
				// (ADR-073).
				if !stdinRequested {
					return apperrors.New(apperrors.KindValidation,
						"no condition given: pass it as an argument, use --config-file, or pass - to read it from standard input", nil)
				}
				configData, err = io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("failed to read condition from stdin: %w", err)
				}
			}

			if repositorySelector != "" {
				pk, slug, err := reposel.Resolve(repositorySelector, cfg)
				if err != nil {
					return err
				}
				var condition openapigenerated.RestDefaultReviewersRequest
				if err := json.Unmarshal(configData, &condition); err != nil {
					return fmt.Errorf("invalid condition JSON: %w", err)
				}
				if d.DryRunEnabled() {
					if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, pk, slug, openapi.RepoAdmin); err != nil {
						return err
					}

					conditions, err := service.ListRepositoryConditions(cmd.Context(), pk, slug)
					if err != nil {
						return err
					}
					predicted := "create"
					reason := "reviewer condition will be created"
					var blocking []string
					if reviewerConditionEquivalentExists(conditions, condition) {
						predicted = "conflict"
						reason = "equivalent reviewer condition already exists"
						blocking = []string{"reviewer condition already exists"}
					}
					preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
						Intent:          "reviewer.condition.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", pk, slug)},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"repository reviewer conditions"},
						BlockingReasons: blocking,
					})
					return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
				}
				created, err := service.CreateRepositoryCondition(cmd.Context(), pk, slug, condition)
				if err != nil {
					return err
				}
				if d.JSONEnabled() {
					return d.WriteJSON(cmd.OutOrStdout(), result.ConditionFrom(created))
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s for repository %s\n", style.Success.Render("Created reviewer condition"), style.Resource.Render(pk+"/"+slug))
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return fmt.Errorf("project key is required (use --project or --repo)")
			}
			var condition openapigenerated.RestDefaultReviewersRequest
			if err := json.Unmarshal(configData, &condition); err != nil {
				return fmt.Errorf("invalid condition JSON: %w", err)
			}
			if d.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), d.PermissionChecker, client, projectKey); err != nil {
					return err
				}

				conditions, err := service.ListProjectConditions(cmd.Context(), projectKey)
				if err != nil {
					return err
				}
				predicted := "create"
				reason := "reviewer condition will be created"
				var blocking []string
				if reviewerConditionEquivalentExists(conditions, condition) {
					predicted = "conflict"
					reason = "equivalent reviewer condition already exists"
					blocking = []string{"reviewer condition already exists"}
				}
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "reviewer.condition.create",
					Target:          map[string]any{"project": projectKey},
					Action:          "create",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"project reviewer conditions"},
					BlockingReasons: blocking,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}
			created, err := service.CreateProjectCondition(cmd.Context(), projectKey, condition)
			if err != nil {
				return err
			}
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), result.ConditionFrom(created))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s for project %s\n", style.Success.Render("Created reviewer condition"), projectKey)
			return nil
		},
	}
	conditionCmd.AddCommand(createCmd)

	updateCmd := &cobra.Command{
		Use:   "update <id> [json-config]",
		Short: "Update a default reviewer condition",
		Long:  "Update a default reviewer condition using JSON from argument, file (--config-file), or stdin (-)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := reviewerservice.NewService(client)
			id := args[0]

			if len(args) > 1 && args[1] != "-" && configFile != "" {
				return apperrors.New(apperrors.KindValidation, "cannot provide condition config as both an argument and via --config-file", nil)
			}

			var configData []byte
			stdinRequested := len(args) > 1 && args[1] == "-"
			if len(args) > 1 && args[1] != "-" {
				configData = []byte(args[1])
			} else if configFile != "" {
				configData, err = os.ReadFile(configFile)
				if err != nil {
					return fmt.Errorf("failed to read config file: %w", err)
				}
			} else {
				// Only read stdin when the caller asked for it. An implicit
				// fallback blocks forever when stdin is an open pipe with
				// nothing coming, which is the shape a CI runner provides
				// (ADR-073).
				if !stdinRequested {
					return apperrors.New(apperrors.KindValidation,
						"no condition given: pass it as an argument, use --config-file, or pass - to read it from standard input", nil)
				}
				configData, err = io.ReadAll(cmd.InOrStdin())
				if err != nil {
					return fmt.Errorf("failed to read condition from stdin: %w", err)
				}
			}

			if repositorySelector != "" {
				pk, slug, err := reposel.Resolve(repositorySelector, cfg)
				if err != nil {
					return err
				}
				var condition openapigenerated.UpdatePullRequestCondition1JSONRequestBody
				if err := json.Unmarshal(configData, &condition); err != nil {
					return fmt.Errorf("invalid condition JSON: %w", err)
				}
				if d.DryRunEnabled() {
					if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, pk, slug, openapi.RepoAdmin); err != nil {
						return err
					}

					conditions, err := service.ListRepositoryConditions(cmd.Context(), pk, slug)
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
					preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
						Intent:          "reviewer.condition.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", pk, slug), "id": id},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"repository reviewer conditions"},
						BlockingReasons: blocking,
					})
					return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
				}
				updated, err := service.UpdateRepositoryCondition(cmd.Context(), pk, slug, id, condition)
				if err != nil {
					return err
				}
				if d.JSONEnabled() {
					return d.WriteJSON(cmd.OutOrStdout(), result.ConditionFrom(updated))
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s %s for repository %s\n", style.Updated.Render("Updated reviewer condition"), style.Resource.Render(id), style.Resource.Render(pk+"/"+slug))
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
			if d.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), d.PermissionChecker, client, projectKey); err != nil {
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
				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "reviewer.condition.update",
					Target:          map[string]any{"project": projectKey, "id": id},
					Action:          "update",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"project reviewer conditions"},
					BlockingReasons: blocking,
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}
			updated, err := service.UpdateProjectCondition(cmd.Context(), projectKey, id, condition)
			if err != nil {
				return err
			}
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), result.ConditionFrom(updated))
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
	fmt.Fprintf(cmd.OutOrStdout(), "Found %s conditions\n", style.Secondary.Render(fmt.Sprintf("%d", len(conditions))))
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

func normalizeJSONShape(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}

	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return value
	}

	return normalized
}
