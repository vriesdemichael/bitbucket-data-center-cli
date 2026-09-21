package compat

import (
	"context"
	"fmt"
	"sync"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

var (
	releasesMu sync.Mutex
	// releases holds the release of every instance asked about, by the address
	// the client sends to. Only an answer is kept: a failure to ask is asked
	// again, since it may not recur.
	releases = map[string]Release{}
)

// Of returns the release of the instance a client talks to.
//
// It asks once per instance for the life of the process, so a command pays one
// request at most and a long-running MCP server one in total. The calls that
// differ between releases are the only ones that ask.
func Of(ctx context.Context, client *openapigenerated.ClientWithResponses) (Release, error) {
	key := serverOf(client)
	if key != "" {
		releasesMu.Lock()
		release, known := releases[key]
		releasesMu.Unlock()
		if known {
			return release, nil
		}
	}

	response, err := client.GetApplicationPropertiesWithResponse(ctx)
	if err != nil {
		return Release{}, apperrors.Transport("failed to read the Bitbucket version", err)
	}
	if err := openapi.MapStatusError(response.StatusCode(), response.Body); err != nil {
		// Named, because this request is one bb makes on its own: without it a
		// 401 on the version reads as a 401 on whatever the caller asked for.
		return Release{}, fmt.Errorf("failed to read the Bitbucket version: %w", err)
	}
	properties := response.ApplicationjsonCharsetUTF8200
	if properties == nil || properties.Version == nil {
		return Release{}, apperrors.New(apperrors.KindPermanent, "Bitbucket did not report its version", nil)
	}

	release, err := ParseRelease(*properties.Version)
	if err != nil {
		return Release{}, err
	}
	if key != "" {
		releasesMu.Lock()
		releases[key] = release
		releasesMu.Unlock()
	}

	return release, nil
}

// Require refuses when the instance a client talks to lacks the capability,
// with the unsupported error that names the release it needs.
func (difference Difference) Require(ctx context.Context, client *openapigenerated.ClientWithResponses) error {
	release, err := Of(ctx, client)
	if err != nil {
		return err
	}
	if !difference.In(release) {
		return difference.Unsupported(release)
	}

	return nil
}

// LackedBy reports whether the instance a client talks to lacks the
// capability, for a call that adapts rather than refuses.
func (difference Difference) LackedBy(ctx context.Context, client *openapigenerated.ClientWithResponses) (bool, error) {
	release, err := Of(ctx, client)
	if err != nil {
		return false, err
	}

	return !difference.In(release), nil
}

// serverOf is the address a generated client sends to, or "" when it cannot be
// read, which only turns the cache off.
func serverOf(client *openapigenerated.ClientWithResponses) string {
	if client == nil {
		return ""
	}
	if inner, ok := client.ClientInterface.(*openapigenerated.Client); ok {
		return inner.Server
	}

	return ""
}
