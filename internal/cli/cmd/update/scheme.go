package updatecmd

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// schemeGuard refuses a request whose URL bb update may not fetch.
//
// It sits on the transport rather than at each call site because every request
// passes through here: the manifest, each asset a manifest names, the address a
// mirror falls back to, and every hop of a redirect, which http.Client follows
// by sending a new request through the same transport. A check on the
// configured base URL alone would let an https mirror redirect to plain HTTP.
type schemeGuard struct {
	base  http.RoundTripper
	check func(rawURL string) error
}

// requireScheme guards the client that talks to the release mirror.
func requireScheme(base http.RoundTripper, permission config.UpdateHTTPPermission) http.RoundTripper {
	return &schemeGuard{base: base, check: permission.CheckURL}
}

// requireTrustHTTPS guards the client that fetches Sigstore trust material,
// which is https only whatever the release mirror is allowed. --allow-http is
// no answer there, so its refusal does not offer it.
func requireTrustHTTPS(base http.RoundTripper) http.RoundTripper {
	return &schemeGuard{base: base, check: func(rawURL string) error {
		parsed, err := url.Parse(rawURL)
		if err != nil {
			return apperrors.New(apperrors.KindValidation, fmt.Sprintf("Sigstore trust material is fetched over https only, and %q is not an https URL", rawURL), err)
		}
		if !strings.EqualFold(parsed.Scheme, "https") || parsed.Host == "" {
			return apperrors.New(apperrors.KindValidation, fmt.Sprintf("Sigstore trust material is fetched over https only, and %q is not an https URL", parsed.Redacted()), nil)
		}
		return nil
	}}
}

func (guard *schemeGuard) RoundTrip(request *http.Request) (*http.Response, error) {
	err := guard.check(request.URL.String())
	if err == nil {
		return guard.base.RoundTrip(request)
	}

	// A RoundTripper closes the request body, including when it fails.
	if request.Body != nil {
		_ = request.Body.Close()
	}

	// A redirect arrives as a new request carrying the response that caused it.
	// Say so: the URL refused is not one anybody configured.
	if request.Response != nil && request.Response.Request != nil {
		return nil, apperrors.New(
			apperrors.KindOf(err),
			fmt.Sprintf("%s redirected to a URL bb update may not fetch: %s", request.Response.Request.URL.Redacted(), apperrors.MessageOf(err)),
			nil,
		)
	}

	return nil, err
}
