package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	githubrelease "github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/githubrelease"
	updatesigstore "github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/sigstore"
)

type stubReleaseClient struct {
	release       githubrelease.Release
	latestErr     error
	downloads     map[string][]byte
	downloadErrs  map[string]error
	latestCalls   int
	downloadCalls []string
}

func (stub *stubReleaseClient) Latest(context.Context, string, string) (githubrelease.Release, error) {
	stub.latestCalls++
	return stub.release, stub.latestErr
}

func (stub *stubReleaseClient) Download(_ context.Context, assetURL string) ([]byte, error) {
	stub.downloadCalls = append(stub.downloadCalls, assetURL)
	if err := stub.downloadErrs[assetURL]; err != nil {
		return nil, err
	}
	return stub.downloads[assetURL], nil
}

type stubSignatureVerifier struct {
	verification updatesigstore.Verification
	err          error
	calls        int
	// artifact and bundle are what the last call was asked to verify.
	artifact []byte
	bundle   []byte
}

func (stub *stubSignatureVerifier) VerifyBlob(_ context.Context, artifact, bundleJSON []byte) (updatesigstore.Verification, error) {
	stub.calls++
	stub.artifact = artifact
	stub.bundle = bundleJSON
	if stub.err != nil {
		return updatesigstore.Verification{}, stub.err
	}
	if stub.verification.CertificateIdentity == "" {
		stub.verification = updatesigstore.Verification{
			CertificateIdentity:            "https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main",
			CertificateOIDCIssuer:          updatesigstore.GitHubActionsIssuer,
			TransparencyLogEntriesVerified: 1,
			VerifiedTimestampCount:         1,
		}
	}
	return stub.verification, nil
}

func newTestRunner(deps Dependencies) *Runner {
	if deps.Verifier == nil {
		deps.Verifier = &stubSignatureVerifier{}
	}
	return NewRunner(deps)
}

func releaseWithSignatureBundle(release githubrelease.Release) githubrelease.Release {
	for _, asset := range release.Assets {
		if asset.Name == "sha256sums.txt" {
			release.Assets = append(release.Assets, githubrelease.Asset{Name: "sha256sums.txt.sigstore.json", BrowserDownloadURL: "bundle"})
			break
		}
	}
	return release
}

func downloadsWithSignatureBundle(downloads map[string][]byte) map[string][]byte {
	if downloads == nil {
		downloads = map[string][]byte{}
	}
	if _, ok := downloads["bundle"]; !ok {
		downloads["bundle"] = []byte("signed-bundle")
	}
	return downloads
}

