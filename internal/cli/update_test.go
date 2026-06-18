package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	githubrelease "github.com/vriesdemichael/bitbucket-server-cli/internal/transport/githubrelease"
	updatesigstore "github.com/vriesdemichael/bitbucket-server-cli/internal/transport/sigstore"
	updateworkflow "github.com/vriesdemichael/bitbucket-server-cli/internal/workflows/update"
)

type updateCommandReleaseClient struct {
	release   githubrelease.Release
	downloads map[string][]byte
	latestErr error
}

func (client updateCommandReleaseClient) Latest(context.Context, string, string) (githubrelease.Release, error) {
	return client.release, client.latestErr
}

func (client updateCommandReleaseClient) Download(_ context.Context, assetURL string) ([]byte, error) {
	return client.downloads[assetURL], nil
}

type updateCommandSignatureVerifier struct{}

func (updateCommandSignatureVerifier) VerifyBlob(context.Context, []byte, []byte) (updatesigstore.Verification, error) {
	return updatesigstore.Verification{
		CertificateIdentity:            "https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main",
		CertificateOIDCIssuer:          updatesigstore.GitHubActionsIssuer,
		TransparencyLogEntriesVerified: 1,
		VerifiedTimestampCount:         1,
	}, nil
}

func releaseAssetsWithBundle(assets []githubrelease.Asset) []githubrelease.Asset {
	return append(assets, githubrelease.Asset{Name: "sha256sums.txt.sigstore.json", BrowserDownloadURL: "https://example.test/sha256sums.txt.sigstore.json"})
}

