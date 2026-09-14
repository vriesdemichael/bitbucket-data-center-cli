package testsupport_test

import (
	"errors"
	"net"
	"net/url"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestRefusedURLIsRefused checks that the machine keeps the promise RefusedURL
// makes.
//
// Every transport-fault test points at it. If something here did listen on
// port 1, those tests would get an answer where they expect a failure, and pass
// or fail for reasons unrelated to what they check. This fails once, by name,
// instead.
func TestRefusedURLIsRefused(t *testing.T) {
	t.Parallel()

	parsed, err := url.Parse(testsupport.RefusedURL)
	if err != nil {
		t.Fatalf("parse %s: %v", testsupport.RefusedURL, err)
	}

	connection, err := net.DialTimeout("tcp", parsed.Host, 5*time.Second)
	if err == nil {
		_ = connection.Close()
		t.Fatalf("something is listening on %s, so the tests that need a refused connection would reach it", parsed.Host)
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		t.Fatalf("connecting to %s timed out instead of being refused: %v", parsed.Host, err)
	}
}
