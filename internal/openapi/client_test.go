package openapi

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/diagnostics"
	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
)

type retryRoundTripperFunc func(*http.Request) (*http.Response, error)

func (function retryRoundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func TestRetryTransport(t *testing.T) {
	t.Run("retries transient status", func(t *testing.T) {
		var attempts atomic.Int32
		transport := &retryTransport{
			base: retryRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				attempt := attempts.Add(1)
				status := http.StatusServiceUnavailable
				if attempt >= 3 {
					status = http.StatusOK
				}
				return &http.Response{
					StatusCode: status,
					Body:       io.NopCloser(strings.NewReader("{}")),
					Header:     make(http.Header),
				}, nil
			}),
			retries:     2,
			baseBackoff: time.Nanosecond,
		}

		request, err := http.NewRequest(http.MethodGet, "http://example.test", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("expected final 200 response, got %d", response.StatusCode)
		}
		if attempts.Load() != 3 {
			t.Fatalf("expected 3 attempts, got %d", attempts.Load())
		}
	})

	t.Run("retries transport error", func(t *testing.T) {
		var attempts atomic.Int32
		transport := &retryTransport{
			base: retryRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				attempt := attempts.Add(1)
				if attempt < 3 {
					return nil, errors.New("temporary failure")
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
			}),
			retries:     2,
			baseBackoff: time.Nanosecond,
		}

		request, err := http.NewRequest(http.MethodGet, "http://example.test", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("expected final 200 response, got %d", response.StatusCode)
		}
		if attempts.Load() != 3 {
			t.Fatalf("expected 3 attempts, got %d", attempts.Load())
		}
	})

	t.Run("returns last response when body cannot be replayed", func(t *testing.T) {
		var attempts atomic.Int32
		transport := &retryTransport{
			base: retryRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				attempts.Add(1)
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body:       io.NopCloser(strings.NewReader("{}")),
					Header:     make(http.Header),
				}, nil
			}),
			retries:     1,
			baseBackoff: time.Nanosecond,
		}

		request, err := http.NewRequest(http.MethodPost, "http://example.test", io.NopCloser(strings.NewReader("payload")))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected 503 response, got %d", response.StatusCode)
		}
		if attempts.Load() != 1 {
			t.Fatalf("expected one attempt due non-replayable body, got %d", attempts.Load())
		}
	})

	t.Run("returns terminal transport error after replay failure", func(t *testing.T) {
		var attempts atomic.Int32
		transport := &retryTransport{
			base: retryRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				attempt := attempts.Add(1)
				if attempt == 1 {
					return nil, errors.New("network down")
				}
				return nil, errors.New("unexpected")
			}),
			retries:     1,
			baseBackoff: time.Nanosecond,
		}

		request, err := http.NewRequest(http.MethodPost, "http://example.test", io.NopCloser(strings.NewReader("payload")))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		_, err = transport.RoundTrip(request)
		if err == nil {
			t.Fatal("expected transport error")
		}
		if !strings.Contains(err.Error(), "network down") {
			t.Fatalf("expected first error to be returned, got: %v", err)
		}
	})

	t.Run("returns last response when request get body fails", func(t *testing.T) {
		transport := &retryTransport{
			base: retryRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusServiceUnavailable,
					Body:       io.NopCloser(strings.NewReader("{}")),
					Header:     make(http.Header),
				}, nil
			}),
			retries:     1,
			baseBackoff: time.Nanosecond,
		}

		request, err := http.NewRequest(http.MethodPost, "http://example.test", strings.NewReader("payload"))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		request.GetBody = func() (io.ReadCloser, error) {
			return nil, errors.New("cannot clone body")
		}

		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("expected last 503 response, got %d", response.StatusCode)
		}
	})

	t.Run("falls back to default transport when base is nil", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)
			_, _ = writer.Write([]byte("{}"))
		}))
		defer server.Close()

		transport := &retryTransport{retries: 0, baseBackoff: time.Nanosecond}
		request, err := http.NewRequest(http.MethodGet, server.URL, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 response, got %d", response.StatusCode)
		}
	})

	t.Run("honors retry-after header over base backoff", func(t *testing.T) {
		var attempts atomic.Int32
		transport := &retryTransport{
			base: retryRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				attempt := attempts.Add(1)
				if attempt == 1 {
					return &http.Response{
						StatusCode: http.StatusTooManyRequests,
						Body:       io.NopCloser(strings.NewReader("rate limited")),
						Header: http.Header{
							"Retry-After": []string{"0"},
						},
					}, nil
				}
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
			}),
			retries:     1,
			baseBackoff: time.Hour,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.test", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("expected retry to complete without waiting an hour, got: %v", err)
		}
		if response.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 response, got %d", response.StatusCode)
		}
		if attempts.Load() != 2 {
			t.Fatalf("expected 2 attempts, got %d", attempts.Load())
		}
	})

	t.Run("returns context error when canceled during status retry wait", func(t *testing.T) {
		transport := &retryTransport{
			base: retryRoundTripperFunc(func(request *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusServiceUnavailable, Body: io.NopCloser(strings.NewReader("{}")), Header: make(http.Header)}, nil
			}),
			retries:     1,
			baseBackoff: time.Hour,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.test", nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}

		_, err = transport.RoundTrip(request)
		if err == nil {
			t.Fatal("expected context cancellation error")
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected deadline exceeded, got %v", err)
		}
	})
}

