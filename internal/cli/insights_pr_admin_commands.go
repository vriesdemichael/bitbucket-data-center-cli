package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/comment"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequestactivity"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
)

func newInsightsCommand(options *rootOptions) *cobra.Command {
	var repositorySelector string
	var reportLimit int

	insightsCmd := &cobra.Command{
		Use:   "insights",
		Short: "Code Insights report and annotation commands",
	}
	insightsCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	reportCmd := &cobra.Command{
		Use:   "report",
		Short: "Code Insights report commands",
	}
	reportCmd.PersistentFlags().IntVar(&reportLimit, "limit", 25, "Page size for list operations")

	var reportBody string
	setReportCmd := &cobra.Command{
		Use:   "set <commit> <key>",
		Short: "Create or update a Code Insights report",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := loadQualityRepoServiceAndClient(repositorySelector)
			if err != nil {
				return err
			}

			request := openapigenerated.SetACodeInsightsReportJSONRequestBody{}
			if err := json.Unmarshal([]byte(reportBody), &request); err != nil {
				return apperrors.New(apperrors.KindValidation, "invalid JSON for --body", err)
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				_, err := service.GetReport(cmd.Context(), repo, args[0], args[1])
				predicted := "create"
				reason := "insights report will be created"
				if err == nil {
					predicted = "update"
					reason = "insights report will be updated"
				} else if apperrors.ExitCode(err) != 4 {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "insights.report.set",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": args[1]},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"insights report get"},
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

			report, err := service.SetReport(cmd.Context(), repo, args[0], args[1], request)
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), report)
		},
	}
	setReportCmd.Flags().StringVar(&reportBody, "body", "", "Raw JSON payload for Code Insights report")
	_ = setReportCmd.MarkFlagRequired("body")
	reportCmd.AddCommand(setReportCmd)

	reportCmd.AddCommand(&cobra.Command{
		Use:   "get <commit> <key>",
		Short: "Get a Code Insights report",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, err := loadQualityRepoAndService(repositorySelector)
			if err != nil {
				return err
			}

			report, err := service.GetReport(cmd.Context(), repo, args[0], args[1])
			if err != nil {
				return err
			}

			return writeJSON(cmd.OutOrStdout(), report)
		},
	})

	reportCmd.AddCommand(&cobra.Command{
		Use:   "delete <commit> <key>",
		Short: "Delete a Code Insights report",
		Args:  cobra.ExactArgs(2),
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

				_, err := service.GetReport(cmd.Context(), repo, args[0], args[1])
				predicted := "delete"
				reason := "insights report will be deleted"
				if err != nil {
					if apperrors.ExitCode(err) == 4 {
						predicted = "no-op"
						reason = "insights report was not found"
					} else {
						return err
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "insights.report.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": args[1]},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"insights report get"},
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

			if err := service.DeleteReport(cmd.Context(), repo, args[0], args[1]); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "commit": args[0], "key": args[1]})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted report %s for commit %s\n", args[1], args[0])
			return nil
		},
	})

	reportCmd.AddCommand(&cobra.Command{
		Use:   "list <commit>",
		Short: "List Code Insights reports for a commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, err := loadQualityRepoAndService(repositorySelector)
			if err != nil {
				return err
			}

			reports, err := service.ListReports(cmd.Context(), repo, args[0], reportLimit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), reports)
			}

			if len(reports) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No reports found")
				return nil
			}

			for _, report := range reports {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", safeString(report.Key), safeString(report.Title), safeStringFromInsightResult(report.Result))
			}

			return nil
		},
	})

	annotationCmd := &cobra.Command{
		Use:   "annotation",
		Short: "Code Insights annotation commands",
	}

	var annotationBody string
	addAnnotationCmd := &cobra.Command{
		Use:   "add <commit> <key>",
		Short: "Add annotations to a Code Insights report",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := loadQualityRepoServiceAndClient(repositorySelector)
			if err != nil {
				return err
			}

			annotations := make([]openapigenerated.RestSingleAddInsightAnnotationRequest, 0)
			if err := json.Unmarshal([]byte(annotationBody), &annotations); err != nil {
				return apperrors.New(apperrors.KindValidation, "invalid JSON for --body (expected array of annotations)", err)
			}

			if options.DryRun {
				checker := options.permissionCheckerFor(client)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "insights.annotation.add",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": args[1], "count": len(annotations)},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "insights annotations will be added",
						Confidence:      capabilityPartial,
						RequiredState:   []string{"insights report context"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			if err := service.AddAnnotations(cmd.Context(), repo, args[0], args[1], annotations); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "count": len(annotations)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added %d annotations to report %s\n", len(annotations), args[1])
			return nil
		},
	}
	addAnnotationCmd.Flags().StringVar(&annotationBody, "body", "", "Raw JSON array payload for annotations")
	_ = addAnnotationCmd.MarkFlagRequired("body")
	annotationCmd.AddCommand(addAnnotationCmd)

	annotationCmd.AddCommand(&cobra.Command{
		Use:   "list <commit> <key>",
		Short: "List annotations for a Code Insights report",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, err := loadQualityRepoAndService(repositorySelector)
			if err != nil {
				return err
			}

			annotations, err := service.ListAnnotations(cmd.Context(), repo, args[0], args[1])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), annotations)
			}

			if len(annotations) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No annotations found")
				return nil
			}

			for _, annotation := range annotations {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", safeString(annotation.ExternalId), safeString(annotation.Severity), safeString(annotation.Message))
			}

			return nil
		},
	})

	var externalID string
	deleteAnnotationCmd := &cobra.Command{
		Use:   "delete <commit> <key>",
		Short: "Delete annotation(s) by external id for a report",
		Args:  cobra.ExactArgs(2),
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

				annotations, err := service.ListAnnotations(cmd.Context(), repo, args[0], args[1])
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "no matching annotation found"
				for _, annotation := range annotations {
					if strings.EqualFold(strings.TrimSpace(safeString(annotation.ExternalId)), strings.TrimSpace(externalID)) {
						predicted = "delete"
						reason = "annotation will be deleted"
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityPartial,
					Items: []dryRunItem{{
						Intent:          "insights.annotation.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": args[1], "external_id": externalID},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityPartial,
						RequiredState:   []string{"insights annotations list"},
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

			if err := service.DeleteAnnotations(cmd.Context(), repo, args[0], args[1], externalID); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "external_id": externalID})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted annotations for external id %s\n", externalID)
			return nil
		},
	}
	deleteAnnotationCmd.Flags().StringVar(&externalID, "external-id", "", "External annotation ID to delete")
	_ = deleteAnnotationCmd.MarkFlagRequired("external-id")
	annotationCmd.AddCommand(deleteAnnotationCmd)

	insightsCmd.AddCommand(reportCmd)
	insightsCmd.AddCommand(annotationCmd)

	return insightsCmd
}

