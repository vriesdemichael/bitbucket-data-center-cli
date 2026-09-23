package download

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// payload is what the fault servers serve: bytes no text handling passes
// through unchanged -- NUL, CR, LF, bytes that are not UTF-8 -- so a download
// that altered one does not match.
func payload(size int) []byte {
	body := make([]byte, size)
	for index := range body {
		body[index] = byte(index*7 + index/251)
	}

	return body
}

// cutOff lets a handler write limit bytes of its body and then drops the
// connection, as a network does mid-transfer.
type cutOff struct {
	http.ResponseWriter
	limit int
}

func (writer *cutOff) Write(chunk []byte) (int, error) {
	if len(chunk) < writer.limit {
		writer.limit -= len(chunk)

		return writer.ResponseWriter.Write(chunk)
	}

	_, _ = writer.ResponseWriter.Write(chunk[:writer.limit])
	writer.ResponseWriter.(http.Flusher).Flush()
	panic(http.ErrAbortHandler)
}

// hang holds a handler until the client goes away, with a ceiling so a test
// that fails cannot hold the server's Close forever.
func hang(request *http.Request) {
	select {
	case <-request.Context().Done():
	case <-time.After(10 * time.Second):
	}
}

func testDownloader(client *http.Client, timeout time.Duration, retries int) *Downloader {
	return New(client, Options{Timeout: timeout, Retries: retries, Backoff: time.Millisecond})
}

// TestADownloadThatStopsSendingFailsAfterTheTimeout is the stall timeout: the
// server sends part of the body and then nothing, and the download fails once
// it has waited the timeout, transient, rather than hanging.
// mock-inventory: transport-fault — a server that stops sending mid-body; no
// live Bitbucket stalls on request, and the subject is the downloader.
func TestADownloadThatStopsSendingFailsAfterTheTimeout(t *testing.T) {
	t.Parallel()

	body := payload(64 << 10)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits.Add(1)
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = writer.Write(body[:len(body)/2])
		writer.(http.Flusher).Flush()
		hang(request)
	}))
	defer server.Close()

	started := time.Now()
	var received Memory
	_, err := testDownloader(server.Client(), 200*time.Millisecond, 1).Get(context.Background(), Request{URL: server.URL}, &received)

	if !apperrors.IsKind(err, apperrors.KindTransient) || !strings.Contains(err.Error(), "no data arrived for 200ms") {
		t.Fatalf("got %v, want a transient stall", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("the stall was noticed after %s, not after the timeout", elapsed)
	}
	// A stall is transient, so it was tried again, and stalled again.
	if got := hits.Load(); got != 2 {
		t.Fatalf("the server was asked %d times, want 2", got)
	}
}

// TestASlowDownloadThatKeepsSendingTakesAsLongAsItNeeds is the other half: no
// deadline for the whole body. The transfer runs several times the timeout and
// completes, because no single wait comes near it.
// mock-inventory: transport-fault — a server that trickles its body out; the
// subject is that the downloader has no deadline for the whole transfer.
func TestASlowDownloadThatKeepsSendingTakesAsLongAsItNeeds(t *testing.T) {
	t.Parallel()

	const chunks = 30
	body := payload(chunks * 100)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		for index := range chunks {
			_, _ = writer.Write(body[index*100 : (index+1)*100])
			writer.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer server.Close()

	const timeout = 500 * time.Millisecond
	started := time.Now()
	var received Memory
	result, err := testDownloader(server.Client(), timeout, 0).Get(context.Background(), Request{URL: server.URL}, &received)
	if err != nil {
		t.Fatalf("a download that never paused for the timeout failed: %v", err)
	}
	if elapsed := time.Since(started); elapsed < 2*timeout {
		t.Fatalf("the transfer took %s; it has to outlast the timeout for this test to mean anything", elapsed)
	}
	if !bytes.Equal(received.Bytes(), body) || result.Bytes != int64(len(body)) {
		t.Fatalf("received %d bytes, want the %d served", len(received.Bytes()), len(body))
	}
}

// TestAServerThatNeverAnswersFailsAfterTheTimeout covers the wait for the
// response headers, which is bounded by the same timeout.
// mock-inventory: transport-fault — a server that accepts the request and
// never answers; the subject is the downloader's timeout.
func TestAServerThatNeverAnswersFailsAfterTheTimeout(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		hits.Add(1)
		hang(request)
	}))
	defer server.Close()

	var received Memory
	_, err := testDownloader(server.Client(), 200*time.Millisecond, 2).Get(context.Background(), Request{URL: server.URL}, &received)
	if !apperrors.IsKind(err, apperrors.KindTransient) || !strings.Contains(err.Error(), "no response within 200ms") {
		t.Fatalf("got %v, want a transient failure naming the wait", err)
	}
	if got := hits.Load(); got != 3 {
		t.Fatalf("the server was asked %d times, want the first attempt and two retries", got)
	}
}