func TestRunnerDryRunPlansUpdateWithoutWritingBinary(t *testing.T) {
	t.Parallel()

	archive := buildTarGzArchive(t, "bb", []byte("new-binary"))
	checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_linux_amd64.tar.gz")

	client := &stubReleaseClient{
		release: releaseWithSignatureBundle(githubrelease.Release{
			TagName: "v1.2.0",
			HTMLURL: "https://example.test/releases/v1.2.0",
			Assets: []githubrelease.Asset{
				{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.test/bb_1.2.0_linux_amd64.tar.gz"},
				{Name: "sha256sums.txt", BrowserDownloadURL: "https://example.test/sha256sums.txt"},
			},
		}),
		downloads: downloadsWithSignatureBundle(map[string][]byte{
			"https://example.test/sha256sums.txt":              []byte(checksum),
			"https://example.test/bb_1.2.0_linux_amd64.tar.gz": archive,
		}),
	}

	written := false
	runner := newTestRunner(Dependencies{
		Releases:        client,
		RepositoryOwner: "vriesdemichael",
		RepositoryName:  "bitbucket-data-center-cli",
		CurrentVersion:  func() string { return "v1.1.0" },
		ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
		Platform:        func() (string, string) { return "linux", "amd64" },
		WriteBinary: func(string, []byte, fs.FileMode) error {
			written = true
			return nil
		},
	})

	result, err := runner.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if written {
		t.Fatal("expected dry-run not to write binary")
	}
	if !result.UpdateAvailable || result.Applied || !result.DryRun || result.PlannedAction != "replace" {
		t.Fatalf("unexpected result: %+v", result)
	}
	// The archive is fetched and hashed like an update would: a checksum entry
	// that exists says nothing about whether the archive beside it matches.
	if !result.ChecksumAvailable || !result.ChecksumVerified {
		t.Fatalf("expected the archive to be verified against its checksum, got %+v", result)
	}
	want := []string{"https://example.test/sha256sums.txt", "bundle", "https://example.test/bb_1.2.0_linux_amd64.tar.gz"}
	if strings.Join(client.downloadCalls, " ") != strings.Join(want, " ") {
		t.Fatalf("dry-run downloads = %v, want %v", client.downloadCalls, want)
	}
}

func TestRunnerAppliesReleaseUpdate(t *testing.T) {
	t.Parallel()

	targetDir := t.TempDir()
	targetPath := filepath.Join(targetDir, "bb")
	if err := os.WriteFile(targetPath, []byte("old-binary"), 0o755); err != nil {
		t.Fatalf("write initial target: %v", err)
	}

	archive := buildTarGzArchive(t, "bb", []byte("new-binary"))
	checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_linux_amd64.tar.gz")

	client := &stubReleaseClient{
		release: releaseWithSignatureBundle(githubrelease.Release{
			TagName: "v1.2.0",
			HTMLURL: "https://example.test/releases/v1.2.0",
			Assets: []githubrelease.Asset{
				{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.test/bb_1.2.0_linux_amd64.tar.gz"},
				{Name: "sha256sums.txt", BrowserDownloadURL: "https://example.test/sha256sums.txt"},
			},
		}),
		downloads: downloadsWithSignatureBundle(map[string][]byte{
			"https://example.test/sha256sums.txt":              []byte(checksum),
			"https://example.test/bb_1.2.0_linux_amd64.tar.gz": archive,
		}),
	}

	runner := newTestRunner(Dependencies{
		Releases:        client,
		RepositoryOwner: "vriesdemichael",
		RepositoryName:  "bitbucket-data-center-cli",
		CurrentVersion:  func() string { return "v1.1.0" },
		ExecutablePath:  func() (string, error) { return targetPath, nil },
		Platform:        func() (string, string) { return "linux", "amd64" },
	})

	result, err := runner.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.Applied || !result.ChecksumVerified {
		t.Fatalf("expected applied verified result, got %+v", result)
	}
	updated, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read updated target: %v", err)
	}
	if string(updated) != "new-binary" {
		t.Fatalf("expected updated binary contents, got %q", string(updated))
	}
	// No backup and no partial download beside it.
	if entries := directoryEntries(t, targetDir); strings.Join(entries, " ") != "bb" {
		t.Fatalf("install directory holds %v, want only bb", entries)
	}
	if len(client.downloadCalls) != 3 {
		t.Fatalf("expected three downloads, got %+v", client.downloadCalls)
	}
}

func TestNewRunnerDefaultsAndSignatureMetadata(t *testing.T) {
	t.Parallel()

	runner := NewRunner(Dependencies{
		Releases:        &stubReleaseClient{},
		RepositoryOwner: " vriesdemichael ",
		RepositoryName:  " bitbucket-data-center-cli ",
	})

	if runner == nil {
		t.Fatal("expected runner")
	}
	if runner.owner != "vriesdemichael" || runner.repo != "bitbucket-data-center-cli" {
		t.Fatalf("expected trimmed repository metadata, got owner=%q repo=%q", runner.owner, runner.repo)
	}
	if runner.currentVersion() != "dev" {
		t.Fatalf("expected default version to be dev, got %q", runner.currentVersion())
	}
	if runner.verifier == nil {
		t.Fatal("expected default signature verifier")
	}

	if _, ok := runner.verifier.(*updatesigstore.Verifier); !ok {
		t.Fatalf("expected GitHub release verifier, got %T", runner.verifier)
	}
}

func TestRunnerDryRunCapturesSignatureMetadata(t *testing.T) {
	t.Parallel()

	archive := buildTarGzArchive(t, "bb", []byte("new-binary"))
	checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_linux_amd64.tar.gz")
	verifier := &stubSignatureVerifier{verification: updatesigstore.Verification{
		CertificateIdentity:            "https://github.com/vriesdemichael/bitbucket-data-center-cli/.github/workflows/release.yml@refs/heads/main",
		CertificateOIDCIssuer:          updatesigstore.GitHubActionsIssuer,
		TransparencyLogEntriesVerified: 1,
		VerifiedTimestampCount:         2,
	}}

	client := &stubReleaseClient{
		release: releaseWithSignatureBundle(githubrelease.Release{
			TagName: "v1.2.0",
			HTMLURL: "https://example.test/releases/v1.2.0",
			Assets: []githubrelease.Asset{
				{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.test/bb_1.2.0_linux_amd64.tar.gz"},
				{Name: "sha256sums.txt", BrowserDownloadURL: "https://example.test/sha256sums.txt"},
			},
		}),
		downloads: downloadsWithSignatureBundle(map[string][]byte{
			"https://example.test/sha256sums.txt":              []byte(checksum),
			"https://example.test/bb_1.2.0_linux_amd64.tar.gz": archive,
		}),
	}

	runner := newTestRunner(Dependencies{
		Releases:        client,
		RepositoryOwner: "vriesdemichael",
		RepositoryName:  "bitbucket-data-center-cli",
		CurrentVersion:  func() string { return "v1.1.0" },
		ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
		Platform:        func() (string, string) { return "linux", "amd64" },
		Verifier:        verifier,
	})

	result, err := runner.Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !result.SignatureVerified || !result.TransparencyLogVerified {
		t.Fatalf("expected verified transparency metadata, got %+v", result)
	}
	if result.SignatureIdentity != verifier.verification.CertificateIdentity {
		t.Fatalf("expected signature identity %q, got %+v", verifier.verification.CertificateIdentity, result)
	}
	if result.SignatureIssuer != verifier.verification.CertificateOIDCIssuer {
		t.Fatalf("expected signature issuer %q, got %+v", verifier.verification.CertificateOIDCIssuer, result)
	}
}

const mirrorTrustSource = "trusted root file /etc/bb/trusted_root.json"

// mirrorServing is a mirror whose latest release is version, laid out the way
// the release workflow publishes one: the linux/amd64 archive, the checksum file
// with its entry, and the signature bundle. Everything in it verifies.
func mirrorServing(t *testing.T, version string) *stubReleaseClient {
	t.Helper()

	assetName := fmt.Sprintf("bb_%s_linux_amd64.tar.gz", strings.TrimPrefix(version, "v"))
	archive := buildTarGzArchive(t, "bb", []byte("bb "+version))

	return &stubReleaseClient{
		release: githubrelease.Release{
			TagName: version,
			HTMLURL: "https://mirror.internal/releases/" + version,
			Assets: []githubrelease.Asset{
				{Name: assetName, BrowserDownloadURL: "https://mirror.internal/" + assetName},
				{Name: "sha256sums.txt", BrowserDownloadURL: "https://mirror.internal/sha256sums.txt"},
				{Name: "sha256sums.txt.sigstore.json", BrowserDownloadURL: "https://mirror.internal/sha256sums.txt.sigstore.json"},
			},
		},
		downloads: map[string][]byte{
			"https://mirror.internal/" + assetName:                 archive,
			"https://mirror.internal/sha256sums.txt":               []byte(fmt.Sprintf("%s  %s\n", sha256Hex(archive), assetName)),
			"https://mirror.internal/sha256sums.txt.sigstore.json": []byte("signed-bundle"),
		},
	}
}

// mirrorCheckDependencies runs a linux/amd64 host that has installed, against
// client. Installing a binary fails the test: none of these runs may.
func mirrorCheckDependencies(t *testing.T, client *stubReleaseClient, installed string, verifier *stubSignatureVerifier) Dependencies {
	t.Helper()

	return Dependencies{
		Releases:        client,
		RepositoryOwner: "vriesdemichael",
		RepositoryName:  "bitbucket-data-center-cli",
		CurrentVersion:  func() string { return installed },
		ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
		Platform:        func() (string, string) { return "linux", "amd64" },
		WriteBinary: func(string, []byte, fs.FileMode) error {
			t.Error("the run installed a binary")
			return nil
		},
		Verifier:    verifier,
		TrustSource: mirrorTrustSource,
	}
}

// An update that finds nothing newer to install downloads nothing, even from a
// mirror whose release would verify. Only a dry run is the mirror check.
func TestRunnerUpdateWithNothingNewerDownloadsNothing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		installed  string
		comparison string
		upToDate   bool
	}{
		{name: "mirror serves the installed release", installed: "v1.2.0", comparison: "equal", upToDate: true},
		{name: "mirror serves an older release", installed: "v1.3.0", comparison: "current_newer"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := mirrorServing(t, "v1.2.0")
			verifier := &stubSignatureVerifier{}
			result, err := NewRunner(mirrorCheckDependencies(t, client, testCase.installed, verifier)).Run(context.Background(), Options{})
			if err != nil {
				t.Fatalf("Run returned error: %v", err)
			}
			if len(client.downloadCalls) != 0 || verifier.calls != 0 {
				t.Fatalf("expected nothing fetched or verified, got downloads %v and %d verifier calls", client.downloadCalls, verifier.calls)
			}
			if result.UpdateAvailable || result.UpToDate != testCase.upToDate || result.Comparison != testCase.comparison {
				t.Fatalf("expected comparison %q with upToDate=%v, got %+v", testCase.comparison, testCase.upToDate, result)
			}
			if result.SignatureVerified || result.ChecksumAvailable || result.ChecksumVerified || result.PlannedAction != "" {
				t.Fatalf("expected nothing reported as verified or planned, got %+v", result)
			}
		})
	}
}

