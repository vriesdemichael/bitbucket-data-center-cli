package httpclient

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// The other ways an exchange fails, seen from DoRequest. The listeners fail at
// the network level -- a closed port, a body cut short, a gateway giving up --
// and none answers a Bitbucket route or payload (ADR-079).

// answerEveryRequest reads each request and replies with raw, counting them.
func answerEveryRequest(t *testing.T, raw string) (string, *atomic.Int32) {
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
				_, _ = io.WriteString(conn, raw)
			}(conn)
		}
	}()

	return "http://" + listener.Addr().String(), &requests
}

func clientFor(baseURL string, retries int) *Client {
	return NewFromConfig(config.AppConfig{
		BitbucketURL:   baseURL,
		RetryCount:     retries,
		RetryBackoff:   time.Millisecond,
		RequestTimeout: 5 * time.Second,
	})
}

// A GET that never reached a server is transient, and retried.
func TestAReadThatNeverConnectedIsRetriedAndTransient(t *testing.T) {
	t.Parallel()

	err := clientFor(testsupport.RefusedURL, 1).GetJSON(context.Background(), "/rest/api/latest/projects", nil, nil)
	if !apperrors.IsKind(err, apperrors.KindTransient) {
		t.Fatalf("got %v, want transient", err)
	}
}

// A body cut short on a GET is a failed read, safe to repeat.
func TestABodyCutShortOnAReadIsTransient(t *testing.T) {
	t.Parallel()

	baseURL, _ := answerEveryRequest(t, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 64\r\n\r\n{\"values\"")

	err := clientFor(baseURL, 0).GetJSON(context.Background(), "/rest/api/latest/projects", nil, nil)
	if !apperrors.IsKind(err, apperrors.KindTransient) {
		t.Fatalf("got %v, want transient", err)
	}
}

// A gateway that gives up on a POST leaves its outcome unknown, and the POST
// is sent once.
func TestAGatewayTimeoutOnAMutationHasAnUnknownOutcome(t *testing.T) {
	t.Parallel()

	baseURL, requests := answerEveryRequest(t, "HTTP/1.1 504 Gateway Timeout\r\nContent-Length: 0\r\n\r\n")

	err := clientFor(baseURL, 2).PostJSON(context.Background(), "/rest/api/latest/projects", nil, map[string]string{"key": "X"}, nil)
	if !apperrors.IsKind(err, apperrors.KindUnknownOutcome) {
		t.Fatalf("got %v, want unknown_outcome", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("the POST was sent %d times", got)
	}
}
