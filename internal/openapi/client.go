package openapi

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/network"
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
		openapigenerated.WithHTTPClient(httpClient),
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
			if request.GetBody == nil && request.Body != nil {
				break
			}

			clone := request.Clone(request.Context())
			if request.GetBody != nil {
				body, err := request.GetBody()
				if err != nil {
					break
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
			if attempt < transport.retries {
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

		if response.StatusCode == http.StatusTooManyRequests || response.StatusCode >= 500 {
			lastResponse = response
			retryDelay := retryDelayFromResponse(response.Header, attempt, transport.baseBackoff)
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

func retryDelayFromResponse(headers http.Header, attempt int, fallbackBase time.Duration) time.Duration {
	if fallbackBase <= 0 {
		fallbackBase = 250 * time.Millisecond
	}

	if headers != nil {
		retryAfter := strings.TrimSpace(headers.Get("Retry-After"))
		if retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil {
				if seconds < 0 {
					seconds = 0
				}
				return time.Duration(seconds) * time.Second
			}

			if retryAt, err := http.ParseTime(retryAfter); err == nil {
				delay := time.Until(retryAt)
				if delay < 0 {
					return 0
				}
				return delay
			}
		}
	}

	return time.Duration(attempt+1) * fallbackBase
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
