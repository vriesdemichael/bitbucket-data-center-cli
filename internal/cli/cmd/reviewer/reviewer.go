package reviewercmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/inherited"
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
			"Note on CODEOWNERS: .bitbucket/CODEOWNERS is a git-tracked file rather than a REST resource, so it is managed through repository contents and not by this command. For server-level reviewer rules use default-reviewer conditions (bb reviewer condition) and reviewer groups (bb reviewer-group); bb pr create and bb pr review reviewer add ask Bitbucket which code owners a change has, so they match what the web interface shows.",
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
		Long: `List the default reviewer conditions of a project, or with --repo of a
repository: its own, and those it inherits from its project, which are marked
as inherited.`,
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
				printReviewerConditions(cmd, conditions, pk)
				return nil
			}

			if projectKey == "" {
				projectKey = cfg.ProjectKey
			}
			if projectKey == "" {
				return apperrors.New(apperrors.KindValidation, "project key is required (use --project or --repo)", nil)
			}

			conditions, err := service.ListProjectConditions(cmd.Context(), projectKey)
			if err != nil {
				return err
			}
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Conditions{Conditions: result.ConditionsFrom(conditions)})
			}
			printReviewerConditions(cmd, conditions, "")
			return nil
		},
	}
	conditionCmd.AddCommand(listCmd)

	deleteCmd := &cobra.Command{
		Use:   "delete <condition-id>",
		Short: "Delete a default reviewer condition",
		Long: `Delete a default reviewer condition of a project, or with --repo of a
repository.

With --repo, a condition the repository inherits from its project is refused:
deleted through the repository, it would be deleted from every repository in
the project. --project deletes it there.`,
		Args: cobra.ExactArgs(1),
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
					if err := refuseInheritedCondition(conditions, id, pk, "delete"); err != nil {
						return err
					}
					predicted := "no-op"
					reason := "reviewer condition not found in repository"
					if reviewerConditionExists(conditions, id) {
						predicted = "delete"
						reason = "reviewer condition will be deleted"
					}
					preview := dryrunpreview.New(dryrunpreview.Item{
						Intent:          "reviewer.condition.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", pk, slug), "id": id},
						Action:          "delete",
						PredictedAction: predicted,
						Tier:            dryrunpreview.TierPreconditionsChecked,
						Reason:          reason,
					})
					return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
				}
				conditions, err := service.ListRepositoryConditions(cmd.Context(), pk, slug)
				if err != nil {
					return err
				}
				if err := refuseInheritedCondition(conditions, id, pk, "delete"); err != nil {
					return err
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
				return apperrors.New(apperrors.KindValidation, "project key is required (use --project or --repo)", nil)
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
				preview := dryrunpreview.New(dryrunpreview.Item{
					Intent:          "reviewer.condition.delete",
					Target:          map[string]any{"project": projectKey, "id": id},
					Action:          "delete",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Reason:          reason,
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
					return apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read config file: %v", err), err)
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
					return apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read condition from stdin: %v", err), err)
				}
			}

			if repositorySelector != "" {
				pk, slug, err := reposel.Resolve(repositorySelector, cfg)
				if err != nil {
					return err
				}
				var condition openapigenerated.RestDefaultReviewersRequest
				if err := json.Unmarshal(configData, &condition); err != nil {
					return apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid condition JSON: %v", err), err)
				}
				if d.DryRunEnabled() {
					if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, pk, slug, openapi.RepoAdmin); err != nil {
						return err
					}
					if err := service.RefuseReviewerGroups(cmd.Context(), condition.ReviewerGroups); err != nil {
						return err
					}

					conditions, err := service.ListRepositoryConditions(cmd.Context(), pk, slug)
					if err != nil {
						return err
					}
					item, err := conditionCreateOutcome(conditions, condition)
					if err != nil {
						return err
					}
					item.Intent = "reviewer.condition.create"
					item.Target = map[string]any{"repository": fmt.Sprintf("%s/%s", pk, slug)}
					item.Action = "create"
					item.Tier = dryrunpreview.TierPreconditionsChecked
					return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), dryrunpreview.New(item))
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
				return apperrors.New(apperrors.KindValidation, "project key is required (use --project or --repo)", nil)
			}
			var condition openapigenerated.RestDefaultReviewersRequest
			if err := json.Unmarshal(configData, &condition); err != nil {
				return apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid condition JSON: %v", err), err)
			}
			if d.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), d.PermissionChecker, client, projectKey); err != nil {
					return err
				}
				if err := service.RefuseReviewerGroups(cmd.Context(), condition.ReviewerGroups); err != nil {
					return err
				}

				conditions, err := service.ListProjectConditions(cmd.Context(), projectKey)
				if err != nil {
					return err
				}
				item, err := conditionCreateOutcome(conditions, condition)
				if err != nil {
					return err
				}
				item.Intent = "reviewer.condition.create"
				item.Target = map[string]any{"project": projectKey}
				item.Action = "create"
				item.Tier = dryrunpreview.TierPreconditionsChecked
				preview := dryrunpreview.New(item)
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
		Use:   "update <condition-id> [json-config]",
		Short: "Update a default reviewer condition",
		Long: `Update a default reviewer condition using JSON from argument, file (--config-file), or stdin (-)

With --repo, a condition the repository inherits from its project is refused;
--project changes it there, for every repository in the project.`,
		Args: cobra.RangeArgs(1, 2),
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
					return apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read config file: %v", err), err)
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
					return apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read condition from stdin: %v", err), err)
				}
			}

			if repositorySelector != "" {
				pk, slug, err := reposel.Resolve(repositorySelector, cfg)
				if err != nil {
					return err
				}
				var condition openapigenerated.UpdatePullRequestCondition1JSONRequestBody
				if err := json.Unmarshal(configData, &condition); err != nil {
					return apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid condition JSON: %v", err), err)
				}
				if d.DryRunEnabled() {
					if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, pk, slug, openapi.RepoAdmin); err != nil {
						return err
					}
					if err := service.RefuseReviewerGroups(cmd.Context(), condition.ReviewerGroups); err != nil {
						return err
					}

					conditions, err := service.ListRepositoryConditions(cmd.Context(), pk, slug)
					if err != nil {
						return err
					}
					if err := refuseInheritedCondition(conditions, id, pk, "update"); err != nil {
						return err
					}
					item, err := conditionUpdateOutcome(conditions, id, condition, "repository")
					if err != nil {
						return err
					}
					item.Intent = "reviewer.condition.update"
					item.Target = map[string]any{"repository": fmt.Sprintf("%s/%s", pk, slug), "id": id}
					item.Action = "update"
					item.Tier = dryrunpreview.TierPreconditionsChecked
					return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), dryrunpreview.New(item))
				}
				conditions, err := service.ListRepositoryConditions(cmd.Context(), pk, slug)
				if err != nil {
					return err
				}
				if err := refuseInheritedCondition(conditions, id, pk, "update"); err != nil {
					return err
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
				return apperrors.New(apperrors.KindValidation, "project key is required (use --project or --repo)", nil)
			}
			var condition openapigenerated.UpdatePullRequestConditionJSONRequestBody
			if err := json.Unmarshal(configData, &condition); err != nil {
				return apperrors.New(apperrors.KindValidation, fmt.Sprintf("invalid condition JSON: %v", err), err)
			}
			if d.DryRunEnabled() {
				if err := preflight.ProjectAdmin(cmd.Context(), d.PermissionChecker, client, projectKey); err != nil {
					return err
				}
				if err := service.RefuseReviewerGroups(cmd.Context(), condition.ReviewerGroups); err != nil {
					return err
				}

				conditions, err := service.ListProjectConditions(cmd.Context(), projectKey)
				if err != nil {
					return err
				}
				item, err := conditionUpdateOutcome(conditions, id, condition, "project")
				if err != nil {
					return err
				}
				item.Intent = "reviewer.condition.update"
				item.Target = map[string]any{"project": projectKey, "id": id}
				item.Action = "update"
				item.Tier = dryrunpreview.TierPreconditionsChecked
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), dryrunpreview.New(item))
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

