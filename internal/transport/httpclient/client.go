package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/diagnostics"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/network"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/retrypolicy"
)

type Client struct {
	baseURL  string
	http     *http.Client
	token    string
	username string
	password string
	retries  int
	backoff  time.Duration
	logger   *diagnostics.Logger
	initErr  error
}

type HealthStatus struct {
	Healthy       bool   `json:"healthy"`
	StatusCode    int    `json:"status_code"`
	Authenticated bool   `json:"authenticated"`
	Message       string `json:"message"`
}

func NewFromConfig(cfg config.AppConfig) *Client {
	transport, err := network.NewSafeTransport(network.TLSOptions{
		CAFile:             cfg.CAFile,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		ClientCertFile:     cfg.ClientCertFile,
		ClientKeyFile:      cfg.ClientKeyFile,
	})
	if err != nil {
		transport = &network.SafeTransport{}
	}

	return &Client{
		baseURL: strings.TrimRight(cfg.BitbucketURL, "/"),
		http: &http.Client{
			Timeout:   cfg.RequestTimeout,
			Transport: transport,
		},
		token:    cfg.BitbucketToken,
		username: cfg.BitbucketUsername,
		password: cfg.BitbucketPassword,
		retries:  cfg.RetryCount,
		backoff:  cfg.RetryBackoff,
		logger: diagnostics.NewLogger(diagnostics.Config{
			Level:  diagnostics.Level(cfg.LogLevel),
			Format: diagnostics.Format(cfg.LogFormat),
		}, diagnostics.EnabledWriter(cfg.DiagnosticsEnabled, diagnostics.OutputWriter())),
		initErr: err,
	}
}

func (client *Client) GetJSON(ctx context.Context, path string, query map[string]string, out any) error {
	return client.doJSON(ctx, http.MethodGet, path, query, nil, out)
}

func (client *Client) PostJSON(ctx context.Context, path string, query map[string]string, in any, out any) error {
	return client.doJSON(ctx, http.MethodPost, path, query, in, out)
}

func (client *Client) PutJSON(ctx context.Context, path string, query map[string]string, in any, out any) error {
	return client.doJSON(ctx, http.MethodPut, path, query, in, out)
}

func (client *Client) DeleteJSON(ctx context.Context, path string, query map[string]string, in any, out any) error {
	return client.doJSON(ctx, http.MethodDelete, path, query, in, out)
}