// Right after a rollout the mirror serves the version every host already runs,
// which is when an operator checks it. The dry run makes every check an update
// would make, and reports each one.
func TestRunnerDryRunVerifiesTheInstalledRelease(t *testing.T) {
	t.Parallel()

	client := mirrorServing(t, "v1.2.0")
	verifier := &stubSignatureVerifier{}
	result, err := NewRunner(mirrorCheckDependencies(t, client, "v1.2.0", verifier)).Run(context.Background(), Options{DryRun: true})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	checksumURL := "https://mirror.internal/sha256sums.txt"
	want := []string{checksumURL, "https://mirror.internal/sha256sums.txt.sigstore.json", "https://mirror.internal/bb_1.2.0_linux_amd64.tar.gz"}
	if strings.Join(client.downloadCalls, " ") != strings.Join(want, " ") {
		t.Fatalf("downloads = %v, want the checksum file, its signature bundle and the archive: %v", client.downloadCalls, want)
	}
	if verifier.calls != 1 || !bytes.Equal(verifier.artifact, client.downloads[checksumURL]) || string(verifier.bundle) != "signed-bundle" {
		t.Fatalf("expected one signature check over the served checksum file and bundle, got %d calls over %q with %q", verifier.calls, verifier.artifact, verifier.bundle)
	}

	if !result.DryRun || !result.UpToDate || result.UpdateAvailable || result.Comparison != "equal" {
		t.Fatalf("expected an up-to-date dry run, got %+v", result)
	}
	if result.TrustSource != mirrorTrustSource {
		t.Fatalf("trust source = %q, want %q", result.TrustSource, mirrorTrustSource)
	}
	if !result.SignatureVerified || result.SignatureSkipped || result.SignatureIdentity == "" || result.SignatureIssuer == "" || !result.TransparencyLogVerified {
		t.Fatalf("expected the verified signature to be reported, got %+v", result)
	}
	if !result.ChecksumAvailable || !result.ChecksumVerified {
		t.Fatalf("expected the checksum entry and the archive to be reported verified, got %+v", result)
	}
	if result.AssetName != "bb_1.2.0_linux_amd64.tar.gz" || result.ChecksumAssetName != "sha256sums.txt" || result.SignatureBundleAssetName != "sha256sums.txt.sigstore.json" {
		t.Fatalf("expected the verified assets to be named, got %+v", result)
	}
	// Verified, but there is nothing to install, so nothing is planned.
	if result.PlannedAction != "" || result.Applied {
		t.Fatalf("expected nothing planned or installed, got %+v", result)
	}
}

// A mirror behind the host it serves has stopped receiving releases. An update
// cannot tell, since it never downgrades; a dry run fails.
func TestRunnerDryRunFailsWhenTheMirrorServesAnOlderRelease(t *testing.T) {
	t.Parallel()

	t.Run("a release that verifies is a conflict", func(t *testing.T) {
		t.Parallel()

		client := mirrorServing(t, "v1.2.0")
		verifier := &stubSignatureVerifier{}
		_, err := NewRunner(mirrorCheckDependencies(t, client, "v1.3.0", verifier)).Run(context.Background(), Options{DryRun: true})
		if !apperrors.IsKind(err, apperrors.KindConflict) || apperrors.ExitCode(err) != 5 {
			t.Fatalf("expected a conflict, exit 5, got %v", err)
		}
		if !strings.Contains(err.Error(), "latest release v1.2.0 is older than the installed v1.3.0") {
			t.Fatalf("expected the message to name both versions, got %v", err)
		}
		// Verified first, like any release a dry run is served.
		if verifier.calls != 1 || len(client.downloadCalls) != 3 {
			t.Fatalf("expected the served release to be verified, got %d verifier calls and downloads %v", verifier.calls, client.downloadCalls)
		}
	})

	t.Run("a release that fails a check reports that failure", func(t *testing.T) {
		t.Parallel()

		client := mirrorServing(t, "v1.2.0")
		client.downloads["https://mirror.internal/bb_1.2.0_linux_amd64.tar.gz"] = []byte("not the archive")
		_, err := NewRunner(mirrorCheckDependencies(t, client, "v1.3.0", &stubSignatureVerifier{})).Run(context.Background(), Options{DryRun: true})
		if !apperrors.IsKind(err, apperrors.KindPermanent) || !strings.Contains(err.Error(), "checksum verification failed") {
			t.Fatalf("expected the checksum failure, got %v", err)
		}
	})
}

