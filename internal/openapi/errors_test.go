package openapi

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestMapStatusError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		status   int
		body     []byte
		wantKind apperrors.Kind
	}{
		{
			name:     "200 OK",
			status:   http.StatusOK,
			body:     nil,
			wantKind: "",
		},
		{
			name:     "400 Bad Request",
			status:   http.StatusBadRequest,
			body:     []byte("invalid input"),
			wantKind: apperrors.KindValidation,
		},
		{
			name:     "401 Unauthorized",
			status:   http.StatusUnauthorized,
			body:     nil,
			wantKind: apperrors.KindAuthentication,
		},
		{
			name:     "403 Forbidden",
			status:   http.StatusForbidden,
			body:     nil,
			wantKind: apperrors.KindAuthorization,
		},
		{
			name:     "404 Not Found",
			status:   http.StatusNotFound,
			body:     nil,
			wantKind: apperrors.KindNotFound,
		},
		{
			name:     "409 Conflict",
			status:   http.StatusConflict,
			body:     nil,
			wantKind: apperrors.KindConflict,
		},
		{
			name:     "429 Too Many Requests",
			status:   http.StatusTooManyRequests,
			body:     nil,
			wantKind: apperrors.KindTransient,
		},
		{
			name:     "500 Internal Server Error",
			status:   http.StatusInternalServerError,
			body:     nil,
			wantKind: apperrors.KindTransient,
		},
		{
			name:     "503 Service Unavailable",
			status:   http.StatusServiceUnavailable,
			body:     nil,
			wantKind: apperrors.KindTransient,
		},
		{
			name:     "418 I'm a teapot (Other 4xx)",
			status:   http.StatusTeapot,
			body:     nil,
			wantKind: apperrors.KindPermanent,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MapStatusError(tt.status, tt.body)
			if tt.wantKind == "" {
				if err != nil {
					t.Errorf("MapStatusError() error = %v, want nil", err)
				}
				return
			}

			if err == nil {
				t.Errorf("MapStatusError() error = nil, want kind %v", tt.wantKind)
				return
			}

			var appErr *apperrors.AppError
			ok := errors.As(err, &appErr)
			if !ok {
				t.Errorf("MapStatusError() error = %T, want *apperrors.AppError", err)
				return
			}

			if appErr.Kind != tt.wantKind {
				t.Errorf("MapStatusError() kind = %v, want %v", appErr.Kind, tt.wantKind)
			}
		})
	}
}

