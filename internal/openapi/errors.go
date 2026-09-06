package openapi

import (
	"bytes"
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
		if kind, ok := kindFromException(body); ok {
			return apperrors.New(kind, baseMessage, nil)
		}
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

// badRequestExceptions are the 400s whose exceptionName says the status is
// wrong about what happened.
//
// 400 is the one ambiguous status Bitbucket sends in practice: 403, 404 and 409
// each mean one thing, and a full live run produces no 5xx at all. So this is a
// short list rather than a table, and it is grown from
// docs/quality/bitbucket-error-registry.json -- an entry earns its place by
// having been observed, not by seeming likely.
//
// DuplicateRefException is `bb branch create` on a name that is already taken.
// The request was well formed and the caller can fix it, but not by correcting
// their input: the branch is there. Reporting validation sends them looking at
// what they typed, and exit 2 tells a script the wrong thing about a repository
// state that exit 5 describes exactly.
var badRequestExceptions = map[string]apperrors.Kind{
	"com.atlassian.bitbucket.repository.DuplicateRefException": apperrors.KindConflict,
}

// kindFromException reads Bitbucket's own name for what went wrong.
//
// Additive by construction: an exception that is not listed, or a body that
// carries none, falls through to the status-only answer. A change here can only
// correct a case, never break one that works today.
func kindFromException(body []byte) (apperrors.Kind, bool) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return "", false
	}

	var envelope struct {
		Errors []struct {
			ExceptionName *string `json:"exceptionName"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(trimmed), &envelope); err != nil {
		return "", false
	}

	for _, one := range envelope.Errors {
		if one.ExceptionName == nil {
			continue
		}
		if kind, ok := badRequestExceptions[strings.TrimSpace(*one.ExceptionName)]; ok {
			return kind, true
		}
	}

	return "", false
}

// MissingPayload says what a 2xx that carried no usable payload means.
//
// It answers one question ten call sites used to answer four different ways:
// six returned an empty success, three a KindPermanent, two a KindInternal.
// Nothing decided that; it is wherever each was written, and the difference
// matters because KindInternal tells the caller bb is broken.
//
// It already has, once. Rebasing a branch that is already on top of its target
// answers 204, and reading that as a failure produced
//
//	internal: unexpected empty rebase response body   (exit 1)
//
// for a pull request that was exactly where it had been asked to be
// (OPENAPI-028). Three situations were being collapsed into one:
//
//   - the server sent nothing where the spec documents a payload. The endpoint
//     and the specification disagree about that, which is not bb malfunctioning.
//   - the server sent something the generated client could not read as the
//     documented type. Same disagreement, and the body goes into the message
//     because it is the evidence for the OPENAPI-* entry that should be written.
//
// Both are permanent: a retry sends the identical request and gets the
// identical answer. Neither is internal, which is the word bb uses for its own
// bugs and which sends the reader to the wrong repository.
//
// This never turns a failure into a success. A 2xx that legitimately carries no
// payload -- a 204 from an endpoint whose spec documents only the 200 -- is the
// call site's to recognise, before it asks here; see Rebase, which returns
// success on a 204 and reaches this only when something else went wrong.
//
// what names the operation, so the message says which call came back empty.
func MissingPayload(status int, body []byte, what string) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return apperrors.New(apperrors.KindPermanent, fmt.Sprintf(
			"%s: the server answered %d with an empty body where the specification documents a payload",
			what, status), nil)
	}

	return apperrors.New(apperrors.KindPermanent, fmt.Sprintf(
		"%s: the server answered %d with a body this client could not read as the documented payload, "+
			"so the specification and the server disagree: %s",
		what, status, strings.TrimSpace(string(body))), nil)
}