// printReviewerConditions lists conditions one to a row. repositoryProject is
// the project of the repository being listed, whose conditions it marks as
// inherited; empty for a project's own listing, where every condition is the
// project's.
func printReviewerConditions(cmd *cobra.Command, conditions []openapigenerated.RestPullRequestCondition, repositoryProject string) {
	if len(conditions) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No conditions found"))
		return
	}

	rows := make([][]string, 0, len(conditions))
	for _, condition := range conditions {
		source, target := "", ""
		if condition.SourceRefMatcher != nil {
			source = matcherText(condition.SourceRefMatcher.DisplayId, condition.SourceRefMatcher.Id)
		}
		if condition.TargetRefMatcher != nil {
			target = matcherText(condition.TargetRefMatcher.DisplayId, condition.TargetRefMatcher.Id)
		}

		label := ""
		if repositoryProject != "" && condition.Scope != nil {
			label = inherited.Label(string(condition.Scope.Type), repositoryProject)
		}

		reviewers := 0
		if condition.Reviewers != nil {
			reviewers = len(*condition.Reviewers)
		}

		rows = append(rows, []string{
			style.Secondary.Render(fmt.Sprintf("%d", safederef.Int32(condition.Id))),
			fmt.Sprintf("approvals=%d", safederef.Int32(condition.RequiredApprovals)),
			source + " -> " + target,
			fmt.Sprintf("reviewers=%d", reviewers),
			style.Secondary.Render(label),
		})
	}
	style.WriteTable(cmd.OutOrStdout(), rows)
}