func TestUpdateCommandJSONDryRun(t *testing.T) {
	t.Setenv("BB_REQUEST_TIMEOUT", "")
	t.Setenv("BB_CA_FILE", "")
	t.Setenv("BB_INSECURE_SKIP_VERIFY", "")

	originalFactory := updateRunnerFactory
	defer func() {
		updateRunnerFactory = originalFactory
	}()

	archiveChecksum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	updateRunnerFactory = func(version string, httpConfig updateCommandHTTPConfig) *updateworkflow.Runner {
		if httpConfig.requestTimeout != defaultUpdateRequestTimeout {
			t.Fatalf("expected default request timeout, got %s", httpConfig.requestTimeout)
		}
		return updateworkflow.NewRunner(updateworkflow.Dependencies{
			Releases: updateCommandReleaseClient{
				release: githubrelease.Release{
					TagName: "v1.2.0",
					HTMLURL: "https://example.test/releases/v1.2.0",
					Assets: releaseAssetsWithBundle([]githubrelease.Asset{
						{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.test/bb_1.2.0_linux_amd64.tar.gz"},
						{Name: "sha256sums.txt", BrowserDownloadURL: "https://example.test/sha256sums.txt"},
					}),
				},
				downloads: map[string][]byte{
					"https://example.test/sha256sums.txt": []byte(fmt.Sprintf("%s  %s\n", archiveChecksum, "bb_1.2.0_linux_amd64.tar.gz")),
					"https://example.test/sha256sums.txt.sigstore.json": []byte("bundle"),
				},
			},
			RepositoryOwner: "vriesdemichael",
			RepositoryName:  "bitbucket-data-center-cli",
			CurrentVersion:  func() string { return version },
			ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
			Platform:        func() (string, string) { return "linux", "amd64" },
			Verifier:        updateCommandSignatureVerifier{},
		})
	}

	command := NewRootCommand()
	command.Version = "v1.1.0"
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs([]string{"--json", "--dry-run", "update"})

	if err := command.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	var envelope jsonoutput.Envelope
	if err := json.Unmarshal(buffer.Bytes(), &envelope); err != nil {
		t.Fatalf("failed to decode json output: %v", err)
	}
	if envelope.Meta.Contract != jsonoutput.ContractName {
		t.Fatalf("expected contract %q, got %q", jsonoutput.ContractName, envelope.Meta.Contract)
	}

	encodedData, err := json.Marshal(envelope.Data)
	if err != nil {
		t.Fatalf("failed to re-encode json data: %v", err)
	}

	var result updateworkflow.Result
	if err := json.Unmarshal(encodedData, &result); err != nil {
		t.Fatalf("failed to decode update result: %v", err)
	}
	if !result.DryRun || !result.UpdateAvailable || result.Applied {
		t.Fatalf("unexpected update result: %+v", result)
	}
	if result.AssetName != "bb_1.2.0_linux_amd64.tar.gz" {
		t.Fatalf("expected asset name in result, got %+v", result)
	}
}

func TestUpdateCommandHumanOutputAndValidation(t *testing.T) {
	t.Setenv("BB_REQUEST_TIMEOUT", "")
	t.Setenv("BB_CA_FILE", "")
	t.Setenv("BB_INSECURE_SKIP_VERIFY", "")

	style.Init(true)

	t.Run("nil options", func(t *testing.T) {
		command := newUpdateCommand(nil)
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		if err := command.RunE(command, nil); !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("human dry run output", func(t *testing.T) {
		originalFactory := updateRunnerFactory
		defer func() {
			updateRunnerFactory = originalFactory
		}()

		updateRunnerFactory = func(version string, httpConfig updateCommandHTTPConfig) *updateworkflow.Runner {
			if httpConfig.requestTimeout != defaultUpdateRequestTimeout {
				t.Fatalf("expected default request timeout, got %s", httpConfig.requestTimeout)
			}
			return updateworkflow.NewRunner(updateworkflow.Dependencies{
				Releases: updateCommandReleaseClient{
					release: githubrelease.Release{
						TagName: "v1.2.0",
						HTMLURL: "https://example.test/releases/v1.2.0",
						Assets: releaseAssetsWithBundle([]githubrelease.Asset{
							{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.test/bb_1.2.0_linux_amd64.tar.gz"},
							{Name: "sha256sums.txt", BrowserDownloadURL: "https://example.test/sha256sums.txt"},
						}),
					},
					downloads: map[string][]byte{
						"https://example.test/sha256sums.txt": []byte("deadbeef  bb_1.2.0_linux_amd64.tar.gz\n"),
						"https://example.test/sha256sums.txt.sigstore.json": []byte("bundle"),
					},
				},
				RepositoryOwner: "vriesdemichael",
				RepositoryName:  "bitbucket-data-center-cli",
				CurrentVersion:  func() string { return version },
				ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
				Platform:        func() (string, string) { return "linux", "amd64" },
				Verifier:        updateCommandSignatureVerifier{},
			})
		}

		command := NewRootCommand()
		command.Version = "v1.1.0"
		buffer := &bytes.Buffer{}
		command.SetOut(buffer)
		command.SetErr(buffer)
		command.SetArgs([]string{"--dry-run", "update"})

		if err := command.Execute(); err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}

		output := buffer.String()
		if !bytes.Contains(buffer.Bytes(), []byte("Dry-run (static, capability=full)")) || !bytes.Contains(buffer.Bytes(), []byte("Update available")) || !bytes.Contains(buffer.Bytes(), []byte("planned_action replace")) {
			t.Fatalf("unexpected human output: %s", output)
		}
	})

	t.Run("up to date human output", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		command := &cobra.Command{}
		command.SetOut(buffer)
		writeUpdateHuman(command, updateworkflow.Result{CurrentVersion: "v1.2.0", UpToDate: true})
		if !bytes.Contains(buffer.Bytes(), []byte("bb is up to date")) {
			t.Fatalf("unexpected human output: %s", buffer.String())
		}
	})

	t.Run("applied human output", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		command := &cobra.Command{}
		command.SetOut(buffer)
		writeUpdateHuman(command, updateworkflow.Result{CurrentVersion: "v1.1.0", LatestVersion: "v1.2.0", Applied: true, AssetName: "bb.tgz", InstallPath: "/tmp/bb", ChecksumAssetName: "sha256sums.txt", ChecksumVerified: true, SignatureBundleAssetName: "sha256sums.txt.sigstore.json", SignatureVerified: true, SignatureIdentity: "https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main", ReleaseURL: "https://example.test/releases/v1.2.0"})
		if !bytes.Contains(buffer.Bytes(), []byte("Updated bb")) || !bytes.Contains(buffer.Bytes(), []byte("checksum sha256sums.txt (verified)")) || !bytes.Contains(buffer.Bytes(), []byte("provenance sha256sums.txt.sigstore.json (verified via sigstore keyless + rekor)")) || !bytes.Contains(buffer.Bytes(), []byte("signed_by https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main")) {
			t.Fatalf("unexpected human output: %s", buffer.String())
		}
	})

	t.Run("scheduled human output", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		command := &cobra.Command{}
		command.SetOut(buffer)
		writeUpdateHuman(command, updateworkflow.Result{CurrentVersion: "v1.1.0", LatestVersion: "v1.2.0", Scheduled: true, Staged: true, InstallPath: "C:/tools/bb.exe", StagedPath: "C:/tools/bb.exe.new", SwapResultPath: "C:/tools/bb.exe.update-result.json", PlannedAction: "schedule_background_replace_after_exit"})
		if !bytes.Contains(buffer.Bytes(), []byte("Scheduled bb update")) || !bytes.Contains(buffer.Bytes(), []byte("staged_path C:/tools/bb.exe.new")) || !bytes.Contains(buffer.Bytes(), []byte("swap_result_path C:/tools/bb.exe.update-result.json")) || !bytes.Contains(buffer.Bytes(), []byte("planned_action schedule_background_replace_after_exit")) {
			t.Fatalf("unexpected human output: %s", buffer.String())
		}
	})

	t.Run("provenance available without verification output", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		command := &cobra.Command{}
		command.SetOut(buffer)
		writeUpdateHuman(command, updateworkflow.Result{CurrentVersion: "v1.1.0", LatestVersion: "v1.2.0", Applied: true, AssetName: "bb.tgz", InstallPath: "/tmp/bb", SignatureBundleAssetName: "sha256sums.txt.sigstore.json"})
		if !bytes.Contains(buffer.Bytes(), []byte("provenance sha256sums.txt.sigstore.json (available)")) {
			t.Fatalf("unexpected human output: %s", buffer.String())
		}
	})

	t.Run("default human output", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		command := &cobra.Command{}
		command.SetOut(buffer)
		writeUpdateHuman(command, updateworkflow.Result{CurrentVersion: "dev"})
		if !bytes.Contains(buffer.Bytes(), []byte("Current version dev")) {
			t.Fatalf("unexpected human output: %s", buffer.String())
		}
	})

	t.Run("update available human output", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		command := &cobra.Command{}
		command.SetOut(buffer)
		writeUpdateHuman(command, updateworkflow.Result{CurrentVersion: "v1.1.0", LatestVersion: "v1.2.0", UpdateAvailable: true, AssetName: "bb.tgz", InstallPath: "/tmp/bb"})
		if !bytes.Contains(buffer.Bytes(), []byte("Update available v1.1.0 -> v1.2.0")) {
			t.Fatalf("unexpected human output: %s", buffer.String())
		}
	})

	t.Run("runner error is returned", func(t *testing.T) {
		originalFactory := updateRunnerFactory
		defer func() {
			updateRunnerFactory = originalFactory
		}()

		updateRunnerFactory = func(version string, httpConfig updateCommandHTTPConfig) *updateworkflow.Runner {
			if httpConfig.requestTimeout != defaultUpdateRequestTimeout {
				t.Fatalf("expected default request timeout, got %s", httpConfig.requestTimeout)
			}
			return updateworkflow.NewRunner(updateworkflow.Dependencies{
				Releases:        updateCommandReleaseClient{latestErr: apperrors.New(apperrors.KindTransient, "boom", nil)},
				RepositoryOwner: "vriesdemichael",
				RepositoryName:  "bitbucket-data-center-cli",
				CurrentVersion:  func() string { return version },
				ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
			})
		}

		command := NewRootCommand()
		command.Version = "v1.1.0"
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		command.SetArgs([]string{"update"})

		if err := command.Execute(); !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
	})

	t.Run("invalid request timeout is rejected", func(t *testing.T) {
		t.Setenv("BB_REQUEST_TIMEOUT", "bad")
		command := NewRootCommand()
		command.Version = "v1.1.0"
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		command.SetArgs([]string{"update"})

		if err := command.Execute(); !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected validation error, got %v", err)
		}
	})

	t.Run("runtime transport env is forwarded", func(t *testing.T) {
		originalFactory := updateRunnerFactory
		defer func() {
			updateRunnerFactory = originalFactory
		}()

		caFile := "/tmp/ca.pem"
		t.Setenv("BB_CA_FILE", caFile)
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "true")
		t.Setenv("BB_REQUEST_TIMEOUT", "45s")

		updateRunnerFactory = func(version string, httpConfig updateCommandHTTPConfig) *updateworkflow.Runner {
			if httpConfig.requestTimeout != 45*time.Second {
				t.Fatalf("expected forwarded timeout, got %s", httpConfig.requestTimeout)
			}
			if httpConfig.tlsOptions.CAFile != caFile || !httpConfig.tlsOptions.InsecureSkipVerify {
				t.Fatalf("unexpected tls options: %+v", httpConfig.tlsOptions)
			}
			return updateworkflow.NewRunner(updateworkflow.Dependencies{
				Releases: updateCommandReleaseClient{release: githubrelease.Release{TagName: "v1.1.0"}},
				RepositoryOwner: "vriesdemichael",
				RepositoryName:  "bitbucket-data-center-cli",
				CurrentVersion:  func() string { return version },
				ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
			})
		}

		command := NewRootCommand()
		command.Version = "v1.1.0"
		command.SetOut(&bytes.Buffer{})
		command.SetErr(&bytes.Buffer{})
		command.SetArgs([]string{"update"})

		if err := command.Execute(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})
}
