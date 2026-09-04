//go:build live

package live_test

import (
	"context"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// The two things the transport claims about Bitbucket rather than about the
// network.
//
// Everything else in the client's unit tests is ours -- retries, backoff,
// whether a mutation is replayed, what a malformed body does -- and none of it
// can be produced by a real server on request. These two can, and were the only
// mocks in that package standing in for Bitbucket rather than for a broken
// connection.
func TestLiveTransportIdentityAndHealth(t *testing.T) {
	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client := httpclient.NewFromConfig(harness.config)

	t.Run("the authenticated account comes back in a response header", func(t *testing.T) {
		// Bitbucket echoes the caller's slug in X-AUSERNAME rather than
		// offering it as a payload, so reading it is cheaper than a lookup.
		// That it is there at all, and carries the account rather than the
		// display name, is a claim only the server settles -- and NeedsWork
		// builds a participant URL out of it, so a wrong answer writes to the
		// wrong person.
		slug, err := client.CurrentUserSlug(ctx)
		if err != nil {
			t.Fatalf("read the current user slug: %v", err)
		}
		if slug != harness.username() {
			t.Fatalf("current user slug = %q, want %q", slug, harness.username())
		}
	})

	t.Run("a working instance is healthy and authenticated", func(t *testing.T) {
		health, err := client.Health(ctx)
		if err != nil {
			t.Fatalf("health check failed: %v", err)
		}
		if !health.Healthy || !health.Authenticated {
			t.Fatalf("health = %+v, want healthy and authenticated", health)
		}
	})

	t.Run("a bad credential is unauthenticated but still reachable", func(t *testing.T) {
		// The distinction the health check exists to draw: "your token is
		// wrong" and "the server is down" are different problems, and telling
		// a user the instance is unreachable when it answered them is the
		// failure worth guarding. Which status Bitbucket picks for a bad
		// credential is its decision, so it is asked rather than assumed.
		badCredential := harness.config
		badCredential.BitbucketToken = "not-a-real-token"
		badCredential.BitbucketUsername = ""
		badCredential.BitbucketPassword = ""

		health, err := httpclient.NewFromConfig(badCredential).Health(ctx)
		if err != nil {
			t.Fatalf("a rejected credential must not fail the health check: %v", err)
		}
		if !health.Healthy {
			t.Errorf("health = %+v, want an instance that answered to count as reachable", health)
		}
		if health.Authenticated {
			t.Errorf("health = %+v, want a rejected credential to read as unauthenticated", health)
		}
	})

	t.Run("nothing answering is not healthy", func(t *testing.T) {
		// The other side of the same distinction, and the only part that needs
		// no Bitbucket: a closed port.
		unreachable := config.AppConfig{BitbucketURL: "http://127.0.0.1:1"}

		if _, err := httpclient.NewFromConfig(unreachable).Health(ctx); err == nil {
			t.Fatal("expected a closed port to fail the health check")
		}
	})
}
