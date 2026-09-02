package openapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// ErrRouteMissing marks a 404 that came from the server not exposing an
// endpoint at all, as opposed to the requested resource not existing. It is
// attached as the cause of the mapped error, so callers detect it with
// IsRouteMissing (or errors.Is).
//
// The distinction matters when Atlassian retires an endpoint: without it, a
// removed route is indistinguishable from a missing pull request, and a client
// reports "not found" for a request that could never have succeeded.
var ErrRouteMissing = errors.New("bitbucket endpoint is not available on this server")

// IsRouteMissing reports whether err came from an endpoint the server does not
// expose.
func IsRouteMissing(err error) bool {
	return errors.Is(err, ErrRouteMissing)
}

// isRouteMissingBody distinguishes the two kinds of 404 Bitbucket returns.
//
// Bitbucket answers a request for a resource that does not exist with its own
// JSON error envelope, which always carries an "errors" array:
//
//	{"errors":[{"message":"Pull request 9 does not exist in P/r.",
//	            "exceptionName":"com.atlassian.bitbucket.pull.NoSuchPullRequestException"}]}
//
// A route the application never registered never reaches that layer, so the
// servlet container answers instead with a status document. On 10.2.1 that was
// XML; on 10.4.2 it is JSON:
//
//	{"message":"HTTP 404 Not Found","status-code":404,"sub-code":-1}
//
// The encoding is therefore not something to key on. Anything that is not a
// Bitbucket error envelope is treated as the route being absent, which holds for
// both forms. Verified against Bitbucket Data Center 10.2.1 and re-probed
// against 10.4.2 for removed endpoints, unknown routes, missing pull requests
// and missing repositories.
func isRouteMissingBody(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		// An empty body carries no evidence either way. Treat it as a normal
		// not-found so a genuinely missing resource is never mistaken for a
		// server that lacks the feature.
		return false
	}

	var envelope struct {
		Errors []struct {
			Message       *string `json:"message"`
			ExceptionName *string `json:"exceptionName"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return true
	}

	return len(envelope.Errors) == 0
}

// MapStatusError maps a Bitbucket API HTTP status code and response body to a domain error.
func MapStatusError(status int, body []byte) error {
	if status >= 200 && status < 300 {
		return nil
	}

	message := strings.TrimSpace(string(body))
	if message == "" {
		message = http.StatusText(status)
	}

	baseMessage := fmt.Sprintf("bitbucket API returned %d: %s", status, message)

	switch status {
	case http.StatusBadRequest:
		return apperrors.New(apperrors.KindValidation, baseMessage, nil)
	case http.StatusUnauthorized:
		return apperrors.New(apperrors.KindAuthentication, baseMessage, nil)
	case http.StatusForbidden:
		return apperrors.New(apperrors.KindAuthorization, baseMessage, nil)
	case http.StatusNotFound:
		if isRouteMissingBody(body) {
			return apperrors.New(apperrors.KindNotFound, baseMessage, ErrRouteMissing)
		}
		return apperrors.New(apperrors.KindNotFound, baseMessage, nil)
	case http.StatusConflict:
		return apperrors.New(apperrors.KindConflict, baseMessage, nil)
	case http.StatusTooManyRequests:
		return apperrors.New(apperrors.KindTransient, baseMessage, nil)
	default:
		if status >= 500 {
			return apperrors.New(apperrors.KindTransient, baseMessage, nil)
		}
		return apperrors.New(apperrors.KindPermanent, baseMessage, nil)
	}
}
