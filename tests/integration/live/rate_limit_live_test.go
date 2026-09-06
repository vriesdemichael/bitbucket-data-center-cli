//go:build live

package live_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestLiveRateLimitedRequestsAreHandled covers what bb does when Bitbucket
// answers 429.
//
// Everything about that path was proven against handlers this repository wrote:
// both transports have unit tests for retrying a 429, for honouring Retry-After
// in seconds and as a date, and for exhausting the retries -- and every one of
// them answers with a response the test itself constructed. What none of them
// can say is what Bitbucket sends. Whether it sets Retry-After at all, in which
// of the two forms, and whether the body is the errors array the CLI's message
// is built from, are the server's answers, and the retry policy is written
// entirely around them (ADR-079).
//
// Deliberately not parallel. Rate limiting is one switch for the whole
// instance -- a per-user limit is accepted while it is off and simply not
// enforced, which was checked before this test was written -- so turning it on
// affects every request anyone makes. Go runs the tests that did not declare
// themselves parallel before it releases any that did, so a sequential test has
// the instance to itself, and the switch goes back off in a cleanup.
func TestLiveRateLimitedRequestsAreHandled(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]

	// A licensed user, because an unlicensed one cannot authenticate at all --
	// Bitbucket answers 401 rather than letting them through unlicensed.
	limited, err := harness.createLicensedUser(ctx)
	if err != nil {
		t.Fatalf("create the rate-limited user failed: %v", err)
	}
	if err := harness.grantRepoPermission(ctx, seeded.Key, repo.Slug, limited.Username, "REPO_READ"); err != nil {
		t.Fatalf("grant read failed: %v", err)
	}

	// One token, refilled one per second: the second request in a burst is
	// refused, which is the smallest limit that produces a 429 reliably.
	harness.limitUserRate(ctx, t, limited.Username, 1, 1)
	harness.enableRateLimiting(ctx, t)

	// Bitbucket's own 429, and what it asks for.
	//
	// Required rather than recorded. If Bitbucket sent no Retry-After the
	// client would fall back to its own backoff and still work, so a test that
	// merely logged the header would pass either way -- and the whole
	// Retry-After path, the part written to obey the server rather than guess,
	// would be dead code nobody noticed. Probed against 10.4.2: the header is
	// there, in the delay-seconds form.
	status, header, body := harness.burstUntilRefused(t, limited, 20)
	if status != http.StatusTooManyRequests {
		t.Fatalf("20 requests against a one-token bucket produced no 429; last status %d", status)
	}

	retryAfter := strings.TrimSpace(header.Get("Retry-After"))
	if retryAfter == "" {
		t.Fatalf("Bitbucket's 429 carries no Retry-After, so bb is guessing when to retry:\n%s", body)
	}

	// Read here rather than through retrypolicy.Delay, which is the function
	// under test. Asking it what the header means and then checking bb waited
	// that long compares the implementation with itself: with the header
	// handling disabled, Delay returned its 1s fallback, the assertion moved
	// down to match, and the test passed against a client that was ignoring
	// Retry-After entirely. That is the shape ADR-079 is about, reached from
	// the other direction -- not a mocked server, but an expectation computed
	// by the code it is meant to hold to account.
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil {
		t.Fatalf("Bitbucket's Retry-After is not a number of seconds: %q", retryAfter)
	}
	wait := time.Duration(seconds) * time.Second

	t.Logf("Bitbucket's 429 carries Retry-After=%q, so a client that obeys it waits %s", retryAfter, wait)

	// The message a caller sees is built from the body, so a body in some other
	// shape means the error reads as nothing useful.
	if !strings.Contains(body, "errors") && !strings.Contains(body, "rate") {
		t.Errorf("the 429 body is neither the errors array nor a rate-limit message, so bb has nothing to report: %s", body)
	}

	configureLiveCLIEnvForUser(t, harness, seeded.Key, repo.Slug, limited)
	setLiveRepoContext(t, seeded.Key, repo.Slug)

	// Both clients, because they are two retry loops rather than one. `bb api`
	// goes through internal/transport/httpclient and everything typed goes
	// through the generated client's RoundTripper; they share the policy that
	// decides whether and how long to wait, and nothing else. A regression in
	// either would leave the other passing.
	assertAbsorbsRateLimit(t, wait, "the raw passthrough",
		"--json", "api", "/rest/api/latest/inbox/pull-requests/count")
	assertAbsorbsRateLimit(t, wait, "a typed command",
		"--json", "project", "list", "--limit", "1")
}

