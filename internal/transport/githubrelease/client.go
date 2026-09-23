package githubrelease

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/network"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/outcome"
)

const defaultBaseURL = "https://api.github.com"

type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type Release struct {
	TagName string  `json:"tag_name"`
	HTMLURL string  `json:"html_url"`
	Assets  []Asset `json:"assets"`
}

type Client struct {
	baseURL   string
	http      *http.Client
	userAgent string
}

func NewClient(baseURL string, httpClient *http.Client, userAgent string) *Client {
	resolvedBaseURL := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if resolvedBaseURL == "" {
		resolvedBaseURL = defaultBaseURL
	}

	if httpClient == nil {
		transport, err := network.NewSafeTransport(network.TLSOptions{})
		if err != nil {
			transport = &network.SafeTransport{}
		}

		httpClient = &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		}
	}

	return &Client{
		baseURL:   resolvedBaseURL,
		http:      httpClient,
		userAgent: strings.TrimSpace(userAgent),
	}
}

func (client *Client) Latest(ctx context.Context, owner, repo string) (Release, error) {
	if client == nil || client.http == nil {
		return Release{}, apperrors.New(apperrors.KindInternal, "release client is not configured", nil)
	}

	owner = strings.TrimSpace(owner)
	repo = strings.TrimSpace(repo)
	if owner == "" || repo == "" {
		return Release{}, apperrors.New(apperrors.KindValidation, "release repository owner and name are required", nil)
	}

	requestURL := fmt.Sprintf("%s/repos/%s/%s/releases/latest", client.baseURL, owner, repo)

	var release Release
	err := client.do(ctx, http.MethodGet, requestURL, &release)
	if err == nil {
		return release, nil
	}

	// Fallback paths on custom mirrors (e.g. Artifactory / Nexus endpoints),
	// which serve the manifest at the root of a generic repository rather than
	// under GitHub's /repos/{owner}/{repo} layout. A mirror that does not hold
	// the manifest can answer with anything from 404 to 403 or a gateway error,
	// so any failure is worth a second look -- on the default base URL nothing
	// changes. A refusal is not: the fallbacks are on the same mirror, and the
	// same policy refuses them.
	if _, refused := refusal(err); refused || !client.usesMirror() {
		return Release{}, err
	}

	failures := []manifestFailure{{url: requestURL, err: err}}
	for _, fallbackURL := range []string{
		fmt.Sprintf("%s/releases/latest", client.baseURL),
		fmt.Sprintf("%s/latest", client.baseURL),
	} {
		var fallbackRelease Release
		fallbackErr := client.do(ctx, http.MethodGet, fallbackURL, &fallbackRelease)
		if fallbackErr == nil && fallbackRelease.TagName != "" {
			return fallbackRelease, nil
		}
		if fallbackErr == nil {
			fallbackErr = apperrors.New(apperrors.KindPermanent, "the release metadata names no tag_name", nil)
		}
		failures = append(failures, manifestFailure{url: fallbackURL, err: fallbackErr})
	}

	return Release{}, noManifest(failures)
}

// manifestFailure is one address a mirror was asked for its manifest at, and
// what came back.
type manifestFailure struct {
	url string
	err error
}

// noManifest reports every address a mirror was asked for its manifest at.
//
// The first failure is rarely the one that matters. A mirror laid out as a
// generic repository answers 404 at GitHub's /repos path by design, and the
// address that does hold its manifest may have said something worth reading:
// broken JSON, a 403, a gateway error. Reporting only the first called a
// mirror whose manifest was unreadable one that had none (#637). So the kind is
// that of the first failure that is not a 404, and the message names them all.
func noManifest(failures []manifestFailure) error {
	kind := apperrors.KindNotFound
	for _, failure := range failures {
		if failed := apperrors.KindOf(failure.err); failed != apperrors.KindNotFound {
			kind = failed
			break
		}
	}

	// A mirror that wants a login answers every address the same way, and the
	// advice that follows from it is said once, after them all.
	advised := false
	reasons := make([]string, 0, len(failures))
	for _, failure := range failures {
		reason := apperrors.MessageOf(failure.err)
		if short, found := strings.CutSuffix(reason, ", and "+loginAdvice); found {
			reason, advised = short, true
		}
		reasons = append(reasons, fmt.Sprintf("%s: %s", failure.url, reason))
	}

	message := "no release metadata could be read from the mirror: " + strings.Join(reasons, "; ")
	if advised {
		message += "; " + loginAdvice
	}

	return apperrors.New(kind, message, nil)
}

