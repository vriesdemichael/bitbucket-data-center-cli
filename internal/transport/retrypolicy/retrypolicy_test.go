package retrypolicy_test

import (
	"net/http"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/retrypolicy"
)

// TestOnlyIdempotentMethodsAreReplayable is #454.
//
// Both transports replayed every method twice by default, so a POST whose
// response was lost could open the same pull request three times and report
// success for whichever attempt answered.
func TestOnlyIdempotentMethodsAreReplayable(t *testing.T) {
	t.Parallel()

	for method, want := range map[string]bool{
		http.MethodGet:     true,
		http.MethodHead:    true,
		http.MethodOptions: true,
		http.MethodPut:     true,
		http.MethodDelete:  true,
		http.MethodTrace:   true,
		http.MethodPost:    false,
		http.MethodPatch:   false,
	} {
		if got := retrypolicy.Replayable(method); got != want {
			t.Errorf("Replayable(%s) = %v, want %v", method, got, want)
		}
	}
}

// TestRetriableStatus covers the one case where replaying a mutation is safe.
func TestRetriableStatus(t *testing.T) {
	t.Parallel()

	t.Run("429 is retriable for every method", func(t *testing.T) {
		t.Parallel()

		// The server is stating it did not process the request, so a replayed
		// POST creates nothing twice. This is the exception worth keeping:
		// rate limiting is the failure most likely to be transient.
		for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete} {
			if !retrypolicy.RetriableStatus(method, http.StatusTooManyRequests) {
				t.Errorf("429 not retriable for %s", method)
			}
		}
	})

	t.Run("5xx is retriable only for idempotent methods", func(t *testing.T) {
		t.Parallel()

		// A 5xx says the opposite of a 429: the server may have applied the
		// request before failing to say so.
		for _, status := range []int{http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable} {
			if !retrypolicy.RetriableStatus(http.MethodGet, status) {
				t.Errorf("%d not retriable for GET", status)
			}
			if retrypolicy.RetriableStatus(http.MethodPost, status) {
				t.Errorf("%d retriable for POST; a lost response would duplicate the resource", status)
			}
		}
	})

	t.Run("a success or a client error is never retried", func(t *testing.T) {
		t.Parallel()

		for _, status := range []int{http.StatusOK, http.StatusCreated, http.StatusBadRequest, http.StatusNotFound, http.StatusConflict} {
			if retrypolicy.RetriableStatus(http.MethodGet, status) {
				t.Errorf("%d was treated as retriable", status)
			}
		}
	})
}
