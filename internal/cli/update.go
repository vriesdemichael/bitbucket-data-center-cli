package cli

import (
	"fmt"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	githubrelease "github.com/vriesdemichael/bitbucket-server-cli/internal/transport/githubrelease"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/network"
	updateworkflow "github.com/vriesdemichael/bitbucket-server-cli/internal/workflows/update"
)

const defaultUpdateRequestTimeout = 20 * time.Second

type updateCommandHTTPConfig struct {
	requestTimeout time.Duration
	tlsOptions     network.TLSOptions
}

var updateRunnerFactory = func(version string, httpConfig updateCommandHTTPConfig) *updateworkflow.Runner {
	transport, err := network.NewSafeTransport(httpConfig.tlsOptions)
	if err != nil {
		transport = &network.SafeTransport{}
	}

	client := githubrelease.NewClient(
		"https://api.github.com",
		&http.Client{Timeout: httpConfig.requestTimeout, Transport: transport},
		fmt.Sprintf("bb/%s", strings.TrimSpace(version)),
	)

	return updateworkflow.NewRunner(updateworkflow.Dependencies{
		Releases:        client,
		RepositoryOwner: "vriesdemichael",
		RepositoryName:  "bitbucket-data-center-cli",
		CurrentVersion:  func() string { return strings.TrimSpace(version) },
		ExecutablePath:  os.Executable,
		Platform:        func() (string, string) { return runtime.GOOS, runtime.GOARCH },
	})
}

func loadUpdateCommandHTTPConfig() (updateCommandHTTPConfig, error) {
	requestTimeout := defaultUpdateRequestTimeout
	if raw := strings.TrimSpace(os.Getenv("BB_REQUEST_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return updateCommandHTTPConfig{}, apperrors.New(apperrors.KindValidation, "BB_REQUEST_TIMEOUT must be a valid duration (example: 20s)", err)
		}
		if parsed <= 0 {
			return updateCommandHTTPConfig{}, apperrors.New(apperrors.KindValidation, "BB_REQUEST_TIMEOUT must be greater than 0", nil)
		}
		requestTimeout = parsed
	}

	insecureSkipVerify := false
	if raw := strings.TrimSpace(os.Getenv("BB_INSECURE_SKIP_VERIFY")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return updateCommandHTTPConfig{}, apperrors.New(apperrors.KindValidation, "BB_INSECURE_SKIP_VERIFY must be a boolean", err)
		}
		insecureSkipVerify = parsed
	}

	return updateCommandHTTPConfig{
		requestTimeout: requestTimeout,
		tlsOptions: network.TLSOptions{
			CAFile:             strings.TrimSpace(os.Getenv("BB_CA_FILE")),
			InsecureSkipVerify: insecureSkipVerify,
		},
	}, nil
}

func newUpdateCommand(options *rootOptions) *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Check for and install the latest bb release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if options == nil {
				return apperrors.New(apperrors.KindInternal, "update command options are not configured", nil)
			}

			httpConfig, err := loadUpdateCommandHTTPConfig()
			if err != nil {
				return err
			}

			runner := updateRunnerFactory(cmd.Root().Version, httpConfig)
			result, err := runner.Run(cmd.Context(), updateworkflow.Options{DryRun: options.DryRun})
			if err != nil {
				return err
			}

			if options.JSON {
				return writeJSON(cmd.OutOrStdout(), result)
			}

			writeUpdateHuman(cmd, result)
			return nil
		},
	}
}

func writeUpdateHuman(cmd *cobra.Command, result updateworkflow.Result) {
	if cmd == nil {
		return
	}

	writer := cmd.OutOrStdout()
	if result.DryRun {
		fmt.Fprintf(writer, "%s\n", style.DryRun.Render("Dry-run (static, capability=full)"))
	}

	switch {
	case result.UpToDate:
		fmt.Fprintf(writer, "%s %s\n", style.Success.Render("bb is up to date"), style.Resource.Render(result.CurrentVersion))
	case result.Scheduled:
		fmt.Fprintf(writer, "%s %s %s %s\n", style.Updated.Render("Scheduled bb update"), style.Secondary.Render(result.CurrentVersion), style.Secondary.Render("->"), style.Resource.Render(result.LatestVersion))
	case result.Staged:
		fmt.Fprintf(writer, "%s %s %s %s\n", style.Updated.Render("Staged bb update"), style.Secondary.Render(result.CurrentVersion), style.Secondary.Render("->"), style.Resource.Render(result.LatestVersion))
	case result.Applied:
		fmt.Fprintf(writer, "%s %s %s %s\n", style.Updated.Render("Updated bb"), style.Secondary.Render(result.CurrentVersion), style.Secondary.Render("->"), style.Resource.Render(result.LatestVersion))
	case result.UpdateAvailable:
		fmt.Fprintf(writer, "%s %s %s %s\n", style.Warning.Render("Update available"), style.Secondary.Render(result.CurrentVersion), style.Secondary.Render("->"), style.Resource.Render(result.LatestVersion))
	default:
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("Current version"), style.Resource.Render(result.CurrentVersion))
	}

	if result.AssetName != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("artifact"), result.AssetName)
	}
	if result.InstallPath != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("install_path"), result.InstallPath)
	}
	if result.StagedPath != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("staged_path"), result.StagedPath)
	}
	if result.SwapResultPath != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("swap_result_path"), result.SwapResultPath)
	}
	if result.ChecksumAssetName != "" {
		status := "available"
		if result.ChecksumVerified {
			status = "verified"
		}
		fmt.Fprintf(writer, "%s %s (%s)\n", style.Secondary.Render("checksum"), result.ChecksumAssetName, status)
	}
	if result.SignatureBundleAssetName != "" {
		status := "available"
		if result.SignatureVerified {
			status = "verified via sigstore keyless + rekor"
		}
		fmt.Fprintf(writer, "%s %s (%s)\n", style.Secondary.Render("provenance"), result.SignatureBundleAssetName, status)
	}
	if result.SignatureIdentity != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("signed_by"), result.SignatureIdentity)
	}
	if result.ReleaseURL != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("release"), result.ReleaseURL)
	}
	if result.PlannedAction != "" && (result.DryRun || result.Staged || result.Scheduled) {
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("planned_action"), result.PlannedAction)
	}
}
