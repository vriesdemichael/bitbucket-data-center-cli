// Package outcome says what a failed HTTP exchange means for the caller.
//
// A request that fails leaves the caller in one of four places, and the error
// kind is how bb tells them which: try again (transient), do not bother
// (permanent), it was stopped (cancelled), or look before trying again,
// because the server may already have done it (unknown_outcome).
//
// Telling those apart takes the method, the error, and whether the request
// reached the connection at all. Both transports -- httpclient and the
// generated OpenAPI client -- decide it here. The classification used to live
// in httpclient alone, so every command on the generated client reported a
// rejected certificate and a lost POST alike as "retry later" (#574).
package outcome

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/retrypolicy"
)

// Exchange is one request on its way to the server.
type Exchange struct {
	method string
	wrote  atomic.Bool
}

type exchangeKey struct{}

// Track returns the request with a trace that records whether its headers were
// written to the connection, and the Exchange that holds the answer.
//
// Written headers are the line between "the server cannot have acted on this"
// and "it may have". The error Go hands back does not say whether anything was
// sent, which is why the retry policy refuses to replay a POST at all. The
// trace does: until WroteHeaders fires the request is not complete on the
// wire, and a server does not act on half a request.
//
// The Exchange travels in the request's context, so a transport further down
// the same request -- a retrying RoundTripper -- finds it with Of.
func Track(request *http.Request) (*http.Request, *Exchange) {
	exchange := &Exchange{method: methodOf(request)}
	trace := &httptrace.ClientTrace{WroteHeaders: func() { exchange.wrote.Store(true) }}

	ctx := httptrace.WithClientTrace(request.Context(), trace)
	ctx = context.WithValue(ctx, exchangeKey{}, exchange)

	return request.WithContext(ctx), exchange
}

// Of returns the Exchange Track attached to request.
//
// A request nobody tracked gets one that knows its method and nothing about
// the wire. That reads as "not written", so its failures classify as they did
// before the trace existed rather than as an outcome nobody observed.
func Of(request *http.Request) *Exchange {
	if exchange, ok := request.Context().Value(exchangeKey{}).(*Exchange); ok {
		return exchange
	}

	return &Exchange{method: methodOf(request)}
}

// Classify turns a transport error into the kind that tells the caller what to
// do next.
//
// An error something already classified comes back as it is, so a transport
// and the layer above it can both ask without the second answer overwriting
// the first.
func (exchange *Exchange) Classify(err error) error {
	if err == nil || classified(err) {
		return err
	}

	switch {
	case certificateRejected(err):
		return apperrors.New(apperrors.KindPermanent,
			"the server's TLS certificate was rejected, which retrying will not change", err)
	case hostUnresolvable(err):
		return apperrors.New(apperrors.KindPermanent,
			"the host does not resolve, which retrying will not change", err)
	case exchange.mayHaveApplied():
		return exchange.unknown(err)
	case errors.Is(err, context.Canceled):
		return apperrors.New(apperrors.KindCancelled, "the request was interrupted", err)
	default:
		return apperrors.Transport("request failed", err)
	}
}

// ClassifyRead classifies a failure reading the body of a response.
//
// A response arrived, so the request reached the server whatever the trace
// saw. For a method that is not replayed that is the lost-answer case exactly:
// the server may have applied it, and the caller is holding nothing that says
// so either way.
func (exchange *Exchange) ClassifyRead(err error) error {
	if err == nil || errors.Is(err, io.EOF) || classified(err) {
		return err
	}

	exchange.wrote.Store(true)

	switch {
	case exchange.mayHaveApplied():
		return exchange.unknown(err)
	case errors.Is(err, context.Canceled):
		return apperrors.New(apperrors.KindCancelled, "reading the response was interrupted", err)
	default:
		return apperrors.Transport("failed to read the response", err)
	}
}

