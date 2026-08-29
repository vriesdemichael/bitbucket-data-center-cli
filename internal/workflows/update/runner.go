package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	githubrelease "github.com/vriesdemichael/bitbucket-server-cli/internal/transport/githubrelease"
	updatesigstore "github.com/vriesdemichael/bitbucket-server-cli/internal/transport/sigstore"
)

type ReleaseClient interface {
	Latest(ctx context.Context, owner, repo string) (githubrelease.Release, error)
	Download(ctx context.Context, assetURL string) ([]byte, error)
}

type Options struct {
	DryRun bool
}

type SignatureVerifier interface {
	VerifyBlob(ctx context.Context, artifact, bundleJSON []byte) (updatesigstore.Verification, error)
}

type Result struct {
	CurrentVersion           string `json:"current_version"`
	LatestVersion            string `json:"latest_version"`
	UpdateAvailable          bool   `json:"update_available"`
	UpToDate                 bool   `json:"up_to_date"`
	Applied                  bool   `json:"applied"`
	Scheduled                bool   `json:"scheduled"`
	Staged                   bool   `json:"staged"`
	DryRun                   bool   `json:"dry_run"`
	InstallPath              string `json:"install_path,omitempty"`
	StagedPath               string `json:"staged_path,omitempty"`
	SwapResultPath           string `json:"swap_result_path,omitempty"`
	ReleaseURL               string `json:"release_url,omitempty"`
	AssetName                string `json:"asset_name,omitempty"`
	AssetURL                 string `json:"asset_url,omitempty"`
	ChecksumAssetName        string `json:"checksum_asset_name,omitempty"`
	SignatureBundleAssetName string `json:"signature_bundle_asset_name,omitempty"`
	ChecksumAvailable        bool   `json:"checksum_available"`
	ChecksumVerified         bool   `json:"checksum_verified"`
	SignatureVerified        bool   `json:"signature_verified"`
	// SignatureSkipped reports that signature verification was deliberately
	// not performed because administrative policy set allow_unverified_update.
	SignatureSkipped bool `json:"signature_skipped"`
	// TrustSource names where the Sigstore trust material came from, so an
	// operator can confirm from `bb update --dry-run` that their offline trust
	// root is actually in use.
	TrustSource              string `json:"trust_source,omitempty"`
	SignatureIdentity        string `json:"signature_identity,omitempty"`
	SignatureIssuer          string `json:"signature_issuer,omitempty"`
	TransparencyLogVerified  bool   `json:"transparency_log_verified"`
	CurrentVersionComparable bool   `json:"current_version_comparable"`
	LatestVersionComparable  bool   `json:"latest_version_comparable"`
	TargetPlatform           string `json:"target_platform,omitempty"`
	PlannedAction            string `json:"planned_action,omitempty"`
	Comparison               string `json:"comparison,omitempty"`
}

type Runner struct {
	releases       ReleaseClient
	owner          string
	repo           string
	currentVersion func() string
	executablePath func() (string, error)
	platform       func() (string, string)
	writeBinary    func(string, []byte, fs.FileMode) error
	processID      func() int
	launchWindows  func(context.Context, windowsSwapLaunchOptions) error
	verifier       SignatureVerifier
	skipSignature  bool
	trustSource    string
}

type Dependencies struct {
	Releases        ReleaseClient
	RepositoryOwner string
	RepositoryName  string
	CurrentVersion  func() string
	ExecutablePath  func() (string, error)
	Platform        func() (string, string)
	WriteBinary     func(string, []byte, fs.FileMode) error
	ProcessID       func() int
	LaunchWindows   func(context.Context, windowsSwapLaunchOptions) error
	Verifier        SignatureVerifier
	// SkipSignatureVerification drops Sigstore verification. It exists for
	// allow_unverified_update, which is administrative policy only — checksum
	// verification stays mandatory either way.
	SkipSignatureVerification bool
	// TrustSource is a human-readable description of where trust material
	// comes from, reported in the result.
	TrustSource string
}

