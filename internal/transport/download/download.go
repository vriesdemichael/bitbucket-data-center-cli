// Package download fetches a response body that may be large or slow: a
// release archive, a repository archive, the bytes of a file.
//
// Every other request bb makes is held to request_timeout from connecting to
// the last byte of the answer. That suits an API call and not a download, whose
// length is set by its size: twenty seconds needs 4.4 Mbit/s for a 10.5 MB
// release, and a large repository could not be archived at all. Here the same
// timeout bounds each wait instead -- for the response headers, and for the
// next bytes of the body -- so a download fails when the server stops sending,
// not when it is merely slow. The caller's context still ends it.
//
// Without a deadline, something else has to bound what a server can make bb
// read, so each call names a cap. A body that breaks off is resumed with a
// Range request where the server offers one, started again where what was
// written can be taken back, and reported where it cannot. ADR-093 has the
// reasoning.
package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/outcome"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/retrypolicy"
)

// Options configures a Downloader.
type Options struct {
	// Timeout bounds every wait on the server: for a response's headers, and
	// for each read of its body. A download fails when the server has sent
	// nothing for this long, however long it has been running. Zero or less
	// waits indefinitely, as http.Client does.
	Timeout time.Duration
	// Retries is how many more attempts a transient failure gets.
	Retries int
	// Backoff is the wait before a retry, multiplied by the retry's number. A
	// Retry-After header wins over it.
	Backoff time.Duration
	// Logger reports each attempt, as the API clients do. Nil reports nothing.
	Logger *diagnostics.Logger
}

// Downloader fetches bodies with GET.
type Downloader struct {
	client  *http.Client
	options Options
}

// New returns a Downloader that sends through client's transport, so whatever
// guards that transport -- the scheme check on bb update's -- still sees every
// request and every redirect. client's Timeout is not used: it is a deadline for
// the whole exchange, which is what a download must not have. A nil client sends
// through http.DefaultTransport.
func New(client *http.Client, options Options) *Downloader {
	var configured http.Client
	if client != nil {
		configured = *client
	}
	configured.Timeout = 0

	base := configured.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	configured.Transport = &watchdog{base: base, timeout: options.Timeout}

	return &Downloader{client: &configured, options: options}
}

// Request is one download.
type Request struct {
	// URL is fetched with GET, following redirects as the client does.
	URL string
	// Header is sent with every attempt. A Range header here asks for that
	// range, and turns off resuming, which would ask for another.
	Header http.Header
	// Limit is the most bytes the body may have. A Content-Length over it fails
	// before anything is read, and a longer body fails as it passes it; neither
	// is retried, because the same request brings the same body. Zero or less
	// is no limit, for a destination that holds nothing in memory.
	Limit int64
}

// Result is what a download did.
type Result struct {
	// Bytes is the size of the body the destination holds.
	Bytes int64
	// Attempts is how many requests it took.
	Attempts int
}

// Get downloads request into destination.
//
// A failure comes back classified as the API clients classify theirs, through
// internal/transport/outcome: a rejected certificate is permanent, a stall or a
// dropped connection transient, an interrupt cancelled. An answer outside 2xx
// comes back as a *StatusError, for the caller to map, because what a status
// means depends on who was asked.
func (downloader *Downloader) Get(ctx context.Context, request Request, destination Destination) (Result, error) {
	if strings.TrimSpace(request.URL) == "" {
		return Result{}, apperrors.New(apperrors.KindValidation, "a download needs a URL", nil)
	}

	transfer := &transfer{downloader: downloader, request: request, destination: destination, total: -1}

	return transfer.run(ctx)
}

// transfer is one Get in progress.
type transfer struct {
	downloader  *Downloader
	request     Request
	destination Destination

	// received is how many bytes the destination holds, and so the offset a
	// resumed request asks for.
	received int64
	// total is the whole body's length, or -1 while it is not known.
	total int64
	// validator is what an If-Range names -- a strong ETag, or a Last-Modified
	// date -- so a resumed request is answered from the same body or not at all.
	validator string
	// resumable is whether the server offers byte ranges, with a validator to
	// hold them to.
	resumable bool
	attempts  int
}

// failure is an attempt that did not complete.
type failure struct {
	err error
	// retriable says another attempt could succeed: the failure is transient,
	// or a status the retry policy replays.
	retriable bool
	// delay is how long to wait before that attempt.
	delay time.Duration
	// explained is an error that already says how far the download got.
	explained bool
}