// TestAWriterThatTakesItsTimeIsNotAStall: time spent writing what arrived --
// to a pipe a slow reader drains -- is not the server stalling, so it does not
// count against the timeout.
// mock-inventory: transport-fault — a prompt server feeding a slow writer; the
// subject is what the stall timeout measures.
func TestAWriterThatTakesItsTimeIsNotAStall(t *testing.T) {
	t.Parallel()

	// More than one read's worth, so at least one write falls between reads.
	body := payload(copyBuffer + copyBuffer/4)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	var written bytes.Buffer
	slow := writerFunc(func(chunk []byte) (int, error) {
		time.Sleep(300 * time.Millisecond)

		return written.Write(chunk)
	})
	_, err := testDownloader(server.Client(), 150*time.Millisecond, 0).Get(context.Background(), Request{URL: server.URL}, To(slow))
	if err != nil {
		t.Fatalf("a slow writer was taken for a stalled server: %v", err)
	}
	if !bytes.Equal(written.Bytes(), body) {
		t.Fatalf("wrote %d bytes, want %d", written.Len(), len(body))
	}
}

// TestADeclaredLengthOverTheLimitFailsBeforeAnythingIsWritten: the cap is
// checked against Content-Length first, and a body over it is not retried.
// mock-inventory: transport-fault — a server declaring a body over the cap; the
// subject is the downloader's size cap.
func TestADeclaredLengthOverTheLimitFailsBeforeAnythingIsWritten(t *testing.T) {
	t.Parallel()

	body := payload(4096)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	var received Memory
	_, err := testDownloader(server.Client(), time.Second, 3).Get(context.Background(), Request{URL: server.URL, Limit: 1024}, &received)

	var limit *LimitError
	if !apperrors.IsKind(err, apperrors.KindPermanent) || !errors.As(err, &limit) || limit.Limit != 1024 || limit.Size != int64(len(body)) {
		t.Fatalf("got %v, want a permanent LimitError naming the limit and the declared size", err)
	}
	if len(received.Bytes()) != 0 {
		t.Fatalf("%d bytes were written before the declared length was checked", len(received.Bytes()))
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("a body over the limit was asked for %d times; the same request brings the same body", got)
	}
}

// TestABodyWithoutALengthFailsAsItPassesTheLimit: with no Content-Length to
// check, the download stops at the cap, having written no more than it.
// mock-inventory: transport-fault — a server streaming an unbounded body; the
// subject is the downloader's size cap.
func TestABodyWithoutALengthFailsAsItPassesTheLimit(t *testing.T) {
	t.Parallel()

	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hits.Add(1)
		// A megabyte, far past the limit, and no Content-Length for it. Not
		// endless: a downloader without the check would read this to the end,
		// and the test should fail rather than fill memory.
		chunk := payload(1000)
		for range 1000 {
			if _, err := writer.Write(chunk); err != nil || request.Context().Err() != nil {
				return
			}
			writer.(http.Flusher).Flush()
		}
	}))
	defer server.Close()

	var received Memory
	_, err := testDownloader(server.Client(), time.Second, 3).Get(context.Background(), Request{URL: server.URL, Limit: 10_500}, &received)

	var limit *LimitError
	if !apperrors.IsKind(err, apperrors.KindPermanent) || !errors.As(err, &limit) || limit.Size != -1 {
		t.Fatalf("got %v, want a permanent LimitError with no declared size", err)
	}
	if got := len(received.Bytes()); got > 10_500 {
		t.Fatalf("%d bytes were written, over the limit of 10500", got)
	}
	if got := hits.Load(); got != 1 {
		t.Fatalf("a body over the limit was asked for %d times", got)
	}
}

