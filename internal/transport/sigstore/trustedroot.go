package sigstore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	sigroot "github.com/sigstore/sigstore-go/pkg/root"
	sigtuf "github.com/sigstore/sigstore-go/pkg/tuf"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// ErrTrustedRootUnavailable marks the failure to obtain Sigstore trust
// material, as opposed to a signature that failed to verify against it.
//
// The distinction matters for diagnostics: the default provider reaches the
// Sigstore TUF CDN, which is exactly the call an air-gapped host cannot make,
// and an admin debugging a mirror needs to be told that rather than being left
// to conclude the signature is bad.
var ErrTrustedRootUnavailable = errors.New("sigstore trust material unavailable")

// FetchTrustedRoot obtains trust material from the public-good Sigstore TUF
// repository. It requires outbound HTTPS to tuf-repo-cdn.sigstore.dev.
func FetchTrustedRoot(context.Context) (sigroot.TrustedMaterial, error) {
	trustedRoot, err := sigroot.FetchTrustedRoot()
	if err != nil {
		return nil, trustedRootError(err)
	}
	return trustedRoot, nil
}

// TrustedRootFromFile reads trust material from a trusted_root.json on disk.
//
// This is the offline path: with it, verification makes no network calls at
// all. SCTs, the Rekor inclusion promise and observer timestamps are all
// checked against keys carried in the file itself.
func TrustedRootFromFile(path string) TrustedMaterialProvider {
	resolvedPath := strings.TrimSpace(path)
	return func(context.Context) (sigroot.TrustedMaterial, error) {
		trustedRoot, err := sigroot.NewTrustedRootFromPath(resolvedPath)
		if err != nil {
			return nil, trustedRootError(fmt.Errorf("reading trusted root %s: %w", resolvedPath, err))
		}
		return trustedRoot, nil
	}
}

// TrustedRootFromTUF reads trust material from a mirrored Sigstore TUF
// repository.
//
// httpClient carries the CA bundle and client certificates resolved for this
// invocation, so a mirror fronted by an internal CA or protected by mTLS is
// reachable — the default TUF fetcher would use http.DefaultTransport and fail
// on both.
func TrustedRootFromTUF(repositoryBaseURL string, httpClient *http.Client) TrustedMaterialProvider {
	resolvedURL := strings.TrimRight(strings.TrimSpace(repositoryBaseURL), "/")
	return func(context.Context) (sigroot.TrustedMaterial, error) {
		options := sigtuf.DefaultOptions().WithRepositoryBaseURL(resolvedURL)
		if httpClient != nil {
			options = options.WithFetcher(&httpFetcher{client: httpClient})
		}

		trustedRoot, err := sigroot.FetchTrustedRootWithOptions(options)
		if err != nil {
			return nil, trustedRootError(fmt.Errorf("fetching trusted root from %s: %w", resolvedURL, err))
		}
		return trustedRoot, nil
	}
}

func trustedRootError(cause error) error {
	return apperrors.New(
		apperrors.KindTransient,
		"failed to load Sigstore trusted roots",
		fmt.Errorf("%w: %w", ErrTrustedRootUnavailable, cause),
	)
}

// httpFetcher adapts an http.Client to the TUF client's fetcher interface.
type httpFetcher struct {
	client *http.Client
}

func (fetcher *httpFetcher) DownloadFile(urlPath string, maxLength int64, _ time.Duration) ([]byte, error) {
	request, err := http.NewRequest(http.MethodGet, urlPath, nil)
	if err != nil {
		return nil, err
	}

	response, err := fetcher.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("downloading %s: unexpected status %d", urlPath, response.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxLength+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxLength {
		return nil, fmt.Errorf("downloading %s: response exceeds %d bytes", urlPath, maxLength)
	}

	return body, nil
}