func (transfer *transfer) run(ctx context.Context) (Result, error) {
	options := transfer.downloader.options

	for {
		started := time.Now()
		failed := transfer.attempt(ctx)
		if failed == nil {
			transfer.log(options.Logger.Debug, "download completed", started, nil)

			return transfer.result(), nil
		}

		retry := failed.retriable && transfer.attempts <= options.Retries && ctx.Err() == nil
		if retry && transfer.received > 0 && !transfer.resumable {
			// The next attempt starts from the first byte, which means taking
			// back what was written. Standard output cannot give it back.
			if !transfer.destination.Rewind() {
				transfer.log(options.Logger.Error, "download failed", started, failed.err)

				return transfer.result(), transfer.cannotStartAgain(failed.err)
			}
			transfer.received = 0
		}

		if !retry {
			transfer.log(options.Logger.Error, "download failed", started, failed.err)
			if failed.explained {
				return transfer.result(), failed.err
			}

			return transfer.result(), transfer.explain(failed.err)
		}

		transfer.log(options.Logger.Warn, "download failed, retrying", started, failed.err)
		if err := wait(ctx, failed.delay); err != nil {
			return transfer.result(), apperrors.Transport("the download was interrupted while waiting to retry", err)
		}
	}
}

func (transfer *transfer) result() Result {
	return Result{Bytes: transfer.received, Attempts: transfer.attempts}
}

// attempt sends one request and writes what it brings.
func (transfer *transfer) attempt(ctx context.Context) *failure {
	transfer.attempts++
	retry := transfer.attempts - 1
	resuming := transfer.received > 0

	request, err := transfer.newRequest(ctx, resuming)
	if err != nil {
		return &failure{err: apperrors.New(apperrors.KindValidation, fmt.Sprintf("%q is not a URL a download can be made from", transfer.request.URL), err)}
	}

	tracked, exchange := outcome.Track(request)
	// The URL is the caller's: the configured Bitbucket host, or the release
	// mirror a manifest resolved to under bb update's scheme guard.
	response, err := transfer.downloader.client.Do(tracked) //nolint:gosec // G704: the destination is the caller's configured host
	if err != nil {
		classified := exchange.Classify(err)

		return &failure{err: classified, retriable: outcome.Retriable(classified), delay: transfer.backoff(retry)}
	}
	defer func() { _ = response.Body.Close() }()

	// The status is the outcome, so a body that fails to read after it is not
	// an unknown one.
	exchange.Answered(response.StatusCode)

	if response.StatusCode < 200 || response.StatusCode > 299 {
		return transfer.refused(response, retry, resuming)
	}
	if failed := transfer.accept(response, resuming); failed != nil {
		return failed
	}

	return transfer.copy(response.Body, exchange, retry)
}

func (transfer *transfer) newRequest(ctx context.Context, resuming bool) (*http.Request, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, transfer.request.URL, nil)
	if err != nil {
		return nil, err
	}

	for name, values := range transfer.request.Header {
		for _, value := range values {
			request.Header.Add(name, value)
		}
	}

	// The body as stored, unless the caller asked otherwise. A body the
	// transport decompresses on the fly is counted in bytes no Range can
	// address -- a range selects bytes of the compressed representation -- so
	// one that broke off could only ever start again.
	if request.Header.Get("Accept-Encoding") == "" {
		request.Header.Set("Accept-Encoding", "identity")
	}

	if resuming {
		request.Header.Set("Range", fmt.Sprintf("bytes=%d-", transfer.received))
		request.Header.Set("If-Range", transfer.validator)
	}

	return request, nil
}

// refused handles an answer outside 2xx.
func (transfer *transfer) refused(response *http.Response, retry int, resuming bool) *failure {
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxStatusBody))
	status := &StatusError{StatusCode: response.StatusCode, Header: response.Header, Body: body}

	if resuming && response.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		// The server has nothing past what was received, under the validator
		// it gave: the body changed. Only a fresh start can finish it.
		transfer.resumable = false

		return &failure{
			err: transient(fmt.Sprintf(
				"the server could not send the rest of the body from byte %d, so it has changed since the download began", transfer.received)),
			retriable: true,
		}
	}

	return &failure{
		err:       status,
		retriable: retrypolicy.RetriableStatus(http.MethodGet, response.StatusCode),
		delay:     retrypolicy.Delay(response.Header, retry, transfer.downloader.options.Backoff),
	}
}

