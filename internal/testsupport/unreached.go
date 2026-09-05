// Package testsupport holds the pieces that several test packages need and
// that must not be written twice, because a copy is where the two stop
// agreeing.
package testsupport

import (
	"net/http"
)

// Reporter is the part of *testing.T this package needs.
//
// It is an interface so the guard below can be proven to guard: passing a
// *testing.T means a handler that fires fails the test that is checking it,
// which is the one thing it cannot do. Callers pass their *testing.T unchanged.
type Reporter interface {
	Helper()
	Errorf(format string, args ...any)
}

// UnreachedHandler is a handler that fails the test if a request arrives.
//
// It is for the tests whose subject is a refusal: a service that validates its
// arguments, or a command that stops on a permission check, must decide before
// it asks. Handing those an empty handler says the same thing to the reader and
// nothing to the test -- a service that dropped its validation would reach an
// empty handler, get a 200 with no body, and fail for the wrong reason or not
// at all.
//
// The name of the request is in the failure, so a test that starts making one
// says which.
func UnreachedHandler(t Reporter) http.HandlerFunc {
	t.Helper()

	return func(_ http.ResponseWriter, request *http.Request) {
		t.Errorf("a request was made where none should have been: %s %s", request.Method, request.URL.Path)
	}
}
