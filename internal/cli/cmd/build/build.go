package buildcmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
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

func safeString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func safeInt32(value *int32) int32 {
	if value == nil {
		return 0
	}
	return *value
}

func safeInt64(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func safeStringSlice(values *[]string) []string {
	if values == nil {
		return []string{}
	}
	return *values
}

func safeStringFromBuildState(state *openapigenerated.RestBuildStatusState) string {
	if state == nil {
		return ""
	}
	return string(*state)
}

// requiredCheckRow is the human rendering of one required build merge check,
// shared by list, create and update so the three describe the same check the
// same way.
// The four fields after the keys are what the check actually does: which
// branches it applies to, which are exempt, and whether it is enforced on pull
// requests and on merge-queue merges. They were in the payload and not in the
// table, so a person creating a check saw an id and a key list and no way to
// tell an enforced check from a dormant one.
func requiredCheckRow(check result.RequiredBuildCheck) []string {
	matcher := func(ref result.RefMatcher) string {
		if ref.DisplayID != "" {
			return ref.DisplayID
		}
		if ref.ID != "" {
			return ref.ID
		}
		return "-"
	}

	return []string{
		style.Secondary.Render(fmt.Sprintf("id=%d", check.ID)),
		fmt.Sprintf("buildParentKeys=%v", check.BuildParentKeys),
		fmt.Sprintf("refMatcher=%s", matcher(check.RefMatcher)),
		fmt.Sprintf("exemptRefMatcher=%s", matcher(check.ExemptRefMatcher)),
		fmt.Sprintf("requiredForPullRequest=%t", check.RequiredForPullRequest),
		fmt.Sprintf("requiredForMergeQueue=%t", check.RequiredForMergeQueue),
	}
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

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
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := qualityservice.NewService(client)
			if d.DryRunEnabled() {
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

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "build.status.set",
						Target:          map[string]any{"commit": args[0], "key": setKey, "state": setState, "url": setURL},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"build statuses list"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "create" {
					preview.Summary.CreateCount = 1
				} else {
					preview.Summary.UpdateCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
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

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), StatusChange{Status: result.OK(), Commit: args[0], Key: setKey})
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

	var getPaging paging.Options
	var getOrderBy string
	statusCmd.AddCommand(&cobra.Command{
		Use:   "get <commit>",
		Short: "Get build statuses for a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			service := qualityservice.NewService(client)
			statuses, err := service.GetBuildStatuses(cmd.Context(), args[0], getPaging.ServiceLimit(), getOrderBy)
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSONList(cmd.OutOrStdout(), buildStatusesFrom(statuses), paging.LimitReached(getPaging, len(statuses)))
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
	getPaging.RegisterPersistent(statusCmd, 25)
	statusCmd.PersistentFlags().StringVar(&getOrderBy, "order-by", "", "Order by NEWEST, OLDEST, or STATUS")

	var includeUnique bool
	statusCmd.AddCommand(&cobra.Command{
		Use:   "stats <commit>...",
		Short: "Get build status summary counts for one or more commits",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, client, err := d.LoadConfigAndClient()
			if err != nil {
				return err
			}

			// The service trims each commit id and keys its answer by the
			// trimmed form, so a lookup by the raw argument misses and reports
			// zeros for a commit that has builds. Trimming here rather than
			// there keeps the rows, the table and the map agreeing on one
			// spelling of each id.
			args = trimmedCommitIDs(args)

			service := qualityservice.NewService(client)
			if len(args) == 1 {
				stats, err := service.GetBuildStatusStats(cmd.Context(), args[0], includeUnique)
				if err != nil {
					return err
				}

				if d.JSONEnabled() {
					return d.WriteJSON(cmd.OutOrStdout(), []CommitBuildStats{statsFrom(args[0], stats)})
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

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), statsListFrom(args, statsMap))
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

	var requiredPaging paging.Options
	requiredPaging.RegisterPersistent(requiredCmd, 25)

	requiredCmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List required build merge checks",
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, _, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			checks, err := service.ListRequiredBuildChecks(cmd.Context(), repo, requiredPaging.ServiceLimit())
			if err != nil {
				return err
			}

			converted := result.RequiredBuildChecksFrom(checks)
			if d.JSONEnabled() {
				return d.WriteJSONList(cmd.OutOrStdout(), converted, paging.LimitReached(requiredPaging, len(checks)))
			}

			if len(converted) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), style.Empty.Render("No required build merge checks found"))
				return nil
			}

			rows := make([][]string, len(converted))
			for i, check := range converted {
				rows[i] = requiredCheckRow(check)
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
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			payload := map[string]any{}
			if err := json.Unmarshal([]byte(createBody), &payload); err != nil {
				return apperrors.New(apperrors.KindValidation, "invalid JSON for --body", err)
			}

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "build.required.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug)},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "required build check will be created",
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"required build checks endpoint availability"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			created, err := service.CreateRequiredBuildCheck(cmd.Context(), repo, payload)
			if err != nil {
				return err
			}

			check := result.RequiredBuildCheckFromMap(created)
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), check)
			}

			style.WriteTable(cmd.OutOrStdout(), [][]string{requiredCheckRow(check)})
			return nil
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
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
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

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityPartial,
					Items: []dryrunpreview.Item{{
						Intent:          "build.required.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": id},
						Action:          "update",
						PredictedAction: "update",
						Supported:       true,
						Reason:          "required build check will be updated",
						Confidence:      dryrunpreview.CapabilityPartial,
						RequiredState:   []string{"required build checks endpoint availability"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1, UpdateCount: 1},
				}
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			updated, err := service.UpdateRequiredBuildCheck(cmd.Context(), repo, id, payload)
			if err != nil {
				return err
			}

			check := result.RequiredBuildCheckFromMap(updated)
			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), check)
			}

			style.WriteTable(cmd.OutOrStdout(), [][]string{requiredCheckRow(check)})
			return nil
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
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return apperrors.New(apperrors.KindValidation, "merge check id must be a valid integer", err)
			}

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoAdmin); err != nil {
					return err
				}

				checks, err := service.ListRequiredBuildChecks(cmd.Context(), repo, requiredPaging.ServiceLimit())
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

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityPartial,
					Items: []dryrunpreview.Item{{
						Intent:          "build.required.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": id},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityPartial,
						RequiredState:   []string{"required build checks list"},
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

			if err := service.DeleteRequiredBuildCheck(cmd.Context(), repo, id); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), RequiredCheckDeletion{Status: result.OK(), Repository: repositoryOf(repo), ID: id})
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
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
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

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "build.set",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": scopedSetKey, "state": scopedSetState},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"scoped build status get"},
					}},
					Summary: dryrunpreview.Summary{Total: 1, Supported: 1},
				}
				if predicted == "create" {
					preview.Summary.CreateCount = 1
				} else {
					preview.Summary.UpdateCount = 1
				}

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
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

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), ScopedStatusChange{Status: result.OK(), Repository: repositoryOf(repo), Commit: args[0], Key: scopedSetKey})
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
			repo, service, _, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			build, err := service.GetScopedBuildStatus(cmd.Context(), repo, args[0], scopedGetKey)
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), buildStatusFrom(build))
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
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
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

				preview := dryrunpreview.Preview{
					DryRun:       true,
					PlanningMode: dryrunpreview.PlanningModeStateful,
					Capability:   dryrunpreview.CapabilityFull,
					Items: []dryrunpreview.Item{{
						Intent:          "build.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": scopedDeleteKey},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      dryrunpreview.CapabilityFull,
						RequiredState:   []string{"scoped build status get"},
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

			err = service.DeleteScopedBuildStatus(cmd.Context(), repo, args[0], scopedDeleteKey)
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), ScopedStatusChange{Status: result.OK(), Repository: repositoryOf(repo), Commit: args[0], Key: scopedDeleteKey})
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