// rangeServer serves body with the standard library's own range handling --
// ETag, If-Range, 206 and Content-Range -- and cuts the first answer off after
// cut bytes. It records the Range and If-Range each request carried.
type rangeServer struct {
	body     []byte
	cut      int
	mu       sync.Mutex
	requests []string
}

func (server *rangeServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	server.mu.Lock()
	server.requests = append(server.requests, request.Header.Get("Range")+" if "+request.Header.Get("If-Range"))
	first := len(server.requests) == 1
	server.mu.Unlock()

	writer.Header().Set("ETag", `"body-v1"`)
	if first {
		writer = &cutOff{ResponseWriter: writer, limit: server.cut}
	}
	http.ServeContent(writer, request, "", time.Time{}, bytes.NewReader(server.body))
}

func (server *rangeServer) seen() []string {
	server.mu.Lock()
	defer server.mu.Unlock()

	return append([]string(nil), server.requests...)
}

// TestADroppedConnectionResumesFromWhereItBrokeOff is resuming: the server
// offers ranges with a validator, so the second request asks for the rest,
// held to the same body by If-Range, and appends it. It works onto a stream,
// which cannot be started again, because appending continues the same bytes.
// mock-inventory: transport-fault — a connection dropped mid-body in front of
// the standard library's range handling; the subject is the downloader's resume.
func TestADroppedConnectionResumesFromWhereItBrokeOff(t *testing.T) {
	t.Parallel()

	body := payload(100_000)
	ranges := &rangeServer{body: body, cut: 40_000}
	server := httptest.NewServer(ranges)
	defer server.Close()

	var written bytes.Buffer
	result, err := testDownloader(server.Client(), time.Second, 2).Get(context.Background(), Request{URL: server.URL}, To(&written))
	if err != nil {
		t.Fatalf("the download did not resume: %v", err)
	}
	if !bytes.Equal(written.Bytes(), body) {
		t.Fatalf("the resumed body differs from the one served (%d bytes, want %d)", written.Len(), len(body))
	}
	if result.Attempts != 2 {
		t.Fatalf("attempts = %d, want 2", result.Attempts)
	}

	seen := ranges.seen()
	if len(seen) != 2 || seen[1] != `bytes=40000- if "body-v1"` {
		t.Fatalf("requests carried %q, want the second to ask for the rest under the ETag", seen)
	}
}

// TestAResumeAnsweredWithTheWholeBodyStartsOver: a server that ignores the
// range sends the whole body, which serves as one once what was written is
// taken back -- and cannot, onto a stream that already has bytes out.
// mock-inventory: transport-fault — a server that advertises ranges and
// ignores them, dropping the first answer; the subject is the downloader.
func TestAResumeAnsweredWithTheWholeBodyStartsOver(t *testing.T) {
	t.Parallel()

	body := payload(50_000)
	serve := func(t *testing.T) (*httptest.Server, *atomic.Int32) {
		t.Helper()

		var hits atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			out := writer
			if hits.Add(1) == 1 {
				out = &cutOff{ResponseWriter: writer, limit: 20_000}
			}
			writer.Header().Set("Accept-Ranges", "bytes")
			writer.Header().Set("ETag", `"body-v1"`)
			writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = out.Write(body)
		}))
		t.Cleanup(server.Close)

		return server, &hits
	}

	t.Run("into memory", func(t *testing.T) {
		t.Parallel()

		server, _ := serve(t)
		var received Memory
		if _, err := testDownloader(server.Client(), time.Second, 2).Get(context.Background(), Request{URL: server.URL}, &received); err != nil {
			t.Fatalf("got %v, want the whole body used from the start", err)
		}
		if !bytes.Equal(received.Bytes(), body) {
			t.Fatalf("received %d bytes that differ from the body served", len(received.Bytes()))
		}
	})

	t.Run("onto a stream", func(t *testing.T) {
		t.Parallel()

		server, _ := serve(t)
		var written bytes.Buffer
		_, err := testDownloader(server.Client(), time.Second, 2).Get(context.Background(), Request{URL: server.URL}, To(&written))
		if !apperrors.IsKind(err, apperrors.KindTransient) || !strings.Contains(err.Error(), "would write those bytes twice") {
			t.Fatalf("got %v, want a transient failure saying the stream cannot start again", err)
		}
		if !bytes.Equal(written.Bytes(), body[:20_000]) {
			t.Fatalf("the stream holds %d bytes; the first 20000 of the body, and nothing twice, was written", written.Len())
		}
	})
}

