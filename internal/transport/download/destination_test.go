package download

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// entries lists what a directory holds, so a test can see a temporary file that
// was left behind.
func entries(t *testing.T, directory string) []string {
	t.Helper()

	listed, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read %s: %v", directory, err)
	}
	names := make([]string, 0, len(listed))
	for _, entry := range listed {
		names = append(names, entry.Name())
	}

	return names
}

// TestAFailedDownloadLeavesNoFileBehind is the reason File exists: a download
// written into its target left a truncated file when it failed, and truncated
// the file already there before the first byte arrived. Now a failure leaves
// the directory as it was, and a success leaves exactly the target.
// mock-inventory: transport-fault — a server that drops its answer mid-body;
// the subject is what the downloader leaves on disk.
func TestAFailedDownloadLeavesNoFileBehind(t *testing.T) {
	t.Parallel()

	body := payload(50_000)
	var dropping atomic.Bool
	dropping.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if dropping.Load() {
			_, _ = (&cutOff{ResponseWriter: writer, limit: 20_000}).Write(body)
		}
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	directory := t.TempDir()
	target := filepath.Join(directory, "archive.zip")
	if err := os.WriteFile(target, []byte("the archive from last week"), 0o600); err != nil {
		t.Fatalf("write the existing archive: %v", err)
	}

	download := func() error {
		file, err := CreateFile(target)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		defer file.Discard()

		if _, err := testDownloader(server.Client(), time.Second, 1).Get(context.Background(), Request{URL: server.URL}, file); err != nil {
			return err
		}

		return file.Commit()
	}

	if err := download(); !apperrors.IsKind(err, apperrors.KindTransient) {
		t.Fatalf("got %v, want the dropped connection reported", err)
	}
	if got := entries(t, directory); len(got) != 1 || got[0] != "archive.zip" {
		t.Fatalf("a failed download left %q in the directory", got)
	}
	if kept, _ := os.ReadFile(target); string(kept) != "the archive from last week" {
		t.Fatalf("a failed download changed the file already there to %d bytes", len(kept))
	}

	dropping.Store(false)
	if err := download(); err != nil {
		t.Fatalf("download: %v", err)
	}
	if got := entries(t, directory); len(got) != 1 || got[0] != "archive.zip" {
		t.Fatalf("a completed download left %q in the directory", got)
	}
	if written, _ := os.ReadFile(target); !bytes.Equal(written, body) {
		t.Fatalf("the target holds %d bytes that differ from the body served", len(written))
	}
}

// TestAFileStartsAgainFromItsFirstByte: a download that has to start over,
// into a file, leaves the file holding the body once.
// mock-inventory: transport-fault — a server without ranges dropping its first
// answer; the subject is File's rewind.
func TestAFileStartsAgainFromItsFirstByte(t *testing.T) {
	t.Parallel()

	body := payload(30_000)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if hits.Add(1) == 1 {
			_, _ = (&cutOff{ResponseWriter: writer, limit: 12_345}).Write(body)
		}
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "file.bin")
	file, err := CreateFile(target)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer file.Discard()

	if _, err := testDownloader(server.Client(), time.Second, 1).Get(context.Background(), Request{URL: server.URL}, file); err != nil {
		t.Fatalf("download: %v", err)
	}
	if err := file.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if written, _ := os.ReadFile(target); !bytes.Equal(written, body) {
		t.Fatalf("the file holds %d bytes, want the body once", len(written))
	}
}

// TestAFileThatDoesNotFinishWritingIsNotPutInPlace: the close is the last
// place a write that did not land shows itself, so a failed one is reported
// and nothing reaches the target. Closing the file first is the portable way
// to make the close fail.
func TestAFileThatDoesNotFinishWritingIsNotPutInPlace(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	target := filepath.Join(directory, "archive.zip")
	file, err := CreateFile(target)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := file.Write([]byte("partial")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = file.temporary.Close()

	err = file.Commit()
	if !apperrors.IsKind(err, apperrors.KindInternal) || !strings.Contains(err.Error(), target) {
		t.Fatalf("got %v, want the failure to finish writing, naming the target", err)
	}
	if got := entries(t, directory); len(got) != 0 {
		t.Fatalf("a file that did not finish writing left %q", got)
	}

	// Discard after a Commit does nothing, and a second Commit is refused.
	file.Discard()
	if err := file.Commit(); err == nil {
		t.Fatal("a second commit was accepted")
	}
}

// TestAFileThatCannotBePutInPlaceLeavesNothing: a rename that fails -- here
// onto a directory that appeared during the download -- is reported, and the
// temporary file goes with it.
func TestAFileThatCannotBePutInPlaceLeavesNothing(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	target := filepath.Join(directory, "archive.zip")
	file, err := CreateFile(target)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := file.Write([]byte("complete")); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(target, "occupied"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if err := file.Commit(); !apperrors.IsKind(err, apperrors.KindInternal) || !strings.Contains(err.Error(), "in place") {
		t.Fatalf("got %v, want the failed rename reported", err)
	}
	if got := entries(t, directory); len(got) != 1 || got[0] != "archive.zip" {
		t.Fatalf("a failed rename left %q", got)
	}
}

// TestATargetThatCannotBeWrittenFailsBeforeTheDownload covers the checks
// CreateFile makes before anything is fetched.
func TestATargetThatCannotBeWrittenFailsBeforeTheDownload(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()

	if _, err := CreateFile(directory); !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("got %v, want a directory refused as a target", err)
	}

	missing := filepath.Join(directory, "no-such-directory", "archive.zip")
	if _, err := CreateFile(missing); !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), missing) {
		t.Fatalf("got %v, want a target in a missing directory refused, naming it", err)
	}
}

// TestADestinationSaysWhetherItCanStartAgain covers Rewind on each
// destination: memory and a file always can, a stream only while nothing has
// gone out.
func TestADestinationSaysWhetherItCanStartAgain(t *testing.T) {
	t.Parallel()

	var memory Memory
	_, _ = memory.Write([]byte("held"))
	if !memory.Rewind() || len(memory.Bytes()) != 0 {
		t.Fatal("memory did not empty on rewind")
	}

	var out bytes.Buffer
	stream := To(&out)
	if !stream.Rewind() {
		t.Fatal("a stream with nothing written refused to start again")
	}
	_, _ = stream.Write([]byte("sent"))
	if stream.Rewind() {
		t.Fatal("a stream with bytes already out claimed it could start again")
	}

	target := filepath.Join(t.TempDir(), "file.bin")
	file, err := CreateFile(target)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer file.Discard()
	_, _ = file.Write([]byte("the first attempt"))
	if !file.Rewind() {
		t.Fatal("a file refused to start again")
	}
	_, _ = file.Write([]byte("second"))
	if err := file.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if written, _ := os.ReadFile(target); string(written) != "second" {
		t.Fatalf("the file holds %q after a rewind, want only what followed it", written)
	}
}
