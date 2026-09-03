package insightscmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/dryrunpreview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/paging"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/preflight"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/reposel"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
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

func safeStringFromInsightResult(result *openapigenerated.RestInsightReportResult) string {
	if result == nil {
		return ""
	}
	return string(*result)
}

func New(deps Dependencies) *cobra.Command {
	d := deps.withDefaults()

	var repositorySelector string
	var reportPaging paging.Options

	insightsCmd := &cobra.Command{
		Use:   "insights",
		Short: "Code Insights report and annotation commands",
	}
	insightsCmd.PersistentFlags().StringVar(&repositorySelector, "repo", "", "Repository as PROJECT/slug (defaults to BITBUCKET_PROJECT_KEY + BITBUCKET_REPO_SLUG)")

	reportCmd := &cobra.Command{
		Use:   "report",
		Short: "Code Insights report commands",
	}
	reportPaging.RegisterPersistent(reportCmd, 25)

	var reportBody string
	setReportCmd := &cobra.Command{
		Use:   "set <commit> <key>",
		Short: "Create or update a Code Insights report",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			request := openapigenerated.SetACodeInsightsReportJSONRequestBody{}
			if err := json.Unmarshal([]byte(reportBody), &request); err != nil {
				return apperrors.New(apperrors.KindValidation, "invalid JSON for --body", err)
			}

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
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

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "insights.report.set",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": args[1]},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Supported:       true,
					Reason:          reason,
					RequiredState:   []string{"insights report get"},
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			report, err := service.SetReport(cmd.Context(), repo, args[0], args[1], request)
			if err != nil {
				return err
			}

			return d.WriteJSON(cmd.OutOrStdout(), reportFrom(report))
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
			repo, service, _, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			report, err := service.GetReport(cmd.Context(), repo, args[0], args[1])
			if err != nil {
				return err
			}

			return d.WriteJSON(cmd.OutOrStdout(), reportFrom(report))
		},
	})

	reportCmd.AddCommand(&cobra.Command{
		Use:   "delete <commit> <key>",
		Short: "Delete a Code Insights report",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
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

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "insights.report.delete",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": args[1]},
					Action:          "delete",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Supported:       true,
					Reason:          reason,
					RequiredState:   []string{"insights report get"},
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.DeleteReport(cmd.Context(), repo, args[0], args[1]); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), ReportChange{Status: result.OK(), Commit: args[0], Key: args[1]})
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
			repo, service, _, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			reports, err := service.ListReports(cmd.Context(), repo, args[0], reportPaging.ServiceLimit())
			if err != nil {
				return err
			}

			// The backing service reads to exhaustion, so --limit only sized the
			// pages until now. A no-op under --all.
			reports = paging.Truncate(reportPaging, reports)

			if d.JSONEnabled() {
				return d.WriteJSONList(cmd.OutOrStdout(), reportsFrom(reports), paging.LimitReached(reportPaging, len(reports)))
			}

			if len(reports) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No reports found")
				return nil
			}

			for _, report := range reports {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", safederef.String(report.Key), safederef.String(report.Title), safeStringFromInsightResult(report.Result))
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
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			annotations := make([]openapigenerated.RestSingleAddInsightAnnotationRequest, 0)
			if err := json.Unmarshal([]byte(annotationBody), &annotations); err != nil {
				return apperrors.New(apperrors.KindValidation, "invalid JSON for --body (expected array of annotations)", err)
			}

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityPartial, dryrunpreview.Item{
					Intent:          "insights.annotation.add",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": args[1], "count": len(annotations)},
					Action:          "create",
					PredictedAction: "create",
					Supported:       true,
					Reason:          "insights annotations will be added",
					RequiredState:   []string{"insights report context"},
				})
				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.AddAnnotations(cmd.Context(), repo, args[0], args[1], annotations); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), AnnotationsAdded{Status: result.OK(), Count: len(annotations)})
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Added %d annotations to report %s\n", len(annotations), args[1])
			return nil
		},
	}
	addAnnotationCmd.Flags().StringVar(&annotationBody, "body", "", "Raw JSON array payload for annotations")
	_ = addAnnotationCmd.MarkFlagRequired("body")
	annotationCmd.AddCommand(addAnnotationCmd)

	annotationCmd.AddCommand(&cobra.Command{
		Use:   "list <commit> [key]",
		Short: "List annotations for a Code Insights report or commit",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, _, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			var annotations []openapigenerated.RestInsightAnnotation
			if len(args) == 2 {
				annotations, err = service.ListAnnotations(cmd.Context(), repo, args[0], args[1])
			} else {
				annotations, err = service.ListCommitAnnotations(cmd.Context(), repo, args[0], openapigenerated.GetAnnotations1Params{})
			}
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSONList(cmd.OutOrStdout(), annotationsFrom(annotations), paging.LimitReached(reportPaging, len(annotations)))
			}

			if len(annotations) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No annotations found")
				return nil
			}

			for _, annotation := range annotations {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", safederef.String(annotation.ExternalId), safederef.String(annotation.Severity), safederef.String(annotation.Message))
			}

			return nil
		},
	})

	var setAnnMessage string
	var setAnnSeverity string
	var setAnnPath string
	var setAnnLine int32
	var setAnnLink string
	var setAnnType string

	setAnnotationCmd := &cobra.Command{
		Use:   "set <commit> <key> <external-id>",
		Short: "Create or replace a Code Insights report annotation",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			request := openapigenerated.RestSingleAddInsightAnnotationRequest{
				Message:  setAnnMessage,
				Severity: setAnnSeverity,
			}
			if cmd.Flags().Changed("path") {
				request.Path = &setAnnPath
			}
			if cmd.Flags().Changed("line") {
				request.Line = &setAnnLine
			}
			if cmd.Flags().Changed("link") {
				request.Link = &setAnnLink
			}
			if cmd.Flags().Changed("type") {
				request.Type = &setAnnType
			}

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				annotations, err := service.ListAnnotations(cmd.Context(), repo, args[0], args[1])
				predicted := "create"
				reason := "insights annotation will be created"
				if err == nil {
					for _, annotation := range annotations {
						if strings.EqualFold(strings.TrimSpace(safederef.String(annotation.ExternalId)), strings.TrimSpace(args[2])) {
							predicted = "update"
							reason = "insights annotation will be updated"
							break
						}
					}
				} else if apperrors.ExitCode(err) != 4 {
					return err
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityFull, dryrunpreview.Item{
					Intent:          "insights.annotation.set",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": args[1], "externalId": args[2]},
					Action:          "update",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Supported:       true,
					Reason:          reason,
					RequiredState:   []string{"insights annotations list"},
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			ann, err := service.SetAnnotation(cmd.Context(), repo, args[0], args[1], args[2], request)
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), annotationFrom(ann))
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Annotation %s set on report %s for commit %s\n", args[2], args[1], args[0])
			return nil
		},
	}

	setAnnotationCmd.Flags().StringVar(&setAnnMessage, "message", "", "Annotation message")
	enumflag.Register(setAnnotationCmd.Flags(), &setAnnSeverity, "severity", "", []string{"LOW", "MEDIUM", "HIGH"}, "Annotation severity")
	setAnnotationCmd.Flags().StringVar(&setAnnPath, "path", "", "File path containing the annotation")
	setAnnotationCmd.Flags().Int32Var(&setAnnLine, "line", 0, "Line number containing the annotation")
	setAnnotationCmd.Flags().StringVar(&setAnnLink, "link", "", "Link associated with the annotation")
	enumflag.Register(setAnnotationCmd.Flags(), &setAnnType, "type", "", []string{"BUG", "CODE_SMELL", "VULNERABILITY"}, "Annotation type")

	_ = setAnnotationCmd.MarkFlagRequired("message")
	_ = setAnnotationCmd.MarkFlagRequired("severity")

	annotationCmd.AddCommand(setAnnotationCmd)

	var externalID string
	deleteAnnotationCmd := &cobra.Command{
		Use:   "delete <commit> <key>",
		Short: "Delete annotation(s) by external id for a report",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			repo, service, client, err := resolveQualityRepoServiceAndClient(repositorySelector, d)
			if err != nil {
				return err
			}

			if d.DryRunEnabled() {
				if err := preflight.RepoPermission(cmd.Context(), d.PermissionChecker, client, repo.ProjectKey, repo.Slug, openapi.RepoWrite); err != nil {
					return err
				}

				annotations, err := service.ListAnnotations(cmd.Context(), repo, args[0], args[1])
				if err != nil {
					return err
				}

				predicted := "no-op"
				reason := "no matching annotation found"
				for _, annotation := range annotations {
					if strings.EqualFold(strings.TrimSpace(safederef.String(annotation.ExternalId)), strings.TrimSpace(externalID)) {
						predicted = "delete"
						reason = "annotation will be deleted"
						break
					}
				}

				preview := dryrunpreview.New(dryrunpreview.PlanningModeStateful, dryrunpreview.CapabilityPartial, dryrunpreview.Item{
					Intent:          "insights.annotation.delete",
					Target:          map[string]any{"repository": fmt.Sprintf("%s/%s", repo.ProjectKey, repo.Slug), "commit": args[0], "key": args[1], "externalId": externalID},
					Action:          "delete",
					PredictedAction: predicted,
					Tier:            dryrunpreview.TierPreconditionsChecked,
					Supported:       true,
					Reason:          reason,
					RequiredState:   []string{"insights annotations list"},
				})

				return dryrunpreview.Write(cmd.OutOrStdout(), d.JSONEnabled(), preview)
			}

			if err := service.DeleteAnnotations(cmd.Context(), repo, args[0], args[1], externalID); err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), AnnotationChange{Status: result.OK(), ExternalID: externalID})
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
