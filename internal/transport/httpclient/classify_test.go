package httpclient

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestATransportFailureIsClassifiedByWhetherRetryingCouldHelp is #574.
//
// Every transport failure was transient, exit 10, "retry later", and retried
// three times -- including a rejected certificate, which will not fix itself,
// and a timed-out mutation, whose outcome is unknown.
func TestATransportFailureIsClassifiedByWhetherRetryingCouldHelp(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		method string
		err    error
		want   apperrors.Kind
		says   string
	}{
		"an untrusted certificate": {
			method: http.MethodGet,
			err:    fmt.Errorf("get: %w", x509.UnknownAuthorityError{}),
			want:   apperrors.KindPermanent,
			says:   "TLS certificate was rejected",
		},
		"a hostname the certificate does not cover": {
			method: http.MethodGet,
			err:    fmt.Errorf("get: %w", x509.HostnameError{Host: "bitbucket.example"}),
			want:   apperrors.KindPermanent,
			says:   "TLS certificate was rejected",
		},
		"a name that does not resolve": {
			method: http.MethodGet,
			err:    fmt.Errorf("dial: %w", &net.DNSError{Err: "no such host", IsNotFound: true}),
			want:   apperrors.KindPermanent,
			says:   "does not resolve",
		},
		"a mutation that timed out": {
			method: http.MethodPost,
			err:    fmt.Errorf("do: %w", context.DeadlineExceeded),
			want:   apperrors.KindPermanent,
			says:   "outcome is unknown",
		},
		"a read that timed out": {
			method: http.MethodGet,
			err:    fmt.Errorf("do: %w", context.DeadlineExceeded),
			want:   apperrors.KindTransient,
			says:   "request failed",
		},
		"a connection refused": {
			method: http.MethodGet,
			err:    errors.New("connect: connection refused"),
			want:   apperrors.KindTransient,
			says:   "request failed",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := classifyTransportError(testCase.method, testCase.err)
			if got := apperrors.KindOf(err); got != testCase.want {
				t.Errorf("classified %s, want %s: %v", got, testCase.want, err)
			}
			if !strings.Contains(err.Error(), testCase.says) {
				t.Errorf("the message does not say %q: %v", testCase.says, err)
			}
			// The cause has to survive, or the detail is lost to the reader.
			if !errors.Is(err, testCase.err) && errors.Unwrap(err) == nil {
				t.Errorf("the underlying error was dropped: %v", err)
			}
		})
	}
}

// A timed-out GET is safe to retry; a timed-out POST is not. The retry policy
// already refuses to replay the POST -- this is the exit code agreeing with it.
func TestATimedOutMutationIsNotReportedRetriable(t *testing.T) {
	t.Parallel()

	post := classifyTransportError(http.MethodPost, fmt.Errorf("do: %w", context.DeadlineExceeded))
	get := classifyTransportError(http.MethodGet, fmt.Errorf("do: %w", context.DeadlineExceeded))

	if apperrors.ExitCode(post) == apperrors.ExitCode(get) {
		t.Fatalf("a timed-out POST and GET report the same exit code %d", apperrors.ExitCode(post))
	}
	if apperrors.ExitCode(get) != 10 {
		t.Errorf("a timed-out read should stay retriable, got exit %d", apperrors.ExitCode(get))
	}
}
