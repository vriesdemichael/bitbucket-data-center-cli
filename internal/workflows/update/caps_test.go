package update

import (
	"archive/zip"
	"bytes"
	"context"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestEachReleaseFileIsDownloadedUnderItsOwnCap: a download has no deadline
// for the whole transfer, so each file is held to a cap instead -- small ones
// for the files the archive is verified by, a large one for the archive.
func TestEachReleaseFileIsDownloadedUnderItsOwnCap(t *testing.T) {
	t.Parallel()

	client := mirrorServing(t, "v1.2.0")
	runner := newTestRunner(mirrorCheckDependencies(t, client, "v1.1.0", &stubSignatureVerifier{}))
	if _, err := runner.Run(context.Background(), Options{DryRun: true}); err != nil {
		t.Fatalf("dry run: %v", err)
	}

	want := map[string]int64{
		"https://mirror.internal/sha256sums.txt":               maxChecksumFileBytes,
		"https://mirror.internal/sha256sums.txt.sigstore.json": maxSignatureBundleBytes,
		"https://mirror.internal/bb_1.2.0_linux_amd64.tar.gz":  maxArchiveBytes,
	}
	if len(client.limits) != len(want) {
		t.Fatalf("downloaded %v, want each of %v once", client.limits, want)
	}
	for assetURL, limit := range want {
		if got := client.limits[assetURL]; got != limit {
			t.Errorf("%s was downloaded under a cap of %d, want %d", assetURL, got, limit)
		}
	}
	if maxChecksumFileBytes >= maxArchiveBytes || maxSignatureBundleBytes >= maxArchiveBytes {
		t.Error("a file the archive is verified by has a cap as large as the archive's")
	}
}

// TestAnArchiveThatUnpacksPastTheCapIsNotInstalled: the binary is capped as
// the archive unpacks, so an entry that decompresses to far more than the
// archive holds -- as a decompression bomb does -- is refused. The cap is what
// the entry unpacks to, not what its header or the archive's size claims.
func TestAnArchiveThatUnpacksPastTheCapIsNotInstalled(t *testing.T) {
	t.Parallel()

	const limit = 64 << 10
	// Zeros compress to almost nothing, which is what makes a bomb one.
	oversized := make([]byte, limit+1)
	atTheLimit := make([]byte, limit)

	for name, format := range map[string]struct {
		asset  string
		binary string
		build  func([]byte) []byte
	}{
		"tar.gz": {"bb_1.2.0_linux_amd64.tar.gz", "bb", func(contents []byte) []byte { return buildTarGzArchive(t, "bb", contents) }},
		"zip":    {"bb_1.2.0_windows_amd64.zip", "bb.exe", func(contents []byte) []byte { return buildDeflatedZipArchive(t, "bb.exe", contents) }},
	} {
		bomb := format.build(oversized)
		if len(bomb) > limit/16 {
			t.Fatalf("%s: the archive is %d bytes; the test needs one far smaller than what it unpacks to", name, len(bomb))
		}

		_, _, err := extractBinaryWithin(format.asset, format.binary, bomb, limit)
		if !apperrors.IsKind(err, apperrors.KindPermanent) || !strings.Contains(err.Error(), format.binary+" in the release archive unpacks to more than") {
			t.Fatalf("%s: got %v, want the binary refused as larger than the cap", name, err)
		}

		payload, _, err := extractBinaryWithin(format.asset, format.binary, format.build(atTheLimit), limit)
		if err != nil || len(payload) != limit {
			t.Fatalf("%s: a binary exactly at the cap came back as %d bytes and %v", name, len(payload), err)
		}
	}
}

// buildDeflatedZipArchive is buildZipArchive compressed, as a release's zip is.
func buildDeflatedZipArchive(t *testing.T, fileName string, contents []byte) []byte {
	t.Helper()

	buffer := &bytes.Buffer{}
	zipWriter := zip.NewWriter(buffer)
	fileWriter, err := zipWriter.CreateHeader(&zip.FileHeader{Name: fileName, Method: zip.Deflate})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := fileWriter.Write(contents); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := zipWriter.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	return buffer.Bytes()
}
