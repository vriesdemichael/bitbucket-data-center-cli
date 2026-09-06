// Package retrypolicy decides which HTTP requests may be replayed.
//
// Both transports used to retry every method on a transport error and on any
// 429 or 5xx, twice by default. A POST that reached Bitbucket and whose
// response was lost -- a reset connection, a proxy answering 502 after the
// write landed, a load balancer timing out -- is indistinguishable from one
// that never arrived, so `bb pr create` could open the same pull request three
// times and report success for whichever attempt answered (#454).
//
// ADR-009 already asked for retries to be "explicit and safe for idempotent
// operations". No guard existed; this is it.
package retrypolicy

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Replayable reports whether a request may be sent again after a transport
// error or a retriable status.
//
// The idempotent methods by definition: sending one twice reaches the same
// state as sending it once, so a lost response costs a duplicate request and
// not a duplicate resource.
//
// POST and PATCH are not on the list, and deliberately are not retried even
// when the failure looks like it happened before the request left. Go does not
// hand back a reliable "the bytes never landed" signal, and the cost of reading
// one wrong is a duplicate mutation nobody notices -- against one extra manual
// retry with an honest message if we simply refuse. On a CLI the operator is
// right there; that is the cheaper mistake.
func Replayable(method string) bool {
	// Upper-cased first. Every caller passes a canonical method today, and a
	// lower-case one would otherwise fall to the default and quietly lose its
	// retries -- a resilience regression with no error to notice.
	switch strings.ToUpper(strings.TrimSpace(method)) {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodPut, http.MethodDelete, http.MethodTrace:
		return true
	default:
		return false
	}
}

// RetriableStatus reports whether a response status may be retried for a
// request using this method.
//
// 429 is retriable whatever the method. The server is stating that it did not
// process the request, so replaying a POST after Retry-After creates nothing
// twice -- that is the one case where a non-idempotent replay is safe, and it
// is worth keeping because rate limiting is the failure most likely to be
// transient.
//
// A 5xx says the opposite: the server may well have applied the request before
// failing to say so.
func RetriableStatus(method string, status int) bool {
	if status == http.StatusTooManyRequests {
		return true
	}

	return status >= 500 && Replayable(method)
}

// Delay reports how long to wait before replaying a request.
//
// Retry-After decides it when the server sends one, in either form the HTTP
// specification allows: a number of seconds, or a date. A rate limiter that
// says when it will have capacity again knows better than any backoff we
// choose, and a request replayed before then is refused again and spends
// another attempt. A date already in the past, or a negative count, means now.
//
// Without the header it is a linear backoff on the attempt number.
//
// This lived twice, once in each transport, in copies that were identical
// character for character. That made the two consistent by coincidence rather
// than by construction, which is the same reason Replayable and RetriableStatus
// are here: a policy about retrying belongs in one place, or the next change
// lands in one transport and not the other.
func Delay(headers http.Header, attempt int, fallbackBase time.Duration) time.Duration {
	if fallbackBase <= 0 {
		fallbackBase = defaultBackoffBase
	}

	if headers != nil {
		if retryAfter := strings.TrimSpace(headers.Get("Retry-After")); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				if seconds < 0 {
					seconds = 0
				}

				return time.Duration(seconds) * time.Second
			}

			if retryAt, err := http.ParseTime(retryAfter); err == nil {
				if delay := time.Until(retryAt); delay > 0 {
					return delay
				}

				return 0
			}
		}
	}

	return time.Duration(attempt+1) * fallbackBase
}

// defaultBackoffBase is used when a caller passes no base of its own.
const defaultBackoffBase = 250 * time.Millisecond