// usesMirror reports whether a release mirror is configured, as opposed to the
// public GitHub API.
func (client *Client) usesMirror() bool {
	return client.baseURL != defaultBaseURL
}

func (client *Client) Download(ctx context.Context, assetURL string) ([]byte, error) {
	if client == nil || client.http == nil {
		return nil, apperrors.New(apperrors.KindInternal, "release client is not configured", nil)
	}

	resolvedURL := strings.TrimSpace(assetURL)
	if resolvedURL == "" {
		return nil, apperrors.New(apperrors.KindValidation, "asset URL is required", nil)
	}

	// A relative asset URL names a file on the mirror, so it resolves against
	// the base URL as a directory. Resolved against the base URL as given, RFC
	// 3986 replaces its last segment: under the hardening guide's own
	// https://artifactory.corp.internal/artifactory/bb-releases,
	// sha256sums.txt became .../artifactory/sha256sums.txt (#637).
	parsed, parseErr := url.Parse(resolvedURL)
	if parseErr == nil && parsed.Scheme == "" {
		if base, baseErr := url.Parse(client.baseURL + "/"); baseErr == nil {
			resolvedURL = base.ResolveReference(parsed).String()
		}
	}

	if !client.usesMirror() || client.hostedOnMirror(resolvedURL) {
		// A URL that already resolves onto the mirror is left alone. A manifest
		// authored for the mirror can point at a path of its own choosing, and
		// second-guessing it with a flattened file name would fetch the wrong
		// object whenever the two disagree.
		return client.fetchAsset(ctx, resolvedURL)
	}

	// An asset URL that points off the mirror is fetched from the mirror, by
	// its file name, and only from there: ADR-059 has every download go
	// through the configured mirror. A manifest mirrored from GitHub still
	// carries github.com asset URLs, which an air-gapped enclave drops rather
	// than refuses, and a mirror that failed used to send bb after them (#637).
	assetName := assetFileName(resolvedURL)
	if assetName == "" {
		return nil, apperrors.New(apperrors.KindPermanent, fmt.Sprintf(
			"release asset URL %s is not on the mirror at %s, and names no file to fetch from it", resolvedURL, client.baseURL), nil)
	}

	mirrorURL := fmt.Sprintf("%s/%s", client.baseURL, assetName)
	body, err := client.fetchAsset(ctx, mirrorURL)
	if err != nil {
		return nil, apperrors.Transport(fmt.Sprintf(
			"failed to download release asset %s from the mirror at %s; the manifest's own address for it, %s, is off the mirror and is not used",
			assetName, mirrorURL, resolvedURL), err)
	}

	return body, nil
}

// hostedOnMirror reports whether an already-resolved asset URL lives under the
// configured mirror base URL.
func (client *Client) hostedOnMirror(resolvedURL string) bool {
	return resolvedURL == client.baseURL || strings.HasPrefix(resolvedURL, client.baseURL+"/")
}

// assetFileName extracts the file name an asset URL ends in, or "" when the URL
// does not end in one.
func assetFileName(assetURL string) string {
	name := path.Base(assetURL)
	if name == "" || name == "." || name == "/" {
		return ""
	}
	return name
}

