package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// watchdog bounds each wait on the server by a timeout, in place of
// http.Client's deadline for the whole exchange.
//
// A request fails when its response headers have not arrived within the
// timeout, which covers the dial, the TLS handshake and the server's answer, as
// a transport's own dial, handshake and response-header timeouts would. A body
// fails when a read has waited that long for its next bytes. Those three
// timeouts cannot simply be set on the caller's transport: bb update's arrives
// wrapped in its scheme guard, and a transport shared with other requests is
// not this package's to reconfigure. Wrapping it instead keeps the guard seeing
// every request, and runs once for every hop of a redirect, as the transport's
// own timeouts would.
type watchdog struct {
	base    http.RoundTripper
	timeout time.Duration
}

func (watchdog *watchdog) RoundTrip(request *http.Request) (*http.Response, error) {
	if watchdog.timeout <= 0 {
		return watchdog.base.RoundTrip(request)
	}

	ctx, cancel := context.WithCancelCause(request.Context())
	unanswered := transient(fmt.Sprintf("no response within %s", watchdog.timeout))
	timer := time.AfterFunc(watchdog.timeout, func() { cancel(unanswered) })

	response, err := watchdog.base.RoundTrip(request.WithContext(ctx))
	timer.Stop()

	if err == nil && errors.Is(context.Cause(ctx), unanswered) {
		// The timeout fired as the headers arrived, and the body is already
		// cut off with the request.
		_ = response.Body.Close()
		err = unanswered
	}
	if err != nil {
		cancel(nil)
		// The transport reports the cancellation in its own words, or in
		// ours: Go passes the cause on, a transport underneath may not. Either
		// way the verdict is the watchdog's, unless the caller cancelled too.
		if errors.Is(context.Cause(ctx), unanswered) && request.Context().Err() == nil {
			return nil, unanswered
		}

		return nil, err
	}

	body := response.Body
	if body == nil {
		body = http.NoBody
	}
	response.Body = &watchedBody{
		body:    body,
		ctx:     ctx,
		outer:   request.Context(),
		cancel:  cancel,
		timeout: watchdog.timeout,
		stalled: transient(fmt.Sprintf("no data arrived for %s", watchdog.timeout)),
	}

	return response, nil
}

// watchedBody fails a read that has waited timeout for data.
//
// The clock runs only inside Read. Time spent between reads is the caller
// writing what it has -- to a pipe a slow reader drains, perhaps -- and that is
// not the server stalling.
type watchedBody struct {
	body    io.ReadCloser
	ctx     context.Context
	outer   context.Context
	cancel  context.CancelCauseFunc
	timeout time.Duration
	stalled error
	timer   *time.Timer
}

func (body *watchedBody) Read(buffer []byte) (int, error) {
	if body.timer == nil {
		body.timer = time.AfterFunc(body.timeout, func() { body.cancel(body.stalled) })
	} else {
		body.timer.Reset(body.timeout)
	}

	count, err := body.body.Read(buffer)
	body.timer.Stop()

	if err != nil && !errors.Is(err, io.EOF) && errors.Is(context.Cause(body.ctx), body.stalled) && body.outer.Err() == nil {
		return count, body.stalled
	}

	return count, err
}

func (body *watchedBody) Close() error {
	if body.timer != nil {
		body.timer.Stop()
	}
	err := body.body.Close()
	body.cancel(nil)

	return err
}
