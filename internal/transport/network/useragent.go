package network

import (
	"runtime"
	"strings"
	"sync/atomic"
)

// The User-Agent every request bb sends carries, unless the request sets its
// own: bb/<version> (<os>/<arch>), and after it the surface that sent it --
// mcp for the MCP server. Without it a request carried Go's default, and an
// administrator could not tell bb's traffic, or the MCP server's, apart from
// any other Go program's in Bitbucket's access log or at a proxy.
var (
	userAgentVersion atomic.Value
	userAgentSurface atomic.Value
)

// SetVersion names the release in the User-Agent. The binary sets it once, as
// it is stamped in at build time; until then it is dev. A release is stamped
// with its tag, v5.0.0, and a product token carries the bare number, as
// curl/8.4.0 and git/2.43.0 do.
func SetVersion(version string) {
	if trimmed := strings.TrimPrefix(strings.TrimSpace(version), "v"); trimmed != "" {
		userAgentVersion.Store(trimmed)
	}
}

// SetSurface names the part of bb sending the requests, such as mcp.
func SetSurface(surface string) {
	userAgentSurface.Store(strings.TrimSpace(surface))
}

// UserAgent is the User-Agent bb sends.
func UserAgent() string {
	version, _ := userAgentVersion.Load().(string)
	if version == "" {
		version = "dev"
	}

	agent := "bb/" + version + " (" + runtime.GOOS + "/" + runtime.GOARCH + ")"
	if surface, _ := userAgentSurface.Load().(string); surface != "" {
		agent += " " + surface
	}

	return agent
}