func TestRetryDelayFromResponse(t *testing.T) {
	t.Run("uses retry-after seconds", func(t *testing.T) {
		delay := retryDelayFromResponse(http.Header{"Retry-After": []string{"3"}}, 0, time.Millisecond)
		if delay != 3*time.Second {
			t.Fatalf("expected 3s delay, got %s", delay)
		}
	})

	t.Run("falls back on invalid retry-after", func(t *testing.T) {
		delay := retryDelayFromResponse(http.Header{"Retry-After": []string{"invalid"}}, 1, 200*time.Millisecond)
		if delay != 400*time.Millisecond {
			t.Fatalf("expected fallback delay 400ms, got %s", delay)
		}
	})

	t.Run("supports retry-after http date", func(t *testing.T) {
		retryAt := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
		delay := retryDelayFromResponse(http.Header{"Retry-After": []string{retryAt}}, 0, time.Millisecond)
		if delay <= 0 || delay > 3*time.Second {
			t.Fatalf("expected positive delay <=3s, got %s", delay)
		}
	})

	t.Run("normalizes negative retry-after seconds", func(t *testing.T) {
		delay := retryDelayFromResponse(http.Header{"Retry-After": []string{"-2"}}, 0, time.Millisecond)
		if delay != 0 {
			t.Fatalf("expected zero delay for negative retry-after, got %s", delay)
		}
	})

	t.Run("falls back when backoff is non-positive", func(t *testing.T) {
		delay := retryDelayFromResponse(nil, 1, 0)
		if delay != 500*time.Millisecond {
			t.Fatalf("expected fallback delay 500ms, got %s", delay)
		}
	})

	t.Run("returns zero for past retry-after date", func(t *testing.T) {
		retryAt := time.Now().Add(-2 * time.Second).UTC().Format(http.TimeFormat)
		delay := retryDelayFromResponse(http.Header{"Retry-After": []string{retryAt}}, 0, time.Millisecond)
		if delay != 0 {
			t.Fatalf("expected zero delay for past date, got %s", delay)
		}
	})
}

func TestSleepWithContext(t *testing.T) {
	t.Run("returns nil for zero delay", func(t *testing.T) {
		if err := sleepWithContext(context.Background(), 0); err != nil {
			t.Fatalf("expected nil error for zero delay, got %v", err)
		}
	})

	t.Run("returns context canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if err := sleepWithContext(ctx, time.Second); err == nil {
			t.Fatal("expected canceled context error")
		}
	})
}