// TestADownloadWithoutRangesStartsAgainWhereItCan: a server that offers no
// ranges is asked for the whole body again, into a destination that can take
// back what it holds -- and not at all onto a stream with bytes already out,
// where a second request would write them twice.
// mock-inventory: transport-fault — a server without range support that drops
// its first answer; the subject is the downloader's restart.
func TestADownloadWithoutRangesStartsAgainWhereItCan(t *testing.T) {
	t.Parallel()

	body := payload(50_000)
	serve := func(t *testing.T) (*httptest.Server, *atomic.Int32, *atomic.Value) {
		t.Helper()

		var hits atomic.Int32
		var lastRange atomic.Value
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			lastRange.Store(request.Header.Get("Range"))
			out := writer
			if hits.Add(1) == 1 {
				out = &cutOff{ResponseWriter: writer, limit: 20_000}
			}
			writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
			_, _ = out.Write(body)
		}))
		t.Cleanup(server.Close)

		return server, &hits, &lastRange
	}

	t.Run("into memory", func(t *testing.T) {
		t.Parallel()

		server, hits, lastRange := serve(t)
		var received Memory
		result, err := testDownloader(server.Client(), time.Second, 2).Get(context.Background(), Request{URL: server.URL}, &received)
		if err != nil || !bytes.Equal(received.Bytes(), body) {
			t.Fatalf("got %d bytes and %v, want the whole body after starting again", len(received.Bytes()), err)
		}
		if hits.Load() != 2 || result.Attempts != 2 || lastRange.Load() != "" {
			t.Fatalf("hits %d, attempts %d, range %q: want a second request for the whole body", hits.Load(), result.Attempts, lastRange.Load())
		}
	})

	t.Run("onto a stream", func(t *testing.T) {
		t.Parallel()

		server, hits, _ := serve(t)
		var written bytes.Buffer
		_, err := testDownloader(server.Client(), time.Second, 2).Get(context.Background(), Request{URL: server.URL}, To(&written))
		if !apperrors.IsKind(err, apperrors.KindTransient) || !strings.Contains(err.Error(), "offers no range to resume it from") {
			t.Fatalf("got %v, want a transient failure saying why it cannot continue", err)
		}
		if hits.Load() != 1 {
			t.Fatalf("the server was asked %d times; a second request would write the start of the body twice", hits.Load())
		}
		if !bytes.Equal(written.Bytes(), body[:20_000]) {
			t.Fatalf("the stream holds %d bytes, want the 20000 that arrived", written.Len())
		}
	})

	t.Run("a stream nothing reached yet", func(t *testing.T) {
		t.Parallel()

		// Nothing went out, so there is nothing to write twice.
		var hits atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			if hits.Add(1) == 1 {
				writer.WriteHeader(http.StatusServiceUnavailable)

				return
			}
			_, _ = writer.Write(body)
		}))
		t.Cleanup(server.Close)

		var written bytes.Buffer
		if _, err := testDownloader(server.Client(), time.Second, 2).Get(context.Background(), Request{URL: server.URL}, To(&written)); err != nil {
			t.Fatalf("got %v, want the retried request to complete", err)
		}
		if !bytes.Equal(written.Bytes(), body) {
			t.Fatalf("wrote %d bytes, want the body once", written.Len())
		}
	})
}