func NewRunner(deps Dependencies) *Runner {
	currentVersion := deps.CurrentVersion
	if currentVersion == nil {
		currentVersion = func() string { return "dev" }
	}

	executablePath := deps.ExecutablePath
	if executablePath == nil {
		executablePath = os.Executable
	}

	platform := deps.Platform
	if platform == nil {
		platform = func() (string, string) { return runtime.GOOS, runtime.GOARCH }
	}

	writeBinary := deps.WriteBinary
	if writeBinary == nil {
		writeBinary = replaceBinary
	}

	processID := deps.ProcessID
	if processID == nil {
		processID = os.Getpid
	}

	launchWindows := deps.LaunchWindows
	if launchWindows == nil {
		launchWindows = launchDetachedWindowsSwap
	}

	verifier := deps.Verifier
	if verifier == nil {
		verifier = updatesigstore.NewGitHubReleaseVerifier(deps.RepositoryOwner, deps.RepositoryName)
	}

	return &Runner{
		releases:       deps.Releases,
		owner:          strings.TrimSpace(deps.RepositoryOwner),
		repo:           strings.TrimSpace(deps.RepositoryName),
		currentVersion: currentVersion,
		executablePath: executablePath,
		platform:       platform,
		writeBinary:    writeBinary,
		processID:      processID,
		launchWindows:  launchWindows,
		verifier:       verifier,
		skipSignature:  deps.SkipSignatureVerification,
		trustSource:    strings.TrimSpace(deps.TrustSource),
	}
}