func TestNewClientWithResponsesFromConfigInvalidCA(t *testing.T) {
	_, err := NewClientWithResponsesFromConfig(config.AppConfig{
		BitbucketURL:   "http://localhost:7990",
		CAFile:         "/definitely/missing/ca.pem",
		RequestTimeout: time.Second,
		RetryCount:     1,
		RetryBackoff:   time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected transport initialization error")
	}
}

func TestNewClientWithResponsesFromConfigInvalidClientCert(t *testing.T) {
	_, err := NewClientWithResponsesFromConfig(config.AppConfig{
		BitbucketURL:   "http://localhost:7990",
		ClientCertFile: "/definitely/missing/client.crt",
		ClientKeyFile:  "/definitely/missing/client.key",
		RequestTimeout: time.Second,
		RetryCount:     1,
		RetryBackoff:   time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected transport initialization error")
	}
}

func TestDiagnosticsWriter(t *testing.T) {
	buffer := &bytes.Buffer{}

	if writer := diagnostics.EnabledWriter(true, buffer); writer != buffer {
		t.Fatalf("expected configured writer when enabled, got %T", writer)
	}

	if writer := diagnostics.EnabledWriter(false, buffer); writer != io.Discard {
		t.Fatalf("expected discard writer when disabled, got %T", writer)
	}
}

func TestNewClientWithResponsesFromConfigAuthAndBasePath(t *testing.T) {
	t.Run("uses bearer token auth", func(t *testing.T) {
		var receivedAuth string
		var receivedPath string
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			receivedAuth = request.Header.Get("Authorization")
			receivedPath = request.URL.Path
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"size":0,"limit":1,"isLastPage":true,"values":[]}`))
		}))
		defer server.Close()

		client, err := NewClientWithResponsesFromConfig(config.AppConfig{
			BitbucketURL:   server.URL,
			BitbucketToken: "abc123",
			RequestTimeout: time.Second,
			RetryCount:     0,
			RetryBackoff:   time.Millisecond,
		})
		if err != nil {
			t.Fatalf("new client: %v", err)
		}

		limit := float32(1)
		response, err := client.GetProjectsWithResponse(context.Background(), &openapigenerated.GetProjectsParams{Limit: &limit})
		if err != nil {
			t.Fatalf("get projects: %v", err)
		}
		if response.StatusCode() != http.StatusOK {
			t.Fatalf("expected 200 response, got %d", response.StatusCode())
		}
		if receivedAuth != "Bearer abc123" {
			t.Fatalf("expected bearer auth header, got %q", receivedAuth)
		}
		if receivedPath != "/rest/api/latest/projects" {
			t.Fatalf("expected /rest path prefix, got %q", receivedPath)
		}
	})

	t.Run("uses basic auth when token is absent", func(t *testing.T) {
		var authHeader string
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			authHeader = request.Header.Get("Authorization")
			writer.Header().Set("Content-Type", "application/json")
			_, _ = writer.Write([]byte(`{"size":0,"limit":1,"isLastPage":true,"values":[]}`))
		}))
		defer server.Close()

		client, err := NewClientWithResponsesFromConfig(config.AppConfig{
			BitbucketURL:      server.URL,
			BitbucketUsername: "alice",
			BitbucketPassword: "secret",
			RequestTimeout:    time.Second,
			RetryCount:        0,
			RetryBackoff:      time.Millisecond,
		})
		if err != nil {
			t.Fatalf("new client: %v", err)
		}

		limit := float32(1)
		if _, err := client.GetProjectsWithResponse(context.Background(), &openapigenerated.GetProjectsParams{Limit: &limit}); err != nil {
			t.Fatalf("get projects: %v", err)
		}
		if !strings.HasPrefix(authHeader, "Basic ") {
			t.Fatalf("expected basic auth header, got %q", authHeader)
		}
	})
}