// TestAResumedAnswerThatDoesNotContinueTheBodyIsNotAppended: a 206 has to
// start where the body broke off and belong to the same body. One that does
// not is discarded, and the download starts over rather than splicing it in.
// mock-inventory: transport-fault — a server whose range answers are wrong; the
// subject is the downloader's check on Content-Range.
func TestAResumedAnswerThatDoesNotContinueTheBodyIsNotAppended(t *testing.T) {
	t.Parallel()

	// Each wrong answer serves the bytes its Content-Range claims, so one that
	// was spliced in would leave the body wrong, not merely asked for twice.
	body := payload(30_000)
	for name, answer := range map[string]struct {
		contentRange string
		bytes        []byte
	}{
		"a different start":  {fmt.Sprintf("bytes 0-%d/%d", len(body)-1, len(body)), body},
		"a different length": {fmt.Sprintf("bytes 10000-%d/%d", len(body), len(body)+1), append(append([]byte(nil), body[10_000:]...), 0x00)},
		"not the rest":       {fmt.Sprintf("bytes 10000-19999/%d", len(body)), body[10_000:20_000]},
		"unreadable":         {"items 10000-29999/30000", body[10_000:]},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Accept-Ranges", "bytes")
				writer.Header().Set("ETag", `"body-v1"`)
				switch hits.Add(1) {
				case 1:
					writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
					_, _ = (&cutOff{ResponseWriter: writer, limit: 10_000}).Write(body)
				case 2:
					writer.Header().Set("Content-Range", answer.contentRange)
					writer.Header().Set("Content-Length", strconv.Itoa(len(answer.bytes)))
					writer.WriteHeader(http.StatusPartialContent)
					_, _ = writer.Write(answer.bytes)
				default:
					if request.Header.Get("Range") != "" {
						t.Errorf("a range was asked for again after a range answer was refused")
					}
					writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
					_, _ = writer.Write(body)
				}
			}))
			defer server.Close()

			var received Memory
			if _, err := testDownloader(server.Client(), time.Second, 3).Get(context.Background(), Request{URL: server.URL}, &received); err != nil {
				t.Fatalf("got %v, want the download to start over and complete", err)
			}
			if !bytes.Equal(received.Bytes(), body) || hits.Load() != 3 {
				t.Fatalf("received %d bytes in %d requests, want the body exactly after starting over", len(received.Bytes()), hits.Load())
			}
		})
	}
}

// TestABodyShorterThanItsLengthIsTransient: a server that declares more than
// it sends and closes is a dropped connection -- retried, and reported as
// transient when the retries run out.
// mock-inventory: transport-fault — a server lying about Content-Length; no
// live Bitbucket does so on request, and the subject is the downloader.
func TestABodyShorterThanItsLengthIsTransient(t *testing.T) {
	t.Parallel()

	body := payload(10_000)
	var lying atomic.Bool
	lying.Store(true)
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		if lying.Load() {
			writer.Header().Set("Content-Length", strconv.Itoa(len(body)+5_000))
			_, _ = writer.Write(body)
			writer.(http.Flusher).Flush()
			panic(http.ErrAbortHandler)
		}
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	var received Memory
	_, err := testDownloader(server.Client(), time.Second, 1).Get(context.Background(), Request{URL: server.URL}, &received)
	if !apperrors.IsKind(err, apperrors.KindTransient) || !strings.Contains(err.Error(), "in 2 attempts") ||
		!strings.Contains(err.Error(), "the connection broke off") {
		t.Fatalf("got %v, want a transient failure after both attempts, saying the connection broke off", err)
	}
	if hits.Load() != 2 {
		t.Fatalf("the server was asked %d times, want 2", hits.Load())
	}

	lying.Store(false)
	received.Rewind()
	if _, err := testDownloader(server.Client(), time.Second, 1).Get(context.Background(), Request{URL: server.URL}, &received); err != nil || !bytes.Equal(received.Bytes(), body) {
		t.Fatalf("got %v, want the honest answer to download", err)
	}
}

