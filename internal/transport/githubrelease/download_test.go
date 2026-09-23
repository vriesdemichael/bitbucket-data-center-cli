package githubrelease

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/download"
)

// archiveBytes stands in for a release archive: bytes that are not text, so a
// download that altered any would not match.
func archiveBytes(size int) []byte {
	body := make([]byte, size)
	for index := range body {
		body[index] = byte(index*31 + index/7)
	}

	return body
}

// TestASlowMirrorIsNotHeldToTheRequestTimeout: the request timeout bounds each
// wait on a download, not the whole transfer. An archive that takes several
// timeouts to arrive, and never pauses for one, is downloaded; it used to fail
// once the timeout had passed, whatever was still arriving.
// mock-inventory: external-service — a release mirror trickling an archive out;
// the subject is bb update's download, not a Bitbucket answer.
func TestASlowMirrorIsNotHeldToTheRequestTimeout(t *testing.T) {
	t.Parallel()

	const chunks = 20
	archive := archiveBytes(chunks * 512)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/bb_1.2.0_linux_amd64.tar.gz" {
			http.NotFound(writer, request)

			return
		}
		writer.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		for index := range chunks {
			_, _ = writer.Write(archive[index*512 : (index+1)*512])
			writer.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer server.Close()

	// As the update command builds it: the request timeout on the client.
	const timeout = 300 * time.Millisecond
	client := NewClient(server.URL, &http.Client{Timeout: timeout, Transport: server.Client().Transport}, "bb/test")

	started := time.Now()
	body, err := client.Download(context.Background(), "bb_1.2.0_linux_amd64.tar.gz", anyLimit)
	if err != nil {
		t.Fatalf("an archive that kept arriving failed: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 2*timeout {
		t.Fatalf("the download took %s; it has to outlast the timeout for this test to mean anything", elapsed)
	}
	if !bytes.Equal(body, archive) {
		t.Fatalf("downloaded %d bytes that differ from the archive served", len(body))
	}
}

// TestAMirrorThatDropsAnAssetMidwayIsAskedForTheRest: a connection that breaks
// off mid-archive is retried as retry_count allows, resuming where it broke
// when the mirror serves ranges. Without retries it is reported, transient.
// mock-inventory: external-service — a release mirror, behind the standard
// library's range handling, whose first answer is cut off; the subject is bb
// update's download.
func TestAMirrorThatDropsAnAssetMidwayIsAskedForTheRest(t *testing.T) {
	t.Parallel()

	archive := archiveBytes(200_000)
	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		mu.Lock()
		ranges = append(ranges, request.Header.Get("Range"))
		first := len(ranges)%2 == 1
		mu.Unlock()

		writer.Header().Set("ETag", `"bb-1.2.0"`)
		if first {
			writer = &cutAfter{ResponseWriter: writer, limit: 64_000}
		}
		http.ServeContent(writer, request, "", time.Time{}, bytes.NewReader(archive))
	}))
	defer server.Close()

	resuming := NewClient(server.URL, server.Client(), "bb/test", Retries(2, time.Millisecond))
	body, err := resuming.Download(context.Background(), "bb_1.2.0_linux_amd64.tar.gz", anyLimit)
	if err != nil || !bytes.Equal(body, archive) {
		t.Fatalf("got %d bytes and %v, want the whole archive", len(body), err)
	}
	mu.Lock()
	asked := append([]string(nil), ranges...)
	ranges = nil
	mu.Unlock()
	if len(asked) != 2 || asked[1] != "bytes=64000-" {
		t.Fatalf("the mirror was asked for %q, want the rest after the first answer broke off", asked)
	}

	once := NewClient(server.URL, server.Client(), "bb/test")
	if _, err := once.Download(context.Background(), "bb_1.2.0_linux_amd64.tar.gz", anyLimit); !apperrors.IsKind(err, apperrors.KindTransient) {
		t.Fatalf("got %v, want a transient failure with no retries configured", err)
	}
}

// TestAReleaseFileOverItsCapIsRefused: a file over the cap the runner gives
// it is refused, permanent, and not asked for again.
// mock-inventory: external-service — a release mirror serving a checksum file
// far larger than any; the subject is bb update's cap.
func TestAReleaseFileOverItsCapIsRefused(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	hits := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		_, _ = writer.Write(archiveBytes(4096))
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), "bb/test", Retries(3, time.Millisecond))
	_, err := client.Download(context.Background(), "sha256sums.txt", 1024)

	var limit *download.LimitError
	if !apperrors.IsKind(err, apperrors.KindPermanent) || !errors.As(err, &limit) || !strings.Contains(err.Error(), "over the 1.0 KiB limit") {
		t.Fatalf("got %v, want the file refused as over its cap", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("a file over its cap was asked for %d times", hits)
	}
}

// TestAManifestOverItsCapIsRefused: the manifest stays an API call, read whole,
// and a mirror answering with megabytes where a manifest belongs is refused.
// mock-inventory: external-service — a release mirror answering with an
// oversized manifest; the subject is bb update's cap on it.
func TestAManifestOverItsCapIsRefused(t *testing.T) {
	t.Parallel()

	oversized := append([]byte(`{"tag_name":"v1.2.0","body":"`), bytes.Repeat([]byte("x"), maxReleaseMetadataBytes)...)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(oversized)
	}))
	defer server.Close()

	_, err := NewClient(server.URL, server.Client(), "bb/test").Latest(context.Background(), "owner", "repo")
	if !apperrors.IsKind(err, apperrors.KindPermanent) || !strings.Contains(err.Error(), "larger than 4 MiB") {
		t.Fatalf("got %v, want the manifest refused as larger than its cap", err)
	}
}

// cutAfter writes limit bytes of a body and then drops the connection, as a
// network does mid-transfer.
type cutAfter struct {
	http.ResponseWriter
	limit int
}

func (writer *cutAfter) Write(chunk []byte) (int, error) {
	if len(chunk) < writer.limit {
		writer.limit -= len(chunk)

		return writer.ResponseWriter.Write(chunk)
	}

	_, _ = writer.ResponseWriter.Write(chunk[:writer.limit])
	writer.ResponseWriter.(http.Flusher).Flush()
	panic(http.ErrAbortHandler)
}
