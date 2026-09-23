package githubrelease

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

// TestClientDownloadNeverFetchesAnAssetOffTheMirror is ADR-059's "every
// download goes through the mirror". A mirror that failed used to send bb after
// the manifest's own address, github.com for a mirrored manifest (#637).
// mock-inventory: external-service — a release mirror and the host a mirrored
// manifest still names; the assertion is about which one bb update asks.
func TestClientDownloadNeverFetchesAnAssetOffTheMirror(t *testing.T) {
	t.Parallel()

	mirror := httptest.NewServer(http.NotFoundHandler())
	defer mirror.Close()

	var externalRequests atomic.Int32
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalRequests.Add(1)
		_, _ = w.Write([]byte("github-content"))
	}))
	defer external.Close()

	client := NewClient(mirror.URL, mirror.Client(), "test-agent")

	manifestAddress := external.URL + "/releases/download/v1.0.0/bb_linux_amd64.tar.gz"
	_, err := client.Download(context.Background(), manifestAddress)
	if !apperrors.IsKind(err, apperrors.KindNotFound) || !strings.Contains(err.Error(), mirror.URL+"/bb_linux_amd64.tar.gz") {
		t.Fatalf("expected the mirror's 404 for the asset, naming the mirror address, got: %v", err)
	}
	if got := externalRequests.Load(); got != 0 {
		t.Fatalf("the manifest's own address, off the mirror, was fetched %d times", got)
	}
	if !strings.Contains(err.Error(), manifestAddress+", is off the mirror and is not used") {
		t.Fatalf("expected the message to say the manifest's address is not used, got: %v", err)
	}
}

// TestClientDownloadResolvesRelativeAssetsUnderAPathPrefixedMirror is the
// hardening guide's own layout. Resolved as RFC 3986 does against the base URL
// as given, a relative name replaced its last segment and left the mirror;
// only the retry by file name made the flat layout work, and a manifest naming
// a subdirectory lost it (#637).
// mock-inventory: external-service — a generic-repository release mirror under
// a path prefix; the assertion is about the addresses bb update asks for.
func TestClientDownloadResolvesRelativeAssetsUnderAPathPrefixedMirror(t *testing.T) {
	t.Parallel()

	var mu sync.Mutex
	var requested []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requested = append(requested, r.URL.Path)
		mu.Unlock()

		switch r.URL.Path {
		case "/artifactory/bb-releases/sha256sums.txt":
			_, _ = w.Write([]byte("checksums"))
		case "/artifactory/bb-releases/v1.2.0/bb_1.2.0_linux_amd64.tar.gz":
			_, _ = w.Write([]byte("the v1.2.0 archive"))
		case "/artifactory/bb-releases/bb_1.2.0_linux_amd64.tar.gz":
			_, _ = w.Write([]byte("an archive filed flat, which the manifest did not name"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL+"/artifactory/bb-releases", server.Client(), "test-agent")

	for relative, want := range map[string]string{
		"sha256sums.txt":                     "checksums",
		"v1.2.0/bb_1.2.0_linux_amd64.tar.gz": "the v1.2.0 archive",
	} {
		mu.Lock()
		requested = nil
		mu.Unlock()

		body, err := client.Download(context.Background(), relative)
		if err != nil || string(body) != want {
			t.Fatalf("%s: got %q, %v; want %q", relative, body, err, want)
		}

		mu.Lock()
		asked := strings.Join(requested, " ")
		mu.Unlock()
		if asked != "/artifactory/bb-releases/"+relative {
			t.Fatalf("%s: asked for %q, want only the address under the mirror", relative, asked)
		}
	}
}

// TestClientLatestReportsWhatEveryMirrorAddressSaid covers the manifest
// fallbacks. Their failures were dropped, so a mirror whose manifest was there
// and unreadable was reported as having none (#637).
// mock-inventory: external-service — release mirrors laid out as generic
// repositories; the assertion is about what bb update reports of them.
func TestClientLatestReportsWhatEveryMirrorAddressSaid(t *testing.T) {
	t.Parallel()

	mirrorAnswering := func(t *testing.T, releasesLatest http.HandlerFunc) *httptest.Server {
		t.Helper()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/releases/latest" {
				releasesLatest(w, r)
				return
			}
			http.NotFound(w, r)
		}))
		t.Cleanup(server.Close)

		return server
	}

	t.Run("an unreadable manifest is not a missing one", func(t *testing.T) {
		t.Parallel()

		server := mirrorAnswering(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"tag_name":`)) })
		_, err := NewClient(server.URL, server.Client(), "test-agent").Latest(context.Background(), "owner", "repo")
		if !apperrors.IsKind(err, apperrors.KindPermanent) {
			t.Fatalf("expected the broken manifest's permanent failure, got: %v", err)
		}
		for _, address := range []string{"/repos/owner/repo/releases/latest", "/releases/latest", "/latest"} {
			if !strings.Contains(err.Error(), server.URL+address+": ") {
				t.Fatalf("expected %s and what it answered in the message, got: %v", address, err)
			}
		}
		if !strings.Contains(err.Error(), "failed to decode release metadata") {
			t.Fatalf("expected the decode failure in the message, got: %v", err)
		}
	})

	t.Run("a manifest without a tag is named as such", func(t *testing.T) {
		t.Parallel()

		server := mirrorAnswering(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"html_url":"x"}`)) })
		_, err := NewClient(server.URL, server.Client(), "test-agent").Latest(context.Background(), "owner", "repo")
		if !apperrors.IsKind(err, apperrors.KindPermanent) || !strings.Contains(err.Error(), "names no tag_name") {
			t.Fatalf("expected the untagged manifest to be reported, got: %v", err)
		}
	})

	t.Run("a mirror without one is not found", func(t *testing.T) {
		t.Parallel()

		server := mirrorAnswering(t, http.NotFound)
		_, err := NewClient(server.URL, server.Client(), "test-agent").Latest(context.Background(), "owner", "repo")
		if !apperrors.IsKind(err, apperrors.KindNotFound) {
			t.Fatalf("expected not_found when every address answered 404, got: %v", err)
		}
	})
}

// TestClientReportsAMirrorCertificateItDoesNotTrustAsPermanent: a retry meets
// the same certificate. Every failed request used to be wrapped as transient,
// exit 10, telling an operator with a wrong mirror certificate to retry (#637).
// mock-inventory: transport-fault — a TLS listener whose certificate this
// client does not trust; the subject is the classification, not the server.
func TestClientReportsAMirrorCertificateItDoesNotTrustAsPermanent(t *testing.T) {
	t.Parallel()

	server := httptest.NewTLSServer(http.NotFoundHandler())
	defer server.Close()

	// A client without the test server's CA, as a host without the mirror's.
	client := NewClient(server.URL, &http.Client{Transport: &http.Transport{}}, "test-agent")

	_, latestErr := client.Latest(context.Background(), "owner", "repo")
	_, downloadErr := client.Download(context.Background(), server.URL+"/sha256sums.txt")
	for name, err := range map[string]error{"metadata": latestErr, "asset": downloadErr} {
		if !apperrors.IsKind(err, apperrors.KindPermanent) || apperrors.ExitCode(err) != 1 {
			t.Fatalf("%s: got %v (exit %d), want permanent, exit 1", name, err, apperrors.ExitCode(err))
		}
	}
}

func TestClientLatestFallsBackOnNonNotFoundMirrorErrors(t *testing.T) {
	t.Parallel()

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