// The checksum file has the entry and the archive beside it is not the one it
// describes. A dry run that stopped at the entry passed such a mirror.
func TestRunnerDryRunFailsOnAnArchiveThatDoesNotMatchItsChecksum(t *testing.T) {
	t.Parallel()

	for _, installed := range []string{"v1.2.0", "v1.1.0"} {
		t.Run("installed "+installed, func(t *testing.T) {
			t.Parallel()

			client := mirrorServing(t, "v1.2.0")
			archiveURL := "https://mirror.internal/bb_1.2.0_linux_amd64.tar.gz"
			// Cut short, as an interrupted mirror sync leaves it.
			client.downloads[archiveURL] = client.downloads[archiveURL][:len(client.downloads[archiveURL])/2]

			_, err := NewRunner(mirrorCheckDependencies(t, client, installed, &stubSignatureVerifier{})).Run(context.Background(), Options{DryRun: true})
			if !apperrors.IsKind(err, apperrors.KindPermanent) || !strings.Contains(err.Error(), "checksum verification failed for bb_1.2.0_linux_amd64.tar.gz") {
				t.Fatalf("expected a checksum failure, got %v", err)
			}
		})
	}
}

// A dry run is held to the trust policy an update is held to.
func TestRunnerDryRunWithoutASignatureBundle(t *testing.T) {
	t.Parallel()

	withoutBundle := func(t *testing.T) *stubReleaseClient {
		t.Helper()
		client := mirrorServing(t, "v1.2.0")
		client.release.Assets = client.release.Assets[:2]
		return client
	}

	t.Run("fails while signatures are required", func(t *testing.T) {
		t.Parallel()

		client := withoutBundle(t)
		_, err := NewRunner(mirrorCheckDependencies(t, client, "v1.2.0", &stubSignatureVerifier{})).Run(context.Background(), Options{DryRun: true})
		if !apperrors.IsKind(err, apperrors.KindNotFound) || !strings.Contains(err.Error(), "sha256sums.txt.sigstore.json was not found") {
			t.Fatalf("expected the missing bundle to be reported, got %v", err)
		}
		if len(client.downloadCalls) != 0 {
			t.Fatalf("expected nothing fetched for a release that cannot be verified, got %v", client.downloadCalls)
		}
	})

	t.Run("verifies the archive under allow_unverified_update", func(t *testing.T) {
		t.Parallel()

		client := withoutBundle(t)
		verifier := &stubSignatureVerifier{}
		deps := mirrorCheckDependencies(t, client, "v1.2.0", verifier)
		deps.SkipSignatureVerification = true

		result, err := NewRunner(deps).Run(context.Background(), Options{DryRun: true})
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if verifier.calls != 0 || !result.SignatureSkipped || result.SignatureVerified || result.SignatureBundleAssetName != "" {
			t.Fatalf("expected the signature to be reported skipped, got %d verifier calls and %+v", verifier.calls, result)
		}
		if !result.ChecksumAvailable || !result.ChecksumVerified {
			t.Fatalf("expected the archive to be verified against its checksum, got %+v", result)
		}
		want := []string{"https://mirror.internal/sha256sums.txt", "https://mirror.internal/bb_1.2.0_linux_amd64.tar.gz"}
		if strings.Join(client.downloadCalls, " ") != strings.Join(want, " ") {
			t.Fatalf("downloads = %v, want %v", client.downloadCalls, want)
		}
	})
}

func buildTarGzArchive(t *testing.T, fileName string, contents []byte) []byte {
	t.Helper()
	return buildTarGzArchiveWithMode(t, fileName, contents, 0o755)
}

func buildTarGzArchiveWithMode(t *testing.T, fileName string, contents []byte, mode int64) []byte {
	t.Helper()

	buffer := &bytes.Buffer{}
	gzipWriter := gzip.NewWriter(buffer)
	tarWriter := tar.NewWriter(gzipWriter)
	header := &tar.Header{Name: fileName, Mode: mode, Size: int64(len(contents))}
	if err := tarWriter.WriteHeader(header); err != nil {
		t.Fatalf("WriteHeader: %v", err)
	}
	if _, err := tarWriter.Write(contents); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}

	return buffer.Bytes()
}

func buildZipArchive(t *testing.T, fileName string, contents []byte) []byte {
	t.Helper()
	return buildZipArchiveWithMode(t, fileName, contents, 0)
}

func buildZipArchiveWithMode(t *testing.T, fileName string, contents []byte, mode fs.FileMode) []byte {
	t.Helper()

	buffer := &bytes.Buffer{}
	zipWriter := zip.NewWriter(buffer)
	header := &zip.FileHeader{Name: fileName}
	header.SetMode(mode)
	fileWriter, err := zipWriter.CreateHeader(header)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := fileWriter.Write(contents); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	return buffer.Bytes()
}

func TestRunnerValidationAndErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("runner not configured", func(t *testing.T) {
		var runner *Runner
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("repository not configured", func(t *testing.T) {
		runner := newTestRunner(Dependencies{Releases: &stubReleaseClient{}})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("latest release failure", func(t *testing.T) {
		runner := newTestRunner(Dependencies{
			Releases:        &stubReleaseClient{latestErr: apperrors.New(apperrors.KindTransient, "boom", nil)},
			RepositoryOwner: "vriesdemichael",
			RepositoryName:  "bitbucket-data-center-cli",
		})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
	})

	t.Run("missing latest tag", func(t *testing.T) {
		runner := newTestRunner(Dependencies{
			Releases:        &stubReleaseClient{release: githubrelease.Release{}},
			RepositoryOwner: "vriesdemichael",
			RepositoryName:  "bitbucket-data-center-cli",
		})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent error, got %v", err)
		}
	})

	t.Run("invalid latest semver", func(t *testing.T) {
		runner := newTestRunner(Dependencies{
			Releases:        &stubReleaseClient{release: githubrelease.Release{TagName: "latest"}},
			RepositoryOwner: "vriesdemichael",
			RepositoryName:  "bitbucket-data-center-cli",
		})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent error, got %v", err)
		}
	})

	t.Run("missing executable path", func(t *testing.T) {
		runner := newTestRunner(Dependencies{
			Releases:        &stubReleaseClient{release: githubrelease.Release{TagName: "v1.2.0"}},
			RepositoryOwner: "vriesdemichael",
			RepositoryName:  "bitbucket-data-center-cli",
			ExecutablePath: func() (string, error) {
				return "", os.ErrNotExist
			},
		})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("signature verifier not configured", func(t *testing.T) {
		runner := newTestRunner(Dependencies{
			Releases:        &stubReleaseClient{release: githubrelease.Release{TagName: "v1.2.0"}},
			RepositoryOwner: "vriesdemichael",
			RepositoryName:  "bitbucket-data-center-cli",
			CurrentVersion:  func() string { return "v1.1.0" },
			ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
		})
		runner.verifier = nil
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})
}

func TestRunnerUpdateErrorCases(t *testing.T) {
	t.Parallel()

	baseRelease := releaseWithSignatureBundle(githubrelease.Release{
		TagName: "v1.2.0",
		Assets:  []githubrelease.Asset{{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "archive"}, {Name: "sha256sums.txt", BrowserDownloadURL: "checksums"}},
	})

	t.Run("missing archive asset", func(t *testing.T) {
		client := &stubReleaseClient{release: releaseWithSignatureBundle(githubrelease.Release{TagName: "v1.2.0", Assets: []githubrelease.Asset{{Name: "sha256sums.txt", BrowserDownloadURL: "checksums"}}})}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Fatalf("expected not found error, got %v", err)
		}
	})

	t.Run("missing checksum asset", func(t *testing.T) {
		client := &stubReleaseClient{release: githubrelease.Release{TagName: "v1.2.0", Assets: []githubrelease.Asset{{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "archive"}}}}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Fatalf("expected not found error, got %v", err)
		}
	})

	t.Run("missing signature bundle asset", func(t *testing.T) {
		client := &stubReleaseClient{release: baseRelease}
		client.release.Assets = client.release.Assets[:2]
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Fatalf("expected not found error, got %v", err)
		}
	})

	t.Run("missing checksum entry", func(t *testing.T) {
		client := &stubReleaseClient{release: baseRelease, downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte("deadbeef  other.tar.gz\n")})}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent error, got %v", err)
		}
	})

	t.Run("checksum download failure", func(t *testing.T) {
		client := &stubReleaseClient{release: baseRelease, downloadErrs: map[string]error{"checksums": apperrors.New(apperrors.KindTransient, "download failed", nil)}}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
	})

	t.Run("signature bundle download failure", func(t *testing.T) {
		client := &stubReleaseClient{release: baseRelease, downloads: downloadsWithSignatureBundle(nil), downloadErrs: map[string]error{"bundle": apperrors.New(apperrors.KindTransient, "download failed", nil)}}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
	})

	t.Run("signature verification failure", func(t *testing.T) {
		client := &stubReleaseClient{release: baseRelease, downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte("deadbeef  bb_1.2.0_linux_amd64.tar.gz\n")})}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }, Verifier: &stubSignatureVerifier{err: apperrors.New(apperrors.KindPermanent, "bad signature", nil)}})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent error, got %v", err)
		}
	})

	t.Run("transient signature verification failure", func(t *testing.T) {
		client := &stubReleaseClient{release: baseRelease, downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte("deadbeef  bb_1.2.0_linux_amd64.tar.gz\n")})}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }, Verifier: &stubSignatureVerifier{err: apperrors.New(apperrors.KindTransient, "try later", nil)}})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
		if err == nil || !strings.Contains(err.Error(), "retry, or install bb with Homebrew, the .deb or .rpm package, or a manual install instead") {
			t.Fatalf("expected retry guidance in error, got %v", err)
		}
	})

	t.Run("checksum mismatch", func(t *testing.T) {
		archive := buildTarGzArchive(t, "bb", []byte("new-binary"))
		client := &stubReleaseClient{release: baseRelease, downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte("deadbeef  bb_1.2.0_linux_amd64.tar.gz\n"), "archive": archive})}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent error, got %v", err)
		}
	})

	t.Run("archive download failure", func(t *testing.T) {
		archive := buildTarGzArchive(t, "bb", []byte("new-binary"))
		checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_linux_amd64.tar.gz")
		client := &stubReleaseClient{release: baseRelease, downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte(checksum)}), downloadErrs: map[string]error{"archive": apperrors.New(apperrors.KindTransient, "download failed", nil)}}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
	})

	t.Run("archive extraction failure", func(t *testing.T) {
		archive := []byte("not-an-archive")
		checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_linux_amd64.tar.gz")
		client := &stubReleaseClient{release: baseRelease, downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte(checksum), "archive": archive})}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent error, got %v", err)
		}
	})

	t.Run("write binary error", func(t *testing.T) {
		archive := buildTarGzArchive(t, "bb", []byte("new-binary"))
		checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_linux_amd64.tar.gz")
		client := &stubReleaseClient{release: baseRelease, downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte(checksum), "archive": archive})}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }, WriteBinary: func(string, []byte, fs.FileMode) error {
			return apperrors.New(apperrors.KindInternal, "write failed", nil)
		}})
		_, err := runner.Run(context.Background(), Options{})
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})
}

