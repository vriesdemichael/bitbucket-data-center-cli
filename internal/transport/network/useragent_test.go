package network

import (
	"net/http"
	"runtime"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

// restoreUserAgent puts the version and surface back as the test found them.
func restoreUserAgent(t *testing.T) {
	t.Helper()

	version, _ := userAgentVersion.Load().(string)
	surface, _ := userAgentSurface.Load().(string)
	t.Cleanup(func() {
		userAgentVersion.Store(version)
		userAgentSurface.Store(surface)
	})
}

// A request bb sends names bb, its release and platform, and the surface that
// sent it; one that names itself keeps its own name. The request handed to the
// transport is left as it was.
func TestEveryRequestNamesBB(t *testing.T) {
	restoreUserAgent(t)

	var seen string
	transport := &SafeTransport{Base: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		seen = request.Header.Get("User-Agent")
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: request}, nil
	})}
	send := func(userAgent string) string {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, "http://127.0.0.1/rest/api/1.0/projects", nil)
		if err != nil {
			t.Fatalf("build request: %v", err)
		}
		if userAgent != "" {
			request.Header.Set("User-Agent", userAgent)
		}
		response, err := transport.RoundTrip(request)
		if err != nil {
			t.Fatalf("round trip: %v", err)
		}
		_ = response.Body.Close()
		if userAgent == "" && request.Header.Get("User-Agent") != "" {
			t.Fatal("the transport changed the request it was handed")
		}
		return seen
	}

	platform := " (" + runtime.GOOS + "/" + runtime.GOARCH + ")"

	// Stamped as the release is, with its tag.
	SetVersion("v5.0.0")
	SetSurface("")
	if got, want := send(""), "bb/5.0.0"+platform; got != want {
		t.Errorf("User-Agent %q, want %q", got, want)
	}

	SetSurface("mcp")
	if got, want := send(""), "bb/5.0.0"+platform+" mcp"; got != want {
		t.Errorf("User-Agent from the MCP server %q, want %q", got, want)
	}

	if got := send("custom/1.0"); got != "custom/1.0" {
		t.Errorf("a request that names itself was renamed to %q", got)
	}
}
