package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func init() {
	// Block external network access during tests by default
	os.Setenv("BB_BLOCK_EXTERNAL_NETWORK", "1")
}

func TestHealthAuthenticated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/1.0/projects" {
			http.NotFound(writer, request)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !health.Healthy || !health.Authenticated {
		t.Fatalf("expected healthy authenticated state, got: %+v", health)
	}
}

func TestNewFromConfigTransportOptions(t *testing.T) {
	client := NewFromConfig(config.AppConfig{
		BitbucketURL:   "http://example.local",
		RequestTimeout: 42 * time.Second,
		RetryCount:     7,
		RetryBackoff:   333 * time.Millisecond,
	})

	if client.http.Timeout != 42*time.Second {
		t.Fatalf("expected timeout 42s, got %s", client.http.Timeout)
	}
	if client.retries != 7 {
		t.Fatalf("expected retries 7, got %d", client.retries)
	}
	if client.backoff != 333*time.Millisecond {
		t.Fatalf("expected backoff 333ms, got %s", client.backoff)
	}
}

func TestHealthUnauthorizedButReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	health, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	if !health.Healthy || health.Authenticated {
		t.Fatalf("expected healthy but unauthenticated state, got: %+v", health)
	}
}

func TestHealthServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	_, err := client.Health(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}

	if apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient exit code 10, got %d", apperrors.ExitCode(err))
	}
}

func TestGetJSONSuccessWithTokenAuth(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/test" {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Query().Get("limit") != "5" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.Header.Get("Authorization") != "Bearer token-123" {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL, BitbucketToken: "token-123"})
	client.retries = 0

	var payload map[string]any
	err := client.GetJSON(context.Background(), "/rest/api/latest/test", map[string]string{"limit": "5"}, &payload)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if ok, _ := payload["ok"].(bool); !ok {
		t.Fatalf("expected ok=true in payload, got: %#v", payload)
	}
}