func TestRunnerWindowsAndVersionComparisonPaths(t *testing.T) {
	t.Parallel()

	t.Run("windows zip update", func(t *testing.T) {
		archive := buildZipArchive(t, "bb.exe", []byte("windows-binary"))
		checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_windows_amd64.zip")
		client := &stubReleaseClient{release: releaseWithSignatureBundle(githubrelease.Release{TagName: "v1.2.0", Assets: []githubrelease.Asset{{Name: "bb_1.2.0_windows_amd64.zip", BrowserDownloadURL: "archive"}, {Name: "sha256sums.txt", BrowserDownloadURL: "checksums"}}}), downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte(checksum), "archive": archive})}
		targetPath := filepath.Join(t.TempDir(), "bb.exe")
		if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
			t.Fatalf("seed target: %v", err)
		}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return targetPath, nil }, Platform: func() (string, string) { return "windows", "amd64" }})
		result, err := runner.Run(context.Background(), Options{DryRun: true})
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if !result.UpdateAvailable || result.PlannedAction != "replace" {
			t.Fatalf("expected dry-run windows plan, got %+v", result)
		}
	})

	// Windows replaces the binary before Run returns, as every other system
	// does: the one it replaced is set aside for the next run to delete.
	t.Run("windows apply replaces the binary before it returns", func(t *testing.T) {
		archive := buildZipArchive(t, "bb.exe", []byte("windows-binary"))
		checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_windows_amd64.zip")
		client := &stubReleaseClient{release: releaseWithSignatureBundle(githubrelease.Release{TagName: "v1.2.0", Assets: []githubrelease.Asset{{Name: "bb_1.2.0_windows_amd64.zip", BrowserDownloadURL: "archive"}, {Name: "sha256sums.txt", BrowserDownloadURL: "checksums"}}}), downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte(checksum), "archive": archive})}
		targetDir := t.TempDir()
		targetPath := filepath.Join(targetDir, "bb.exe")
		if err := os.WriteFile(targetPath, []byte("old"), 0o755); err != nil {
			t.Fatalf("seed target: %v", err)
		}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.1.0" }, ExecutablePath: func() (string, error) { return targetPath, nil }, Platform: func() (string, string) { return "windows", "amd64" }})
		result, err := runner.Run(context.Background(), Options{})
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if !result.Applied || result.PlannedAction != "replace" || result.InstallPath != targetPath {
			t.Fatalf("expected the update applied in place, got %+v", result)
		}
		assertHolds(t, targetPath, "windows-binary")

		entries := directoryEntries(t, targetDir)
		if len(entries) != 2 || entries[0] != "bb.exe" || !isOldBinary("bb.exe", entries[1]) {
			t.Fatalf("install directory holds %v, want bb.exe and the binary it replaced", entries)
		}
		assertHolds(t, filepath.Join(targetDir, entries[1]), "old")

		osFileSystem().removeLeftovers(targetPath)
		if entries := directoryEntries(t, targetDir); !slices.Equal(entries, []string{"bb.exe"}) {
			t.Fatalf("after the next run's cleanup the install directory holds %v, want only bb.exe", entries)
		}
	})

	t.Run("current newer", func(t *testing.T) {
		client := &stubReleaseClient{release: githubrelease.Release{TagName: "v1.2.0"}}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "v1.3.0" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }})
		result, err := runner.Run(context.Background(), Options{})
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if result.UpdateAvailable || result.Comparison != "current_newer" {
			t.Fatalf("expected current_newer result, got %+v", result)
		}
	})

	t.Run("unknown current version", func(t *testing.T) {
		archive := buildTarGzArchive(t, "bb", []byte("new-binary"))
		checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_linux_amd64.tar.gz")
		client := &stubReleaseClient{release: releaseWithSignatureBundle(githubrelease.Release{TagName: "v1.2.0", Assets: []githubrelease.Asset{{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "archive"}, {Name: "sha256sums.txt", BrowserDownloadURL: "checksums"}}}), downloads: downloadsWithSignatureBundle(map[string][]byte{"checksums": []byte(checksum), "archive": archive})}
		runner := newTestRunner(Dependencies{Releases: client, RepositoryOwner: "vriesdemichael", RepositoryName: "bitbucket-data-center-cli", CurrentVersion: func() string { return "dev" }, ExecutablePath: func() (string, error) { return "/tmp/bb", nil }, Platform: func() (string, string) { return "linux", "amd64" }})
		result, err := runner.Run(context.Background(), Options{DryRun: true})
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
		if !result.UpdateAvailable || result.Comparison != "unknown_current" {
			t.Fatalf("expected unknown_current result, got %+v", result)
		}
	})
}

