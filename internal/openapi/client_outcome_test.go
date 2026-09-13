package openapi

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
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// The generated client is where most commands send their requests, and where
// #574's classification never reached: a service wrapped every transport error
// as transient, so a rejected certificate and a lost POST both exited 10.
// These go through the client as a service does, wrapping with
// apperrors.Transport. No listener answers a Bitbucket route (ADR-079).

func hangUpAfterReading(t *testing.T, afterRequest func(net.Conn)) (string, *atomic.Int32) {
	t.Helper()

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
				request, readErr := http.ReadRequest(bufio.NewReader(conn))
				if readErr != nil {
					return
				}
				_, _ = io.Copy(io.Discard, request.Body)
				requests.Add(1)
				afterRequest(conn)
			}(conn)
		}
	}()

	return "http://" + listener.Addr().String(), &requests
}

func generatedClient(t *testing.T, baseURL string) *openapigenerated.ClientWithResponses {
	t.Helper()

	client, err := NewClientWithResponsesFromConfig(config.AppConfig{
		BitbucketURL:   baseURL,
		RetryCount:     2,
		RetryBackoff:   time.Millisecond,
		RequestTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("build client: %v", err)
	}

	return client
}

func TestAMutationOnTheGeneratedClientWhoseAnswerWasLostHasAnUnknownOutcome(t *testing.T) {
	t.Parallel()

	baseURL, requests := hangUpAfterReading(t, func(net.Conn) {})

	_, err := generatedClient(t, baseURL).CreateProjectWithResponse(context.Background(), openapigenerated.RestProject{})
	err = apperrors.Transport("failed to create project", err)

	if !apperrors.IsKind(err, apperrors.KindUnknownOutcome) || apperrors.ExitCode(err) != 13 {
		t.Fatalf("got %v (exit %d), want unknown_outcome and exit 13", err, apperrors.ExitCode(err))
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("the POST was sent %d times", got)
	}
}

func TestAnAnswerLostWhileTheGeneratedClientReadsItHasAnUnknownOutcome(t *testing.T) {
	t.Parallel()

	// The status arrives and the body is cut short: the project exists, and
	// the caller holds a fragment of the reply that says so.
	baseURL, _ := hangUpAfterReading(t, func(conn net.Conn) {
		_, _ = io.WriteString(conn, "HTTP/1.1 201 Created\r\nContent-Type: application/json\r\nContent-Length: 64\r\n\r\n{\"key\"")
	})

	_, err := generatedClient(t, baseURL).CreateProjectWithResponse(context.Background(), openapigenerated.RestProject{})
	err = apperrors.Transport("failed to create project", err)

	if !apperrors.IsKind(err, apperrors.KindUnknownOutcome) {
		t.Fatalf("got %v, want unknown_outcome", err)
	}
}

func TestTheGeneratedClientDoesNotRetryARejectedCertificate(t *testing.T) {
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

	_, err := generatedClient(t, server.URL).GetProjectsWithResponse(context.Background(), &openapigenerated.GetProjectsParams{})
	err = apperrors.Transport("failed to list projects", err)

	if !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("got %v, want permanent", err)
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("a rejected certificate was tried %d times", got)
	}
}
