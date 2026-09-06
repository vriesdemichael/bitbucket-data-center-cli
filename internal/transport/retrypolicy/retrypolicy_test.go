package retrypolicy_test

import (
	"net/http"
	"testing"
	"time"

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

// TestMethodMatchingIsCaseInsensitive keeps a non-canonical method from
// silently losing its retries.
//
// Every caller passes an upper-case method today, so this guards a trap rather
// than a bug: "get" would have fallen to the default and stopped being retried,
// with nothing to notice it by.
func TestMethodMatchingIsCaseInsensitive(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"get", "Get", " GET ", "delete"} {
		if !retrypolicy.Replayable(method) {
			t.Errorf("Replayable(%q) = false; an idempotent method lost its retries", method)
		}
	}

	// And the unsafe direction stays unsafe whatever the spelling.
	for _, method := range []string{"post", "Post", "patch"} {
		if retrypolicy.Replayable(method) {
			t.Errorf("Replayable(%q) = true; a mutation became replayable", method)
		}
		if retrypolicy.RetriableStatus(method, 503) {
			t.Errorf("RetriableStatus(%q, 503) = true", method)
		}
	}
}

// TestDelay covers what the two transports each used to test against their own
// copy of this function.
//
// The copies were identical, so the two suites made the same six assertions
// twice; they are here once now, against the one implementation both call.
func TestDelay(t *testing.T) {
	t.Parallel()

	t.Run("uses retry-after seconds", func(t *testing.T) {
		t.Parallel()

		if delay := retrypolicy.Delay(http.Header{"Retry-After": []string{"3"}}, 0, time.Millisecond); delay != 3*time.Second {
			t.Fatalf("expected 3s delay, got %s", delay)
		}
	})

	t.Run("falls back on invalid retry-after", func(t *testing.T) {
		t.Parallel()

		if delay := retrypolicy.Delay(http.Header{"Retry-After": []string{"invalid"}}, 1, 200*time.Millisecond); delay != 400*time.Millisecond {
			t.Fatalf("expected fallback delay 400ms, got %s", delay)
		}
	})

	t.Run("supports retry-after http date", func(t *testing.T) {
		t.Parallel()

		retryAt := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
		delay := retrypolicy.Delay(http.Header{"Retry-After": []string{retryAt}}, 0, time.Millisecond)
		if delay <= 0 || delay > 3*time.Second {
			t.Fatalf("expected positive delay <=3s, got %s", delay)
		}
	})

	t.Run("normalizes negative retry-after seconds", func(t *testing.T) {
		t.Parallel()

		if delay := retrypolicy.Delay(http.Header{"Retry-After": []string{"-2"}}, 0, time.Millisecond); delay != 0 {
			t.Fatalf("expected zero delay for negative retry-after, got %s", delay)
		}
	})

	t.Run("returns zero for a retry-after date already past", func(t *testing.T) {
		t.Parallel()

		retryAt := time.Now().Add(-2 * time.Second).UTC().Format(http.TimeFormat)
		if delay := retrypolicy.Delay(http.Header{"Retry-After": []string{retryAt}}, 0, time.Millisecond); delay != 0 {
			t.Fatalf("expected zero delay for past date, got %s", delay)
		}
	})

	t.Run("falls back when the caller supplies no base", func(t *testing.T) {
		t.Parallel()

		if delay := retrypolicy.Delay(nil, 1, 0); delay != 500*time.Millisecond {
			t.Fatalf("expected fallback delay 500ms, got %s", delay)
		}
	})
}
