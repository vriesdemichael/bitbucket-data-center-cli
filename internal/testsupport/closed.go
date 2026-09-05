package testsupport

import (
	"net/http/httptest"
	"testing"
)

// ClosedListenerURL returns a URL nothing is listening on.
//
// A port is taken and released, so the address is real and refused rather than
// unroutable: connecting fails immediately instead of waiting for a DNS lookup
// or a timeout, which is what makes this usable in a unit test.
//
// It is how a test asks for a transport fault. A client cannot be made to lose
// a connection by a server that is answering, and no live Bitbucket can be
// asked to drop one on cue, so this is the only honest way to check that a
// failure below the API is reported rather than swallowed.
//
// The listener is opened with a handler that fails the test, which cannot fire
// -- and is deliberate. If a request ever does arrive, the port was reused and
// the test is no longer measuring what it thinks.
func ClosedListenerURL(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(UnreachedHandler(t))
	url := server.URL
	server.Close()

	return url
}
