package bulkcmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/openapi"
	qualityservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/quality"
	reposettings "github.com/vriesdemichael/bitbucket-server-cli/internal/services/reposettings"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/services/repository"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/httpclient"
	bulkworkflow "github.com/vriesdemichael/bitbucket-server-cli/internal/workflows/bulk"
)

type Dependencies struct {
	JSONEnabled func() bool
	LoadConfig  func() (config.AppConfig, error)
	WriteJSON   func(io.Writer, any) error
}

func New(deps Dependencies) *cobra.Command {
	if deps.LoadConfig == nil {
		deps.LoadConfig = config.LoadFromEnv
	}
	if deps.WriteJSON == nil {
		deps.WriteJSON = jsonoutput.Write
	}

	isJSON := func() bool {
		if deps.JSONEnabled == nil {
			return false
		}
		return deps.JSONEnabled()
	}

	bulkCmd := &cobra.Command{
		Use:           "bulk",
		Short:         "Plan and apply multi-repository policies",
		SilenceErrors: true,
		SilenceUsage:  true,
	}

	var policyFile string
	var planOutputFile string
	planCmd := &cobra.Command{
		Use:   "plan",
		Short: "Resolve a bulk policy into a deterministic reviewed plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			rawPolicy, err := readFile(policyFile, "bulk policy file")
			if err != nil {
				return err
			}

			policy, err := bulkworkflow.ParsePolicyYAML(rawPolicy)
			if err != nil {
				return err
			}

			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			planner := bulkworkflow.NewPlanner(repository.NewService(httpclient.NewFromConfig(cfg)))
			plan, err := planner.Plan(cmd.Context(), policy)
			if err != nil {
				return err
			}

			if strings.TrimSpace(planOutputFile) != "" {
				if err := writeJSONFile(planOutputFile, plan); err != nil {
					return err
				}
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), plan)
			}

			writePlanHuman(cmd.OutOrStdout(), plan, planOutputFile)
			return nil
		},
	}
	planCmd.Flags().StringVarP(&policyFile, "file", "f", "", "Path to bulk policy YAML file")
	planCmd.Flags().StringVarP(&planOutputFile, "output", "o", "", "Optional path to write the reviewed plan JSON artifact")
	_ = planCmd.MarkFlagRequired("file")
	bulkCmd.AddCommand(planCmd)

	var planInputFile string
	applyCmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply operations from a reviewed bulk plan",
		RunE: func(cmd *cobra.Command, args []string) error {
			rawPlan, err := readFile(planInputFile, "bulk plan file")
			if err != nil {
				return err
			}

			plan, err := bulkworkflow.LoadPlanJSON(rawPlan)
			if err != nil {
				return err
			}

			cfg, err := deps.LoadConfig()
			if err != nil {
				return err
			}

			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			if err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to initialize API client", err)
			}

			statusDir, err := statusStoreDir()
			if err != nil {
				return err
			}

			executor := bulkworkflow.NewExecutor(
				bulkworkflow.NewServiceRunner(reposettings.NewService(client), qualityservice.NewService(client)),
				bulkworkflow.NewStatusStore(statusDir),
			)

			status, applyErr := executor.Apply(cmd.Context(), plan)
			if applyErr == nil {
				applyErr = applyFailureError(status)
			}

			// The id goes on the error as a field, not only into its sentence.
			// Under --json the failure envelope is the only document, so this
			// is the caller's sole handle on the artifact -- and telling them
			// to find it in the message is the scraping the machine contract
			// exists to end (#474, ADR-075).
			if applyErr != nil && status.OperationID != "" {
				applyErr = apperrors.WithDetail(applyErr, "operation_id", status.OperationID)
			}

			// Human output prints the status whatever happened. The error line
			// goes to stderr, so the two do not collide, and the reader gets
			// the detail without needing a handle to fetch it with.
			//
			// An empty operation id means Apply refused the plan before
			// starting: there is no run to report, only the error.
			if !isJSON() {
				if status.OperationID != "" {
					writeStatusHuman(cmd.OutOrStdout(), status)
				}
				return applyErr
			}

			// Machine output gets exactly one document (ADR-075). Printing the
			// status and then returning put a second envelope after it, from
			// cmd/bb: a strict decoder rejects that outright, and jq -- which
			// reads a value stream -- quietly emits a result per document and
			// exits 0, so a script taking the last line gets the error
			// envelope's null. The failure envelope wins, and carries the
			// operation id in error.details, so the artifact is one
			// `bb bulk status <id> --json` away.
			if applyErr != nil {
				return applyErr
			}

			return deps.WriteJSON(cmd.OutOrStdout(), status)
		},
	}
	applyCmd.Flags().StringVar(&planInputFile, "from-plan", "", "Path to reviewed bulk plan JSON file")
	_ = applyCmd.MarkFlagRequired("from-plan")
	bulkCmd.AddCommand(applyCmd)

	statusCmd := &cobra.Command{
		Use:   "status <operation-id>",
		Short: "Show the saved status for a prior bulk apply operation",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			statusDir, err := statusStoreDir()
			if err != nil {
				return err
			}

			status, err := bulkworkflow.NewStatusStore(statusDir).Load(args[0])
			if err != nil {
				return err
			}

			if isJSON() {
				return deps.WriteJSON(cmd.OutOrStdout(), status)
			}

			writeStatusHuman(cmd.OutOrStdout(), status)
			return nil
		},
	}
	bulkCmd.AddCommand(statusCmd)

	return bulkCmd
}

