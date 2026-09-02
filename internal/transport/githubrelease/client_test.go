package githubrelease

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestClientLatest(t *testing.T) {
	t.Setenv("BB_BLOCK_EXTERNAL_NETWORK", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/repos/vriesdemichael/bitbucket-data-center-cli/releases/latest" {
			t.Fatalf("unexpected path: %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"tag_name":"v1.2.3","html_url":"https://example.test/releases/v1.2.3","assets":[{"name":"sha256sums.txt","browser_download_url":"https://example.test/sha256sums.txt"}]}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), "bb/test")
	release, err := client.Latest(context.Background(), "vriesdemichael", "bitbucket-data-center-cli")
	if err != nil {
		t.Fatalf("Latest returned error: %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Fatalf("expected latest tag v1.2.3, got %q", release.TagName)
	}
	if len(release.Assets) != 1 || release.Assets[0].Name != "sha256sums.txt" {
		t.Fatalf("unexpected assets: %+v", release.Assets)
	}
}

func TestClientDownloadMapsNotFound(t *testing.T) {
	t.Setenv("BB_BLOCK_EXTERNAL_NETWORK", "1")
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.NotFound(writer, request)
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), "bb/test")
	_, err := client.Download(context.Background(), server.URL+"/missing")
	if err == nil {
		t.Fatal("expected download error")
	}
	if !apperrors.IsKind(err, apperrors.KindNotFound) {
		t.Fatalf("expected not_found error, got %v", err)
	}
}

func TestClientLatestValidationAndErrorPaths(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		var client *Client
		_, err := client.Latest(context.Background(), "vriesdemichael", "bitbucket-data-center-cli")
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("missing owner repo", func(t *testing.T) {
		client := NewClient("http://example.test", &http.Client{}, "bb/test")
		_, err := client.Latest(context.Background(), "", "")
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected validation error, got %v", err)
		}
	})

	t.Run("invalid base url", func(t *testing.T) {
		client := NewClient(":", &http.Client{}, "bb/test")
		_, err := client.Latest(context.Background(), "vriesdemichael", "bitbucket-data-center-cli")
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("transient status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()

		client := NewClient(server.URL, server.Client(), "bb/test")
		_, err := client.Latest(context.Background(), "vriesdemichael", "bitbucket-data-center-cli")
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
	})

	t.Run("permanent status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			writer.WriteHeader(http.StatusBadRequest)
		}))
		defer server.Close()

		client := NewClient(server.URL, server.Client(), "bb/test")
		_, err := client.Latest(context.Background(), "vriesdemichael", "bitbucket-data-center-cli")
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent error, got %v", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			_, _ = writer.Write([]byte(`{"tag_name":`))
		}))
		defer server.Close()

		client := NewClient(server.URL, server.Client(), "bb/test")
		_, err := client.Latest(context.Background(), "vriesdemichael", "bitbucket-data-center-cli")
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected permanent decode error, got %v", err)
		}
	})
}

func TestClientDownloadValidationAndBodyErrors(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		var client *Client
		_, err := client.Download(context.Background(), "http://example.test")
		if !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Fatalf("expected internal error, got %v", err)
		}
	})

	t.Run("empty url", func(t *testing.T) {
		client := NewClient("http://example.test", &http.Client{}, "bb/test")
		_, err := client.Download(context.Background(), "")
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected validation error, got %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		client := NewClient("http://example.test", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("boom")
		})}, "bb/test")
		_, err := client.Download(context.Background(), "http://example.test/file")
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient error, got %v", err)
		}
	})

	t.Run("read body error", func(t *testing.T) {
		client := NewClient("http://example.test", &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: errReadCloser{}}, nil
		})}, "bb/test")
		_, err := client.Download(context.Background(), "http://example.test/file")
		if !apperrors.IsKind(err, apperrors.KindTransient) {
			t.Fatalf("expected transient body read error, got %v", err)
		}
	})
}