func (runner *Runner) Run(ctx context.Context, options Options) (Result, error) {
	if runner == nil || runner.releases == nil {
		return Result{}, apperrors.New(apperrors.KindInternal, "update runner is not configured", nil)
	}
	if runner.owner == "" || runner.repo == "" {
		return Result{}, apperrors.New(apperrors.KindInternal, "update repository is not configured", nil)
	}
	if runner.verifier == nil && !runner.skipSignature {
		return Result{}, apperrors.New(apperrors.KindInternal, "update signature verifier is not configured", nil)
	}

	currentVersion := strings.TrimSpace(runner.currentVersion())
	if currentVersion == "" {
		currentVersion = "dev"
	}

	goos, goarch := runner.platform()
	release, err := runner.releases.Latest(ctx, runner.owner, runner.repo)
	if err != nil {
		return Result{}, err
	}

	latestVersion := strings.TrimSpace(release.TagName)
	if latestVersion == "" {
		return Result{}, apperrors.New(apperrors.KindPermanent, "latest release is missing a tag name", nil)
	}

	currentNormalized := normalizeSemver(currentVersion)
	latestNormalized := normalizeSemver(latestVersion)
	if latestNormalized == "" {
		return Result{}, apperrors.New(apperrors.KindPermanent, fmt.Sprintf("latest release tag %q is not a semantic version", latestVersion), nil)
	}

	targetPath, err := runner.executablePath()
	if err != nil {
		return Result{}, apperrors.New(apperrors.KindInternal, "failed to determine current bb executable path", err)
	}

	result := Result{
		CurrentVersion:           currentVersion,
		LatestVersion:            latestVersion,
		DryRun:                   options.DryRun,
		InstallPath:              targetPath,
		ReleaseURL:               strings.TrimSpace(release.HTMLURL),
		CurrentVersionComparable: currentNormalized != "",
		LatestVersionComparable:  true,
		TargetPlatform:           fmt.Sprintf("%s/%s", goos, goarch),
		TrustSource:              runner.trustSource,
	}

	result.UpdateAvailable, result.Comparison = isUpdateAvailable(currentVersion, currentNormalized, latestVersion, latestNormalized)
	result.UpToDate = !result.UpdateAvailable && result.Comparison == "equal"
	if !result.UpdateAvailable {
		return result, nil
	}

	assetName := archiveName(latestVersion, goos, goarch)
	asset, ok := findAsset(release.Assets, assetName)
	if !ok {
		return Result{}, apperrors.New(apperrors.KindNotFound, fmt.Sprintf("release asset %q was not found", assetName), nil)
	}

	checksumAsset, ok := findAsset(release.Assets, "sha256sums.txt")
	if !ok {
		return Result{}, apperrors.New(apperrors.KindNotFound, "release checksum file sha256sums.txt was not found", nil)
	}

	signatureBundleAsset, hasSignatureBundle := findAsset(release.Assets, checksumAsset.Name+".sigstore.json")
	if !hasSignatureBundle && !runner.skipSignature {
		return Result{}, apperrors.New(apperrors.KindNotFound, "release signature bundle sha256sums.txt.sigstore.json was not found; use winget, scoop, or manual install", nil)
	}

	result.AssetName = asset.Name
	result.AssetURL = asset.BrowserDownloadURL
	result.ChecksumAssetName = checksumAsset.Name
	result.PlannedAction = plannedAction(goos)
	if hasSignatureBundle {
		result.SignatureBundleAssetName = signatureBundleAsset.Name
	}

	checksumsRaw, err := runner.releases.Download(ctx, checksumAsset.BrowserDownloadURL)
	if err != nil {
		return Result{}, err
	}

	if runner.skipSignature {
		// Policy has accepted an unauthenticated manifest. The checksums below
		// are still enforced, so this catches corruption but not tampering.
		result.SignatureSkipped = true
	} else {
		bundleRaw, err := runner.releases.Download(ctx, signatureBundleAsset.BrowserDownloadURL)
		if err != nil {
			return Result{}, err
		}

		signatureVerification, err := runner.verifier.VerifyBlob(ctx, checksumsRaw, bundleRaw)
		if err != nil {
			return Result{}, signatureFailure(err)
		}
		result.SignatureVerified = true
		result.SignatureIdentity = signatureVerification.CertificateIdentity
		result.SignatureIssuer = signatureVerification.CertificateOIDCIssuer
		result.TransparencyLogVerified = signatureVerification.TransparencyLogEntriesVerified > 0
	}

	checksums, err := parseChecksums(checksumsRaw)
	if err != nil {
		return Result{}, err
	}

	expectedChecksum, ok := checksums[asset.Name]
	if !ok {
		return Result{}, apperrors.New(apperrors.KindPermanent, fmt.Sprintf("checksum entry for %q was not found", asset.Name), nil)
	}
	result.ChecksumAvailable = true

	if options.DryRun {
		return result, nil
	}

	archiveBytes, err := runner.releases.Download(ctx, asset.BrowserDownloadURL)
	if err != nil {
		return Result{}, err
	}

	actualChecksum := sha256Hex(archiveBytes)
	if !strings.EqualFold(actualChecksum, expectedChecksum) {
		return Result{}, apperrors.New(apperrors.KindPermanent, fmt.Sprintf("checksum verification failed for %s", asset.Name), nil)
	}
	result.ChecksumVerified = true

	binaryName := binaryFileName(goos)
	binaryBytes, fileMode, err := extractBinary(asset.Name, binaryName, archiveBytes)
	if err != nil {
		return Result{}, err
	}
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		stagedPath, err := stageWindowsBinary(targetPath, binaryBytes, fileMode)
		if err != nil {
			return Result{}, err
		}

		swapResultPath := windowsSwapResultPath(targetPath)
		launchOptions := windowsSwapLaunchOptions{
			ParentPID:     runner.processID(),
			TargetPath:    targetPath,
			StagedPath:    stagedPath,
			ResultPath:    swapResultPath,
			WaitTimeout:   windowsSwapWaitTimeout,
			RetryInterval: windowsSwapRetryInterval,
			RetryTimeout:  windowsSwapRetryTimeout,
		}
		if err := runner.launchWindows(context.Background(), launchOptions); err != nil {
			kind := apperrors.KindOf(err)
			if kind == "" {
				kind = apperrors.KindInternal
			}
			return Result{}, apperrors.New(kind, fmt.Sprintf("failed to schedule Windows background update; staged binary remains at %s", stagedPath), err)
		}

		result.Scheduled = true
		result.Staged = true
		result.StagedPath = stagedPath
		result.SwapResultPath = swapResultPath
		return result, nil
	}

	if err := runner.writeBinary(targetPath, binaryBytes, fileMode); err != nil {
		return Result{}, err
	}

	result.Applied = true
	return result, nil
}

