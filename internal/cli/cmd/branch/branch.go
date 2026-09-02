package branchcmd

import (
	"context"
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/safederef"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
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

func resolveBranchRepositoryReference(selector string, cfg config.AppConfig) (branchservice.RepositoryRef, error) {
	projectKey, slug, err := reposel.Resolve(selector, cfg)
	if err != nil {
		return branchservice.RepositoryRef{}, err
	}
	return branchservice.RepositoryRef{ProjectKey: projectKey, Slug: slug}, nil
}

func safeUsers(values *[]openapigenerated.RestApplicationUser) []openapigenerated.RestApplicationUser {
	if values == nil {
		return []openapigenerated.RestApplicationUser{}
	}
	return *values
}

func normalizeAccessKeyIDs(values []int) ([]int32, error) {
	const maxInt32Value = int(^uint32(0) >> 1)
	normalized := make([]int32, 0, len(values))
	for _, value := range values {
		if value < 0 || value > maxInt32Value {
			return nil, apperrors.New(apperrors.KindValidation, "access-key-id must be between 0 and 2147483647", nil)
		}
		normalized = append(normalized, int32(value))
	}
	return normalized, nil
}

func NormalizeBranchName(name string) string {
	trimmed := strings.TrimSpace(name)
	if strings.HasPrefix(trimmed, "refs/heads/") {
		return trimmed
	}
	return "refs/heads/" + trimmed
}

func MatchesRestrictionSignature(restriction openapigenerated.RestRefRestriction, restrictionType, matcherType, matcherID string) bool {
	if !strings.EqualFold(safederef.String(restriction.Type), strings.TrimSpace(restrictionType)) {
		return false
	}
	if restriction.Matcher == nil || restriction.Matcher.Type == nil {
		return false
	}
	if !strings.EqualFold(string(restriction.Matcher.Type.Id), strings.TrimSpace(matcherType)) {
		return false
	}
	targetMatcherID := strings.TrimSpace(matcherID)
	matcherIDVal := strings.TrimSpace(safederef.String(restriction.Matcher.Id))
	if strings.EqualFold(matcherIDVal, targetMatcherID) {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(safederef.String(restriction.Matcher.DisplayId)), targetMatcherID) {
		return true
	}
	if strings.EqualFold(strings.TrimPrefix(matcherIDVal, "refs/heads/"), strings.TrimPrefix(targetMatcherID, "refs/heads/")) {
		return true
	}
	return false
}

func MatchesRestrictionUpdate(restriction openapigenerated.RestRefRestriction, restrictionType, matcherType, matcherID string, users, groups []string, accessKeyIDs []int32) bool {
	if !MatchesRestrictionSignature(restriction, restrictionType, matcherType, matcherID) {
		return false
	}

	actualUsers := make([]string, 0)
	for _, u := range safeUsers(restriction.Users) {
		if name := safederef.String(u.Name); name != "" {
			actualUsers = append(actualUsers, strings.ToLower(strings.TrimSpace(name)))
		}
	}
	expectedUsers := make([]string, 0, len(users))
	for _, u := range users {
		trimmed := strings.ToLower(strings.TrimSpace(u))
		if trimmed != "" {
			expectedUsers = append(expectedUsers, trimmed)
		}
	}
	slices.Sort(actualUsers)
	slices.Sort(expectedUsers)
	if !slices.Equal(actualUsers, expectedUsers) {
		return false
	}

	actualGroups := make([]string, 0)
	for _, g := range safederef.StringSlice(restriction.Groups) {
		trimmed := strings.ToLower(strings.TrimSpace(g))
		if trimmed != "" {
			actualGroups = append(actualGroups, trimmed)
		}
	}
	expectedGroups := make([]string, 0, len(groups))
	for _, g := range groups {
		trimmed := strings.ToLower(strings.TrimSpace(g))
		if trimmed != "" {
			expectedGroups = append(expectedGroups, trimmed)
		}
	}
	slices.Sort(actualGroups)
	slices.Sort(expectedGroups)
	if !slices.Equal(actualGroups, expectedGroups) {
		return false
	}

	actualKeys := make([]int32, 0)
	if restriction.AccessKeys != nil {
		for _, k := range *restriction.AccessKeys {
			if k.Key != nil && k.Key.Id != nil {
				actualKeys = append(actualKeys, *k.Key.Id)
			}
		}
	}
	expectedKeys := append([]int32(nil), accessKeyIDs...)
	slices.Sort(actualKeys)
	slices.Sort(expectedKeys)
	return slices.Equal(actualKeys, expectedKeys)
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	var repositorySelector string
	var listPaging paging.Options
	var start int

	branchCmd := &cobra.Command{
		Use:   "branch",
		Short: "Repository branch and branch restriction commands",
	}

	branchCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")
	listPaging.RegisterPersistent(branchCmd, 25)
	branchCmd.PersistentFlags().IntVar(&start, "start", 0, "Start offset for list operations")

	var orderBy string
	var filterText string
	var base string
	var details bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List repository branches",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
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
				MaxResults: listPaging.ServiceLimit(),
				Start:      start,
				OrderBy:    orderBy,
				FilterText: filterText,
				Base:       base,
				Details:    detailsFilter,
			})
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Branches{Repository: repositoryOf(repo), Branches: branchesFrom(branches)})
			}

			if len(branches) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No branches found"))
				return nil
			}

			rows := make([][]string, len(branches))
			for i, branch := range branches {
				rows[i] = []string{
					style.Resource.Render(safederef.String(branch.DisplayId)),
					style.Secondary.Render(safederef.String(branch.Id)),
					style.Secondary.Render(safederef.String(branch.LatestCommit)),
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				branches, err := service.List(cmd.Context(), repo, branchservice.ListOptions{MaxResults: 1000, FilterText: args[0]})
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "branch will be created"
				normalizedRequested := NormalizeBranchName(args[0])
				for _, branch := range branches {
					if strings.EqualFold(strings.TrimSpace(safederef.String(branch.DisplayId)), strings.TrimSpace(args[0])) ||
						strings.EqualFold(strings.TrimSpace(safederef.String(branch.Id)), normalizedRequested) {
						predicted = "conflict"
						reason = "branch already exists"
						break
					}
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "branch.create",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "name": args[0], "startPoint": createStartPoint},
					Action:          "create",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"branch list (filtered by name)"},
					BlockingReasons: func() []string {
						if predicted == "conflict" {
							return []string{"branch already exists"}
						}
						return nil
					}(),
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			created, err := service.Create(cmd.Context(), repo, args[0], createStartPoint)
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), BranchCreation{Repository: repositoryOf(repo), Branch: branchFrom(created)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created branch"), style.Resource.Render(safederef.String(created.DisplayId)))
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if err := service.Delete(cmd.Context(), repo, args[0], deleteEndPoint, d.DryRunEnabled()); err != nil {
				return err
			}

			if d.JSONEnabled() {
				if d.DryRunEnabled() {
					reason := "validated through Bitbucket branch delete dry-run endpoint"
					if strings.TrimSpace(deleteEndPoint) != "" {
						reason = "validated through Bitbucket branch delete dry-run endpoint with end-point precondition"
					}
					return d.WriteJSON(cmd.OutOrStdout(), dryrunpreview.New(
						dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull,
						dryrunpreview.Item{
							Intent: "branch.delete",
							Target: map[string]any{
								"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug),
								"branch":     args[0],
								"endPoint":   strings.TrimSpace(deleteEndPoint),
							},
							Action:          "delete",
							PredictedAction: dryrunpreview.PredictedDelete,
							Supported:       true,
							Reason:          reason,
							Confidence:      dryrunpreview.CapabilityFull,
							RequiredState:   []string{"branch delete preflight validation"},
						}))
				}

				return d.WriteJSON(cmd.OutOrStdout(), BranchDeletion{Status: result.OK(), Repository: repositoryOf(repo), Branch: args[0]})
			}

			if d.DryRunEnabled() {
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
			cfg, client, err := d.LoadConfigAndClient()
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

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), DefaultBranch{Repository: repositoryOf(repo), DefaultBranch: result.RefFrom(defaultBranch)})
			}

			style.WriteTable(cmd.OutOrStdout(), [][]string{{style.Resource.Render(safederef.String(defaultBranch.DisplayId)), style.Secondary.Render(safederef.String(defaultBranch.Id))}})
			return nil
		},
	}
	defaultCmd.AddCommand(defaultGetCmd)

	defaultSetCmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Set repository default branch",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}

				currentDefault, err := service.GetDefault(cmd.Context(), repo)
				if err != nil {
					return err
				}
				predicted := "update"
				reason := "default branch will be updated"
				currentDefaultID := strings.TrimSpace(safederef.String(currentDefault.DisplayId))
				if currentDefaultID == "" {
					currentDefaultID = strings.TrimPrefix(strings.TrimSpace(safederef.String(currentDefault.Id)), "refs/heads/")
				}
				if strings.EqualFold(currentDefaultID, strings.TrimSpace(args[0])) {
					predicted = "no-op"
					reason = "default branch already set to requested value"
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "branch.default.set",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "defaultBranch": args[0]},
					Action:          "update",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"default branch"},
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.SetDefault(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), DefaultBranchChange{Status: result.OK(), Repository: repositoryOf(repo), DefaultBranch: args[0]})
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			refs, err := service.FindByCommit(cmd.Context(), repo, args[0], listPaging.ServiceLimit())
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), CommitRefs{Repository: repositoryOf(repo), Commit: args[0], Refs: result.RefsFrom(refs)})
			}

			if len(refs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No matching refs found"))
				return nil
			}

			rows := make([][]string, len(refs))
			for i, ref := range refs {
				rows[i] = []string{style.Resource.Render(safederef.String(ref.DisplayId)), style.Secondary.Render(safederef.String(ref.Id))}
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}

				currentDefault, err := service.GetDefault(cmd.Context(), repo)
				if err != nil {
					return err
				}
				predicted := "update"
				reason := "branch model default will be updated"
				currentDefaultID := strings.TrimSpace(safederef.String(currentDefault.DisplayId))
				if currentDefaultID == "" {
					currentDefaultID = strings.TrimPrefix(strings.TrimSpace(safederef.String(currentDefault.Id)), "refs/heads/")
				}
				if strings.EqualFold(currentDefaultID, strings.TrimSpace(args[0])) {
					predicted = "no-op"
					reason = "branch model default already set to requested value"
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "branch.model.update",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "defaultBranch": args[0]},
					Action:          "update",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"default branch"},
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.SetDefault(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), DefaultBranchChange{Status: result.OK(), Repository: repositoryOf(repo), DefaultBranch: args[0]})
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			restrictions, err := service.ListRestrictions(cmd.Context(), repo, branchservice.RestrictionListOptions{
				MaxResults:  listPaging.ServiceLimit(),
				Type:        restrictionType,
				MatcherType: matcherType,
				MatcherID:   matcherID,
			})
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), Restrictions{Repository: repositoryOf(repo), Restrictions: result.RestrictionsFrom(restrictions)})
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
					style.Secondary.Render(fmt.Sprintf("%d", safederef.Int32(restriction.Id))),
					safederef.String(restriction.Type),
					matcher,
					fmt.Sprintf("users=%d", len(safeUsers(restriction.Users))),
					fmt.Sprintf("groups=%d", len(safederef.StringSlice(restriction.Groups))),
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
			cfg, client, err := d.LoadConfigAndClient()
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

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), SingleRestriction{Repository: repositoryOf(repo), Restriction: result.RestrictionFrom(restriction)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\n", style.Secondary.Render(fmt.Sprintf("id=%d", safederef.Int32(restriction.Id))), safederef.String(restriction.Type))
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
			cfg, client, err := d.LoadConfigAndClient()
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
			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}

				restrictions, err := service.ListRestrictions(cmd.Context(), repo, branchservice.RestrictionListOptions{MaxResults: 1000})
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "branch restriction will be created"
				for _, restriction := range restrictions {
					if MatchesRestrictionSignature(restriction, createRestrictionType, createMatcherType, createMatcherID) {
						predicted = "conflict"
						reason = "matching branch restriction already exists"
						break
					}
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "branch.restriction.create",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "type": createRestrictionType, "matcherType": createMatcherType, "matcherId": createMatcherID},
					Action:          "create",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"branch restrictions list"},
					BlockingReasons: func() []string {
						if predicted == "conflict" {
							return []string{"matching restriction exists"}
						}
						return nil
					}(),
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
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

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), SingleRestriction{Repository: repositoryOf(repo), Restriction: result.RestrictionFrom(created)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Success.Render("Created restriction"), style.Secondary.Render(fmt.Sprintf("%d", safederef.Int32(created.Id))))
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
			cfg, client, err := d.LoadConfigAndClient()
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
			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}

				current, err := service.GetRestriction(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}
				predicted := "update"
				reason := "branch restriction will be updated"
				if MatchesRestrictionUpdate(current, updateRestrictionType, updateMatcherType, updateMatcherID, updateUsers, updateGroups, accessKeyIDs) {
					predicted = "no-op"
					reason = "branch restriction already matches requested values"
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "branch.restriction.update",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
					Action:          "update",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"branch restriction"},
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
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

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), SingleRestriction{Repository: repositoryOf(repo), Restriction: result.RestrictionFrom(updated)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Updated.Render("Updated restriction"), style.Secondary.Render(fmt.Sprintf("%d", safederef.Int32(updated.Id))))
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
			cfg, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolveBranchRepositoryReference(repositorySelector, cfg)
			if err != nil {
				return err
			}

			service := branchservice.NewService(client)
			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
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

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "branch.restriction.delete",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
					Action:          "delete",
					PredictedAction: predicted,
					Supported:       true,
					Reason:          reason,
					Confidence:      dryrunpreview.CapabilityFull,
					RequiredState:   []string{"branch restriction"},
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.DeleteRestriction(cmd.Context(), repo, args[0]); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), RestrictionDeletion{Status: result.OK(), Repository: repositoryOf(repo), RestrictionID: args[0]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", style.Deleted.Render("Deleted restriction"), style.Resource.Render(args[0]))
			return nil
		},
	}
	restrictionCmd.AddCommand(restrictionDeleteCmd)

	branchCmd.AddCommand(restrictionCmd)

	return branchCmd
}
