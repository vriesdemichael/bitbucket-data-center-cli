//go:build live

package live_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/compat"
)

var (
	liveReleaseMu    sync.Mutex
	liveReleaseKnown bool
	liveRelease      compat.Release
)

// release is the Bitbucket release the suite runs against, as the instance
// reports it.
//
// A test whose behaviour differs by release asserts each side of the difference,
// keyed on this and on a boundary the test states itself. Not on the one
// internal/compat holds: a boundary read from the code under test agrees with
// itself wherever it is wrong.
func (h *liveHarness) release(t *testing.T) compat.Release {
	t.Helper()

	liveReleaseMu.Lock()
	defer liveReleaseMu.Unlock()
	if liveReleaseKnown {
		return liveRelease
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	properties, err := h.liveJSON(ctx, http.MethodGet, "/rest/api/latest/application-properties", nil)
	if err != nil {
		t.Fatalf("read the Bitbucket release: %v", err)
	}
	release, err := compat.ParseRelease(asString(properties["version"]))
	if err != nil {
		t.Fatalf("read the Bitbucket release from %v: %v", properties, err)
	}
	liveRelease, liveReleaseKnown = release, true

	return release
}