func TestUpdateHelpers(t *testing.T) {
	t.Parallel()

	if got := archiveName("v1.2.3", "linux", "amd64"); got != "bb_1.2.3_linux_amd64.tar.gz" {
		t.Fatalf("unexpected archive name: %s", got)
	}
	if got := archiveName("v1.2.3", "windows", "arm64"); got != "bb_1.2.3_windows_arm64.zip" {
		t.Fatalf("unexpected archive name: %s", got)
	}
	if binaryFileName("windows") != "bb.exe" || binaryFileName("linux") != "bb" {
		t.Fatal("unexpected binary file names")
	}
	if _, ok := findAsset([]githubrelease.Asset{{Name: "a"}}, "a"); !ok {
		t.Fatal("expected asset to be found")
	}
	if _, ok := findAsset([]githubrelease.Asset{{Name: "a"}}, "b"); ok {
		t.Fatal("expected asset miss")
	}

	checksums, err := parseChecksums([]byte("deadbeef  file.tar.gz\n"))
	if err != nil || checksums["file.tar.gz"] != "deadbeef" {
		t.Fatalf("unexpected checksums parse result: %+v %v", checksums, err)
	}
	checksums, err = parseChecksums([]byte("deadbeef  ./bb_1.2.0_linux_amd64.tar.gz\n"))
	if err != nil || checksums["bb_1.2.0_linux_amd64.tar.gz"] != "deadbeef" {
		t.Fatalf("expected normalized checksum file name, got %+v %v", checksums, err)
	}
	if _, err := parseChecksums([]byte("broken-line")); !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("expected malformed checksum error, got %v", err)
	}
	if _, err := parseChecksums([]byte("\n")); !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("expected empty checksum error, got %v", err)
	}

	tarArchive := buildTarGzArchive(t, "bb", []byte("payload"))
	if extracted, _, err := extractBinary("archive.tar.gz", "bb", tarArchive); err != nil || string(extracted) != "payload" {
		t.Fatalf("unexpected tar extract result: %q %v", string(extracted), err)
	}
	tarDefaultMode := buildTarGzArchive(t, "bb", []byte("payload"))
	if _, mode, err := extractBinaryFromTarGz("bb", tarDefaultMode); err != nil || mode == 0 {
		t.Fatalf("expected tar mode, got %o %v", mode, err)
	}
	if _, mode, err := extractBinaryFromZip("bb.exe", buildZipArchive(t, "bb.exe", []byte("payload"))); err != nil || mode != 0o755 {
		t.Fatalf("expected default zip mode, got %o %v", mode, err)
	}
	zipArchive := buildZipArchive(t, "bb.exe", []byte("payload"))
	if extracted, _, err := extractBinary("archive.zip", "bb.exe", zipArchive); err != nil || string(extracted) != "payload" {
		t.Fatalf("unexpected zip extract result: %q %v", string(extracted), err)
	}
	if _, _, err := extractBinaryFromZip("bb.exe", buildZipArchive(t, "other.exe", []byte("payload"))); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("expected missing zip binary error, got %v", err)
	}
	invalidTarBuffer := &bytes.Buffer{}
	gzipWriter := gzip.NewWriter(invalidTarBuffer)
	if _, err := gzipWriter.Write([]byte("not-a-tar-stream")); err != nil {
		t.Fatalf("gzip write: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if _, _, err := extractBinaryFromTarGz("bb", invalidTarBuffer.Bytes()); !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("expected tar read error, got %v", err)
	}
	if _, _, err := extractBinary("archive.tar.gz", "bb", []byte("not-a-tar")); !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("expected bad tar error, got %v", err)
	}
	if _, _, err := extractBinary("archive.zip", "bb.exe", []byte("not-a-zip")); !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("expected bad zip error, got %v", err)
	}
	if _, _, err := extractBinary("archive.bin", "bb", []byte("x")); !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("expected unsupported archive error, got %v", err)
	}

	missingArchive := buildTarGzArchive(t, "other", []byte("payload"))
	if _, _, err := extractBinary("archive.tar.gz", "bb", missingArchive); !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("expected missing binary error, got %v", err)
	}

	if normalizeSemver("1.2.3") != "v1.2.3" || normalizeSemver("v1.2.3-beta.1") != "v1.2.3-beta.1" || normalizeSemver("bad") != "" {
		t.Fatal("unexpected normalizeSemver results")
	}
	if normalizeSemver("  ") != "" {
		t.Fatal("expected empty normalized version")
	}
	if compareSemver("v1.2.3", "v1.2.4") >= 0 || compareSemver("v1.2.4", "v1.2.3") <= 0 || compareSemver("v1.2.3", "v1.2.3") != 0 {
		t.Fatal("unexpected compareSemver results")
	}
	if compareSemver("alpha", "beta") >= 0 {
		t.Fatal("expected fallback lexical comparison for invalid semver")
	}
	if compareSemver("v1.2.3-beta.1", "v1.2.3") >= 0 {
		t.Fatal("expected prerelease to sort before stable")
	}
	if comparePrerelease("alpha.1", "alpha.2") >= 0 || comparePrerelease("", "alpha") <= 0 || comparePrerelease("alpha", "") >= 0 || comparePrerelease("alpha.1", "alpha") <= 0 || comparePrerelease("alpha", "alpha") != 0 || compareIdentifier("1", "alpha") >= 0 || compareIdentifier("alpha", "1") <= 0 || compareIdentifier("alpha", "beta") >= 0 || compareIdentifier("2", "2") != 0 {
		t.Fatal("unexpected prerelease comparison results")
	}
	if compareInt(1, 2) >= 0 || compareInt(2, 1) <= 0 || compareInt(2, 2) != 0 {
		t.Fatal("unexpected compareInt results")
	}
	if update, comparison := isUpdateAvailable("v1.0.1", "v1.0.1", "v1.0.1", "v1.0.1"); update || comparison != "equal" {
		t.Fatalf("unexpected equal result: %v %s", update, comparison)
	}
	if update, comparison := isUpdateAvailable("dev", "", "dev", "v1.0.1"); update || comparison != "equal" {
		t.Fatalf("unexpected unknown-current equal result: %v %s", update, comparison)
	}
	if value, ok := parseSemver("v1.2.3+build.5"); !ok || value.original != "v1.2.3" {
		t.Fatalf("unexpected build metadata parse result: %+v %v", value, ok)
	}
	if _, ok := parseSemver("1.2.3"); ok {
		t.Fatal("expected missing-v semver parse failure")
	}
	if _, ok := parseSemver("v1.2"); ok {
		t.Fatal("expected short semver parse failure")
	}
	if _, ok := parseSemver("v1.x.3"); ok {
		t.Fatal("expected invalid major/minor semver parse failure")
	}
	if update, comparison := isUpdateAvailable("v1.0.0", "v1.0.0", "v1.0.1", "v1.0.1"); !update || comparison != "upgrade_available" {
		t.Fatalf("unexpected isUpdateAvailable result: %v %s", update, comparison)
	}
	if files := SortedChecksumFiles(map[string]string{"b": "2", "a": "1"}); len(files) != 2 || files[0] != "a" || files[1] != "b" {
		t.Fatalf("unexpected sorted files: %+v", files)
	}
}