// TestAFailedDownloadIsReportedAsWhatItWas covers the classification, which is
// outcome's: a certificate this host does not trust is permanent and asked for
// once; an interrupt is cancelled and not retried; a status goes back to the
// caller to map, retried first when the retry policy replays it.
// mock-inventory: transport-fault — a TLS listener this client does not trust,
// a body cut short by an interrupt and statuses with no payload; the subject is
// how the downloader classifies, not what any server says.
func TestAFailedDownloadIsReportedAsWhatItWas(t *testing.T) {
	t.Parallel()

	t.Run("a certificate this host does not trust", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewTLSServer(http.NotFoundHandler())
		defer server.Close()

		var received Memory
		_, err := testDownloader(&http.Client{Transport: &http.Transport{}}, time.Second, 3).Get(context.Background(), Request{URL: server.URL}, &received)
		if !apperrors.IsKind(err, apperrors.KindPermanent) || strings.Contains(err.Error(), "attempts") {
			t.Fatalf("got %v, want permanent, from the one attempt", err)
		}
	})

	t.Run("an interrupt", func(t *testing.T) {
		t.Parallel()

		var hits atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			hits.Add(1)
			_, _ = writer.Write(payload(1000))
			writer.(http.Flusher).Flush()
			hang(request)
		}))
		defer server.Close()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		interrupting := writerFunc(func(chunk []byte) (int, error) {
			cancel()

			return len(chunk), nil
		})
		_, err := testDownloader(server.Client(), 10*time.Second, 3).Get(ctx, Request{URL: server.URL}, To(interrupting))
		if !apperrors.IsKind(err, apperrors.KindCancelled) || !strings.Contains(err.Error(), "the download was interrupted") {
			t.Fatalf("got %v, want cancelled, saying so", err)
		}
		if hits.Load() != 1 {
			t.Fatalf("an interrupted download was sent %d times", hits.Load())
		}
	})

	t.Run("statuses", func(t *testing.T) {
		t.Parallel()

		for status, wantHits := range map[int]int32{http.StatusNotFound: 1, http.StatusServiceUnavailable: 3, http.StatusTooManyRequests: 3} {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				hits.Add(1)
				writer.WriteHeader(status)
				_, _ = writer.Write([]byte(`{"errors":[{"message":"no"}]}`))
			}))

			var received Memory
			_, err := testDownloader(server.Client(), time.Second, 2).Get(context.Background(), Request{URL: server.URL}, &received)
			server.Close()

			var answered *StatusError
			if !errors.As(err, &answered) || answered.StatusCode != status || string(answered.Body) != `{"errors":[{"message":"no"}]}` {
				t.Fatalf("%d: got %v, want a StatusError carrying the answer", status, err)
			}
			if hits.Load() != wantHits {
				t.Fatalf("%d: asked %d times, want %d", status, hits.Load(), wantHits)
			}
		}
	})
}

// recordingTransport stands in for a guard like bb update's scheme check: it
// sees each request, and refuses the ones it is told to.
type recordingTransport struct {
	base   http.RoundTripper
	refuse string
	mu     sync.Mutex
	paths  []string
}

func (transport *recordingTransport) seen() string {
	transport.mu.Lock()
	defer transport.mu.Unlock()

	return strings.Join(transport.paths, " ")
}

func (transport *recordingTransport) forget() {
	transport.mu.Lock()
	defer transport.mu.Unlock()

	transport.paths = nil
}

func (transport *recordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	transport.mu.Lock()
	transport.paths = append(transport.paths, request.URL.Path)
	transport.mu.Unlock()

	if transport.refuse != "" && request.URL.Path == transport.refuse {
		return nil, apperrors.New(apperrors.KindValidation, "refused "+request.URL.Path, nil)
	}

	return transport.base.RoundTrip(request)
}

