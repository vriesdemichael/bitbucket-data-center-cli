package updatecmd

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	githubrelease "github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/githubrelease"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/network"
	updatesigstore "github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/sigstore"
	updateworkflow "github.com/vriesdemichael/bitbucket-data-center-cli/internal/workflows/update"
)

const (
	defaultUpdateRequestTimeout = 20 * time.Second
	repositoryOwner             = "vriesdemichael"
	repositoryName              = "bitbucket-data-center-cli"
)

type UpdateCommandHTTPConfig struct {
	RequestTimeout time.Duration
	// RetryCount and RetryBackoff are retry_count and retry_backoff: how often
	// a download that failed in transit is tried again, and the wait between.
	RetryCount    int
	RetryBackoff  time.Duration
	TLSOptions    network.TLSOptions
	UpdateBaseURL string
	// HTTPPermission decides whether UpdateBaseURL, the assets a manifest names
	// and any redirect may use plain HTTP.
	HTTPPermission config.UpdateHTTPPermission
	// WarnPlainHTTP is called with the first plain-HTTP URL a run actually
	// fetches, when nothing has warned about one already. Nil is how the
	// command says it has warned itself, about a base URL that is http.
	WarnPlainHTTP func(fetchedURL string)
	// Trust carries the administrative policy that decides who may vouch for
	// the binary this command is about to install, and where the Sigstore trust
	// material backing that decision comes from.
	Trust config.UpdateTrust
}

// UpdateRunnerFactory builds the runner an update uses.
//
// It fails when the TLS settings do not load. The update used to carry on with
// a bare transport instead, reaching the mirror without the CA that signs it or
// the client certificate it asks for, so the failure that followed pointed
// somewhere else (#637).
var UpdateRunnerFactory = func(version string, httpConfig UpdateCommandHTTPConfig) (*updateworkflow.Runner, error) {
	transport, err := network.NewSafeTransport(httpConfig.TLSOptions)
	if err != nil {
		return nil, apperrors.New(apperrors.KindValidation, "failed to load the TLS settings for bb update", err)
	}

	baseURL := strings.TrimSpace(httpConfig.UpdateBaseURL)
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	// The manifest is fetched with this client, and held to its Timeout as an
	// API call is. The release's files go through a downloader over the same
	// guarded transport, where the timeout bounds each wait rather than the
	// whole transfer, so a slow link can still carry the archive.
	httpClient := &http.Client{Timeout: httpConfig.RequestTimeout, Transport: requireScheme(transport, httpConfig.HTTPPermission, httpConfig.WarnPlainHTTP)}
	client := githubrelease.NewClient(
		baseURL,
		httpClient,
		fmt.Sprintf("bb/%s", strings.TrimSpace(version)),
		githubrelease.Retries(httpConfig.RetryCount, httpConfig.RetryBackoff),
	)

	// The Sigstore TUF mirror stays https whatever the release mirror may use:
	// update_tuf_url must be https, and a redirect must not undo that.
	trustClient := &http.Client{Timeout: httpConfig.RequestTimeout, Transport: requireTrustHTTPS(transport)}

	trust := httpConfig.Trust
	verifier := updatesigstore.NewReleaseVerifier(updatesigstore.ReleaseVerifierOptions{
		Owner:            repositoryOwner,
		Repo:             repositoryName,
		TrustedRootPath:  trust.TrustedRootPath,
		TUFRepositoryURL: trust.TUFRepositoryURL,
		ExpectedIdentity: trust.SignatureIdentity,
		ExpectedIssuer:   trust.SignatureIssuer,
		HTTPClient:       trustClient,
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
	}), nil
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
	baseURLFlag := ""
	if len(optionalBaseURL) > 0 {
		baseURLFlag = optionalBaseURL[0]
	}
	return loadUpdateCommandHTTPConfig(overrides, baseURLFlag, nil)
}