// Status reports a gateway answer that leaves a mutation's outcome unknown, and
// nil for every other status.
//
// A 502 or a 504 comes from something standing between bb and Bitbucket, which
// may have passed the request on and given up waiting for the reply. For a
// method the retry policy will not replay, reporting that as transient invites
// exactly the replay the policy refused. mapped is what the status would
// otherwise have been reported as, kept as the cause so its message survives.
func (exchange *Exchange) Status(status int, mapped error) error {
	if status != http.StatusBadGateway && status != http.StatusGatewayTimeout {
		return nil
	}
	if retrypolicy.Replayable(exchange.method) {
		return nil
	}

	return apperrors.New(apperrors.KindUnknownOutcome, fmt.Sprintf(
		"a gateway answered the %s with %d, so whether Bitbucket applied it is unknown: check before sending it again",
		exchange.method, status), mapped)
}

// AnswerFailed reports a request Bitbucket answered with an error it raised
// while writing the answer, which may come after it applied the request.
//
// For a method the retry policy will not replay that is unknown_outcome, as a
// gateway's 502 is. A request it would replay lost nothing, and gets nil.
func (exchange *Exchange) AnswerFailed(mapped error) error {
	if retrypolicy.Replayable(exchange.method) {
		return nil
	}

	return apperrors.New(apperrors.KindUnknownOutcome, fmt.Sprintf(
		"Bitbucket failed while answering the %s, possibly after applying it, so whether it did is unknown: check before sending it again",
		exchange.method), mapped)
}

// Body wraps a response body so a failure reading it is classified like any
// other failure of the exchange.
func (exchange *Exchange) Body(body io.ReadCloser) io.ReadCloser {
	if body == nil {
		return nil
	}

	return &classifyingBody{ReadCloser: body, exchange: exchange}
}

// Retriable reports whether a classified failure is one a replay could fix.
//
// Only transient is. Permanent will not change, cancelled was stopped on
// purpose, and unknown_outcome may already have happened. The method still
// has to be replayable on top of this; that half is retrypolicy's.
func Retriable(err error) bool {
	return apperrors.KindOf(err) == apperrors.KindTransient
}

func (exchange *Exchange) mayHaveApplied() bool {
	return exchange.wrote.Load() && !retrypolicy.Replayable(exchange.method)
}

func (exchange *Exchange) unknown(err error) error {
	how := "no answer came back"
	switch {
	case errors.Is(err, context.Canceled):
		how = "it was interrupted before the answer came back"
	case timedOut(err):
		how = "it timed out before the answer came back"
	}

	return apperrors.New(apperrors.KindUnknownOutcome, fmt.Sprintf(
		"the %s reached the server and %s, so whether it was applied is unknown: check before sending it again",
		exchange.method, how), err)
}

type classifyingBody struct {
	io.ReadCloser
	exchange *Exchange
}

func (body *classifyingBody) Read(buffer []byte) (int, error) {
	count, err := body.ReadCloser.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		err = body.exchange.ClassifyRead(err)
	}

	return count, err
}

func methodOf(request *http.Request) string {
	method := strings.ToUpper(strings.TrimSpace(request.Method))
	if method == "" {
		return http.MethodGet
	}

	return method
}

func classified(err error) bool {
	var appError *apperrors.AppError

	return errors.As(err, &appError)
}

func certificateRejected(err error) bool {
	var verification *tls.CertificateVerificationError
	var unknownAuthority x509.UnknownAuthorityError
	var invalid x509.CertificateInvalidError
	var hostname x509.HostnameError

	return errors.As(err, &verification) ||
		errors.As(err, &unknownAuthority) ||
		errors.As(err, &invalid) ||
		errors.As(err, &hostname)
}

func hostUnresolvable(err error) bool {
	var dnsError *net.DNSError

	return errors.As(err, &dnsError) && dnsError.IsNotFound
}

func timedOut(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	var netError net.Error

	return errors.As(err, &netError) && netError.Timeout()
}
