package updatecmd

import (
	"net/http"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// schemeGuard refuses a request whose URL bb update may not fetch.
//
// It sits on the transport rather than at each call site because every request
// passes through here: the manifest, each asset a manifest names, the address a
// mirror falls back to, and every hop of a redirect, which http.Client follows
// by sending a new request through the same transport. A check on the
// configured base URL alone would let an https mirror redirect to plain HTTP.
type schemeGuard struct {
	base       http.RoundTripper
	permission config.UpdateHTTPPermission
}

func requireScheme(base http.RoundTripper, permission config.UpdateHTTPPermission) http.RoundTripper {
	return &schemeGuard{base: base, permission: permission}
}

func (guard *schemeGuard) RoundTrip(request *http.Request) (*http.Response, error) {
	if err := guard.permission.CheckURL(request.URL.String()); err != nil {
		// A RoundTripper closes the request body, including when it fails.
		if request.Body != nil {
			_ = request.Body.Close()
		}
		return nil, err
	}

	return guard.base.RoundTrip(request)
}