func TestDecodeJSONAndMapHTTPError(t *testing.T) {
	if err := decodeJSON([]byte(`{"tag_name":"v1.2.3"}`), &Release{}); err != nil {
		t.Fatalf("expected valid json decode, got %v", err)
	}
	if err := decodeJSON([]byte(`{"tag_name":`), &Release{}); !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("expected permanent decode error, got %v", err)
	}

	if !apperrors.IsKind(mapHTTPError(http.StatusNotFound, "x"), apperrors.KindNotFound) {
		t.Fatal("expected not found mapping")
	}
	if !apperrors.IsKind(mapHTTPError(http.StatusBadGateway, "x"), apperrors.KindTransient) {
		t.Fatal("expected transient mapping")
	}
	if !apperrors.IsKind(mapHTTPError(http.StatusBadRequest, "x"), apperrors.KindPermanent) {
		t.Fatal("expected permanent mapping")
	}

	defaultClient := NewClient("", nil, "  bb/test  ")
	if defaultClient.baseURL != defaultBaseURL {
		t.Fatalf("expected default base url, got %q", defaultClient.baseURL)
	}
	if strings.TrimSpace(defaultClient.userAgent) != "bb/test" {
		t.Fatalf("expected trimmed user agent, got %q", defaultClient.userAgent)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

type errReadCloser struct{}

func (errReadCloser) Read([]byte) (int, error) {
	return 0, errors.New("read failed")
}

func (errReadCloser) Close() error {
	return nil
}

func TestClientMirrorFallback(t *testing.T) {
	// Server responds 404 to /repos/o/r/releases/latest, but 200 to /releases/latest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/releases/latest":
			http.NotFound(w, r)
		case "/releases/latest":
			_ = json.NewEncoder(w).Encode(Release{TagName: "v2.0.0", HTMLURL: "https://mirror/release"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), "test-agent")
	rel, err := client.Latest(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("expected fallback to succeed, got: %v", err)
	}
	if rel.TagName != "v2.0.0" {
		t.Fatalf("expected v2.0.0, got: %s", rel.TagName)
	}
}

func TestClientDownloadRelativeAndMirrorFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/assets/bb_linux_amd64.tar.gz":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("binary-content"))
		case "/bb_linux_amd64.tar.gz":
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("mirror-fallback-content"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), "test-agent")

	// 1. Relative URL resolves against mirror baseURL
	data, err := client.Download(context.Background(), "/assets/bb_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("download relative: %v", err)
	}
	if string(data) != "binary-content" {
		t.Fatalf("unexpected content: %s", string(data))
	}

	// 2. Firewalled / failed external github.com URL falls back to mirror baseURL/{assetName}
	data2, err := client.Download(context.Background(), "https://unreachable-github.test/releases/download/v1.0.0/bb_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("download fallback: %v", err)
	}
	if string(data2) != "mirror-fallback-content" {
		t.Fatalf("unexpected content from fallback: %s", string(data2))
	}
}

func TestClientDownloadPrefersMirrorOverManifestURL(t *testing.T) {
	externalRequests := 0
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalRequests++
		_, _ = w.Write([]byte("github-content"))
	}))
	defer external.Close()

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bb_linux_amd64.tar.gz" {
			_, _ = w.Write([]byte("mirror-content"))
			return
		}
		http.NotFound(w, r)
	}))
	defer mirror.Close()

	client := NewClient(mirror.URL, mirror.Client(), "test-agent")

	body, err := client.Download(context.Background(), external.URL+"/releases/download/v1.0.0/bb_linux_amd64.tar.gz")
	if err != nil {
		t.Fatalf("expected the mirror to serve the asset, got: %v", err)
	}
	if string(body) != "mirror-content" {
		t.Fatalf("expected mirror content, got: %s", body)
	}
	if externalRequests != 0 {
		t.Fatalf("expected no request to the manifest URL, got %d", externalRequests)
	}
}

func TestClientDownloadReportsBothAddressesWhenMirrorAndManifestFail(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer mirror.Close()

	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer external.Close()

	client := NewClient(mirror.URL, mirror.Client(), "test-agent")

	_, err := client.Download(context.Background(), external.URL+"/releases/download/v1.0.0/bb_linux_amd64.tar.gz")
	if err == nil {
		t.Fatal("expected an error when neither address serves the asset")
	}
	if !strings.Contains(err.Error(), mirror.URL+"/bb_linux_amd64.tar.gz") {
		t.Fatalf("expected the mirror address in the message, got: %v", err)
	}
}

func TestClientLatestFallsBackOnNonNotFoundMirrorErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/owner/repo/releases/latest":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/releases/latest":
			_ = json.NewEncoder(w).Encode(Release{TagName: "v3.0.0"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, server.Client(), "test-agent")
	release, err := client.Latest(context.Background(), "owner", "repo")
	if err != nil {
		t.Fatalf("expected the mirror fallback path to be tried, got: %v", err)
	}
	if release.TagName != "v3.0.0" {
		t.Fatalf("unexpected tag: %s", release.TagName)
	}
}
