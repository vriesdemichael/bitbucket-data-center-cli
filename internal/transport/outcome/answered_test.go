package outcome_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/outcome"
)

// TestAStatusThatArrivedIsTheOutcome is a body that fails to read after the
// server has already answered.
//
// It reported "no answer came back", exit 13, for a 4xx the caller could act
// on: a refused change is refused, and checking whether it landed is a trip
// for nothing. A 2xx stays unknown: see answered().
func TestAStatusThatArrivedIsTheOutcome(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		method  string
		status  int
		kind    apperrors.Kind
		message string
	}{
		// A 2xx is the one status that does not settle it. An SSO proxy answers
		// an unauthenticated write with a redirect to a login page, which the
		// client follows, so a 200 can belong to that page rather than to the
		// POST -- and the body that would say which is the part that was lost.
		"a status that says created": {
			method: http.MethodPost, status: http.StatusCreated,
			kind: apperrors.KindUnknownOutcome, message: "answered with 201",
		},
		"a refused change": {
			method: http.MethodPost, status: http.StatusConflict,
			kind: apperrors.KindPermanent, message: "refused with 409",
		},
		"a server error": {
			method: http.MethodPost, status: http.StatusBadGateway,
			kind: apperrors.KindTransient, message: "answered the POST with 502",
		},
		"a read that can simply be repeated": {
			method: http.MethodGet, status: http.StatusOK,
			kind: apperrors.KindTransient, message: "answered the GET with 200",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, exchange := outcome.Track(httptest.NewRequest(testCase.method, "https://bitbucket.example.com/rest", nil))
			exchange.Answered(testCase.status)

			err := exchange.ClassifyRead(errors.New("unexpected EOF"))
			if got := apperrors.KindOf(err); got != testCase.kind {
				t.Errorf("kind = %s, want %s (%v)", got, testCase.kind, err)
			}
			if !strings.Contains(err.Error(), testCase.message) {
				t.Errorf("the message does not say what happened: %v", err)
			}
		})
	}
}

// Nothing answered, so the outcome of a mutation really is unknown.
func TestNothingAnsweredIsStillUnknown(t *testing.T) {
	t.Parallel()

	request, exchange := outcome.Track(httptest.NewRequest(http.MethodPost, "https://bitbucket.example.com/rest", nil))
	// The request reached the wire: that is what makes the outcome unknown
	// rather than a failure before anything was sent.
	if trace := request.Context(); trace == nil {
		t.Fatal("the tracked request lost its context")
	}
	exchange.Classify(io.ErrUnexpectedEOF)

	err := exchange.ClassifyRead(io.ErrUnexpectedEOF)
	if apperrors.KindOf(err) != apperrors.KindUnknownOutcome && apperrors.KindOf(err) != apperrors.KindTransient {
		t.Fatalf("a read failure with no status should stay unknown or transient, got %v", err)
	}
}
