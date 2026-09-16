package openapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/network"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/outcome"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/retrypolicy"
)

func NewClientWithResponsesFromConfig(cfg config.AppConfig) (*openapigenerated.ClientWithResponses, error) {
	serverURL := strings.TrimRight(cfg.BitbucketURL, "/") + "/rest"

	transport, err := network.NewSafeTransport(network.TLSOptions{
		CAFile:             cfg.CAFile,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		ClientCertFile:     cfg.ClientCertFile,
		ClientKeyFile:      cfg.ClientKeyFile,
	})
	if err != nil {
		return nil, err
	}

	httpClient := &http.Client{
		Timeout: cfg.RequestTimeout,
		Transport: &retryTransport{
			base:        transport,
			retries:     cfg.RetryCount,
			baseBackoff: cfg.RetryBackoff,
			logger: diagnostics.NewLogger(diagnostics.Config{
				Level:  diagnostics.Level(cfg.LogLevel),
				Format: diagnostics.Format(cfg.LogFormat),
			}, diagnostics.EnabledWriter(cfg.DiagnosticsEnabled, diagnostics.OutputWriter())),
		},
	}

	return openapigenerated.NewClientWithResponses(
		serverURL,
		openapigenerated.WithHTTPClient(classifyingDoer{client: httpClient}),
		openapigenerated.WithRequestEditorFn(func(_ context.Context, request *http.Request) error {
			if cfg.BitbucketToken != "" {
				request.Header.Set("Authorization", "Bearer "+cfg.BitbucketToken)
				return nil
			}
			if cfg.BitbucketUsername != "" && cfg.BitbucketPassword != "" {
				request.SetBasicAuth(cfg.BitbucketUsername, cfg.BitbucketPassword)
			}
			return nil
		}),
	)
}

// classifyingDoer classifies every exchange the generated client makes, so a
// command on it reports what httpclient's do: a rejected certificate as
// permanent, a lost mutation as unknown_outcome (#574).
//
// It wraps the http.Client rather than living in retryTransport because the
// client stands between the two. When its Timeout fires it replaces whatever
// the transport returned with an error of its own, so a classification made
// inside the transport would not reach the service that reports it.
type classifyingDoer struct {
	client *http.Client
}

func (doer classifyingDoer) Do(request *http.Request) (*http.Response, error) {
	tracked, exchange := outcome.Track(request)

	// The request is the generated client's own, built from the configured
	// Bitbucket URL and a path from the specification; this adds a trace and
	// classifies what comes back, and never chooses where it goes.
	response, err := doer.client.Do(tracked) //nolint:gosec // G704: the destination is the configured Bitbucket host, not caller input
	if err != nil {
		return nil, exchange.Classify(err)
	}

	// The status is the outcome, so a body that fails to read after it is not
	// an unknown one.
	exchange.Answered(response.StatusCode)

	// Answered here rather than by the service that reads the status: the
	// service has the status and not the method, and a gateway's 502 or 504
	// means something different for a request that is never replayed.
	if exchange.Status(response.StatusCode, nil) != nil {
		body, _ := io.ReadAll(response.Body)
		_ = response.Body.Close()

		return nil, exchange.Status(response.StatusCode, MapStatusError(response.StatusCode, body))
	}

	// A 400 is read here too. The one Bitbucket sends when writing its answer
	// failed leaves a mutation's outcome unknown, and only this layer has the
	// method. Any other 400 goes on to the service with its body as it came.
	if response.StatusCode == http.StatusBadRequest {
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			return nil, exchange.ClassifyRead(err)
		}
		if FailedWritingAnswer(response.StatusCode, body) {
			if unknown := exchange.AnswerFailed(MapStatusError(response.StatusCode, body)); unknown != nil {
				return nil, unknown
			}
		}
		response.Body = io.NopCloser(bytes.NewReader(body))

		return response, nil
	}

	response.Body = exchange.Body(response.Body)

	return response, nil
}

// errBodyNotReplayable is returned rather than a response whose body has
// already been consumed, which is what breaking out of the retry loop used to
// produce.
var errBodyNotReplayable = errors.New("request body cannot be replayed for a retry")

type retryTransport struct {
	base        http.RoundTripper
	retries     int
	baseBackoff time.Duration
	logger      *diagnostics.Logger
}

func (transport *retryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	base := transport.base
	if base == nil {
		base = http.DefaultTransport
	}

	var lastResponse *http.Response
	var lastError error

	for attempt := 0; attempt <= transport.retries; attempt++ {
		started := time.Now()
		activeRequest := request
		if attempt > 0 {
			// A body that cannot be rewound cannot be replayed. Breaking here
			// used to fall through to the return below and hand back
			// lastResponse, whose body the retriable-status branch had already
			// drained and closed -- a caller would read zero bytes and see no
			// error. Not reachable while every call site passes a
			// bytes.Reader, which populates GetBody, but it is the trap the
			// first streaming upload would fall into.
			if request.GetBody == nil && request.Body != nil {
				return nil, errBodyNotReplayable
			}

			clone := request.Clone(request.Context())
			if request.GetBody != nil {
				body, err := request.GetBody()
				if err != nil {
					return nil, err
				}
				clone.Body = body
			}
			activeRequest = clone
		}

		response, err := base.RoundTrip(activeRequest)
		if err != nil {
			lastError = err
			fields := map[string]any{
				"method":      request.Method,
				"endpoint":    request.URL.Path,
				"attempt":     attempt + 1,
				"retry_count": transport.retries,
				"duration_ms": time.Since(started).Milliseconds(),
				"error":       err.Error(),
			}
			// Classified only to decide: the doer above classifies what the caller
			// sees. A certificate the server will keep presenting is not retried.
			if attempt < transport.retries && retrypolicy.Replayable(request.Method) &&
				outcome.Retriable(outcome.Of(request).Classify(err)) {
				transport.logger.Warn("http request failed", fields)
				if sleepErr := sleepWithContext(request.Context(), time.Duration(attempt+1)*transport.baseBackoff); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			transport.logger.Error("http request failed", fields)
			return nil, err
		}

		transport.logger.Debug("http request completed", map[string]any{
			"method":      request.Method,
			"endpoint":    request.URL.Path,
			"status":      response.StatusCode,
			"attempt":     attempt + 1,
			"retry_count": transport.retries,
			"duration_ms": time.Since(started).Milliseconds(),
		})

		if retrypolicy.RetriableStatus(request.Method, response.StatusCode) {
			lastResponse = response
			retryDelay := retrypolicy.Delay(response.Header, attempt, transport.baseBackoff)
			fields := map[string]any{
				"method":      request.Method,
				"endpoint":    request.URL.Path,
				"status":      response.StatusCode,
				"attempt":     attempt + 1,
				"retry_count": transport.retries,
				"duration_ms": time.Since(started).Milliseconds(),
				"retry_delay": retryDelay.String(),
			}
			if attempt < transport.retries {
				transport.logger.Warn("http retriable response", fields)
				_, _ = io.Copy(io.Discard, response.Body)
				_ = response.Body.Close()
				if sleepErr := sleepWithContext(request.Context(), retryDelay); sleepErr != nil {
					return nil, sleepErr
				}
				continue
			}
			transport.logger.Error("http retriable response", fields)
		}

		return response, nil
	}

	if lastResponse != nil {
		return lastResponse, nil
	}

	return nil, lastError
}

func sleepWithContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
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