// accept decides what a 2xx answer continues, before any of its body is read.
func (transfer *transfer) accept(response *http.Response, resuming bool) *failure {
	partial := response.StatusCode == http.StatusPartialContent

	switch {
	case resuming && partial:
		if err := transfer.checkRange(response.Header.Get("Content-Range")); err != nil {
			// A range that does not continue the body is no use, and neither
			// is asking for another: the next attempt starts from the top.
			transfer.resumable = false

			var limit *LimitError
			if errors.As(err, &limit) {
				return &failure{err: err}
			}

			return &failure{err: err, retriable: true}
		}

		return nil
	case resuming:
		// The whole body, in answer to a range: it changed under its
		// validator, or the server stopped honouring ranges. It is still the
		// whole body, so it serves -- once what was written is taken back.
		if !transfer.destination.Rewind() {
			return &failure{explained: true, err: transfer.cannotStartAgain(transient(
				"the server sent the whole body again rather than the rest of it"))}
		}
		transfer.received = 0
	case partial && !transfer.callerRange():
		return &failure{err: apperrors.New(apperrors.KindPermanent,
			"the server answered with part of the body, which was not asked for", nil)}
	}

	transfer.learn(response)

	if limit := transfer.request.Limit; limit > 0 && transfer.total > limit {
		return &failure{err: overLimit(limit, transfer.total)}
	}

	if opener, ok := transfer.destination.(Opener); ok {
		if err := opener.Open(response.Header); err != nil {
			return &failure{err: err}
		}
	}

	return nil
}

// learn records what a body from its first byte says about itself.
func (transfer *transfer) learn(response *http.Response) {
	transfer.total = response.ContentLength
	transfer.validator = validatorOf(response.Header)
	transfer.resumable = !transfer.callerRange() && transfer.validator != "" && acceptsByteRanges(response.Header)
}

// callerRange reports a request that asked for a range of its own.
func (transfer *transfer) callerRange() bool {
	return transfer.request.Header.Get("Range") != ""
}

// checkRange holds a resumed answer to the body it continues: it starts where
// the body broke off, belongs to a body of the same length, and runs to its end.
func (transfer *transfer) checkRange(contentRange string) error {
	start, end, total, ok := parseContentRange(contentRange)

	switch {
	case !ok:
		return transient(fmt.Sprintf(
			"the server answered the resumed request with the range %q, which cannot be read", contentRange))
	case start != transfer.received:
		return transient(fmt.Sprintf(
			"the server resumed at byte %d rather than at byte %d, where the body broke off", start, transfer.received))
	case transfer.total >= 0 && total >= 0 && total != transfer.total:
		return transient(fmt.Sprintf(
			"the body is %d bytes now and was %d when the download began", total, transfer.total))
	case total >= 0 && end != total-1:
		return transient(fmt.Sprintf(
			"the server sent bytes %d-%d of %d rather than the rest of the body", start, end, total))
	case transfer.request.Limit > 0 && total > transfer.request.Limit:
		return overLimit(transfer.request.Limit, total)
	}

	if transfer.total < 0 {
		transfer.total = total
	}

	return nil
}

// copy writes a body to the destination, holding it to the request's limit.
func (transfer *transfer) copy(body io.Reader, exchange *outcome.Exchange, retry int) *failure {
	limit := transfer.request.Limit
	buffer := make([]byte, copyBuffer)

	for {
		count, readErr := body.Read(buffer)
		if count > 0 {
			if limit > 0 && transfer.received+int64(count) > limit {
				return &failure{err: overLimit(limit, -1)}
			}

			written, writeErr := transfer.destination.Write(buffer[:count])
			transfer.received += int64(written)
			if writeErr == nil && written < count {
				writeErr = io.ErrShortWrite
			}
			if writeErr != nil {
				return &failure{err: writeFailed(writeErr)}
			}
		}

		if errors.Is(readErr, io.EOF) {
			return transfer.complete(retry)
		}
		if readErr != nil {
			failed := bodyFailed(readErr, exchange)

			return &failure{err: failed, retriable: outcome.Retriable(failed), delay: transfer.backoff(retry)}
		}
	}
}

// bodyFailed reports a body that stopped arriving: classified by outcome, as
// every failed read is, and described here, because outcome's words name
// Bitbucket and a download reads release mirrors too.
func bodyFailed(readErr error, exchange *outcome.Exchange) error {
	classified := exchange.ClassifyRead(readErr)

	// The watchdog's verdict already says what happened.
	var described *apperrors.AppError
	if errors.As(readErr, &described) {
		return classified
	}

	kind := apperrors.KindOf(classified)
	if kind == apperrors.KindCancelled {
		return apperrors.New(kind, "the download was interrupted", readErr)
	}

	return apperrors.New(kind, "the connection broke off", readErr)
}

// complete checks a body that ended against the length it declared. The
// transport already does for one answer with a Content-Length; a body sent
// without one, or a range that ended early, is only caught here.
func (transfer *transfer) complete(retry int) *failure {
	if transfer.total < 0 || transfer.received == transfer.total || transfer.callerRange() {
		return nil
	}

	return &failure{
		err:       transient(fmt.Sprintf("the body ended at byte %d of %d", transfer.received, transfer.total)),
		retriable: true,
		delay:     transfer.backoff(retry),
	}
}

