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

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
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
		defer func() { _ = response.Body.Close() }()
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
		defer func() { _ = response.Body.Close() }()
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
		defer func() { _ = response.Body.Close() }()
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

		failed, err := transport.RoundTrip(request)
		closeResponse(failed)
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
		defer func() { _ = response.Body.Close() }()
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
		defer func() { _ = response.Body.Close() }()
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
		closeResponse(response)
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

		cancelled, err := transport.RoundTrip(request)
		closeResponse(cancelled)
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

// The auth and base-path test that stood here is gone.
//
// It recorded the Authorization header a mock received and checked it began
// with "Bearer " or "Basic ", which says the client wrote the string it was
// told to write. Whether Bitbucket honours either form is the server's answer:
// the whole live suite authenticates with basic, and
// TestLiveAuthTokenLifecycle now creates a real access token and makes a
// request with it as a bearer credential. The base path it also asserted --
// /rest/api/latest -- is proved by every live request that reaches an endpoint
// at all.

func TestDiagnosticsWriter(t *testing.T) {
	buffer := &bytes.Buffer{}

	if writer := diagnostics.EnabledWriter(true, buffer); writer != buffer {
		t.Fatalf("expected configured writer when enabled, got %T", writer)
	}

	if writer := diagnostics.EnabledWriter(false, buffer); writer != io.Discard {
		t.Fatalf("expected discard writer when disabled, got %T", writer)
	}
}

// closeResponse releases a response body when there is one.
//
// A request that fails returns a nil response, and one that succeeds holds a
// connection open until its body is closed. Tests that discard the response
// leak the second kind, which is what bodyclose reports and what this makes
// unnecessary to think about at each call site.
func closeResponse(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}

// TestRetriesDoNotReplayMutations is #454, through the real transport.
//
// The counts are the point: a POST answered with 503 must be issued exactly
// once, because a response lost after the write landed is indistinguishable
// from one that never arrived.
func TestRetriesDoNotReplayMutations(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		method     string
		status     int
		retryAfter string
		want       int
	}{
		{name: "POST on 503 is issued once", method: http.MethodPost, status: http.StatusServiceUnavailable, want: 1},
		{name: "PATCH on 503 is issued once", method: http.MethodPatch, status: http.StatusServiceUnavailable, want: 1},
		{name: "POST on 429 is retried", method: http.MethodPost, status: http.StatusTooManyRequests, retryAfter: "0", want: 3},
		{name: "GET on 503 is retried", method: http.MethodGet, status: http.StatusServiceUnavailable, want: 3},
		{name: "DELETE on 503 is retried", method: http.MethodDelete, status: http.StatusServiceUnavailable, want: 3},
		{name: "PUT on 503 is retried", method: http.MethodPut, status: http.StatusServiceUnavailable, want: 3},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var attempts int
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				attempts++
				if testCase.retryAfter != "" {
					writer.Header().Set("Retry-After", testCase.retryAfter)
				}
				writer.WriteHeader(testCase.status)
			}))
			defer server.Close()

			transport := &retryTransport{
				base:        http.DefaultTransport,
				retries:     2,
				baseBackoff: time.Millisecond,
				logger:      diagnostics.NewLogger(diagnostics.Config{}, io.Discard),
			}

			request, err := http.NewRequestWithContext(context.Background(), testCase.method, server.URL, bytes.NewReader([]byte(`{}`)))
			if err != nil {
				t.Fatalf("request: %v", err)
			}

			response, err := transport.RoundTrip(request)
			if err != nil {
				t.Fatalf("round trip: %v", err)
			}
			_ = response.Body.Close()

			if attempts != testCase.want {
				t.Errorf("server saw %d requests, want %d", attempts, testCase.want)
			}
		})
	}
}