// signatureFailure separates "we could not obtain the trust material" from "the
// signature did not verify against it".
//
// They demand opposite responses — the first is a configuration or connectivity
// problem on this host, the second means the artifact is not the one we signed —
// and conflating them leaves an operator on an air-gapped host debugging a
// signature that is in fact perfectly good.
func signatureFailure(err error) error {
	kind := apperrors.KindOf(err)

	if errors.Is(err, updatesigstore.ErrTrustedRootUnavailable) {
		return apperrors.New(
			kind,
			"could not load the Sigstore trust material needed to verify the release manifest; without network access to the Sigstore TUF repository, deploy a trusted root and set update_trusted_root (or update_tuf_url) in system configuration",
			err,
		)
	}

	message := "failed to verify the signed release manifest; use winget, scoop, or manual install"
	if kind == apperrors.KindTransient {
		message = "failed to verify the signed release manifest right now; retry or use winget, scoop, or manual install"
	}
	return apperrors.New(kind, message, err)
}

func archiveName(version, goos, goarch string) string {
	versionNoPrefix := strings.TrimPrefix(strings.TrimSpace(version), "v")
	archiveBase := fmt.Sprintf("bb_%s_%s_%s", versionNoPrefix, strings.TrimSpace(goos), strings.TrimSpace(goarch))
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		return archiveBase + ".zip"
	}
	return archiveBase + ".tar.gz"
}

func binaryFileName(goos string) string {
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		return "bb.exe"
	}
	return "bb"
}

func findAsset(assets []githubrelease.Asset, name string) (githubrelease.Asset, bool) {
	target := strings.TrimSpace(name)
	for _, asset := range assets {
		if strings.TrimSpace(asset.Name) == target {
			return asset, true
		}
	}
	return githubrelease.Asset{}, false
}

func parseChecksums(raw []byte) (map[string]string, error) {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
	checksums := make(map[string]string)
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		parts := strings.Fields(trimmed)
		if len(parts) < 2 {
			return nil, apperrors.New(apperrors.KindPermanent, "release checksum file is malformed", nil)
		}

		fileName := strings.TrimSpace(strings.TrimPrefix(parts[len(parts)-1], "*"))
		fileName = strings.TrimPrefix(fileName, "./")
		fileName = filepath.Base(fileName)
		checksums[fileName] = strings.ToLower(strings.TrimSpace(parts[0]))
	}

	if len(checksums) == 0 {
		return nil, apperrors.New(apperrors.KindPermanent, "release checksum file is empty", nil)
	}

	return checksums, nil
}

func plannedAction(goos string) string {
	if strings.EqualFold(strings.TrimSpace(goos), "windows") {
		return "schedule_background_replace_after_exit"
	}
	return "replace"
}

func extractBinary(assetName, binaryName string, archiveBytes []byte) ([]byte, fs.FileMode, error) {
	trimmedAssetName := strings.TrimSpace(assetName)
	switch {
	case strings.HasSuffix(trimmedAssetName, ".tar.gz"):
		return extractBinaryFromTarGz(binaryName, archiveBytes)
	case strings.HasSuffix(trimmedAssetName, ".zip"):
		return extractBinaryFromZip(binaryName, archiveBytes)
	default:
		return nil, 0, apperrors.New(apperrors.KindPermanent, fmt.Sprintf("unsupported archive format for %s", assetName), nil)
	}
}