func (transfer *transfer) backoff(retry int) time.Duration {
	return retrypolicy.Delay(nil, retry, transfer.downloader.options.Backoff)
}

// explain adds what the download had done to the failure that ended it, when
// there is anything to add. A status goes back untouched, for the caller to
// map.
func (transfer *transfer) explain(err error) error {
	var status *StatusError
	if errors.As(err, &status) {
		return err
	}

	var progress []string
	if transfer.received > 0 {
		progress = append(progress, "after "+transfer.progress())
	}
	if transfer.attempts > 1 {
		progress = append(progress, fmt.Sprintf("in %d attempts", transfer.attempts))
	}
	if len(progress) == 0 {
		return err
	}

	return apperrors.Transport("the download failed "+strings.Join(progress, ", "), err)
}

// cannotStartAgain reports a download that broke off where the only way to
// finish it is from the first byte, and what it wrote cannot be taken back.
//
// It keeps the kind of the failure that ended it, which a retry would have
// fixed: the command as a whole can be run again. What cannot happen is bb
// doing so on its own, which would write the start of the body twice.
func (transfer *transfer) cannotStartAgain(cause error) error {
	return apperrors.Transport(fmt.Sprintf(
		"the download broke off after %s had been written, and the server offers no range to resume it from, so it can only start again from the first byte, which would write those bytes twice",
		transfer.progress()), cause)
}

// transient is a failure the downloader decides for itself. Nothing
// classified it, so there is no cause whose kind to keep.
func transient(message string) error {
	return apperrors.New(apperrors.KindTransient, message, nil)
}

// progress says how much of the body has arrived.
func (transfer *transfer) progress() string {
	if transfer.total > 0 && !transfer.callerRange() {
		return fmt.Sprintf("%s of %s", size(transfer.received), size(transfer.total))
	}

	return size(transfer.received)
}

// log reports an attempt at the given level. The fields are the API clients'.
func (transfer *transfer) log(at func(string, map[string]any), message string, started time.Time, err error) {
	endpoint := transfer.request.URL
	if parsed, parseErr := url.Parse(endpoint); parseErr == nil {
		endpoint = parsed.Path
	}

	fields := map[string]any{
		"method":      http.MethodGet,
		"endpoint":    endpoint,
		"attempt":     transfer.attempts,
		"retry_count": transfer.downloader.options.Retries,
		"bytes":       transfer.received,
		"duration_ms": time.Since(started).Milliseconds(),
	}
	if err != nil {
		fields["error"] = err.Error()
	}

	at(message, fields)
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// validatorOf returns what an If-Range can hold a resumed request to: a strong
// ETag, which names exactly one body, or failing that a Last-Modified date. A
// weak ETag may not be used in an If-Range at all.
func validatorOf(header http.Header) string {
	if etag := strings.TrimSpace(header.Get("ETag")); etag != "" && !strings.HasPrefix(etag, "W/") {
		return etag
	}

	if modified := strings.TrimSpace(header.Get("Last-Modified")); modified != "" {
		if _, err := http.ParseTime(modified); err == nil {
			return modified
		}
	}

	return ""
}

// acceptsByteRanges reports a server that said it serves byte ranges.
func acceptsByteRanges(header http.Header) bool {
	for _, value := range header.Values("Accept-Ranges") {
		for _, unit := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(unit), "bytes") {
				return true
			}
		}
	}

	return false
}

// parseContentRange reads "bytes start-end/total", where total may be "*".
func parseContentRange(value string) (start, end, total int64, ok bool) {
	unit, rest, found := strings.Cut(strings.TrimSpace(value), " ")
	if !found || !strings.EqualFold(unit, "bytes") {
		return 0, 0, 0, false
	}

	span, length, found := strings.Cut(rest, "/")
	if !found {
		return 0, 0, 0, false
	}

	first, last, found := strings.Cut(span, "-")
	if !found {
		return 0, 0, 0, false
	}

	start, startErr := strconv.ParseInt(strings.TrimSpace(first), 10, 64)
	end, endErr := strconv.ParseInt(strings.TrimSpace(last), 10, 64)
	if startErr != nil || endErr != nil || start < 0 || end < start {
		return 0, 0, 0, false
	}

	total = -1
	if length = strings.TrimSpace(length); length != "*" {
		parsed, err := strconv.ParseInt(length, 10, 64)
		if err != nil || parsed <= end {
			return 0, 0, 0, false
		}
		total = parsed
	}

	return start, end, total, true
}

const (
	// copyBuffer is the most read from the body at a time.
	copyBuffer = 32 << 10
	// maxStatusBody is how much of an answer outside 2xx is kept for the
	// caller to read an error document from.
	maxStatusBody = 64 << 10
)