// TestTheCallersTransportSeesEveryRequestAndRedirect: the downloader wraps the
// transport it is given, so a guard on it sees each hop of a redirect, and a
// refusal comes back as the guard's own, unretried.
// mock-inventory: transport-fault — a server that redirects, behind a
// recording transport; the subject is what the caller's transport sees.
func TestTheCallersTransportSeesEveryRequestAndRedirect(t *testing.T) {
	t.Parallel()

	body := payload(5_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/start", "/refused-start":
			target := "/asset"
			if request.URL.Path == "/refused-start" {
				target = "/refused"
			}
			http.Redirect(writer, request, target, http.StatusFound)
		default:
			_, _ = writer.Write(body)
		}
	}))
	defer server.Close()

	guard := &recordingTransport{base: server.Client().Transport, refuse: "/refused"}
	downloader := testDownloader(&http.Client{Transport: guard}, time.Second, 3)

	var received Memory
	if _, err := downloader.Get(context.Background(), Request{URL: server.URL + "/start"}, &received); err != nil || !bytes.Equal(received.Bytes(), body) {
		t.Fatalf("got %v, want the redirected asset", err)
	}
	if got := guard.seen(); got != "/start /asset" {
		t.Fatalf("the caller's transport saw %q, want both hops", got)
	}

	guard.forget()
	_, err := downloader.Get(context.Background(), Request{URL: server.URL + "/refused-start"}, &received)
	var refused *apperrors.AppError
	if !errors.As(err, &refused) || refused.Kind != apperrors.KindValidation || refused.Message != "refused /refused" {
		t.Fatalf("got %v, want the guard's own refusal", err)
	}
	if got := guard.seen(); got != "/refused-start /refused" {
		t.Fatalf("a refusal was retried: the transport saw %q", got)
	}
}

// TestARangeTheCallerAskedForIsTheAnswer: a Range in the request is the
// caller's, so a 206 is the body it asked for, and the downloader does not ask
// for another range on top.
// mock-inventory: transport-fault — the standard library's range handling; the
// subject is that the downloader leaves a caller's range alone.
func TestARangeTheCallerAskedForIsTheAnswer(t *testing.T) {
	t.Parallel()

	body := payload(10_000)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("ETag", `"body-v1"`)
		http.ServeContent(writer, request, "", time.Time{}, bytes.NewReader(body))
	}))
	defer server.Close()

	var received Memory
	header := http.Header{"Range": []string{"bytes=100-199"}}
	if _, err := testDownloader(server.Client(), time.Second, 0).Get(context.Background(), Request{URL: server.URL, Header: header}, &received); err != nil {
		t.Fatalf("got %v, want the range asked for", err)
	}
	if !bytes.Equal(received.Bytes(), body[100:200]) {
		t.Fatalf("received %d bytes, want bytes 100-199", len(received.Bytes()))
	}
}

// TestAnOpenerSeesTheHeaderBeforeTheBody: an Opener decides what to do with a
// body from its header, before a byte of it is written, and its refusal ends
// the download as it is.
// mock-inventory: transport-fault — a server whose body the destination
// refuses; the subject is the downloader's Opener hook.
func TestAnOpenerSeesTheHeaderBeforeTheBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/html")
		_, _ = writer.Write([]byte("<html>a login page</html>"))
	}))
	defer server.Close()

	refusal := apperrors.New(apperrors.KindAuthentication, "a login page", nil)
	opener := &openerFunc{open: func(header http.Header) error {
		if header.Get("Content-Type") == "text/html" {
			return refusal
		}

		return nil
	}}
	_, err := testDownloader(server.Client(), time.Second, 3).Get(context.Background(), Request{URL: server.URL}, opener)
	if !errors.Is(err, refusal) {
		t.Fatalf("got %v, want the opener's refusal", err)
	}
	if opener.written.Len() != 0 {
		t.Fatalf("%d bytes were written before the opener refused", opener.written.Len())
	}
}

// TestAGetWithoutAURLIsRefused covers the one input checked before sending.
func TestAGetWithoutAURLIsRefused(t *testing.T) {
	t.Parallel()

	var received Memory
	if _, err := testDownloader(nil, time.Second, 0).Get(context.Background(), Request{URL: " "}, &received); !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("got %v, want validation", err)
	}
	if _, err := testDownloader(nil, time.Second, 0).Get(context.Background(), Request{URL: "http://[::1"}, &received); !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("got %v, want validation for a URL that does not parse", err)
	}
}

type writerFunc func([]byte) (int, error)

func (write writerFunc) Write(chunk []byte) (int, error) { return write(chunk) }

type openerFunc struct {
	open    func(http.Header) error
	written bytes.Buffer
}

func (opener *openerFunc) Open(header http.Header) error   { return opener.open(header) }
func (opener *openerFunc) Write(chunk []byte) (int, error) { return opener.written.Write(chunk) }
func (opener *openerFunc) Rewind() bool                    { opener.written.Reset(); return true }