// matcherText is a ref matcher as a person reads it: its display id, or its
// id when it has none, and "any" for the matcher of every ref.
func matcherText(displayID, id *string) string {
	matcherID := strings.TrimSpace(safederef.String(id))
	if matcherID == "" || openapi.IsAnyRefMatcherID(matcherID) {
		return "any"
	}
	if text := strings.TrimSpace(safederef.String(displayID)); text != "" {
		return text
	}

	return matcherID
}

// refuseInheritedCondition refuses a condition the repository inherits from
// its project. The repository's route takes that condition's id and acts on
// it for the whole project: a delete removes it from every repository in it
// (#657). change is the subcommand the caller ran, which does the same on the
// project.
func refuseInheritedCondition(conditions []openapigenerated.RestPullRequestCondition, id, projectKey, change string) error {
	condition, found := findReviewerCondition(conditions, id)
	if !found || condition.Scope == nil || !inherited.FromProject(string(condition.Scope.Type)) {
		return nil
	}

	trimmed := strings.TrimSpace(id)

	return inherited.Refusal("reviewer condition", trimmed, projectKey, change,
		fmt.Sprintf("bb reviewer condition %s %s --project %s", change, trimmed, projectKey))
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

// conditionRefusal is how Bitbucket refuses a condition body it will not
// store, or nil when it takes it.
//
// Bitbucket checks the body before it looks at anything else, the condition an
// update names included, so a preview that does not has predicted a create or
// an update the real run is refused -- and "not found" for an update the real
// run is refused as invalid. The rules and their wording are the ones 10.4.3
// applies, each seen refusing a request: an id and a type on both matchers, a
// reviewer or a reviewer group, and a count of required approvals. A reviewer
// named without an id is looked up as user -1 and answered with a 404.
//
// condition is the body as the command sends it, so what is checked is what
// would be sent rather than what was typed.
func conditionRefusal(condition any) error {
	encoded, err := json.Marshal(condition)
	if err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to encode the condition", err)
	}

	type matcher struct {
		ID   *string `json:"id"`
		Type *struct {
			ID string `json:"id"`
		} `json:"type"`
	}
	var body struct {
		SourceMatcher *matcher `json:"sourceMatcher"`
		TargetMatcher *matcher `json:"targetMatcher"`
		Reviewers     []struct {
			ID *int64 `json:"id"`
		} `json:"reviewers"`
		ReviewerGroups    []json.RawMessage `json:"reviewerGroups"`
		RequiredApprovals *int64            `json:"requiredApprovals"`
	}
	if err := json.Unmarshal(encoded, &body); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to read the condition back", err)
	}

	complete := func(m *matcher) bool { return m != nil && m.ID != nil && m.Type != nil }
	switch {
	case !complete(body.SourceMatcher):
		return apperrors.New(apperrors.KindValidation, "A sourceMatcher with ID and type is required when creating or updating a new condition.", nil)
	case !complete(body.TargetMatcher):
		return apperrors.New(apperrors.KindValidation, "A targetMatcher with an ID and type is required when creating or updating a new condition.", nil)
	case len(body.Reviewers)+len(body.ReviewerGroups) == 0:
		return apperrors.New(apperrors.KindValidation, "Reviewers or reviewer groups are required.", nil)
	case body.RequiredApprovals == nil || *body.RequiredApprovals < 0:
		return apperrors.New(apperrors.KindValidation, "Required approvals must be >= 0.", nil)
	}

	for _, reviewer := range body.Reviewers {
		if reviewer.ID == nil {
			return apperrors.New(apperrors.KindNotFound,
				"a reviewer is named without an id, which Bitbucket looks up as user -1: User with ID -1 does not exist. Name reviewers by their numeric id", nil)
		}
	}

	return nil
}