// assertAbsorbsRateLimit runs a command until it sees one invocation that had
// to wait, and fails if none did.
//
// Repeated rather than run once, because whether a given call is refused
// depends on whether the bucket happens to hold a token: after an invocation
// waits out a five-second Retry-After the bucket has refilled, so the next one
// goes straight through. Alternating like that is correct behaviour and made a
// single-shot assertion a coin toss -- the first version of this test asserted
// on one call and passed for the wrong reason.
//
// What is asserted is the waiting. Success alone is also what an unrefused
// request looks like; an invocation that ignored Retry-After and used the
// 250ms fallback would come back in a quarter of a second having failed, and
// one that did not retry at all would come back failed immediately.
func assertAbsorbsRateLimit(t *testing.T, wait time.Duration, subject string, args ...string) {
	t.Helper()

	const attempts = 6

	for attempt := 0; attempt < attempts; attempt++ {
		started := time.Now()
		output, err := executeLiveCLI(t, args...)
		elapsed := time.Since(started)

		if err != nil {
			// Transient is still the right classification -- retrying is the
			// caller's remedy and the kind is what tells them so -- but bb gave
			// up where it could have waited, so it is reported either way.
			if kind := apperrors.KindOf(err); kind != apperrors.KindTransient {
				t.Fatalf("%s reported %v for a rate-limited request rather than transient: %v\noutput: %s",
					subject, kind, err, output)
			}
			t.Fatalf("%s surfaced the rate limit after %s instead of waiting the %s Bitbucket asked for: %v",
				subject, elapsed, wait, err)
		}

		// Half, not the whole, because the bucket refills while the request is
		// in flight and the server may name a shorter wait on the retry.
		if elapsed >= wait/2 {
			t.Logf("%s absorbed the refusal: waited %s, then succeeded", subject, elapsed)

			return
		}
	}

	t.Fatalf("%s was never made to wait in %d attempts against a bucket of one token per second, "+
		"so either the requests are not counted against the limit or the wait is not happening", subject, attempts)
}

// enableRateLimiting turns the instance-wide switch on and off again.
func (h *liveHarness) enableRateLimiting(ctx context.Context, t *testing.T) {
	t.Helper()

	settings, err := h.liveJSON(ctx, http.MethodGet, "/rest/api/latest/admin/rate-limit/settings", nil)
	if err != nil {
		t.Fatalf("read the rate-limit settings failed: %v", err)
	}
	previous, _ := settings["enabled"].(bool)

	t.Cleanup(func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := h.liveJSON(restoreCtx, http.MethodPut, "/rest/api/latest/admin/rate-limit/settings",
			map[string]any{"enabled": previous}); err != nil {
			// Loud, because leaving it on rate-limits every later run against
			// this instance and the symptom looks like something else entirely.
			t.Errorf("could not turn rate limiting back %v: %v", previous, err)
		}
	})

	if _, err := h.liveJSON(ctx, http.MethodPut, "/rest/api/latest/admin/rate-limit/settings",
		map[string]any{"enabled": true}); err != nil {
		t.Fatalf("enable rate limiting failed: %v", err)
	}
}

// limitUserRate gives one user a bucket small enough to overflow.
func (h *liveHarness) limitUserRate(ctx context.Context, t *testing.T, username string, capacity, fillRate int) {
	t.Helper()

	path := "/rest/api/latest/admin/rate-limit/settings/users/" + username

	t.Cleanup(func() {
		restoreCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, _ = h.liveJSON(restoreCtx, http.MethodDelete, path, nil)
	})

	if _, err := h.liveJSON(ctx, http.MethodPut, path,
		map[string]any{"settings": map[string]any{"capacity": capacity, "fillRate": fillRate}}); err != nil {
		t.Fatalf("set the per-user rate limit failed: %v", err)
	}
}

// burstUntilRefused sends requests as the user until one is refused, and
// returns that response.
func (h *liveHarness) burstUntilRefused(t *testing.T, user restrictedUser, attempts int) (int, http.Header, string) {
	t.Helper()

	endpoint := strings.TrimRight(h.config.BitbucketURL, "/") + "/rest/api/latest/inbox/pull-requests/count"

	status := 0
	for attempt := 0; attempt < attempts; attempt++ {
		request, err := http.NewRequest(http.MethodGet, endpoint, nil)
		if err != nil {
			t.Fatalf("build the burst request failed: %v", err)
		}
		request.SetBasicAuth(user.Username, user.Password)

		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("burst request %d failed: %v", attempt+1, err)
		}

		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()
		status = response.StatusCode

		if status == http.StatusTooManyRequests {
			return status, response.Header, string(body)
		}
	}

	return status, nil, fmt.Sprintf("no refusal in %d requests", attempts)
}