func TestGetJSONRetriesAndSucceeds(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempt := attempts.Add(1)
		if attempt < 3 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			_, _ = writer.Write([]byte("temporary"))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"items":[1]}`))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	client.retries = 2

	var payload map[string]any
	err := client.GetJSON(context.Background(), "/rest/api/latest/retry", nil, &payload)
	if err != nil {
		t.Fatalf("expected no error after retries, got: %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got: %d", attempts.Load())
	}
}

func TestGetJSONMapsStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusForbidden)
		_, _ = writer.Write([]byte("no access"))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	client.retries = 0

	var payload map[string]any
	err := client.GetJSON(context.Background(), "/rest/api/latest/forbidden", nil, &payload)
	if err == nil {
		t.Fatal("expected authorization error")
	}
	if apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected authorization exit code 3, got %d", apperrors.ExitCode(err))
	}
}

func TestGetJSONDecodeFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("not-json"))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	client.retries = 0

	var payload map[string]any
	err := client.GetJSON(context.Background(), "/rest/api/latest/ok", nil, &payload)
	if err == nil {
		t.Fatal("expected decode error")
	}
	if apperrors.ExitCode(err) != 1 {
		t.Fatalf("expected permanent exit code 1, got %d", apperrors.ExitCode(err))
	}
}

func TestApplyAuthUsesBasicCredentials(t *testing.T) {
	client := NewFromConfig(config.AppConfig{BitbucketURL: "http://example.local", BitbucketUsername: "alice", BitbucketPassword: "secret"})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.local/test", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	client.applyAuth(req)
	if !strings.HasPrefix(req.Header.Get("Authorization"), "Basic ") {
		t.Fatal("expected basic auth header")
	}

	username, password, ok := req.BasicAuth()
	if !ok || username != "alice" || password != "secret" {
		t.Fatalf("expected basic auth credentials, got ok=%v username=%q password=%q", ok, username, password)
	}
}

func TestHealthRedirectConsideredReachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Location", "/login")
		writer.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	client.retries = 0
	client.http = &http.Client{
		CheckRedirect: func(request *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	status, err := client.Health(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !status.Healthy || status.Authenticated {
		t.Fatalf("expected healthy but unauthenticated for redirect status, got: %+v", status)
	}
}

func TestGetJSONInvalidBaseURL(t *testing.T) {
	client := NewFromConfig(config.AppConfig{BitbucketURL: "%"})
	var payload map[string]any
	err := client.GetJSON(context.Background(), "/rest/api/latest/test", nil, &payload)
	if err == nil {
		t.Fatal("expected invalid request URL error")
	}
	if !strings.Contains(err.Error(), "invalid request URL") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetJSONOutputTargetType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		_, _ = writer.Write([]byte(`{"value":"x"}`))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	client.retries = 0

	type payloadType struct {
		Value string `json:"value"`
	}
	var payload payloadType
	err := client.GetJSON(context.Background(), "/rest/api/latest/typed", nil, &payload)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	encoded, _ := json.Marshal(payload)
	if !strings.Contains(string(encoded), "x") {
		t.Fatalf("expected decoded typed payload, got: %+v", payload)
	}
}

func TestWriteJSONMethods(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost && request.Method != http.MethodPut && request.Method != http.MethodDelete {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	client.retries = 0

	var payload map[string]any
	if err := client.PostJSON(context.Background(), "/rest/api/latest/test", nil, map[string]string{"name": "a"}, &payload); err != nil {
		t.Fatalf("post json failed: %v", err)
	}
	if err := client.PutJSON(context.Background(), "/rest/api/latest/test", nil, map[string]string{"name": "a"}, &payload); err != nil {
		t.Fatalf("put json failed: %v", err)
	}
	if err := client.DeleteJSON(context.Background(), "/rest/api/latest/test", nil, map[string]string{"name": "a"}, &payload); err != nil {
		t.Fatalf("delete json failed: %v", err)
	}
}

func TestGetJSONTransportAndRetryExhaustion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}))
	baseURL := server.URL
	server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: baseURL})
	client.retries = 1

	var payload map[string]any
	err := client.GetJSON(context.Background(), "/rest/api/latest/test", nil, &payload)
	if err == nil {
		t.Fatal("expected transient transport error")
	}
	if apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestGetJSONTooManyRequestsRetryExhaustion(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempts.Add(1)
		writer.WriteHeader(http.StatusTooManyRequests)
		_, _ = writer.Write([]byte("rate limited"))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	client.retries = 2

	var payload map[string]any
	err := client.GetJSON(context.Background(), "/rest/api/latest/test", nil, &payload)
	if err == nil {
		t.Fatal("expected transient error after retry exhaustion")
	}
	if apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts.Load())
	}
}

func TestGetJSONTooManyRequestsUsesRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			writer.Header().Set("Retry-After", "0")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte("rate limited"))
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	client.retries = 1
	client.backoff = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	var payload map[string]any
	err := client.GetJSON(ctx, "/rest/api/latest/test", nil, &payload)
	if err != nil {
		t.Fatalf("expected request to honor Retry-After and succeed, got: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts.Load())
	}
}

func TestGetJSONRetryCanceledDuringBackoff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusServiceUnavailable)
		_, _ = writer.Write([]byte("temporary"))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
	client.retries = 1
	client.backoff = time.Hour

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	var payload map[string]any
	err := client.GetJSON(ctx, "/rest/api/latest/test", nil, &payload)
	if err == nil {
		t.Fatal("expected retry cancellation error")
	}
	if apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
	}
}

func TestHealthTransportAndPermanentErrorBranches(t *testing.T) {
	t.Run("transport failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusOK)
		}))
		baseURL := server.URL
		server.Close()

		client := NewFromConfig(config.AppConfig{BitbucketURL: baseURL})
		client.retries = 1

		_, err := client.Health(context.Background())
		if err == nil {
			t.Fatal("expected transient transport error")
		}
		if apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("non-retriable permanent status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusTeapot)
		}))
		defer server.Close()

		client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
		client.retries = 0

		_, err := client.Health(context.Background())
		if err == nil {
			t.Fatal("expected permanent status error")
		}
		if apperrors.ExitCode(err) != 1 {
			t.Fatalf("expected permanent exit code 1, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})

	t.Run("retriable status emits diagnostics", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		diagnostics.SetOutputWriter(buffer)
		t.Cleanup(func() {
			diagnostics.SetOutputWriter(io.Discard)
		})

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL, DiagnosticsEnabled: true, LogLevel: "warn", LogFormat: "jsonl"})
		client.retries = 1

		_, err := client.Health(context.Background())
		if err == nil {
			t.Fatal("expected transient status error")
		}

		if !strings.Contains(buffer.String(), "health probe returned retriable status") {
			t.Fatalf("expected retriable health diagnostics output, got: %s", buffer.String())
		}
	})

	t.Run("retriable status honors retry-after", func(t *testing.T) {
		var attempts atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			attempt := attempts.Add(1)
			if attempt == 1 {
				writer.Header().Set("Retry-After", "0")
				writer.WriteHeader(http.StatusTooManyRequests)
				return
			}
			writer.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
		client.retries = 1
		client.backoff = time.Hour

		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()

		health, err := client.Health(ctx)
		if err != nil {
			t.Fatalf("expected health retry to succeed, got: %v", err)
		}
		if !health.Healthy {
			t.Fatalf("expected healthy status after retry, got: %+v", health)
		}
		if attempts.Load() != 2 {
			t.Fatalf("expected 2 attempts, got %d", attempts.Load())
		}
	})

	t.Run("retriable status canceled during backoff", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusTooManyRequests)
		}))
		defer server.Close()

		client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})
		client.retries = 1
		client.backoff = time.Hour

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()

		_, err := client.Health(ctx)
		if err == nil {
			t.Fatal("expected transient cancellation error")
		}
		if apperrors.ExitCode(err) != 10 {
			t.Fatalf("expected transient exit code 10, got %d (%v)", apperrors.ExitCode(err), err)
		}
	})
}

func TestRetryDelayFromResponse(t *testing.T) {
	t.Run("uses retry-after seconds", func(t *testing.T) {
		delay := retryDelayFromResponse(http.Header{"Retry-After": []string{"2"}}, 0, time.Millisecond)
		if delay != 2*time.Second {
			t.Fatalf("expected 2s delay, got %s", delay)
		}
	})

	t.Run("uses retry-after http date", func(t *testing.T) {
		retryAt := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
		delay := retryDelayFromResponse(http.Header{"Retry-After": []string{retryAt}}, 0, time.Millisecond)
		if delay <= 0 || delay > 3*time.Second {
			t.Fatalf("expected positive delay <=3s, got %s", delay)
		}
	})

	t.Run("falls back on invalid retry-after", func(t *testing.T) {
		delay := retryDelayFromResponse(http.Header{"Retry-After": []string{"invalid"}}, 1, 200*time.Millisecond)
		if delay != 400*time.Millisecond {
			t.Fatalf("expected fallback delay 400ms, got %s", delay)
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

func TestApplyAuthPrefersTokenOverBasic(t *testing.T) {
	client := NewFromConfig(config.AppConfig{BitbucketURL: "http://example.local", BitbucketToken: "tok", BitbucketUsername: "alice", BitbucketPassword: "secret"})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.local/test", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}

	client.applyAuth(req)
	if req.Header.Get("Authorization") != "Bearer tok" {
		t.Fatalf("expected bearer auth, got %q", req.Header.Get("Authorization"))
	}
}

func TestClientInitErrorFromInvalidCA(t *testing.T) {
	client := NewFromConfig(config.AppConfig{
		BitbucketURL:   "http://localhost:7990",
		CAFile:         "/definitely/missing/ca.pem",
		RequestTimeout: time.Second,
		RetryCount:     1,
		RetryBackoff:   time.Millisecond,
	})

	if err := client.GetJSON(context.Background(), "/rest/api/latest/test", nil, nil); err == nil {
		t.Fatal("expected initialization validation error")
	}

	if _, err := client.Health(context.Background()); err == nil {
		t.Fatal("expected health initialization validation error")
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

func TestGetRawReturnsBodyVerbatim(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/projects/TEST/repos/demo/raw/dir/file.txt" {
			http.NotFound(writer, request)
			return
		}
		if request.URL.Query().Get("at") != "refs/heads/main" {
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		// GetRaw must not ask for JSON: the endpoint streams file contents.
		if request.Header.Get("Accept") != "" {
			writer.WriteHeader(http.StatusNotAcceptable)
			return
		}
		writer.Header().Set("Content-Type", "text/plain")
		_, _ = writer.Write([]byte("line one\nline two\n"))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})

	body, err := client.GetRaw(
		context.Background(),
		"/rest/api/latest/projects/TEST/repos/demo/raw/dir/file.txt",
		map[string]string{"at": "refs/heads/main"},
	)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if string(body) != "line one\nline two\n" {
		t.Fatalf("expected verbatim body, got %q", string(body))
	}
}

func TestGetRawMapsStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusNotFound)
		_, _ = writer.Write([]byte(`{"errors":[{"message":"file not found"}]}`))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})

	_, err := client.GetRaw(context.Background(), "/rest/api/latest/missing", nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if apperrors.ExitCode(err) != 4 {
		t.Fatalf("expected not-found exit code 4, got %d: %v", apperrors.ExitCode(err), err)
	}
}

func TestGetRawRetriesServerErrors(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if calls.Add(1) == 1 {
			writer.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = writer.Write([]byte("recovered"))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{
		BitbucketURL: server.URL,
		RetryCount:   2,
		RetryBackoff: time.Millisecond,
	})

	body, err := client.GetRaw(context.Background(), "/rest/api/latest/flaky", nil)
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	if string(body) != "recovered" {
		t.Fatalf("expected recovered body, got %q", string(body))
	}
	if calls.Load() != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls.Load())
	}
}

func TestGetRawTransientNetworkFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		hijacker, ok := writer.(http.Hijacker)
		if !ok {
			http.Error(writer, "hijacking unsupported", http.StatusInternalServerError)
			return
		}
		connection, _, err := hijacker.Hijack()
		if err == nil {
			_ = connection.Close()
		}
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})

	if _, err := client.GetRaw(context.Background(), "/rest/api/latest/dropped", nil); err == nil || apperrors.ExitCode(err) != 10 {
		t.Fatalf("expected transient exit code 10, got %v", err)
	}
}

func TestCurrentUserSlugReadsResponseHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/rest/api/latest/users" {
			http.NotFound(writer, request)
			return
		}
		writer.Header().Set("X-AUSERNAME", " alice ")
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"values":[]}`))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})

	slug, err := client.CurrentUserSlug(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if slug != "alice" {
		t.Fatalf("expected trimmed slug alice, got %q", slug)
	}
}

func TestCurrentUserSlugMissingHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"values":[]}`))
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})

	_, err := client.CurrentUserSlug(context.Background())
	if err == nil {
		t.Fatal("expected error when the header is absent")
	}
	if !strings.Contains(err.Error(), "X-AUSERNAME") {
		t.Fatalf("expected the error to name the missing header, got: %v", err)
	}
	if apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected authentication exit code 3, got %d", apperrors.ExitCode(err))
	}
}

func TestCurrentUserSlugPropagatesStatusErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := NewFromConfig(config.AppConfig{BitbucketURL: server.URL})

	if _, err := client.CurrentUserSlug(context.Background()); err == nil || apperrors.ExitCode(err) != 3 {
		t.Fatalf("expected authentication exit code 3, got %v", err)
	}
}