// conditionCreateOutcome is what creating condition would come to, beside the
// conditions already there: the outcome fields of its preview item.
//
// An equivalent condition does not stop it. Bitbucket stores a second
// condition identical to the first rather than refusing it, so the preview
// predicts the create the real run makes, and says what it duplicates.
func conditionCreateOutcome(conditions []openapigenerated.RestPullRequestCondition, condition openapigenerated.RestDefaultReviewersRequest) (dryrunpreview.Item, error) {
	if refusal := conditionRefusal(condition); refusal != nil {
		return refusedOutcome(refusal)
	}

	if reviewerConditionEquivalentExists(conditions, condition) {
		return dryrunpreview.Item{
			PredictedAction: dryrunpreview.PredictedCreate,
			Reason:          "an equivalent reviewer condition already exists; Bitbucket adds this one beside it",
		}, nil
	}

	return dryrunpreview.Item{PredictedAction: dryrunpreview.PredictedCreate, Reason: "reviewer condition will be created"}, nil
}

// conditionUpdateOutcome is what updating condition id to body would come to.
// where names the scope it was looked for in, for the reason.
func conditionUpdateOutcome(conditions []openapigenerated.RestPullRequestCondition, id string, body any, where string) (dryrunpreview.Item, error) {
	if refusal := conditionRefusal(body); refusal != nil {
		return refusedOutcome(refusal)
	}

	existing, found := findReviewerCondition(conditions, id)
	if !found {
		// Refused by the update itself, once the body has passed: a 404.
		return dryrunpreview.Item{
			PredictedAction: dryrunpreview.PredictedBlocked,
			Reason:          "reviewer condition not found in " + where,
			BlockingReasons: []string{"reviewer condition not found"},
			Fails:           apperrors.KindNotFound,
		}, nil
	}
	if reviewerConditionUpdateEquivalent(existing, body) {
		return dryrunpreview.Item{PredictedAction: dryrunpreview.PredictedNoop, Reason: "reviewer condition already matches requested update"}, nil
	}

	return dryrunpreview.Item{PredictedAction: dryrunpreview.PredictedUpdate, Reason: "reviewer condition will be updated"}, nil
}

// refusedOutcome is the preview of a body Bitbucket refuses. A body bb could
// not read back is no verdict on anything, so it is returned as the failure it
// is.
func refusedOutcome(refusal error) (dryrunpreview.Item, error) {
	if apperrors.IsKind(refusal, apperrors.KindInternal) {
		return dryrunpreview.Item{}, refusal
	}

	message := apperrors.MessageOf(refusal)

	return dryrunpreview.Item{
		PredictedAction: dryrunpreview.PredictedBlocked,
		Reason:          message,
		BlockingReasons: []string{message},
		Fails:           apperrors.KindOf(refusal),
	}, nil
}

