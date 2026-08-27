package updatecmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/style"
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
	if BuildDisablesSelfUpdate {
		t.Skip("skipping in no_self_update build")
	}

	t.Setenv("BB_REQUEST_TIMEOUT", "")
	t.Setenv("BB_CA_FILE", "")
	t.Setenv("BB_INSECURE_SKIP_VERIFY", "")

	originalFactory := UpdateRunnerFactory
	defer func() {
		UpdateRunnerFactory = originalFactory
	}()

	archiveChecksum := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	UpdateRunnerFactory = func(version string, httpConfig UpdateCommandHTTPConfig) *updateworkflow.Runner {
		if httpConfig.RequestTimeout != defaultUpdateRequestTimeout {
			t.Fatalf("expected default request timeout, got %s", httpConfig.RequestTimeout)
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
					"https://example.test/sha256sums.txt":               []byte(fmt.Sprintf("%s  %s\n", archiveChecksum, "bb_1.2.0_linux_amd64.tar.gz")),
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

	root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
	deps := Dependencies{
		JSONEnabled:   func() bool { return true },
		DryRunEnabled: func() bool { return true },
	}
	root.AddCommand(New(deps))

	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)
	root.SetArgs([]string{"update"})

	if err := root.Execute(); err != nil {
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
	if BuildDisablesSelfUpdate {
		t.Skip("skipping in no_self_update build")
	}

	t.Setenv("BB_REQUEST_TIMEOUT", "")
	t.Setenv("BB_CA_FILE", "")
	t.Setenv("BB_INSECURE_SKIP_VERIFY", "")

	style.Init(true)

	t.Run("human dry run output", func(t *testing.T) {
		originalFactory := UpdateRunnerFactory
		defer func() {
			UpdateRunnerFactory = originalFactory
		}()

		UpdateRunnerFactory = func(version string, httpConfig UpdateCommandHTTPConfig) *updateworkflow.Runner {
			if httpConfig.RequestTimeout != defaultUpdateRequestTimeout {
				t.Fatalf("expected default request timeout, got %s", httpConfig.RequestTimeout)
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
						"https://example.test/sha256sums.txt":               []byte("deadbeef  bb_1.2.0_linux_amd64.tar.gz\n"),
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

		root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
		deps := Dependencies{
			JSONEnabled:   func() bool { return false },
			DryRunEnabled: func() bool { return true },
		}
		root.AddCommand(New(deps))

		buffer := &bytes.Buffer{}
		root.SetOut(buffer)
		root.SetErr(buffer)
		root.SetArgs([]string{"update"})

		if err := root.Execute(); err != nil {
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
		originalFactory := UpdateRunnerFactory
		defer func() {
			UpdateRunnerFactory = originalFactory
		}()

		UpdateRunnerFactory = func(version string, httpConfig UpdateCommandHTTPConfig) *updateworkflow.Runner {
			if httpConfig.RequestTimeout != defaultUpdateRequestTimeout {
				t.Fatalf("expected default request timeout, got %s", httpConfig.RequestTimeout)
			}
			return updateworkflow.NewRunner(updateworkflow.Dependencies{
				Releases:        updateCommandReleaseClient{latestErr: apperrors.New(apperrors.KindTransient, "boom", nil)},
				RepositoryOwner: "vriesdemichael",
				RepositoryName:  "bitbucket-data-center-cli",
				CurrentVersion:  func() string { return version },
				ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
			})
		}

		root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
		root.AddCommand(New(Dependencies{}))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs([]string{"update"})

		if err := root.Execute(); !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
	})

	t.Run("invalid request timeout is rejected", func(t *testing.T) {
		t.Setenv("BB_REQUEST_TIMEOUT", "bad")
		root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
		root.AddCommand(New(Dependencies{}))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs([]string{"update"})

		if err := root.Execute(); !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected validation error, got %v", err)
		}
	})

	t.Run("runtime transport env is forwarded", func(t *testing.T) {
		originalFactory := UpdateRunnerFactory
		defer func() {
			UpdateRunnerFactory = originalFactory
		}()

		caFile := "/tmp/ca.pem"
		t.Setenv("BB_CA_FILE", caFile)
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "true")
		t.Setenv("BB_REQUEST_TIMEOUT", "45s")

		UpdateRunnerFactory = func(version string, httpConfig UpdateCommandHTTPConfig) *updateworkflow.Runner {
			if httpConfig.RequestTimeout != 45*time.Second {
				t.Fatalf("expected forwarded timeout, got %s", httpConfig.RequestTimeout)
			}
			if httpConfig.TLSOptions.CAFile != caFile || !httpConfig.TLSOptions.InsecureSkipVerify {
				t.Fatalf("unexpected tls options: %+v", httpConfig.TLSOptions)
			}
			return updateworkflow.NewRunner(updateworkflow.Dependencies{
				Releases:        updateCommandReleaseClient{release: githubrelease.Release{TagName: "v1.1.0"}},
				RepositoryOwner: "vriesdemichael",
				RepositoryName:  "bitbucket-data-center-cli",
				CurrentVersion:  func() string { return version },
				ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
			})
		}

		root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
		root.AddCommand(New(Dependencies{}))
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		root.SetArgs([]string{"update"})

		if err := root.Execute(); err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	})

	t.Run("LoadUpdateCommandHTTPConfig validation errors", func(t *testing.T) {
		t.Setenv("BB_REQUEST_TIMEOUT", "0s")
		if _, err := LoadUpdateCommandHTTPConfig(); err == nil {
			t.Fatal("expected error for 0s timeout")
		}

		t.Setenv("BB_REQUEST_TIMEOUT", "20s")
		t.Setenv("BB_INSECURE_SKIP_VERIFY", "not-a-bool")
		if _, err := LoadUpdateCommandHTTPConfig(); err == nil {
			t.Fatal("expected error for invalid bool")
		}
	})

	t.Run("default UpdateRunnerFactory", func(t *testing.T) {
		runner := UpdateRunnerFactory("v1.0.0", UpdateCommandHTTPConfig{
			RequestTimeout: 10 * time.Second,
		})
		if runner == nil {
			t.Fatal("expected non-nil runner from default factory")
		}
	})
}

func TestUpdateCommandDisabledInBuild(t *testing.T) {
	if !BuildDisablesSelfUpdate {
		t.Skip("skipping test meant for no_self_update tag")
	}

	root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
	root.AddCommand(New(Dependencies{}))
	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)
	root.SetArgs([]string{"update"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when BuildDisablesSelfUpdate is true, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got %v", err)
	}
	if !strings.Contains(err.Error(), "self-update is disabled in this build; update bb using your system package manager") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestUpdateCommandDisabledByPolicy(t *testing.T) {
	if BuildDisablesSelfUpdate {
		t.Skip("skipping in no_self_update build")
	}

	tempDir := t.TempDir()
	sysPath := filepath.Join(tempDir, "system-config.yaml")

	t.Setenv("BB_SYSTEM_CONFIG_PATH", sysPath)
	t.Setenv("BB_CONFIG_PATH", filepath.Join(tempDir, "user.yaml"))
	t.Setenv("BB_DISABLE_UPDATE", "")

	// 1. Via environment variable BB_DISABLE_UPDATE=1
	t.Setenv("BB_DISABLE_UPDATE", "1")
	root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
	root.AddCommand(New(Dependencies{}))
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"update"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error with BB_DISABLE_UPDATE=1, got nil")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got %v", err)
	}
	if !strings.Contains(err.Error(), "self-update is disabled by administrative policy; update bb using your system package manager") {
		t.Fatalf("unexpected message: %v", err)
	}

	// 2. Via system config policy disable_update: true
	t.Setenv("BB_DISABLE_UPDATE", "")
	if err := os.WriteFile(sysPath, []byte("disable_update: true\n"), 0o600); err != nil {
		t.Fatalf("write sys: %v", err)
	}
	root2 := &cobra.Command{Use: "bb", Version: "v1.1.0"}
	root2.AddCommand(New(Dependencies{}))
	buf2 := &bytes.Buffer{}
	root2.SetOut(buf2)
	root2.SetErr(buf2)
	root2.SetArgs([]string{"update"})
	err2 := root2.Execute()
	if err2 == nil {
		t.Fatal("expected error with disable_update: true in system config, got nil")
	}
	if !apperrors.IsKind(err2, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got %v", err2)
	}
	if !strings.Contains(err2.Error(), "self-update is disabled by administrative policy; update bb using your system package manager") {
		t.Fatalf("unexpected message: %v", err2)
	}
}

func TestUpdateCommandCustomBaseURLFlagAndEnv(t *testing.T) {
	if BuildDisablesSelfUpdate {
		t.Skip("skipping in no_self_update build")
	}

	originalFactory := UpdateRunnerFactory
	defer func() {
		UpdateRunnerFactory = originalFactory
	}()

	capturedBaseURL := ""
	UpdateRunnerFactory = func(version string, httpConfig UpdateCommandHTTPConfig) *updateworkflow.Runner {
		capturedBaseURL = httpConfig.UpdateBaseURL
		return updateworkflow.NewRunner(updateworkflow.Dependencies{
			Releases: updateCommandReleaseClient{
				release: githubrelease.Release{
					TagName: "v1.1.0",
					HTMLURL: "https://example.test/release",
				},
			},
			RepositoryOwner: "vriesdemichael",
			RepositoryName:  "bitbucket-data-center-cli",
			CurrentVersion:  func() string { return "v1.1.0" },
			ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
			Platform:        func() (string, string) { return "linux", "amd64" },
		})
	}

	// 1. Via flag --base-url
	t.Setenv("BB_DISABLE_UPDATE", "")
	root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
	root.AddCommand(New(Dependencies{}))
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"update", "--base-url", "https://mirror.internal/releases"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if capturedBaseURL != "https://mirror.internal/releases" {
		t.Fatalf("expected mirror base URL from flag, got: %s", capturedBaseURL)
	}

	// 2. Via BB_UPDATE_BASE_URL env var
	t.Setenv("BB_UPDATE_BASE_URL", "https://env-mirror.internal/releases")
	root2 := &cobra.Command{Use: "bb", Version: "v1.1.0"}
	root2.AddCommand(New(Dependencies{}))
	buf2 := &bytes.Buffer{}
	root2.SetOut(buf2)
	root2.SetErr(buf2)
	root2.SetArgs([]string{"update"})
	if err := root2.Execute(); err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if capturedBaseURL != "https://env-mirror.internal/releases" {
		t.Fatalf("expected mirror base URL from env, got: %s", capturedBaseURL)
	}
}

func TestUpdateCommandDisabledInBuildToggle(t *testing.T) {
	orig := BuildDisablesSelfUpdate
	BuildDisablesSelfUpdate = true
	defer func() { BuildDisablesSelfUpdate = orig }()

	root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
	root.AddCommand(New(Dependencies{}))
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"update"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error when self-update is disabled in build")
	}
	if !apperrors.IsKind(err, apperrors.KindAuthorization) {
		t.Fatalf("expected KindAuthorization, got %v", err)
	}
	if !strings.Contains(err.Error(), "self-update is disabled in this build; update bb using your system package manager") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestUpdateCommandPolicyError(t *testing.T) {
	if BuildDisablesSelfUpdate {
		t.Skip("skipping in no_self_update build")
	}

	dir := t.TempDir()
	badConfig := filepath.Join(dir, "invalid.yaml")
	if err := os.WriteFile(badConfig, []byte("policies:\n  require_keyring: [invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BB_SYSTEM_CONFIG_PATH", badConfig)

	root := &cobra.Command{Use: "bb", Version: "v1.1.0"}
	root.AddCommand(New(Dependencies{}))
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"update"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error with corrupted system config")
	}
}

func TestLoadUpdateCommandHTTPConfigInvalidBaseURL(t *testing.T) {
	_, err := LoadUpdateCommandHTTPConfig(":\x7finvalid-url")
	if err == nil {
		t.Fatal("expected error for control characters in base URL")
	}
}