func extractBinaryFromTarGz(binaryName string, archiveBytes []byte) ([]byte, fs.FileMode, error) {
	gzipReader, err := gzip.NewReader(bytes.NewReader(archiveBytes))
	if err != nil {
		return nil, 0, apperrors.New(apperrors.KindPermanent, "failed to open tar.gz archive", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, apperrors.New(apperrors.KindPermanent, "failed to read tar.gz archive", err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}
		if filepath.Base(header.Name) != binaryName {
			continue
		}

		payload, err := io.ReadAll(tarReader)
		if err != nil {
			return nil, 0, apperrors.New(apperrors.KindPermanent, "failed to extract binary from tar.gz archive", err)
		}

		mode := fs.FileMode(header.Mode)
		if mode == 0 {
			mode = 0o755
		}
		return payload, mode, nil
	}

	return nil, 0, apperrors.New(apperrors.KindNotFound, fmt.Sprintf("archive does not contain %s", binaryName), nil)
}

func extractBinaryFromZip(binaryName string, archiveBytes []byte) ([]byte, fs.FileMode, error) {
	zipReader, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return nil, 0, apperrors.New(apperrors.KindPermanent, "failed to open zip archive", err)
	}

	for _, file := range zipReader.File {
		if filepath.Base(file.Name) != binaryName {
			continue
		}

		reader, err := file.Open()
		if err != nil {
			return nil, 0, apperrors.New(apperrors.KindPermanent, "failed to open zipped binary", err)
		}

		payload, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, 0, apperrors.New(apperrors.KindPermanent, "failed to extract binary from zip archive", readErr)
		}
		if closeErr != nil {
			return nil, 0, apperrors.New(apperrors.KindPermanent, "failed to close zipped binary", closeErr)
		}

		mode := file.Mode()
		if mode == 0 {
			mode = 0o755
		}
		return payload, mode, nil
	}

	return nil, 0, apperrors.New(apperrors.KindNotFound, fmt.Sprintf("archive does not contain %s", binaryName), nil)
}

func replaceBinary(targetPath string, binary []byte, mode fs.FileMode) error {
	resolvedTargetPath := strings.TrimSpace(targetPath)
	if resolvedTargetPath == "" {
		return apperrors.New(apperrors.KindValidation, "target executable path is required", nil)
	}

	targetDir := filepath.Dir(resolvedTargetPath)
	tempFile, err := os.CreateTemp(targetDir, ".bb-update-*")
	if err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to create temporary file for update", err)
	}

	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(binary); err != nil {
		_ = tempFile.Close()
		return apperrors.New(apperrors.KindInternal, "failed to write updated binary", err)
	}
	if err := tempFile.Close(); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to close updated binary", err)
	}

	finalMode := mode
	if info, err := os.Stat(resolvedTargetPath); err == nil {
		finalMode = info.Mode()
	}
	if finalMode == 0 {
		finalMode = 0o755
	}
	if err := os.Chmod(tempPath, finalMode); err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to set permissions on updated binary", err)
	}

	backupPath := resolvedTargetPath + ".bak"
	_ = os.Remove(backupPath)

	if _, err := os.Stat(resolvedTargetPath); err == nil {
		if err := os.Rename(resolvedTargetPath, backupPath); err != nil {
			return apperrors.New(apperrors.KindInternal, "failed to stage existing bb binary for replacement", err)
		}
	}

	if err := os.Rename(tempPath, resolvedTargetPath); err != nil {
		if _, backupErr := os.Stat(backupPath); backupErr == nil {
			_ = os.Rename(backupPath, resolvedTargetPath)
		}
		return apperrors.New(apperrors.KindInternal, "failed to replace bb binary", err)
	}

	cleanupTemp = false
	_ = os.Remove(backupPath)
	return nil
}

func stageWindowsBinary(targetPath string, binary []byte, mode fs.FileMode) (string, error) {
	resolvedTargetPath := strings.TrimSpace(targetPath)
	if resolvedTargetPath == "" {
		return "", apperrors.New(apperrors.KindValidation, "target executable path is required", nil)
	}

	targetDir := filepath.Dir(resolvedTargetPath)
	stagedPath := resolvedTargetPath + ".new"

	tempFile, err := os.CreateTemp(targetDir, ".bb-update-stage-*")
	if err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to create staged update file", err)
	}

	tempPath := tempFile.Name()
	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := tempFile.Write(binary); err != nil {
		_ = tempFile.Close()
		return "", apperrors.New(apperrors.KindInternal, "failed to write staged update file", err)
	}
	if err := tempFile.Close(); err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to close staged update file", err)
	}

	finalMode := mode
	if info, err := os.Stat(resolvedTargetPath); err == nil {
		finalMode = info.Mode()
	}
	if finalMode == 0 {
		finalMode = 0o755
	}
	if err := os.Chmod(tempPath, finalMode); err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to set permissions on staged update file", err)
	}

	_ = os.Remove(stagedPath)
	if err := os.Rename(tempPath, stagedPath); err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to stage Windows update binary", err)
	}

	cleanupTemp = false
	return stagedPath, nil
}