func (client *Client) fetchAsset(ctx context.Context, resolvedURL string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, resolvedURL, nil)
	if err != nil {
		return nil, apperrors.New(apperrors.KindInternal, "failed to build release download request", err)
	}
	request.Header.Set("Accept", "application/octet-stream")
	if client.userAgent != "" {
		request.Header.Set("User-Agent", client.userAgent)
	}

	tracked, exchange := outcome.Track(request)
	response, err := client.http.Do(tracked)
	if err != nil {
		if refused, ok := refusal(err); ok {
			return nil, refused
		}
		return nil, apperrors.Transport("failed to download release asset", exchange.Classify(err))
	}
	defer func() { _ = response.Body.Close() }()
	exchange.Answered(response.StatusCode)

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, client.statusError(response.StatusCode, "failed to download release asset")
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, apperrors.Transport("failed to read release asset", exchange.ClassifyRead(err))
	}

	return body, nil
}

func (client *Client) do(ctx context.Context, method, requestURL string, out any) error {
	request, err := http.NewRequestWithContext(ctx, method, requestURL, nil)
	if err != nil {
		return apperrors.New(apperrors.KindInternal, "failed to build release metadata request", err)
	}
	request.Header.Set("Accept", "application/json")
	if client.userAgent != "" {
		request.Header.Set("User-Agent", client.userAgent)
	}

	// Classified by the exchange, as every request on the Bitbucket clients
	// is: a certificate the mirror presents is not a failure a retry fixes,
	// and wrapping the bare error reported it as transient, exit 10 (#637).
	tracked, exchange := outcome.Track(request)
	response, err := client.http.Do(tracked)
	if err != nil {
		if refused, ok := refusal(err); ok {
			return refused
		}
		return apperrors.Transport("failed to fetch release metadata", exchange.Classify(err))
	}
	defer func() { _ = response.Body.Close() }()
	exchange.Answered(response.StatusCode)

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return client.statusError(response.StatusCode, "failed to fetch release metadata")
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return apperrors.Transport("failed to read release metadata", exchange.ClassifyRead(err))
	}

	if err := decodeJSON(body, out); err != nil {
		return err
	}

	return nil
}

// refusal returns the error a transport refused a request with, as opposed to
// one it failed with. A URL the updater may not fetch is not a network problem
// to retry, and the url.Error and "failed to download" around it would only
// repeat the URL and the kind.
func refusal(err error) (*apperrors.AppError, bool) {
	var classified *apperrors.AppError
	if errors.As(err, &classified) && (classified.Kind == apperrors.KindValidation || classified.Kind == apperrors.KindAuthorization) {
		return classified, true
	}
	return nil, false
}

func decodeJSON(body []byte, out any) error {
	decoder := jsonDecoder(bytes.NewReader(body))
	if err := decoder.Decode(out); err != nil {
		return apperrors.New(apperrors.KindPermanent, "failed to decode release metadata", err)
	}
	return nil
}

var jsonDecoder = func(reader io.Reader) interface{ Decode(any) error } {
	return json.NewDecoder(reader)
}

// statusError is what an answer outside 2xx means.
//
// A mirror that answers 401 or 403 wants credentials, and bb update sends none.
// That is deliberate: an estate whose artifact server requires a login for every
// download installs bb by its own means rather than through bb update (#637),
// so the message says so instead of a bare status. The public GitHub API
// answers 403 for a rate limit rather than for a login, so only a mirror gets
// that advice.
func (client *Client) statusError(statusCode int, message string) error {
	if client.usesMirror() && (statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden) {
		return apperrors.New(apperrors.KindPermanent, fmt.Sprintf("%s: the mirror answered %d, and %s", message, statusCode, loginAdvice), nil)
	}

	return mapHTTPError(statusCode, message)
}

// loginAdvice is what statusError tells an operator whose mirror wants a login.
const loginAdvice = "bb update sends no credentials: the mirror has to allow anonymous downloads, or bb is installed another way, such as a package manager or the _noupdate build"

func mapHTTPError(statusCode int, message string) error {
	switch {
	case statusCode == http.StatusNotFound:
		return apperrors.New(apperrors.KindNotFound, message, nil)
	case statusCode == http.StatusTooManyRequests || statusCode >= 500:
		return apperrors.New(apperrors.KindTransient, message, nil)
	default:
		return apperrors.New(apperrors.KindPermanent, message, nil)
	}
}