// The bodies below are verbatim from Bitbucket Data Center 10.2.1. If Atlassian
// changes either format, this fails rather than silently reclassifying every
// removed endpoint as a missing resource.
func TestIsRouteMissingClassification(t *testing.T) {
	t.Parallel()

	const containerStatusDocument = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<status><status-code>404</status-code><message>HTTP 404 Not Found</message></status>`
	const missingPullRequest = `{"errors":[{"context":null,` +
		`"message":"Pull request 999999 does not exist in LT549400/lt-repo-1-9400.",` +
		`"exceptionName":"com.atlassian.bitbucket.pull.NoSuchPullRequestException"}]}`
	const missingRepository = `{"errors":[{"context":null,` +
		`"message":"Repository LT549400/nope does not exist.",` +
		`"exceptionName":"com.atlassian.bitbucket.repository.NoSuchRepositoryException"}]}`

	cases := []struct {
		name string
		body string
		want bool
	}{
		{name: "removed endpoint", body: containerStatusDocument, want: true},
		{name: "unknown route", body: containerStatusDocument, want: true},
		{name: "html error page", body: "<html><body>Not Found</body></html>", want: true},
		{name: "plain text", body: "404 page not found", want: true},
		{name: "json without an errors array", body: `{"message":"HTTP 404 Not Found","status-code":404}`, want: true},
		{name: "empty errors array", body: `{"errors":[]}`, want: true},
		{name: "missing pull request", body: missingPullRequest, want: false},
		{name: "missing repository", body: missingRepository, want: false},
		{name: "empty body is not evidence", body: "", want: false},
		{name: "whitespace body is not evidence", body: "   \n ", want: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := MapStatusError(http.StatusNotFound, []byte(testCase.body))
			if got := IsRouteMissing(err); got != testCase.want {
				t.Fatalf("IsRouteMissing() = %v, want %v for body %q", got, testCase.want, testCase.body)
			}
			if !apperrors.IsKind(err, apperrors.KindNotFound) {
				t.Fatalf("expected a not_found error regardless of classification, got %v", err)
			}
		})
	}
}

// Only 404 can mean "this route does not exist"; other statuses reached the
// application, so their bodies must never be classified that way.
func TestIsRouteMissingOnlyAppliesToNotFound(t *testing.T) {
	t.Parallel()

	const containerStatusDocument = `<status><status-code>500</status-code><message>HTTP 500</message></status>`

	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusConflict,
		http.StatusInternalServerError,
		http.StatusBadGateway,
	} {
		if err := MapStatusError(status, []byte(containerStatusDocument)); IsRouteMissing(err) {
			t.Fatalf("status %d must not be classified as a missing route", status)
		}
	}
}

func TestIsRouteMissingIgnoresUnrelatedErrors(t *testing.T) {
	t.Parallel()

	if IsRouteMissing(nil) {
		t.Fatal("nil must not be classified as a missing route")
	}
	if IsRouteMissing(apperrors.New(apperrors.KindNotFound, "plain not found", nil)) {
		t.Fatal("a plain not-found error must not be classified as a missing route")
	}
	if IsRouteMissing(errors.New("boom")) {
		t.Fatal("an unrelated error must not be classified as a missing route")
	}
}

// TestMissingPayloadTellsAnEmptyAnswerFromAnUnreadableOne covers the two shapes
// the helper exists to distinguish.
//
// Both are permanent, because the identical request returns the identical
// answer, and neither is internal -- which is the word bb reserves for its own
// faults and which sent readers to the wrong repository when a no-op rebase
// reported one (OPENAPI-028).
func TestMissingPayloadTellsAnEmptyAnswerFromAnUnreadableOne(t *testing.T) {
	t.Parallel()

	t.Run("nothing at all", func(t *testing.T) {
		err := MissingPayload(200, nil, "reading a commit")
		if err == nil {
			t.Fatal("an empty payload was accepted")
		}
		if kind := apperrors.KindOf(err); kind != apperrors.KindPermanent {
			t.Errorf("kind = %v, want permanent", kind)
		}
		if !strings.Contains(err.Error(), "empty body") || !strings.Contains(err.Error(), "reading a commit") {
			t.Errorf("message does not say what came back empty: %v", err)
		}
	})

	t.Run("something the client could not read", func(t *testing.T) {
		err := MissingPayload(200, []byte(`{"unexpected":"shape"}`), "reading the diff")
		if err == nil {
			t.Fatal("an unreadable payload was accepted")
		}
		if kind := apperrors.KindOf(err); kind != apperrors.KindPermanent {
			t.Errorf("kind = %v, want permanent", kind)
		}
		// The body is the evidence for the OPENAPI-* entry somebody has to
		// write, so it belongs in the message rather than being swallowed.
		if !strings.Contains(err.Error(), `{"unexpected":"shape"}`) {
			t.Errorf("the body is missing from the message: %v", err)
		}
		if !strings.Contains(err.Error(), "disagree") {
			t.Errorf("the message does not say the spec and the server disagree: %v", err)
		}
	})

	t.Run("whitespace is nothing at all", func(t *testing.T) {
		err := MissingPayload(200, []byte("  \n\t "), "reading a commit")
		if err == nil || !strings.Contains(err.Error(), "empty body") {
			t.Errorf("a whitespace body was not read as empty: %v", err)
		}
	})
}

// TestMapStatusErrorReadsTheExceptionOnA400 pins the one correction the error
// registry justified, and the fallthrough that keeps it from breaking anything.
func TestMapStatusErrorReadsTheExceptionOnA400(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		body string
		want apperrors.Kind
	}{
		{
			name: "a duplicate ref is a conflict, not bad input",
			body: `{"errors":[{"message":"Branch 'x' already exists","exceptionName":"com.atlassian.bitbucket.repository.DuplicateRefException"}]}`,
			want: apperrors.KindConflict,
		},
		{
			name: "an exception with no entry keeps the status answer",
			body: `{"errors":[{"exceptionName":"com.atlassian.bitbucket.validation.ArgumentValidationException"}]}`,
			want: apperrors.KindValidation,
		},
		{
			name: "a 400 with no exceptionName keeps the status answer",
			body: `{"errors":[{"message":"The project key must be specified."}]}`,
			want: apperrors.KindValidation,
		},
		{name: "a body that is not JSON keeps the status answer", body: "<html>bad request</html>", want: apperrors.KindValidation},
		{name: "no body at all keeps the status answer", body: "", want: apperrors.KindValidation},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := MapStatusError(400, []byte(testCase.body))
			if err == nil {
				t.Fatal("a 400 produced no error")
			}
			if kind := apperrors.KindOf(err); kind != testCase.want {
				t.Errorf("kind = %v, want %v", kind, testCase.want)
			}
		})
	}
}

func TestNamesExceptionRecognisesOnlyTheNameAsked(t *testing.T) {
	t.Parallel()

	const outOfDate = "com.atlassian.bitbucket.pull.PullRequestOutOfDateException"
	body := []byte(`{"errors":[{"message":"You are attempting to modify a pull request based on out-of-date information.","exceptionName":"com.atlassian.bitbucket.pull.PullRequestOutOfDateException","currentVersion":1,"expectedVersion":0}]}`)

	if !NamesException(body, outOfDate) {
		t.Fatal("did not recognise the exception it names")
	}
	if NamesException(body, "com.atlassian.bitbucket.pull.SomeOtherException") {
		t.Fatal("matched an exception the body does not name")
	}

	// A caller that cannot read the body treats the status at face value, so
	// every unusable shape has to report false rather than guess.
	for name, unusable := range map[string][]byte{
		"empty":          {},
		"whitespace":     []byte("   \n"),
		"not json":       []byte("<html>502 Bad Gateway</html>"),
		"no errors key":  []byte(`{"message":"nope"}`),
		"null exception": []byte(`{"errors":[{"message":"nope","exceptionName":null}]}`),
	} {
		if NamesException(unusable, outOfDate) {
			t.Fatalf("%s body reported a match", name)
		}
	}
}

// TestAnUpstreamBodyIsSummarizedNotPasted is #574: one bad project key put
// 18,414 characters of HTML into a single error.message.
func TestAnUpstreamBodyIsSummarizedNotPasted(t *testing.T) {
	// Bitbucket's own sentence is the whole answer, and it was buried in the
	// JSON it arrived in. This body is the one the issue reports.
	approval := []byte(`{"errors":[{"message":"Authors may not update their status.","exceptionName":"com.atlassian.bitbucket.pull.InvalidPullRequestRoleException"}]}`)
	err := MapStatusError(400, approval)
	if err == nil {
		t.Fatal("expected an error for a 400")
	}
	if !strings.Contains(err.Error(), "Authors may not update their status.") {
		t.Fatalf("the message does not carry Bitbucket's sentence: %v", err)
	}
	if strings.Contains(err.Error(), "exceptionName") {
		t.Fatalf("the message still pastes the raw envelope: %v", err)
	}

	// A body with no envelope to read is truncated, and says so.
	page := []byte("<html>" + strings.Repeat("x", 18_000) + "</html>")
	err = MapStatusError(400, page)
	message := err.Error()
	if len(message) > 1_000 {
		t.Fatalf("an 18KB body produced a %d character message", len(message))
	}
	if !strings.Contains(message, "--full-error-body") {
		t.Fatalf("the truncated message does not say how to see the rest: %s", message)
	}
	if !strings.Contains(message, "more characters") {
		t.Fatalf("the truncated message does not say how much was dropped: %s", message)
	}
}

// The way out has to work, for somebody debugging a server bb cannot summarise.
func TestFullUpstreamBodiesPrintsTheWholeThing(t *testing.T) {
	page := []byte("<html>" + strings.Repeat("y", 5_000) + "</html>")

	FullUpstreamBodies = true
	t.Cleanup(func() { FullUpstreamBodies = false })

	message := MapStatusError(500, page).Error()
	if len(message) < 5_000 {
		t.Fatalf("--full-error-body produced a %d character message for a 5KB body", len(message))
	}
}