func newPRCommand(options *rootOptions) *cobra.Command {
	var repository string
	var state string
	var limit int
	var start int
	var sourceBranch string
	var targetBranch string

	prCmd := &cobra.Command{
		Use:   "pr",
		Short: "Pull request commands",
	}
	prCmd.PersistentFlags().StringVar(&repository, "repo", "", "Repository as PROJECT/slug (defaults to inferred repository context; otherwise requires BITBUCKET_PROJECT_KEY and BITBUCKET_REPO_SLUG)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List pull requests",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			pullRequests, err := service.List(cmd.Context(), repo, pullrequestservice.ListOptions{
				State:        state,
				Limit:        limit,
				Start:        start,
				SourceBranch: sourceBranch,
				TargetBranch: targetBranch,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"repository":    repo,
					"filters":       map[string]any{"state": strings.ToLower(strings.TrimSpace(state)), "start": start, "limit": limit, "source_branch": sourceBranch, "target_branch": targetBranch},
					"pull_requests": pullRequests,
				})
			}

			if len(pullRequests) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No pull requests found")
				return nil
			}

			for _, pullRequest := range pullRequests {
				fmt.Fprintf(
					cmd.OutOrStdout(),
					"#%d\t%s\t%s -> %s\t%s\n",
					pullRequest.ID,
					pullRequest.State,
					pullRequest.SourceBranch,
					pullRequest.TargetBranch,
					pullRequest.Title,
				)
			}

			return nil
		},
	}
	listCmd.Flags().StringVar(&state, "state", "open", "Pull request state filter: open, closed, all")
	listCmd.Flags().IntVar(&limit, "limit", 25, "Page size for Bitbucket pull request list operations")
	listCmd.Flags().IntVar(&start, "start", 0, "Start offset for Bitbucket pull request list operations")
	listCmd.Flags().StringVar(&sourceBranch, "source-branch", "", "Optional source branch filter")
	listCmd.Flags().StringVar(&targetBranch, "target-branch", "", "Optional target branch filter")
	prCmd.AddCommand(listCmd)

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get pull request details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			pullRequest, err := service.Get(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": pullRequest})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "#%d\t%s\t%s -> %s\t%s\n", pullRequest.ID, pullRequest.State, pullRequest.SourceBranch, pullRequest.TargetBranch, pullRequest.Title)
			if len(pullRequest.Reviewers) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Reviewers: %d\n", len(pullRequest.Reviewers))
			}
			if pullRequest.Mergeability != nil {
				mergeability := pullRequest.Mergeability
				mergeableLabel := "no"
				if mergeability.Mergeable {
					mergeableLabel = "yes"
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Mergeable: %s\n", mergeableLabel)
				if mergeability.Outcome != "" {
					fmt.Fprintf(cmd.OutOrStdout(), "Merge outcome: %s\n", mergeability.Outcome)
				}
				if mergeability.Conflicted {
					fmt.Fprintln(cmd.OutOrStdout(), "Merge conflicts: yes")
				}
				if len(mergeability.Blockers) > 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "Merge blockers:")
					for _, blocker := range mergeability.Blockers {
						message := blocker.Summary
						if message == "" {
							message = blocker.Detail
						} else if blocker.Detail != "" && !strings.EqualFold(blocker.Detail, blocker.Summary) {
							message = fmt.Sprintf("%s (%s)", blocker.Summary, blocker.Detail)
						}
						fmt.Fprintf(cmd.OutOrStdout(), "- %s\n", message)
					}
				}
			}

			return nil
		},
	}
	prCmd.AddCommand(getCmd)

	var createFromRef string
	var createToRef string
	var createTitle string
	var createDescription string
	var createReviewers []string
	createCmd := &cobra.Command{
		Use:   "create",
		Short: "Create a pull request",
		Example: "  # Create a pull request\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\"\n\n" +
			"  # Create a pull request and assign reviewers (repeatable or comma-separated)\n" +
			"  bb pr create --repo PROJ/repo --from-ref feature/x --to-ref main --title \"My change\" --reviewers alice,bob",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				existing, err := service.List(cmd.Context(), repo, pullrequestservice.ListOptions{
					State:        "open",
					Limit:        200,
					SourceBranch: createFromRef,
					TargetBranch: createToRef,
				})
				if err != nil {
					return err
				}

				predicted := "create"
				reason := "pull request will be created"
				for _, pullRequest := range existing {
					if strings.EqualFold(strings.TrimSpace(pullRequest.SourceBranch), strings.TrimSpace(createFromRef)) &&
						strings.EqualFold(strings.TrimSpace(pullRequest.TargetBranch), strings.TrimSpace(createToRef)) {
						predicted = "conflict"
						reason = "an open pull request already exists for the same source and target branches"
						break
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "from_ref": createFromRef, "to_ref": createToRef, "title": createTitle, "reviewers": createReviewers},
						Action:          "create",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"open pull requests"},
						BlockingReasons: func() []string {
							if predicted == "conflict" {
								return []string{"matching open pull request exists"}
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

			created, err := service.Create(cmd.Context(), repo, pullrequestservice.CreateInput{
				FromRef:     createFromRef,
				ToRef:       createToRef,
				Title:       createTitle,
				Description: createDescription,
				Reviewers:   createReviewers,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": created})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created pull request #%d\n", created.ID)
			return nil
		},
	}
	createCmd.Flags().StringVar(&createFromRef, "from-ref", "", "Source branch (name or refs/heads/name)")
	createCmd.Flags().StringVar(&createToRef, "to-ref", "", "Target branch (name or refs/heads/name)")
	createCmd.Flags().StringVar(&createTitle, "title", "", "Pull request title")
	createCmd.Flags().StringVar(&createDescription, "description", "", "Pull request description")
	createCmd.Flags().StringSliceVar(&createReviewers, "reviewers", nil, "Reviewer usernames to add (repeatable or comma-separated, e.g. --reviewers alice,bob)")
	_ = createCmd.MarkFlagRequired("from-ref")
	_ = createCmd.MarkFlagRequired("to-ref")
	_ = createCmd.MarkFlagRequired("title")
	prCmd.AddCommand(createCmd)

	var updateTitle string
	var updateDescription string
	var updateVersion int
	updateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update pull request metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "pull request metadata will be updated"
				if strings.EqualFold(strings.TrimSpace(current.Title), strings.TrimSpace(updateTitle)) &&
					strings.EqualFold(strings.TrimSpace(current.Description), strings.TrimSpace(updateDescription)) {
					predicted = "no-op"
					reason = "pull request already matches requested metadata"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0], "title": updateTitle, "description": updateDescription, "version": updateVersion},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			updated, err := service.Update(cmd.Context(), repo, args[0], pullrequestservice.UpdateInput{
				Title:       updateTitle,
				Description: updateDescription,
				Version:     updateVersion,
			})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated pull request #%d\n", updated.ID)
			return nil
		},
	}
	updateCmd.Flags().StringVar(&updateTitle, "title", "", "Updated pull request title")
	updateCmd.Flags().StringVar(&updateDescription, "description", "", "Updated pull request description")
	updateCmd.Flags().IntVar(&updateVersion, "version", 0, "Expected pull request version")
	_ = updateCmd.MarkFlagRequired("version")
	prCmd.AddCommand(updateCmd)

	var transitionVersion int
	mergeCmd := &cobra.Command{
		Use:   "merge <id>",
		Short: "Merge a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "pull request will be merged"
				blocking := []string{}
				if strings.EqualFold(strings.TrimSpace(current.State), "MERGED") {
					predicted = "no-op"
					reason = "pull request is already merged"
				} else if !strings.EqualFold(strings.TrimSpace(current.State), "OPEN") {
					predicted = "blocked"
					reason = "pull request is not open"
					blocking = []string{"pull request is not open"}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.merge",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request"},
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

			var version *int
			if cmd.Flags().Changed("version") {
				version = &transitionVersion
			}

			merged, err := service.Merge(cmd.Context(), repo, args[0], version)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": merged})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Merged pull request #%d\n", merged.ID)
			return nil
		},
	}
	mergeCmd.Flags().IntVar(&transitionVersion, "version", 0, "Expected pull request version")
	prCmd.AddCommand(mergeCmd)

	declineCmd := &cobra.Command{
		Use:   "decline <id>",
		Short: "Decline a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "pull request will be declined"
				if strings.EqualFold(strings.TrimSpace(current.State), "DECLINED") {
					predicted = "no-op"
					reason = "pull request is already declined"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.decline",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &transitionVersion
			}

			declined, err := service.Decline(cmd.Context(), repo, args[0], version)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": declined})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Declined pull request #%d\n", declined.ID)
			return nil
		},
	}
	declineCmd.Flags().IntVar(&transitionVersion, "version", 0, "Expected pull request version")
	prCmd.AddCommand(declineCmd)

	reopenCmd := &cobra.Command{
		Use:   "reopen <id>",
		Short: "Reopen a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}

				predicted := "update"
				reason := "pull request will be reopened"
				if strings.EqualFold(strings.TrimSpace(current.State), "OPEN") {
					predicted = "no-op"
					reason = "pull request is already open"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.reopen",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &transitionVersion
			}

			reopened, err := service.Reopen(cmd.Context(), repo, args[0], version)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": reopened})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Reopened pull request #%d\n", reopened.ID)
			return nil
		},
	}
	reopenCmd.Flags().IntVar(&transitionVersion, "version", 0, "Expected pull request version")
	prCmd.AddCommand(reopenCmd)

	reviewCmd := &cobra.Command{Use: "review", Short: "Pull request review commands"}

	reviewApproveCmd := &cobra.Command{
		Use:   "approve <id>",
		Short: "Approve a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)
				predicted := "update"
				reason := "pull request approval will be added"
				if currentUser != "" && reviewerApprovedByUser(current.Reviewers, currentUser) {
					predicted = "no-op"
					reason = "current user has already approved this pull request"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.review.approve",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			pullRequest, err := service.Approve(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": pullRequest})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Approved pull request #%d\n", pullRequest.ID)
			return nil
		},
	}
	reviewCmd.AddCommand(reviewApproveCmd)

	reviewUnapproveCmd := &cobra.Command{
		Use:   "unapprove <id>",
		Short: "Remove pull request approval",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}
				currentUser := strings.TrimSpace(cfg.BitbucketUsername)
				predicted := "update"
				reason := "pull request approval will be removed"
				if currentUser != "" && !reviewerApprovedByUser(current.Reviewers, currentUser) {
					predicted = "no-op"
					reason = "current user has not approved this pull request"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.review.unapprove",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0]},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			pullRequest, err := service.Unapprove(cmd.Context(), repo, args[0])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": pullRequest})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed approval for pull request #%d\n", pullRequest.ID)
			return nil
		},
	}
	reviewCmd.AddCommand(reviewUnapproveCmd)

	reviewerCmd := &cobra.Command{Use: "reviewer", Short: "Manage pull request reviewers"}
	var reviewerUsername string
	reviewerAddCmd := &cobra.Command{
		Use:   "add <id>",
		Short: "Add a reviewer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}
				predicted := "update"
				reason := "reviewer will be added"
				if hasReviewer(current.Reviewers, reviewerUsername) {
					predicted = "no-op"
					reason = "reviewer already present"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.review.reviewer.add",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0], "user": reviewerUsername},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1},
				}
				if predicted == "update" {
					preview.Summary.UpdateCount = 1
				} else {
					preview.Summary.NoopCount = 1
				}

				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			pullRequest, err := service.AddReviewer(cmd.Context(), repo, args[0], reviewerUsername)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": pullRequest})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added reviewer %s to pull request #%d\n", reviewerUsername, pullRequest.ID)
			return nil
		},
	}
	reviewerAddCmd.Flags().StringVar(&reviewerUsername, "user", "", "Reviewer username")
	_ = reviewerAddCmd.MarkFlagRequired("user")
	reviewerCmd.AddCommand(reviewerAddCmd)

	reviewerRemoveCmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Remove a reviewer",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOWRITE); err != nil {
					return err
				}

				current, err := service.Get(cmd.Context(), repo, args[0])
				if err != nil {
					return err
				}
				predicted := "delete"
				reason := "reviewer will be removed"
				if !hasReviewer(current.Reviewers, reviewerUsername) {
					predicted = "no-op"
					reason = "reviewer is not present"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.review.reviewer.remove",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0], "user": reviewerUsername},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request"},
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
			pullRequest, err := service.RemoveReviewer(cmd.Context(), repo, args[0], reviewerUsername)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request": pullRequest})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Removed reviewer %s from pull request #%d\n", reviewerUsername, pullRequest.ID)
			return nil
		},
	}
	reviewerRemoveCmd.Flags().StringVar(&reviewerUsername, "user", "", "Reviewer username")
	_ = reviewerRemoveCmd.MarkFlagRequired("user")
	reviewerCmd.AddCommand(reviewerRemoveCmd)

	reviewCmd.AddCommand(reviewerCmd)
	prCmd.AddCommand(reviewCmd)

	commentCmd := &cobra.Command{Use: "comment", Short: "Pull request comment commands"}

	var commentPath string
	var commentLimit int
	commentListCmd := &cobra.Command{
		Use:   "list <id>",
		Short: "List comments for a pull request",
		Long:  "List pull request comments. Without --path this uses the pull request activity timeline to return the aggregate comment view. With --path it uses the path-scoped comments endpoint.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			trimmedCommentPath := strings.TrimSpace(commentPath)

			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			source := "comments"
			comments := make([]openapigenerated.RestComment, 0)
			if trimmedCommentPath == "" {
				source = "activities"
				activityService := pullrequestactivityservice.NewService(client)
				activities, err := activityService.List(cmd.Context(), pullrequestactivityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, args[0], pullrequestactivityservice.ListOptions{Limit: commentLimit})
				if err != nil {
					return err
				}
				comments = pullrequestactivityservice.ExtractComments(activities)
			} else {
				service := commentservice.NewService(client)
				comments, err = service.List(cmd.Context(), commentservice.Target{
					Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
					PullRequestID: args[0],
				}, trimmedCommentPath, commentLimit)
				if err != nil {
					return err
				}
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"repository":      repo,
					"pull_request_id": args[0],
					"source":          source,
					"path":            trimmedCommentPath,
					"comments":        comments,
				})
			}

			if len(comments) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No comments found")
				return nil
			}

			for _, comment := range comments {
				fmt.Fprintln(cmd.OutOrStdout(), formatCommentSummary(comment))
			}

			return nil
		},
	}
	commentListCmd.Flags().StringVar(&commentPath, "path", "", "Optional file path for path-scoped pull request comment listing")
	commentListCmd.Flags().IntVar(&commentLimit, "limit", 25, "Page size for pull request comment list operations")
	commentCmd.AddCommand(commentListCmd)

	commentGetCmd := &cobra.Command{
		Use:   "get <pr-id> <comment-id>",
		Short: "Get a pull request comment",
		Long:  "Get a single pull request comment by id. This is the authoritative single-comment view and is better suited than list output when you need the full rendered comment payload.",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := commentservice.NewService(client)
			comment, err := service.Get(cmd.Context(), commentservice.Target{
				Repository:    commentservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug},
				PullRequestID: args[0],
			}, args[1])
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": args[0], "comment": comment})
			}

			fmt.Fprintln(cmd.OutOrStdout(), formatCommentDetail(comment))
			return nil
		},
	}
	commentCmd.AddCommand(commentGetCmd)
	prCmd.AddCommand(commentCmd)

	activityCmd := &cobra.Command{
		Use:   "activity",
		Short: "Pull request activity commands",
		Long:  "Pull request activity commands. This is an explicit exception to the stable versioned API and is intended only for AI ingestion and debugging.",
	}

	var activityLimit int
	activityListCmd := &cobra.Command{
		Use:   "list <id>",
		Short: "List raw pull request activity items",
		Long:  "List raw pull request activity items. This output is an explicit exception to the stable versioned API and is intended only for AI ingestion and debugging.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, client, err := loadConfigAndClient()
			if err != nil {
				return err
			}

			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestactivityservice.NewService(client)
			activities, err := service.List(cmd.Context(), pullrequestactivityservice.RepositoryRef{ProjectKey: repo.ProjectKey, Slug: repo.Slug}, args[0], pullrequestactivityservice.ListOptions{Limit: activityLimit})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": args[0], "activities": activities})
			}

			if len(activities) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No activities found")
				return nil
			}

			for _, activity := range activities {
				fmt.Fprintln(cmd.OutOrStdout(), formatPullRequestActivitySummary(activity))
			}

			return nil
		},
	}
	activityListCmd.Flags().IntVar(&activityLimit, "limit", 25, "Page size for pull request activity list operations")
	activityCmd.AddCommand(activityListCmd)
	prCmd.AddCommand(activityCmd)

	taskCmd := &cobra.Command{Use: "task", Short: "Pull request task commands"}

	var taskState string
	var taskLimit int
	var taskStart int
	taskListCmd := &cobra.Command{
		Use:   "list <id>",
		Short: "List tasks for a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			tasks, err := service.ListTasks(cmd.Context(), repo, args[0], pullrequestservice.TaskListOptions{State: taskState, Limit: taskLimit, Start: taskStart})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": args[0], "tasks": tasks})
			}

			if len(tasks) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No tasks found")
				return nil
			}

			for _, task := range tasks {
				fmt.Fprintf(cmd.OutOrStdout(), "%d\t%s\t%s\n", task.ID, task.State, task.Text)
			}

			return nil
		},
	}
	taskListCmd.Flags().StringVar(&taskState, "state", "open", "Task state filter: open, resolved, all")
	taskListCmd.Flags().IntVar(&taskLimit, "limit", 25, "Page size for task list operations")
	taskListCmd.Flags().IntVar(&taskStart, "start", 0, "Start offset for task list operations")
	taskCmd.AddCommand(taskListCmd)

	var taskText string
	taskCreateCmd := &cobra.Command{
		Use:   "create <id>",
		Short: "Create a task on a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.task.create",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0], "text": taskText},
						Action:          "create",
						PredictedAction: "create",
						Supported:       true,
						Reason:          "pull request task will be created",
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request reference"},
					}},
					Summary: dryRunSummary{Total: 1, Supported: 1, CreateCount: 1},
				}
				return writeDryRunPreview(cmd.OutOrStdout(), options.JSON, preview)
			}
			task, err := service.CreateTask(cmd.Context(), repo, args[0], taskText)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": args[0], "task": task})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Created task %d\n", task.ID)
			return nil
		},
	}
	taskCreateCmd.Flags().StringVar(&taskText, "text", "", "Task text")
	_ = taskCreateCmd.MarkFlagRequired("text")
	taskCmd.AddCommand(taskCreateCmd)

	var taskID string
	var taskResolved bool
	var taskVersion int
	taskUpdateCmd := &cobra.Command{
		Use:   "update <id>",
		Short: "Update a task on a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			var resolved *bool
			if cmd.Flags().Changed("resolved") {
				resolved = &taskResolved
			}
			var version *int
			if cmd.Flags().Changed("version") {
				version = &taskVersion
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				tasks, err := service.ListTasks(cmd.Context(), repo, args[0], pullrequestservice.TaskListOptions{State: "all", Limit: 200, Start: 0})
				if err != nil {
					return err
				}

				predicted := "blocked"
				reason := "task was not found"
				blocking := []string{"task not found"}
				if existing, ok := findTask(tasks, taskID); ok {
					blocking = nil
					predicted = "update"
					reason = "task will be updated"
					if taskUpdateEquivalent(existing, taskText, resolved) {
						predicted = "no-op"
						reason = "task already matches requested values"
					}
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.task.update",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0], "task": taskID},
						Action:          "update",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request tasks list"},
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
			updated, err := service.UpdateTask(cmd.Context(), repo, args[0], taskID, taskText, resolved, version)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"repository": repo, "pull_request_id": args[0], "task": updated})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Updated task %d\n", updated.ID)
			return nil
		},
	}
	taskUpdateCmd.Flags().StringVar(&taskID, "task", "", "Task ID")
	taskUpdateCmd.Flags().StringVar(&taskText, "text", "", "Task text")
	taskUpdateCmd.Flags().BoolVar(&taskResolved, "resolved", false, "Mark task as resolved/unresolved")
	taskUpdateCmd.Flags().IntVar(&taskVersion, "version", 0, "Expected task version")
	_ = taskUpdateCmd.MarkFlagRequired("task")
	taskCmd.AddCommand(taskUpdateCmd)

	taskDeleteCmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "Delete a task from a pull request",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, apiClient, err := loadConfigAndClient()
			if err != nil {
				return err
			}
			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			var version *int
			if cmd.Flags().Changed("version") {
				version = &taskVersion
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			if options.DryRun {
				checker := options.permissionCheckerFor(apiClient)
				if err := checker.CheckRepoPermission(cmd.Context(), repo.ProjectKey, repo.Slug, openapigenerated.REPOREAD); err != nil {
					return err
				}

				tasks, err := service.ListTasks(cmd.Context(), repo, args[0], pullrequestservice.TaskListOptions{State: "all", Limit: 200, Start: 0})
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "task was not found"
				if _, ok := findTask(tasks, taskID); ok {
					predicted = "delete"
					reason = "task will be deleted"
				}

				preview := dryRunPreview{
					DryRun:       true,
					PlanningMode: planningModeStateful,
					Capability:   capabilityFull,
					Items: []dryRunItem{{
						Intent:          "pr.task.delete",
						Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "id": args[0], "task": taskID},
						Action:          "delete",
						PredictedAction: predicted,
						Supported:       true,
						Reason:          reason,
						Confidence:      capabilityFull,
						RequiredState:   []string{"pull request tasks list"},
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
			if err := service.DeleteTask(cmd.Context(), repo, args[0], taskID, version); err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{"status": "ok", "repository": repo, "pull_request_id": args[0], "task_id": taskID})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Deleted task %s\n", taskID)
			return nil
		},
	}
	taskDeleteCmd.Flags().StringVar(&taskID, "task", "", "Task ID")
	taskDeleteCmd.Flags().IntVar(&taskVersion, "version", 0, "Expected task version")
	_ = taskDeleteCmd.MarkFlagRequired("task")
	taskCmd.AddCommand(taskDeleteCmd)

	prCmd.AddCommand(taskCmd)

	// pr build
	var buildLimit int
	buildCmd := &cobra.Command{
		Use:   "build",
		Short: "Pull request build status commands",
	}
	buildCmd.PersistentFlags().IntVar(&buildLimit, "limit", 25, "Page size for build status results")

	buildStatusCmd := &cobra.Command{
		Use:   "status <id>",
		Short: "Show build statuses for a pull request's source commit",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			repo, err := resolvePullRequestRepositoryReference(repository, cfg)
			if err != nil {
				return err
			}

			service := pullrequestservice.NewService(httpclient.NewFromConfig(cfg))
			statuses, err := service.GetBuildStatuses(cmd.Context(), repo, args[0], buildLimit)
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), map[string]any{
					"repository":   repo,
					"pull_request": args[0],
					"statuses":     statuses,
				})
			}

			if len(statuses) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No build statuses found")
				return nil
			}

			for _, s := range statuses {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", s.Key, s.State, s.URL)
			}

			return nil
		},
	}
	buildCmd.AddCommand(buildStatusCmd)
	prCmd.AddCommand(buildCmd)

	return prCmd
}

