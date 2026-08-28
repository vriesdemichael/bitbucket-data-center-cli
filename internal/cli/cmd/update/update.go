package updatecmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	githubrelease "github.com/vriesdemichael/bitbucket-server-cli/internal/transport/githubrelease"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/network"
	updateworkflow "github.com/vriesdemichael/bitbucket-server-cli/internal/workflows/update"
)

const defaultUpdateRequestTimeout = 20 * time.Second

type UpdateCommandHTTPConfig struct {
	RequestTimeout time.Duration
	TLSOptions     network.TLSOptions
	UpdateBaseURL  string
}

var UpdateRunnerFactory = func(version string, httpConfig UpdateCommandHTTPConfig) *updateworkflow.Runner {
	transport, err := network.NewSafeTransport(httpConfig.TLSOptions)
	if err != nil {
		transport = &network.SafeTransport{}
	}

	baseURL := strings.TrimSpace(httpConfig.UpdateBaseURL)
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	client := githubrelease.NewClient(
		baseURL,
		&http.Client{Timeout: httpConfig.RequestTimeout, Transport: transport},
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

func LoadUpdateCommandHTTPConfig(optionalBaseURL ...string) (UpdateCommandHTTPConfig, error) {
	requestTimeout := defaultUpdateRequestTimeout
	if raw := strings.TrimSpace(os.Getenv("BB_REQUEST_TIMEOUT")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return UpdateCommandHTTPConfig{}, apperrors.New(apperrors.KindValidation, "BB_REQUEST_TIMEOUT must be a valid duration (example: 20s)", err)
		}
		if parsed <= 0 {
			return UpdateCommandHTTPConfig{}, apperrors.New(apperrors.KindValidation, "BB_REQUEST_TIMEOUT must be greater than 0", nil)
		}
		requestTimeout = parsed
	}

	insecureSkipVerify := false
	if raw := strings.TrimSpace(os.Getenv("BB_INSECURE_SKIP_VERIFY")); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			return UpdateCommandHTTPConfig{}, apperrors.New(apperrors.KindValidation, "BB_INSECURE_SKIP_VERIFY must be a boolean", err)
		}
		insecureSkipVerify = parsed
	}

	flagVal := ""
	if len(optionalBaseURL) > 0 {
		flagVal = optionalBaseURL[0]
	}
	baseURL, err := config.ResolveUpdateBaseURL(flagVal)
	if err != nil {
		return UpdateCommandHTTPConfig{}, err
	}

	return UpdateCommandHTTPConfig{
		RequestTimeout: requestTimeout,
		TLSOptions: network.TLSOptions{
			CAFile:             strings.TrimSpace(os.Getenv("BB_CA_FILE")),
			InsecureSkipVerify: insecureSkipVerify,
		},
		UpdateBaseURL: baseURL,
	}, nil
}

type Dependencies struct {
	JSONEnabled   func() bool
	DryRunEnabled func() bool
	WriteJSON     func(io.Writer, any) error
}

func (d Dependencies) withDefaults() Dependencies {
	if d.JSONEnabled == nil {
		d.JSONEnabled = func() bool { return false }
	}
	if d.DryRunEnabled == nil {
		d.DryRunEnabled = func() bool { return false }
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
	var baseURL string

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install the latest bb release",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if BuildDisablesSelfUpdate {
				return apperrors.New(apperrors.KindAuthorization, "self-update is disabled in this build; update bb using your system package manager", nil)
			}

			if disabled, msg, err := config.IsUpdateDisabled(); err != nil {
				return err
			} else if disabled {
				return apperrors.New(apperrors.KindAuthorization, msg, nil)
			}

			httpConfig, err := LoadUpdateCommandHTTPConfig(baseURL)
			if err != nil {
				return err
			}

			runner := UpdateRunnerFactory(cmd.Root().Version, httpConfig)
			result, err := runner.Run(cmd.Context(), updateworkflow.Options{DryRun: d.DryRunEnabled()})
			if err != nil {
				return err
			}

			if d.JSONEnabled() {
				return d.WriteJSON(cmd.OutOrStdout(), result)
			}

			writeUpdateHuman(cmd, result)
			return nil
		},
	}

	cmd.Flags().StringVar(&baseURL, "base-url", "", "Custom release mirror base URL")

	return cmd
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
		fmt.Fprintf(writer, "%s %s %s %s\n", style.Success.Render("Scheduled bb update"), style.Secondary.Render(result.CurrentVersion), style.Secondary.Render("->"), style.Resource.Render(result.LatestVersion))
	case result.Staged:
		fmt.Fprintf(writer, "%s %s %s %s\n", style.Success.Render("Staged bb update"), style.Secondary.Render(result.CurrentVersion), style.Secondary.Render("->"), style.Resource.Render(result.LatestVersion))
	case result.Applied:
		fmt.Fprintf(writer, "%s %s %s %s\n", style.Success.Render("Updated bb"), style.Secondary.Render(result.CurrentVersion), style.Secondary.Render("->"), style.Resource.Render(result.LatestVersion))
	case result.UpdateAvailable:
		fmt.Fprintf(writer, "%s %s %s %s\n", style.Warning.Render("Update available"), style.Secondary.Render(result.CurrentVersion), style.Secondary.Render("->"), style.Resource.Render(result.LatestVersion))
	default:
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("Current version"), style.Resource.Render(result.CurrentVersion))
	}
}
