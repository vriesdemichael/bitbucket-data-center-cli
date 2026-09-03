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

import "net/http"

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
	switch method {
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