func sha256Hex(raw []byte) string {
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:])
}

func isUpdateAvailable(currentRaw, currentNormalized, latestRaw, latestNormalized string) (bool, string) {
	if currentNormalized == "" {
		if strings.TrimSpace(currentRaw) == strings.TrimSpace(latestRaw) {
			return false, "equal"
		}
		return true, "unknown_current"
	}

	comparison := compareSemver(currentNormalized, latestNormalized)
	switch {
	case comparison < 0:
		return true, "upgrade_available"
	case comparison == 0:
		return false, "equal"
	default:
		return false, "current_newer"
	}
}

func normalizeSemver(version string) string {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" {
		return ""
	}
	if !strings.HasPrefix(trimmed, "v") {
		trimmed = "v" + trimmed
	}
	parsed, ok := parseSemver(trimmed)
	if !ok {
		return ""
	}
	return parsed.original
}

type semverValue struct {
	original   string
	major      int
	minor      int
	patch      int
	prerelease string
}

func compareSemver(left, right string) int {
	leftValue, leftOK := parseSemver(left)
	rightValue, rightOK := parseSemver(right)
	if !leftOK || !rightOK {
		return strings.Compare(left, right)
	}

	if leftValue.major != rightValue.major {
		return compareInt(leftValue.major, rightValue.major)
	}
	if leftValue.minor != rightValue.minor {
		return compareInt(leftValue.minor, rightValue.minor)
	}
	if leftValue.patch != rightValue.patch {
		return compareInt(leftValue.patch, rightValue.patch)
	}

	return comparePrerelease(leftValue.prerelease, rightValue.prerelease)
}

func parseSemver(version string) (semverValue, bool) {
	trimmed := strings.TrimSpace(version)
	if !strings.HasPrefix(trimmed, "v") {
		return semverValue{}, false
	}

	withoutBuild := strings.SplitN(trimmed[1:], "+", 2)[0]
	parts := strings.SplitN(withoutBuild, "-", 2)
	core := strings.Split(parts[0], ".")
	if len(core) != 3 {
		return semverValue{}, false
	}

	major, err := strconv.Atoi(core[0])
	if err != nil {
		return semverValue{}, false
	}
	minor, err := strconv.Atoi(core[1])
	if err != nil {
		return semverValue{}, false
	}
	patch, err := strconv.Atoi(core[2])
	if err != nil {
		return semverValue{}, false
	}

	value := semverValue{
		original: "v" + fmt.Sprintf("%d.%d.%d", major, minor, patch),
		major:    major,
		minor:    minor,
		patch:    patch,
	}
	if len(parts) == 2 {
		value.prerelease = parts[1]
		value.original += "-" + value.prerelease
	}

	return value, true
}

func compareInt(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func comparePrerelease(left, right string) int {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	switch {
	case left == "" && right == "":
		return 0
	case left == "":
		return 1
	case right == "":
		return -1
	}

	leftParts := strings.Split(left, ".")
	rightParts := strings.Split(right, ".")
	limit := len(leftParts)
	if len(rightParts) > limit {
		limit = len(rightParts)
	}

	for index := 0; index < limit; index++ {
		if index >= len(leftParts) {
			return -1
		}
		if index >= len(rightParts) {
			return 1
		}

		comparison := compareIdentifier(leftParts[index], rightParts[index])
		if comparison != 0 {
			return comparison
		}
	}

	return 0
}

func compareIdentifier(left, right string) int {
	leftNumber, leftErr := strconv.Atoi(left)
	rightNumber, rightErr := strconv.Atoi(right)
	switch {
	case leftErr == nil && rightErr == nil:
		return compareInt(leftNumber, rightNumber)
	case leftErr == nil:
		return -1
	case rightErr == nil:
		return 1
	default:
		return strings.Compare(left, right)
	}
}

func SortedChecksumFiles(checksums map[string]string) []string {
	files := make([]string, 0, len(checksums))
	for name := range checksums {
		files = append(files, name)
	}
	sort.Strings(files)
	return files
}