func newAdminCommand(options *rootOptions) *cobra.Command {
	adminCmd := &cobra.Command{
		Use:   "admin",
		Short: "Local environment/admin commands",
	}

	adminCmd.AddCommand(&cobra.Command{
		Use:   "health",
		Short: "Check local stack health",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}

			client := httpclient.NewFromConfig(cfg)
			health, err := client.Health(cmd.Context())
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), health)
			}

			if health.Authenticated {
				fmt.Fprintf(cmd.OutOrStdout(), "Bitbucket health: OK (status=%d, auth=ok)\n", health.StatusCode)
				return nil
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Bitbucket health: OK (status=%d, auth=limited)\n", health.StatusCode)
			return nil
		},
	})

	return adminCmd
}

func hasApprovedReviewer(reviewers []pullrequestservice.Reviewer) bool {
	for _, reviewer := range reviewers {
		if reviewer.Approved || strings.EqualFold(strings.TrimSpace(reviewer.Status), "APPROVED") {
			return true
		}
	}

	return false
}

func reviewerApprovedByUser(reviewers []pullrequestservice.Reviewer, username string) bool {
	trimmed := strings.TrimSpace(username)
	if trimmed == "" {
		return false
	}
	for _, reviewer := range reviewers {
		if strings.EqualFold(strings.TrimSpace(reviewer.Name), trimmed) && (reviewer.Approved || strings.EqualFold(strings.TrimSpace(reviewer.Status), "APPROVED")) {
			return true
		}
	}
	return false
}

func hasReviewer(reviewers []pullrequestservice.Reviewer, username string) bool {
	trimmed := strings.TrimSpace(username)
	for _, reviewer := range reviewers {
		if strings.EqualFold(strings.TrimSpace(reviewer.Name), trimmed) {
			return true
		}
	}

	return false
}

func findTask(tasks []pullrequestservice.Task, taskID string) (pullrequestservice.Task, bool) {
	trimmed := strings.TrimSpace(taskID)
	for _, task := range tasks {
		if strings.TrimSpace(fmt.Sprintf("%d", task.ID)) == trimmed {
			return task, true
		}
	}

	return pullrequestservice.Task{}, false
}

func taskUpdateEquivalent(task pullrequestservice.Task, text string, resolved *bool) bool {
	if strings.TrimSpace(text) != "" && !strings.EqualFold(strings.TrimSpace(task.Text), strings.TrimSpace(text)) {
		return false
	}
	if resolved != nil && task.Resolved != *resolved {
		return false
	}

	return true
}
