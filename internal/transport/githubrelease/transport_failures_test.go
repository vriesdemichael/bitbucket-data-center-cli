package githubrelease

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// A release check that fails at the network level: nothing listening, or a
// manifest cut short. Both are reads, safe to repeat, so both are transient.
func TestAReleaseCheckThatFailsInTransitIsTransient(t *testing.T) {
	t.Parallel()

	t.Run("nothing listening", func(t *testing.T) {
		t.Parallel()

		_, err := NewClient(testsupport.RefusedURL, &http.Client{}, "bb/test").Latest(context.Background(), "vriesdemichael", "bitbucket-data-center-cli")
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("got %v, want transient", err)
		}
	})

	t.Run("manifest cut short", func(t *testing.T) {
		t.Parallel()

		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen: %v", err)
		}
		t.Cleanup(func() { _ = listener.Close() })
		go func() {
			for {
				conn, acceptErr := listener.Accept()
				if acceptErr != nil {
					return
				}
				go func(conn net.Conn) {
					defer func() { _ = conn.Close() }()
					if _, readErr := http.ReadRequest(bufio.NewReader(conn)); readErr != nil {
						return
					}
					_, _ = io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 64\r\n\r\n{\"tag_name\"")
				}(conn)
			}
		}()

		_, err = NewClient("http://"+listener.Addr().String(), &http.Client{}, "bb/test").Latest(context.Background(), "vriesdemichael", "bitbucket-data-center-cli")
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("got %v, want transient", err)
		}
	})
}
