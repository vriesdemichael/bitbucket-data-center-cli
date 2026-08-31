package updatecmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	githubrelease "github.com/vriesdemichael/bitbucket-server-cli/internal/transport/githubrelease"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/transport/network"
	updatesigstore "github.com/vriesdemichael/bitbucket-server-cli/internal/transport/sigstore"
	updateworkflow "github.com/vriesdemichael/bitbucket-server-cli/internal/workflows/update"
)

const (
	defaultUpdateRequestTimeout = 20 * time.Second
	repositoryOwner             = "vriesdemichael"
	repositoryName              = "bitbucket-data-center-cli"
)

type UpdateCommandHTTPConfig struct {
	RequestTimeout time.Duration
	TLSOptions     network.TLSOptions
	UpdateBaseURL  string
	// Trust carries the administrative policy that decides who may vouch for
	// the binary this command is about to install, and where the Sigstore trust
	// material backing that decision comes from.
	Trust config.UpdateTrust
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

	httpClient := &http.Client{Timeout: httpConfig.RequestTimeout, Transport: transport}
	client := githubrelease.NewClient(
		baseURL,
		httpClient,
		fmt.Sprintf("bb/%s", strings.TrimSpace(version)),
	)

	trust := httpConfig.Trust
	verifier := updatesigstore.NewReleaseVerifier(updatesigstore.ReleaseVerifierOptions{
		Owner:            repositoryOwner,
		Repo:             repositoryName,
		TrustedRootPath:  trust.TrustedRootPath,
		TUFRepositoryURL: trust.TUFRepositoryURL,
		ExpectedIdentity: trust.SignatureIdentity,
		ExpectedIssuer:   trust.SignatureIssuer,
		HTTPClient:       httpClient,
	})

	return updateworkflow.NewRunner(updateworkflow.Dependencies{
		Releases:                  client,
		RepositoryOwner:           repositoryOwner,
		RepositoryName:            repositoryName,
		CurrentVersion:            func() string { return strings.TrimSpace(version) },
		ExecutablePath:            os.Executable,
		Platform:                  func() (string, string) { return runtime.GOOS, runtime.GOARCH },
		Verifier:                  verifier,
		SkipSignatureVerification: trust.AllowUnverified,
		TrustSource:               trustSourceDescription(trust),
	})
}

// trustSourceDescription names where trust material comes from, for the result
// and for `--dry-run` output. An operator deploying an offline trust root needs
// to be able to confirm it is actually the one in use.
func trustSourceDescription(trust config.UpdateTrust) string {
	switch {
	case trust.AllowUnverified:
		return "none (signature verification disabled by administrative policy)"
	case trust.TrustedRootPath != "":
		return fmt.Sprintf("trusted root file %s", trust.TrustedRootPath)
	case trust.TUFRepositoryURL != "":
		return fmt.Sprintf("mirrored Sigstore TUF repository %s", trust.TUFRepositoryURL)
	default:
		return "public Sigstore TUF repository"
	}
}

// LoadUpdateCommandHTTPConfig resolves the transport settings for the update
// path, honouring the global flags.
//
// overrides carries them. This command runs without a configured Bitbucket
// host, so it cannot go through LoadWithOverrides, and it used to read BB_*
// itself -- which worked only while flags were written into those variables.
func LoadUpdateCommandHTTPConfig(overrides config.Overrides, optionalBaseURL ...string) (UpdateCommandHTTPConfig, error) {
	requestTimeout, err := config.ResolveRequestTimeoutWith(overrides, defaultUpdateRequestTimeout)
	if err != nil {
		return UpdateCommandHTTPConfig{}, err
	}

	// The update path downloads and then executes a new binary, so it resolves
	// TLS through the same policy-aware helper the API client uses rather than
	// reading BB_* variables directly (issue #448).
	tlsSettings, err := config.ResolveTLSSettingsWith(overrides)
	if err != nil {
		return UpdateCommandHTTPConfig{}, err
	}

	flagVal := ""
	if len(optionalBaseURL) > 0 {
		flagVal = optionalBaseURL[0]
	}
	baseURL, err := config.ResolveUpdateBaseURL(flagVal)
	if err != nil {
		return UpdateCommandHTTPConfig{}, err
	}

	trust, err := config.ResolveUpdateTrust()
	if err != nil {
		return UpdateCommandHTTPConfig{}, err
	}

	return UpdateCommandHTTPConfig{
		RequestTimeout: requestTimeout,
		Trust:          trust,
		TLSOptions: network.TLSOptions{
			CAFile:             tlsSettings.CAFile,
			InsecureSkipVerify: tlsSettings.InsecureSkipVerify,
			ClientCertFile:     tlsSettings.ClientCertFile,
			ClientKeyFile:      tlsSettings.ClientKeyFile,
		},
		UpdateBaseURL: baseURL,
	}, nil
}

type Dependencies struct {
	JSONEnabled   func() bool
	DryRunEnabled func() bool
	WriteJSON     func(io.Writer, any) error
	// RuntimeOverrides carries the global flags. This command has no Bitbucket
	// host and so no config load to inherit them from; without it, --ca-file and
	// --request-timeout stop reaching the path that downloads a binary.
	RuntimeOverrides func() config.Overrides
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

			httpConfig, err := LoadUpdateCommandHTTPConfig(d.runtimeOverrides(), baseURL)
			if err != nil {
				return err
			}

			runner := UpdateRunnerFactory(cmd.Root().Version, httpConfig)
			result, err := runner.Run(cmd.Context(), updateworkflow.Options{DryRun: d.DryRunEnabled()})
			if err != nil {
				return err
			}

			// Warned on stderr in both output modes, and on every run rather
			// than once at configuration time: an unverified update path is a
			// standing condition, and the person running the command is the one
			// who needs to know the binary was not authenticated.
			if result.SignatureSkipped {
				fmt.Fprintln(cmd.ErrOrStderr(), style.Warning.Render(
					"Warning: release signature verification is disabled by administrative policy (allow_unverified_update); this release was checked against its SHA256 checksum only, which detects corruption but not tampering",
				))
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

	writeVerificationDetail(writer, result)
}

// writeVerificationDetail reports what verification did on a dry run.
//
// A dry run is the only way to exercise a mirror without replacing a binary, so
// it is where an operator finds out whether their offline trust root is in use
// and whether the manifest actually verifies against it. A failure aborts with
// its own message; this covers the case where everything passed and the
// operator still needs to see which trust material was used.
func writeVerificationDetail(writer io.Writer, result updateworkflow.Result) {
	if !result.DryRun || !result.UpdateAvailable {
		return
	}

	if result.TrustSource != "" {
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("Trust material"), result.TrustSource)
	}

	switch {
	case result.SignatureSkipped:
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("Signature"), style.Warning.Render("not verified (allow_unverified_update)"))
	case result.SignatureVerified:
		fmt.Fprintf(writer, "%s %s %s\n", style.Secondary.Render("Signature"), style.Success.Render("verified"), style.Secondary.Render(result.SignatureIdentity))
	}

	if result.ChecksumAvailable {
		fmt.Fprintf(writer, "%s %s\n", style.Secondary.Render("Checksum"), style.Success.Render(fmt.Sprintf("entry present for %s", result.AssetName)))
	}
}

// runtimeOverrides is the global flags, or none when the caller wired nothing.
func (d Dependencies) runtimeOverrides() config.Overrides {
	if d.RuntimeOverrides == nil {
		return config.Overrides{}
	}
	return d.RuntimeOverrides()
}