// GetRaw performs a GET request and returns the raw response body as bytes.
// Unlike GetJSON it does not set Accept: application/json and does not attempt
// to unmarshal the response, so it suits endpoints that stream file contents.
func (client *Client) GetRaw(ctx context.Context, path string, query map[string]string) ([]byte, error) {
	body, _, err := client.do(ctx, http.MethodGet, path, query, nil, false)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// CurrentUserSlug returns the authenticated user's slug. Bitbucket echoes the
// authenticated username in the X-AUSERNAME response header on every
// authenticated request, so a cheap GET is enough to resolve it without the
// side effects of writing anything to the server.
func (client *Client) CurrentUserSlug(ctx context.Context) (string, error) {
	_, header, err := client.do(ctx, http.MethodGet, "/rest/api/latest/users", map[string]string{"limit": "1"}, nil, true)
	if err != nil {
		return "", err
	}

	slug := strings.TrimSpace(header.Get("X-AUSERNAME"))
	if slug == "" {
		return "", apperrors.New(apperrors.KindAuthentication, "could not determine the authenticated user: no X-AUSERNAME header in the response", nil)
	}
	return slug, nil
}

func (client *Client) doJSON(ctx context.Context, method string, path string, query map[string]string, in any, out any) error {
	var payload []byte
	if in != nil {
		encoded, err := json.Marshal(in)
		if err != nil {
			return apperrors.New(apperrors.KindValidation, "failed to encode request body", err)
		}
		payload = encoded
	}

	body, _, err := client.do(ctx, method, path, query, payload, true)
	if err != nil {
		return err
	}

	if out == nil || len(bytes.TrimSpace(body)) == 0 {
		return nil
	}

	if err := json.Unmarshal(body, out); err != nil {
		return apperrors.New(apperrors.KindPermanent, "failed to decode API response", err)
	}
	return nil
}

// RequestOptions configures a raw HTTP request.
type RequestOptions struct {
	Method  string
	Path    string
	Query   url.Values
	Headers http.Header
	Body    []byte
}

// RawResponse represents the raw HTTP response.
type RawResponse struct {
	StatusCode int
	Header     http.Header
	Body       []byte
}

// DoRequest performs a HTTP request with full control over method, headers, query, and body,
// applying the client's retry, backoff, logging, TLS, and auth policies.
func (client *Client) DoRequest(ctx context.Context, opts RequestOptions) (*RawResponse, error) {
	if client.initErr != nil {
		return nil, apperrors.New(apperrors.KindValidation, "failed to initialize HTTP transport", client.initErr)
	}

	method := strings.ToUpper(strings.TrimSpace(opts.Method))
	if method == "" {
		method = http.MethodGet
	}

	rawPath := opts.Path
	var requestURL *url.URL
	var err error
	if strings.HasPrefix(rawPath, "http://") || strings.HasPrefix(rawPath, "https://") {
		requestURL, err = url.Parse(rawPath)
	} else {
		if !strings.HasPrefix(rawPath, "/") {
			rawPath = "/" + rawPath
		}
		requestURL, err = url.Parse(client.baseURL + rawPath)
	}
	if err != nil {
		return nil, apperrors.New(apperrors.KindValidation, "invalid request URL", err)
	}

	values := requestURL.Query()
	for k, vs := range opts.Query {
		for _, v := range vs {
			values.Add(k, v)
		}
	}
	requestURL.RawQuery = values.Encode()

	var lastErr error
	for attempt := 0; attempt <= client.retries; attempt++ {
		started := time.Now()
		var bodyReader io.Reader
		if opts.Body != nil {
			bodyReader = bytes.NewReader(opts.Body)
		}

		request, err := http.NewRequestWithContext(ctx, method, requestURL.String(), bodyReader)
		if err != nil {
			return nil, apperrors.New(apperrors.KindInternal, "failed to build request", err)
		}

		if len(opts.Body) > 0 && (opts.Headers == nil || opts.Headers.Get("Content-Type") == "") {
			request.Header.Set("Content-Type", "application/json")
		}

		for k, vs := range opts.Headers {
			for _, v := range vs {
				request.Header.Add(k, v)
			}
		}

		if request.Header.Get("Authorization") == "" {
			client.applyAuth(request)
		}

		response, err := client.http.Do(request)
		if err != nil {
			fields := map[string]any{
				"method":      method,
				"endpoint":    requestURL.Path,
				"attempt":     attempt + 1,
				"retry_count": client.retries,
				"duration_ms": time.Since(started).Milliseconds(),
				"error":       err.Error(),
			}
			lastErr = apperrors.New(apperrors.KindTransient, "request failed", err)
			if attempt < client.retries && retrypolicy.Replayable(method) {
				client.logger.Warn("http request failed", fields)
				if sleepErr := sleepWithContext(ctx, time.Duration(attempt+1)*client.backoff); sleepErr != nil {
					return nil, apperrors.New(apperrors.KindTransient, "request canceled while waiting to retry", sleepErr)
				}
				continue
			}
			client.logger.Error("http request failed", fields)
			return nil, lastErr
		}

		body, readErr := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if readErr != nil {
			return nil, apperrors.New(apperrors.KindTransient, "failed to read response", readErr)
		}

		if response.StatusCode >= 200 && response.StatusCode < 300 {
			client.logger.Debug("http request completed", map[string]any{
				"method":      method,
				"endpoint":    requestURL.Path,
				"status":      response.StatusCode,
				"attempt":     attempt + 1,
				"retry_count": client.retries,
				"duration_ms": time.Since(started).Milliseconds(),
			})
			return &RawResponse{
				StatusCode: response.StatusCode,
				Header:     response.Header,
				Body:       body,
			}, nil
		}

		mappedErr := openapi.MapStatusError(response.StatusCode, body)
		fields := map[string]any{
			"method":      method,
			"endpoint":    requestURL.Path,
			"status":      response.StatusCode,
			"attempt":     attempt + 1,
			"retry_count": client.retries,
			"duration_ms": time.Since(started).Milliseconds(),
			"error":       mappedErr.Error(),
		}
		if retrypolicy.RetriableStatus(method, response.StatusCode) {
			lastErr = mappedErr
			retryDelay := retryDelayFromResponse(response.Header, attempt, client.backoff)
			fields["retry_delay"] = retryDelay.String()
			if attempt < client.retries {
				client.logger.Warn("http request returned error status", fields)
				if sleepErr := sleepWithContext(ctx, retryDelay); sleepErr != nil {
					return nil, apperrors.New(apperrors.KindTransient, "request canceled while waiting to retry", sleepErr)
				}
				continue
			}
			client.logger.Error("http request returned error status", fields)
			return nil, lastErr
		}

		client.logger.Error("http request returned error status", fields)
		return nil, mappedErr
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, apperrors.New(apperrors.KindTransient, "request failed after retries", nil)
}

// do issues a request under the client's retry, logging and error-mapping
// policy and returns the response body together with its headers.
func (client *Client) do(ctx context.Context, method string, path string, query map[string]string, payload []byte, acceptJSON bool) ([]byte, http.Header, error) {
	q := make(url.Values)
	for k, v := range query {
		q.Set(k, v)
	}
	var headers http.Header
	if acceptJSON {
		headers = make(http.Header)
		headers.Set("Accept", "application/json")
	}
	resp, err := client.DoRequest(ctx, RequestOptions{
		Method:  method,
		Path:    path,
		Query:   q,
		Headers: headers,
		Body:    payload,
	})
	if err != nil {
		return nil, nil, err
	}
	return resp.Body, resp.Header, nil
}

func (client *Client) Health(ctx context.Context) (HealthStatus, error) {
	if client.initErr != nil {
		return HealthStatus{}, apperrors.New(apperrors.KindValidation, "failed to initialize HTTP transport", client.initErr)
	}

	requestURL, err := url.Parse(client.baseURL + "/rest/api/1.0/projects?limit=1")
	if err != nil {
		return HealthStatus{}, apperrors.New(apperrors.KindValidation, "invalid health probe URL", err)
	}

	var lastErr error
	for attempt := 0; attempt <= client.retries; attempt++ {
		started := time.Now()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
		if err != nil {
			return HealthStatus{}, apperrors.New(apperrors.KindInternal, "failed to build health request", err)
		}

		request.Header.Set("Accept", "application/json")
		client.applyAuth(request)

		response, err := client.http.Do(request)
		if err != nil {
			fields := map[string]any{
				"method":      http.MethodGet,
				"endpoint":    requestURL.Path,
				"attempt":     attempt + 1,
				"retry_count": client.retries,
				"duration_ms": time.Since(started).Milliseconds(),
				"error":       err.Error(),
			}
			lastErr = apperrors.New(apperrors.KindTransient, "health probe failed", err)
			if attempt < client.retries {
				client.logger.Warn("health probe failed", fields)
				time.Sleep(time.Duration(attempt+1) * client.backoff)
				continue
			}
			client.logger.Error("health probe failed", fields)
			return HealthStatus{}, lastErr
		}

		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()

		switch {
		case response.StatusCode >= 200 && response.StatusCode < 300:
			client.logger.Debug("health probe completed", map[string]any{
				"status":      response.StatusCode,
				"endpoint":    requestURL.Path,
				"attempt":     attempt + 1,
				"retry_count": client.retries,
				"duration_ms": time.Since(started).Milliseconds(),
			})
			return HealthStatus{
				Healthy:       true,
				StatusCode:    response.StatusCode,
				Authenticated: true,
				Message:       "Bitbucket API reachable and authenticated",
			}, nil
		case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden || response.StatusCode >= 300 && response.StatusCode < 400:
			client.logger.Info("health probe unauthenticated", map[string]any{
				"status":      response.StatusCode,
				"endpoint":    requestURL.Path,
				"attempt":     attempt + 1,
				"retry_count": client.retries,
				"duration_ms": time.Since(started).Milliseconds(),
			})
			return HealthStatus{
				Healthy:       true,
				StatusCode:    response.StatusCode,
				Authenticated: false,
				Message:       "Bitbucket reachable but credentials are missing or insufficient",
			}, nil
		case response.StatusCode >= 500 || response.StatusCode == http.StatusTooManyRequests:
			fields := map[string]any{
				"status":      response.StatusCode,
				"endpoint":    requestURL.Path,
				"attempt":     attempt + 1,
				"retry_count": client.retries,
				"duration_ms": time.Since(started).Milliseconds(),
			}
			lastErr = openapi.MapStatusError(response.StatusCode, nil)
			retryDelay := retryDelayFromResponse(response.Header, attempt, client.backoff)
			fields["retry_delay"] = retryDelay.String()
			if attempt < client.retries {
				client.logger.Warn("health probe returned retriable status", fields)
				if sleepErr := sleepWithContext(ctx, retryDelay); sleepErr != nil {
					return HealthStatus{}, apperrors.New(apperrors.KindTransient, "health check canceled while waiting to retry", sleepErr)
				}
				continue
			}
			client.logger.Error("health probe returned retriable status", fields)
			return HealthStatus{}, lastErr
		default:
			return HealthStatus{}, openapi.MapStatusError(response.StatusCode, nil)
		}
	}

	if lastErr != nil {
		return HealthStatus{}, lastErr
	}

	return HealthStatus{}, apperrors.New(apperrors.KindTransient, "health probe failed after retries", nil)
}

func (client *Client) applyAuth(request *http.Request) {
	if client.token != "" {
		request.Header.Set("Authorization", "Bearer "+client.token)
		return
	}

	if client.username != "" && client.password != "" {
		request.SetBasicAuth(client.username, client.password)
	}
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