func TestRunnerSkipsSignatureVerificationUnderPolicy(t *testing.T) {
	t.Parallel()

	archive := buildTarGzArchive(t, "bb", []byte("new-binary"))
	checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_linux_amd64.tar.gz")

	// A mirror that re-publishes the artifacts without a Sigstore bundle: the
	// signature asset is absent entirely, which is why the bundle lookup must
	// not be fatal once policy has accepted an unverified update.
	client := &stubReleaseClient{
		release: githubrelease.Release{
			TagName: "v1.2.0",
			Assets: []githubrelease.Asset{
				{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://mirror.internal/bb_1.2.0_linux_amd64.tar.gz"},
				{Name: "sha256sums.txt", BrowserDownloadURL: "https://mirror.internal/sha256sums.txt"},
			},
		},
		downloads: map[string][]byte{
			"https://mirror.internal/sha256sums.txt":              []byte(checksum),
			"https://mirror.internal/bb_1.2.0_linux_amd64.tar.gz": archive,
		},
	}

	verifier := &stubSignatureVerifier{}
	var written []byte
	runner := NewRunner(Dependencies{
		Releases:        client,
		RepositoryOwner: "vriesdemichael",
		RepositoryName:  "bitbucket-data-center-cli",
		CurrentVersion:  func() string { return "v1.1.0" },
		ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
		Platform:        func() (string, string) { return "linux", "amd64" },
		WriteBinary: func(_ string, binary []byte, _ fs.FileMode) error {
			written = binary
			return nil
		},
		Verifier:                  verifier,
		SkipSignatureVerification: true,
		TrustSource:               "none (signature verification disabled by administrative policy)",
	})

	result, err := runner.Run(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if verifier.calls != 0 {
		t.Fatalf("expected no verification calls, got %d", verifier.calls)
	}
	if !result.SignatureSkipped || result.SignatureVerified {
		t.Fatalf("expected a skipped signature, got %+v", result)
	}
	if !result.ChecksumVerified {
		t.Fatal("checksum verification must remain mandatory when signatures are skipped")
	}
	if result.TrustSource == "" {
		t.Fatal("expected the trust source to be reported")
	}
	if string(written) != "new-binary" {
		t.Fatalf("unexpected binary written: %s", written)
	}
}

func TestRunnerStillRequiresChecksumsWhenSignatureIsSkipped(t *testing.T) {
	t.Parallel()

	client := &stubReleaseClient{
		release: githubrelease.Release{
			TagName: "v1.2.0",
			Assets: []githubrelease.Asset{
				{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://mirror.internal/bb_1.2.0_linux_amd64.tar.gz"},
			},
		},
	}

	runner := NewRunner(Dependencies{
		Releases:                  client,
		RepositoryOwner:           "vriesdemichael",
		RepositoryName:            "bitbucket-data-center-cli",
		CurrentVersion:            func() string { return "v1.1.0" },
		ExecutablePath:            func() (string, error) { return "/tmp/bb", nil },
		Platform:                  func() (string, string) { return "linux", "amd64" },
		SkipSignatureVerification: true,
	})

	_, err := runner.Run(context.Background(), Options{})
	if !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("expected the missing checksum manifest to be fatal, got: %v", err)
	}
}

func TestRunnerReportsUnavailableTrustMaterialDistinctly(t *testing.T) {
	t.Parallel()

	archive := buildTarGzArchive(t, "bb", []byte("new-binary"))
	checksum := fmt.Sprintf("%s  %s\n", sha256Hex(archive), "bb_1.2.0_linux_amd64.tar.gz")

	client := &stubReleaseClient{
		release: releaseWithSignatureBundle(githubrelease.Release{
			TagName: "v1.2.0",
			Assets: []githubrelease.Asset{
				{Name: "bb_1.2.0_linux_amd64.tar.gz", BrowserDownloadURL: "https://example.test/bb_1.2.0_linux_amd64.tar.gz"},
				{Name: "sha256sums.txt", BrowserDownloadURL: "https://example.test/sha256sums.txt"},
			},
		}),
		downloads: downloadsWithSignatureBundle(map[string][]byte{
			"https://example.test/sha256sums.txt": []byte(checksum),
		}),
	}

	trustRootErr := apperrors.New(
		apperrors.KindTransient,
		"failed to load Sigstore trusted roots",
		fmt.Errorf("%w: dial tcp: i/o timeout", updatesigstore.ErrTrustedRootUnavailable),
	)

	runner := newTestRunner(Dependencies{
		Releases:        client,
		RepositoryOwner: "vriesdemichael",
		RepositoryName:  "bitbucket-data-center-cli",
		CurrentVersion:  func() string { return "v1.1.0" },
		ExecutablePath:  func() (string, error) { return "/tmp/bb", nil },
		Platform:        func() (string, string) { return "linux", "amd64" },
		Verifier:        &stubSignatureVerifier{err: trustRootErr},
	})

	_, err := runner.Run(context.Background(), Options{DryRun: true})
	if err == nil {
		t.Fatal("expected an error when trust material cannot be loaded")
	}
	if !strings.Contains(err.Error(), "update_trusted_root") {
		t.Fatalf("expected the message to name the offline trust root setting, got: %v", err)
	}
	if strings.Contains(err.Error(), "install bb with") {
		t.Fatalf("expected trust material failures not to be reported as a bad signature, got: %v", err)
	}
}