// reviewerConditionEquivalent reports whether Bitbucket already holds the
// condition being asked for.
//
// It compares the four things that identify one -- the two ref matchers, the
// approval count, and who the reviewers are -- rather than the objects whole.
//
// Whole objects cannot match. A caller names a reviewer as {"id": 116} or
// {"name": "alice"}; the server answers with the full user -- display name,
// email address, avatar URL, active flag, links -- and enriches each matcher
// with a displayId and a type name it derived itself. A deep comparison between
// what was sent and what came back is therefore never equal, so the preview
// never recognised a condition that already existed. The mocked test that
// covered this echoed the request back verbatim, which is the one server shape
// that made the comparison work.
func reviewerConditionEquivalent(existing openapigenerated.RestPullRequestCondition, desired openapigenerated.RestDefaultReviewersRequest) bool {
	if safederef.Int32(existing.RequiredApprovals) != safederef.Int32(desired.RequiredApprovals) {
		return false
	}

	existingSourceType, existingSourceID := "", ""
	if matcher := existing.SourceRefMatcher; matcher != nil {
		existingSourceID = safederef.String(matcher.Id)
		if matcher.Type != nil {
			existingSourceType = string(matcher.Type.Id)
		}
	}
	existingTargetType, existingTargetID := "", ""
	if matcher := existing.TargetRefMatcher; matcher != nil {
		existingTargetID = safederef.String(matcher.Id)
		if matcher.Type != nil {
			existingTargetType = string(matcher.Type.Id)
		}
	}

	desiredSourceType, desiredSourceID := "", ""
	if matcher := desired.SourceMatcher; matcher != nil {
		desiredSourceID = safederef.String(matcher.Id)
		if matcher.Type != nil {
			desiredSourceType = string(matcher.Type.Id)
		}
	}
	desiredTargetType, desiredTargetID := "", ""
	if matcher := desired.TargetMatcher; matcher != nil {
		desiredTargetID = safederef.String(matcher.Id)
		if matcher.Type != nil {
			desiredTargetType = string(matcher.Type.Id)
		}
	}

	if !sameRefMatcher(existingSourceType, existingSourceID, desiredSourceType, desiredSourceID) {
		return false
	}
	if !sameRefMatcher(existingTargetType, existingTargetID, desiredTargetType, desiredTargetID) {
		return false
	}

	return sameReviewerSet(existing.Reviewers, desired.Reviewers)
}

// anyRefMatcherType is the matcher type that means "any branch".
//
// Its id is not the caller's. A condition created with
// {"id": "ANY_REF", "type": {"id": "ANY_REF"}} comes back with
// id "ANY_REF_MATCHER_ID", so for this type the id says nothing and comparing
// it makes an existing condition look like a new one.
const anyRefMatcherType = "ANY_REF"

// sameRefMatcher reports whether two ref matchers select the same refs.
func sameRefMatcher(existingType, existingID, desiredType, desiredID string) bool {
	if !strings.EqualFold(existingType, desiredType) {
		return false
	}
	if strings.EqualFold(existingType, anyRefMatcherType) {
		return true
	}

	return existingID == desiredID
}

// sameReviewerSet compares two reviewer lists by whichever identifier both
// sides carry, ignoring order.
//
// A caller may name a reviewer by id or by name and Bitbucket answers with
// both, so a desired reviewer counts as present when either matches.
func sameReviewerSet(existing *[]openapigenerated.RestReviewerGroup, desired *[]openapigenerated.RestApplicationUser) bool {
	existingIDs, existingNames := map[int64]bool{}, map[string]bool{}
	if existing != nil {
		for _, reviewer := range *existing {
			if reviewer.Id != nil {
				existingIDs[*reviewer.Id] = true
			}
			if name := strings.TrimSpace(safederef.String(reviewer.Name)); name != "" {
				existingNames[strings.ToLower(name)] = true
			}
		}
	}

	desiredCount := 0
	if desired != nil {
		desiredCount = len(*desired)
	}
	// A condition with a different number of reviewers is a different
	// condition, whichever identifier each side happens to carry.
	if desiredCount != len(existingIDs) && desiredCount != len(existingNames) {
		return false
	}
	if desired == nil {
		return true
	}

	for _, reviewer := range *desired {
		if reviewer.Id != nil && existingIDs[int64(*reviewer.Id)] {
			continue
		}
		if name := strings.TrimSpace(safederef.String(reviewer.Name)); name != "" && existingNames[strings.ToLower(name)] {
			continue
		}

		return false
	}

	return true
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