func readFile(filePath string, label string) ([]byte, error) {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return nil, apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s path is required", label), nil)
	}

	raw, err := os.ReadFile(trimmedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, apperrors.New(apperrors.KindNotFound, fmt.Sprintf("%s not found: %s", label, trimmedPath), err)
		}
		return nil, apperrors.New(apperrors.KindInternal, fmt.Sprintf("failed to read %s", label), err)
	}

	return raw, nil
}

func writeJSONFile(filePath string, payload any) error {
	trimmedPath := strings.TrimSpace(filePath)
	if trimmedPath == "" {
		return apperrors.New(apperrors.KindValidation, "plan output path is required", nil)
	}

	if err := os.MkdirAll(filepath.Dir(trimmedPath), 0o700); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to create bulk plan output directory", err)
	}

	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to encode bulk plan artifact", err)
	}

	if err := os.WriteFile(trimmedPath, encoded, 0o600); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to write bulk plan artifact", err)
	}

	return nil
}

func statusStoreDir() (string, error) {
	if custom := strings.TrimSpace(os.Getenv("BB_BULK_STATUS_DIR")); custom != "" {
		return custom, nil
	}

	configPath, err := config.ConfigPath()
	if err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to resolve bulk status directory", err)
	}

	return filepath.Join(filepath.Dir(configPath), "bulk-status"), nil
}

func writePlanHuman(writer io.Writer, plan bulkworkflow.Plan, outputFile string) {
	fmt.Fprintf(writer, "%s %d target(s), %d operation(s), %s=%s\n",
		style.Label.Render("Bulk plan ready:"),
		plan.Summary.TargetCount,
		plan.Summary.OperationCount,
		style.Secondary.Render("hash"),
		style.Secondary.Render(plan.PlanHash),
	)
	if strings.TrimSpace(outputFile) != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Hint.Render("Plan artifact:"), strings.TrimSpace(outputFile))
	}
	for _, target := range plan.Targets {
		fmt.Fprintln(writer, style.Resource.Render(target.Repository.ProjectKey+"/"+target.Repository.Slug))
		for _, operation := range target.Operations {
			fmt.Fprintf(writer, "  - %s\n", bulkworkflow.DescribeOperation(operation))
		}
	}
}

func writeStatusHuman(writer io.Writer, status bulkworkflow.ApplyStatus) {
	fmt.Fprintf(writer, "%s %s: %s\n",
		style.Label.Render("Bulk apply"),
		style.Secondary.Render(status.OperationID),
		style.ActionStyle(status.Status).Render(status.Status),
	)
	fmt.Fprintf(writer, "%s %s\n", style.Label.Render("Plan hash:"), style.Secondary.Render(status.PlanHash))
	fmt.Fprintf(writer,
		"%s total=%d %s=%d %s=%d\n",
		style.Label.Render("Targets:"),
		status.Summary.TargetCount,
		style.Success.Render("successful"),
		status.Summary.SuccessfulTargets,
		style.Deleted.Render("failed"),
		status.Summary.FailedTargets,
	)
	fmt.Fprintf(writer,
		"%s total=%d %s=%d %s=%d skipped=%d\n",
		style.Label.Render("Operations:"),
		status.Summary.OperationCount,
		style.Success.Render("successful"),
		status.Summary.SuccessfulOperations,
		style.Deleted.Render("failed"),
		status.Summary.FailedOperations,
		status.Summary.SkippedOperations,
	)
	for _, target := range status.Targets {
		fmt.Fprintf(writer, "%s  %s\n",
			style.Resource.Render(target.Repository.ProjectKey+"/"+target.Repository.Slug),
			style.ActionStyle(target.Status).Render(target.Status),
		)
		for _, operation := range target.Operations {
			if strings.TrimSpace(operation.Error) == "" {
				fmt.Fprintf(writer, "  - %s  %s\n", style.ActionStyle(operation.Status).Render(operation.Status), operation.Type)
				continue
			}
			fmt.Fprintf(writer, "  - %s  %s  %s\n", style.ActionStyle(operation.Status).Render(operation.Status), operation.Type, style.Deleted.Render(operation.Error))
		}
	}
	fmt.Fprintf(writer, "%s\n", style.Hint.Render("Inspect saved status with: bb bulk status "+status.OperationID))
}

func applyFailureError(status bulkworkflow.ApplyStatus) error {
	if !status.HasFailures() {
		return nil
	}

	kind := apperrors.KindConflict
	for _, target := range status.Targets {
		for _, operation := range target.Operations {
			if operation.Status != "failed" {
				continue
			}
			kind = parseErrorKind(operation.ErrorKind)
			if kind == "" {
				kind = apperrors.KindConflict
			}
			return apperrors.New(kind, fmt.Sprintf("bulk apply %s completed with failures", status.OperationID), nil)
		}
	}

	return apperrors.New(kind, fmt.Sprintf("bulk apply %s completed with failures", status.OperationID), nil)
}

func parseErrorKind(value string) apperrors.Kind {
	switch strings.TrimSpace(value) {
	case string(apperrors.KindAuthentication):
		return apperrors.KindAuthentication
	case string(apperrors.KindAuthorization):
		return apperrors.KindAuthorization
	case string(apperrors.KindValidation):
		return apperrors.KindValidation
	case string(apperrors.KindNotFound):
		return apperrors.KindNotFound
	case string(apperrors.KindConflict):
		return apperrors.KindConflict
	case string(apperrors.KindTransient):
		return apperrors.KindTransient
	case string(apperrors.KindPermanent):
		return apperrors.KindPermanent
	case string(apperrors.KindNotImplemented):
		return apperrors.KindNotImplemented
	case string(apperrors.KindInternal):
		return apperrors.KindInternal
	default:
		return ""
	}
}
