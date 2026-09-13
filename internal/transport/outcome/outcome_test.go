package outcome_test

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/outcome"
)

// These exercise the wire, not Bitbucket. Every listener here reads a request
// and then misbehaves the way a network does -- closes, stalls, cuts a response
// short -- and none answers a Bitbucket route or payload (ADR-079). Whether a
// request reached the connection is exactly what a value-only test cannot
// show, and it is the whole of the difference between transient and
// unknown_outcome.

// listen accepts connections, reads each request in full, and hands the
// connection to after. It returns the base URL and a channel that receives once
// per request read.
func listen(t *testing.T, after func(net.Conn)) (string, <-chan struct{}) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	received := make(chan struct{}, 8)
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()

				request, readErr := http.ReadRequest(bufio.NewReader(conn))
				if readErr != nil {
					return
				}
				_, _ = io.Copy(io.Discard, request.Body)
				received <- struct{}{}
				after(conn)
			}(conn)
		}
	}()

	return "http://" + listener.Addr().String(), received
}

// stall holds the connection open until the test ends.
func stall(t *testing.T) func(net.Conn) {
	done := make(chan struct{})
	t.Cleanup(func() { close(done) })

	return func(net.Conn) { <-done }
}

func send(ctx context.Context, client *http.Client, method, url string) (*http.Response, *outcome.Exchange, error) {
	request, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader("{}"))
	if err != nil {
		return nil, nil, err
	}

	tracked, exchange := outcome.Track(request)
	response, err := client.Do(tracked)

	return response, exchange, err
}

func assertKind(t *testing.T, err error, want apperrors.Kind, says string) {
	t.Helper()

	if got := apperrors.KindOf(err); got != want {
		t.Fatalf("classified %s, want %s: %v", got, want, err)
	}
	if says != "" && !strings.Contains(err.Error(), says) {
		t.Fatalf("the message does not say %q: %v", says, err)
	}
}

func TestARequestThatNeverReachedTheServerIsTransient(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	refused := "http://" + listener.Addr().String()
	_ = listener.Close()

	// A POST included: nothing was sent, so nothing can have been applied, and
	// retrying later is the honest advice even for a mutation.
	for _, method := range []string{http.MethodPost, http.MethodGet} {
		_, exchange, err := send(context.Background(), &http.Client{Timeout: 2 * time.Second}, method, refused)
		if err == nil {
			t.Fatalf("%s to a closed port succeeded", method)
		}
		assertKind(t, exchange.Classify(err), apperrors.KindTransient, "request failed")
	}
}

func TestAMutationWhoseAnswerWasLostHasAnUnknownOutcome(t *testing.T) {
	t.Parallel()

	// Read the request, then drop the connection without a word: the server
	// had everything it needed to act, and bb heard nothing back.
	url, _ := listen(t, func(net.Conn) {})

	_, exchange, err := send(context.Background(), &http.Client{Timeout: 2 * time.Second}, http.MethodPost, url)
	if err == nil {
		t.Fatal("a dropped connection succeeded")
	}
	classified := exchange.Classify(err)
	assertKind(t, classified, apperrors.KindUnknownOutcome, "check before sending it again")
	if apperrors.ExitCode(classified) != 13 {
		t.Fatalf("exit %d, want 13", apperrors.ExitCode(classified))
	}

	// The same loss on a GET is only a failed read, and safe to repeat.
	_, exchange, err = send(context.Background(), &http.Client{Timeout: 2 * time.Second}, http.MethodGet, url)
	if err == nil {
		t.Fatal("a dropped connection succeeded")
	}
	assertKind(t, exchange.Classify(err), apperrors.KindTransient, "")
}

func TestAMutationThatTimedOutHasAnUnknownOutcome(t *testing.T) {
	t.Parallel()

	url, _ := listen(t, stall(t))
	client := &http.Client{Timeout: 300 * time.Millisecond}

	_, exchange, err := send(context.Background(), client, http.MethodPost, url)
	if err == nil {
		t.Fatal("a stalled request succeeded")
	}
	assertKind(t, exchange.Classify(err), apperrors.KindUnknownOutcome, "timed out")

	_, exchange, err = send(context.Background(), client, http.MethodGet, url)
	if err == nil {
		t.Fatal("a stalled request succeeded")
	}
	assertKind(t, exchange.Classify(err), apperrors.KindTransient, "")
}

func TestAnInterruptIsCancelledUnlessTheMutationHadAlreadyGone(t *testing.T) {
	t.Parallel()

	t.Run("before the request left", func(t *testing.T) {
		t.Parallel()

		url, _ := listen(t, stall(t))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, exchange, err := send(ctx, &http.Client{}, http.MethodPost, url)
		if err == nil {
			t.Fatal("a cancelled request succeeded")
		}
		classified := exchange.Classify(err)
		assertKind(t, classified, apperrors.KindCancelled, "interrupted")
		if apperrors.ExitCode(classified) != 12 {
			t.Fatalf("exit %d, want 12", apperrors.ExitCode(classified))
		}
	})

	t.Run("after the server had it", func(t *testing.T) {
		t.Parallel()

		url, received := listen(t, stall(t))
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			<-received
			cancel()
		}()

		_, exchange, err := send(ctx, &http.Client{}, http.MethodPost, url)
		if err == nil {
			t.Fatal("an interrupted request succeeded")
		}
		assertKind(t, exchange.Classify(err), apperrors.KindUnknownOutcome, "interrupted")
	})
}

