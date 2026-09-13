package httpclient

import (
	"bufio"
	"context"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// These check that DoRequest acts on the classification, not only that it
// makes one. Neither listener answers a Bitbucket route (ADR-079): one fails
// the TLS handshake, the other reads a request and hangs up.

// TestARejectedCertificateIsNotRetried is #574. The certificate failure was
// classified permanent and then tried RetryCount more times anyway, burying
// the cause under attempts that could not succeed.
func TestARejectedCertificateIsNotRetried(t *testing.T) {
	t.Parallel()

	var connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("the handshake should have failed before any request was served")
	}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	client := NewFromConfig(config.AppConfig{
		BitbucketURL:   server.URL,
		RetryCount:     2,
		RetryBackoff:   time.Millisecond,
		RequestTimeout: 5 * time.Second,
	})

	err := client.GetJSON(context.Background(), "/rest/api/latest/projects", nil, nil)
	if !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("got %v, want a permanent failure", err)
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("a rejected certificate was tried %d times", got)
	}
}

// TestAMutationWhoseAnswerWasLostIsNotReportedRetriable is the reviewer's
// scenario: the server reads the POST and hangs up.
func TestAMutationWhoseAnswerWasLostIsNotReportedRetriable(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	var requests atomic.Int32
	go func() {
		for {
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			go func(conn net.Conn) {
				defer func() { _ = conn.Close() }()
				if request, readErr := http.ReadRequest(bufio.NewReader(conn)); readErr == nil {
					_, _ = io.Copy(io.Discard, request.Body)
					requests.Add(1)
				}
			}(conn)
		}
	}()

	client := NewFromConfig(config.AppConfig{
		BitbucketURL:   "http://" + listener.Addr().String(),
		RetryCount:     2,
		RetryBackoff:   time.Millisecond,
		RequestTimeout: 5 * time.Second,
	})

	err = client.PostJSON(context.Background(), "/rest/api/latest/projects/P/repos/r/pull-requests", nil, map[string]string{"title": "x"}, nil)
	if !apperrors.IsKind(err, apperrors.KindUnknownOutcome) {
		t.Fatalf("got %v, want unknown_outcome", err)
	}
	if apperrors.ExitCode(err) != 13 {
		t.Fatalf("exit %d, want 13", apperrors.ExitCode(err))
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("the POST was sent %d times", got)
	}
}