// loadUpdateCommandHTTPConfig is LoadUpdateCommandHTTPConfig with bb update's
// own flags: --base-url, and --allow-http when it was passed.
func loadUpdateCommandHTTPConfig(overrides config.Overrides, baseURLFlag string, allowHTTPFlag *bool) (UpdateCommandHTTPConfig, error) {
	requestTimeout, err := config.ResolveRequestTimeoutWith(overrides, defaultUpdateRequestTimeout)
	if err != nil {
		return UpdateCommandHTTPConfig{}, err
	}

	retryCount, retryBackoff, err := config.ResolveRetriesWith(overrides)
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

	baseURL, err := config.ResolveUpdateBaseURL(baseURLFlag)
	if err != nil {
		return UpdateCommandHTTPConfig{}, err
	}

	// Refused here, before anything is fetched; the transport holds every later
	// request to the same permission (requireScheme).
	httpPermission, err := config.ResolveUpdateHTTPPermission(allowHTTPFlag)
	if err != nil {
		return UpdateCommandHTTPConfig{}, err
	}
	if err := httpPermission.CheckURL(baseURL); err != nil {
		return UpdateCommandHTTPConfig{}, err
	}

	trust, err := config.ResolveUpdateTrust()
	if err != nil {
		return UpdateCommandHTTPConfig{}, err
	}

	return UpdateCommandHTTPConfig{
		RequestTimeout: requestTimeout,
		RetryCount:     retryCount,
		RetryBackoff:   retryBackoff,
		Trust:          trust,
		TLSOptions: network.TLSOptions{
			CAFile:             tlsSettings.CAFile,
			InsecureSkipVerify: tlsSettings.InsecureSkipVerify,
			ClientCertFile:     tlsSettings.ClientCertFile,
			ClientKeyFile:      tlsSettings.ClientKeyFile,
		},
		UpdateBaseURL:  baseURL,
		HTTPPermission: httpPermission,
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
	var allowHTTP bool

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for and install the latest bb release",
		Long: `Check for and install the latest bb release.

bb installs a release only after verifying it: the signature on its checksum
file against the configured trust material, unless administrative policy sets
allow_unverified_update; the checksum file's entry for this platform's archive;
and the archive itself against that entry.

It puts the new binary in place before it exits. When bb runs through a
symbolic link, it replaces the file the link names and leaves the link as it
is. On Windows, which will not delete a running executable, the binary it
replaced stays beside the new one, as bb.exe.old- and a random suffix, until
the next bb run deletes it.

With --dry-run, bb update makes the same checks and installs nothing. It checks
the latest release even when that is the version already installed, which makes
it the way to verify a release mirror, and it fails with exit status 5
(conflict) when the latest release is older than the installed one.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if BuildDisablesSelfUpdate {
				return apperrors.New(apperrors.KindAuthorization, "self-update is disabled in this build; update bb using your system package manager", nil)
			}

			if disabled, msg, err := config.IsUpdateDisabled(); err != nil {
				return err
			} else if disabled {
				return apperrors.New(apperrors.KindAuthorization, msg, nil)
			}

			var allowHTTPFlag *bool
			if cmd.Flags().Changed("allow-http") {
				allowHTTPFlag = &allowHTTP
			}
			httpConfig, err := loadUpdateCommandHTTPConfig(d.runtimeOverrides(), baseURL, allowHTTPFlag)
			if err != nil {
				return err
			}

			if warning := plainHTTPWarning(httpConfig); warning != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), style.Warning.Render(warning))
			} else {
				// The base URL is https, which is not the same as the run
				// staying on https: a mirror can redirect, and a manifest can
				// name an asset elsewhere. The guard sees each request, so it
				// carries the other half of ADR-059's "every run that uses
				// plain HTTP warns".
				httpConfig.WarnPlainHTTP = func(fetched string) {
					fmt.Fprintln(cmd.ErrOrStderr(), style.Warning.Render(fmt.Sprintf(
						"Warning: bb update followed an https URL to a plain-HTTP one (%s), permitted by %s; anyone on the network path can read, delay or withhold what it serves",
						fetched, httpConfig.HTTPPermission.Source,
					)))
				}
			}

			runner, err := UpdateRunnerFactory(cmd.Root().Version, httpConfig)
			if err != nil {
				return err
			}
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
				return d.WriteJSON(cmd.OutOrStdout(), updateFrom(result))
			}

			writeUpdateHuman(cmd, result)
			return nil
		},
	}

	cmd.Flags().StringVar(&baseURL, "base-url", "", "Custom release mirror base URL; https unless --allow-http")
	cmd.Flags().BoolVar(&allowHTTP, "allow-http", false, "Permit a plain-HTTP release mirror; refused when administrative policy sets allow_http_update: false")

	return cmd
}

// plainHTTPWarning is what a run against a plain-HTTP mirror prints on stderr.
//
// Every run, like the unverified-update warning: it is a standing condition,
// and the person running the command is the one who needs to know.
func plainHTTPWarning(httpConfig UpdateCommandHTTPConfig) string {
	parsed, err := url.Parse(httpConfig.UpdateBaseURL)
	if err != nil || !strings.EqualFold(parsed.Scheme, "http") {
		return ""
	}

	return fmt.Sprintf(
		"Warning: bb update is fetching from a plain-HTTP release mirror (%s), permitted by %s; anyone on the network path can read, delay or withhold what it serves",
		parsed.Redacted(), httpConfig.HTTPPermission.Source,
	)
}

func writeUpdateHuman(cmd *cobra.Command, result updateworkflow.Result) {
	if cmd == nil {
		return
	}

	writer := cmd.OutOrStdout()
	// The verdict line every dry run opens with (ADR-096). What bb update
	// checks -- the release, its signature and its checksum -- is the release
	// source's own answer, so it is server-validated.
	if result.DryRun {
		verdict := "would change nothing"
		if result.UpdateAvailable {
			verdict = "would install " + result.LatestVersion
		}
		fmt.Fprintf(writer, "%s\n", style.DryRun.Render(fmt.Sprintf("Dry run: bb update %s (server-validated)", verdict)))
	}

	switch {
	case result.UpToDate:
		fmt.Fprintf(writer, "%s %s\n", style.Success.Render("bb is up to date"), style.Resource.Render(result.CurrentVersion))
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
// and whether the release the mirror serves actually verifies against it. It
// verifies that release even when it is the installed version, so this prints
// on every dry run that passed. A failure aborts with its own message; this
// covers the case where everything passed and the operator still needs to see
// which trust material was used.
func writeVerificationDetail(writer io.Writer, result updateworkflow.Result) {
	if !result.DryRun {
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

	if result.ChecksumVerified {
		fmt.Fprintf(writer, "%s %s %s\n", style.Secondary.Render("Checksum"), style.Success.Render("verified"), style.Secondary.Render(result.AssetName))
	}
}

// runtimeOverrides is the global flags, or none when the caller wired nothing.
func (d Dependencies) runtimeOverrides() config.Overrides {
	if d.RuntimeOverrides == nil {
		return config.Overrides{}
	}
	return d.RuntimeOverrides()
}