func TestAnAnswerLostWhileReadingIsClassifiedByMethod(t *testing.T) {
	t.Parallel()

	// The status arrives and the body does not: the server acted and bb holds
	// a fragment of what it said.
	url, _ := listen(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 201 Created\r\nContent-Length: 64\r\n\r\n{\"id\"")
	})

	for method, want := range map[string]apperrors.Kind{
		http.MethodPost: apperrors.KindUnknownOutcome,
		http.MethodGet:  apperrors.KindTransient,
	} {
		response, exchange, err := send(context.Background(), &http.Client{Timeout: 2 * time.Second}, method, url)
		if err != nil {
			t.Fatalf("%s: the status line did not arrive: %v", method, err)
		}

		_, readErr := io.ReadAll(exchange.Body(response.Body))
		_ = response.Body.Close()
		if readErr == nil {
			t.Fatalf("%s: a short body read in full", method)
		}
		assertKind(t, readErr, want, "")
	}
}

func TestARejectedCertificateIsPermanent(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the handshake should have failed before any request was served")
	}))
	t.Cleanup(server.Close)

	// A client that does not trust the test server's certificate.
	_, exchange, err := send(context.Background(), &http.Client{Timeout: 2 * time.Second}, http.MethodGet, server.URL)
	if err == nil {
		t.Fatal("an untrusted certificate was accepted")
	}
	classified := exchange.Classify(err)
	assertKind(t, classified, apperrors.KindPermanent, "TLS certificate was rejected")
	if outcome.Retriable(classified) {
		t.Fatal("a rejected certificate is retriable")
	}
}

// A name that does not resolve depends on the resolver the test runs under,
// so it is classified from the error value rather than provoked.
func TestAnUnresolvableHostIsPermanent(t *testing.T) {
	t.Parallel()

	request, _ := http.NewRequest(http.MethodGet, "http://bitbucket.invalid", nil)
	_, exchange := outcome.Track(request)

	classified := exchange.Classify(&net.DNSError{Err: "no such host", Name: "bitbucket.invalid", IsNotFound: true})
	assertKind(t, classified, apperrors.KindPermanent, "does not resolve")
}

func TestAGatewayAnswerToAMutationHasAnUnknownOutcome(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		method string
		status int
		want   apperrors.Kind
	}{
		{method: http.MethodPost, status: http.StatusGatewayTimeout, want: apperrors.KindUnknownOutcome},
		{method: http.MethodPatch, status: http.StatusBadGateway, want: apperrors.KindUnknownOutcome},
		// Replayable: a gateway failure is only a failure to answer.
		{method: http.MethodPut, status: http.StatusGatewayTimeout},
		{method: http.MethodGet, status: http.StatusBadGateway},
		// Bitbucket's own 5xx and 503 are the server answering, not a gateway
		// losing the answer, so they map as they always have.
		{method: http.MethodPost, status: http.StatusInternalServerError},
		{method: http.MethodPost, status: http.StatusServiceUnavailable},
	} {
		request, _ := http.NewRequest(testCase.method, "http://bitbucket.example", nil)
		_, exchange := outcome.Track(request)

		mapped := apperrors.New(apperrors.KindTransient, "bitbucket API returned a gateway page", nil)
		got := exchange.Status(testCase.status, mapped)
		if testCase.want == "" {
			if got != nil {
				t.Errorf("%s answered %d was reclassified: %v", testCase.method, testCase.status, got)
			}
			continue
		}
		assertKind(t, got, testCase.want, "check before sending it again")
		if !errors.Is(got, mapped) {
			t.Errorf("%s answered %d lost the mapped cause: %v", testCase.method, testCase.status, got)
		}
	}
}

func TestOnlyTransientIsRetriable(t *testing.T) {
	t.Parallel()

	for _, kind := range apperrors.Kinds() {
		retriable := outcome.Retriable(apperrors.New(kind, "x", nil))
		if retriable != (kind == apperrors.KindTransient) {
			t.Errorf("Retriable(%s) = %v", kind, retriable)
		}
	}
}

// The transport's answer has to survive the layers above it: a service wraps
// the failure with its own context, and the kind a caller sees is the
// outermost one.
func TestAClassificationSurvivesBeingWrapped(t *testing.T) {
	t.Parallel()

	url, _ := listen(t, func(net.Conn) {})
	_, exchange, err := send(context.Background(), &http.Client{Timeout: 2 * time.Second}, http.MethodPost, url)
	if err == nil {
		t.Fatal("a dropped connection succeeded")
	}

	wrapped := apperrors.Transport("failed to create repository branch", exchange.Classify(err))
	assertKind(t, wrapped, apperrors.KindUnknownOutcome, "failed to create repository branch")

	// Classifying twice does not overwrite the first answer.
	if again := exchange.Classify(wrapped); !errors.Is(again, wrapped) {
		t.Fatalf("a classified error was classified again: %v", again)
	}
}
