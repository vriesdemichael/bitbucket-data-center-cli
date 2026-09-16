//go:build live

package live_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestLiveWebhookRealPingDelivery(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedRepo(ctx, repoSeed{})
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	pings, target := newWebhookPingReceiver(t)
	storedURL := target + "/ping"

	// Not bb's default event, so an --event that never arrived cannot read back
	// as one that did, and a username for the ping to carry (expectWebhookPing).
	webhookName := testsupport.UniqueName("live-ping-test-")
	createOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "create",
		webhookName, storedURL, "--event", "pr:opened", "--credentials-username", "pinguser")
	if err != nil {
		t.Fatalf("create webhook for ping delivery test failed: %v\noutput: %s", err, createOutput)
	}

	webhookID, ok := webhookIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected valid webhook ID in create output: %s", createOutput)
	}
	defer func() {
		_, _ = executeLiveCLI(t, "repo", "settings", "workflow", "webhooks", "delete", webhookID, "--yes")
	}()

	created := webhookAsStored(t, webhookID)
	expectWebhookStoredAsSent(t, created, sentWebhook{name: webhookName, url: storedURL, events: []string{"pr:opened"}})
	if created["credentialsUsername"] != "pinguser" {
		t.Errorf("credentialsUsername = %v, want pinguser", created["credentialsUsername"])
	}

	// Execute webhook test ping via bb CLI
	testOutput, err := executeLiveCLI(t, "--json", "webhook", "test", webhookID)
	if err != nil {
		t.Fatalf("webhook test call failed: %v\noutput: %s", err, testOutput)
	}

	// The ping has to arrive. bb reporting a 200 says the instance accepted the
	// request, not that it delivered anything.
	select {
	case ping := <-pings:
		expectWebhookPing(t, ping, "/ping", "pinguser")
	case <-time.After(30 * time.Second):
		// The assertion, where there used to be none. Both branches of this
		// select logged and returned, so the test named RealPingDelivery
		// passed whether or not a ping was ever delivered -- and it never was,
		// because the webhook was registered against the listener's own
		// 127.0.0.1 URL, which inside the container is the container.
		t.Fatalf(
			"bb reported the test ping succeeded, but nothing arrived at %s within 30s. "+
				"The instance could not reach the receiver: check that docker/compose.yml still maps "+
				"host.docker.internal and see webhookReceiverAddress.\noutput: %s",
			target, testOutput,
		)
	}
}

// webhookPing is what a receiver saw of one delivery: where it was sent, and
// the credentials it carried.
type webhookPing struct {
	path          string
	authorization string
}

// newWebhookPingReceiver starts a receiver the instance can deliver to and
// reports every delivery, for a test that has to see where a ping went.
func newWebhookPingReceiver(t *testing.T) (<-chan webhookPing, string) {
	t.Helper()

	pings := make(chan webhookPing, 8)
	_, target := newContainerReachableReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		select {
		case pings <- webhookPing{path: r.URL.Path, authorization: r.Header.Get("Authorization")}:
		default:
		}
	})

	return pings, target
}

// awaitWebhookPing waits for the next delivery. None arriving fails the test:
// a receiver the instance cannot reach is an answer, not a reason to skip.
func awaitWebhookPing(t *testing.T, pings <-chan webhookPing, target string) webhookPing {
	t.Helper()

	select {
	case ping := <-pings:
		return ping
	case <-time.After(30 * time.Second):
		t.Fatalf("nothing arrived at %s within 30s. The instance could not reach the receiver: check that "+
			"docker/compose.yml still maps host.docker.internal and see webhookReceiverAddress.", target)

		return webhookPing{}
	}
}

// expectWebhookPing checks where a test ping was delivered and that it carried
// the tested webhook's username.
//
// The username is what shows Bitbucket was told which webhook it tested: a test
// sent with a url alone is delivered too, with no credentials at all.
func expectWebhookPing(t *testing.T, ping webhookPing, path, username string) {
	t.Helper()

	if ping.path != path {
		t.Errorf("the ping was delivered to %s, want %s", ping.path, path)
	}
	if want := webhookBasicAuthorization(username, ""); ping.authorization != want {
		t.Errorf("the ping carried Authorization %q, want %q: Bitbucket did not test the webhook bb named",
			ping.authorization, want)
	}
}

// webhookBasicAuthorization is the header Bitbucket sends for a webhook's
// endpoint credentials.
func webhookBasicAuthorization(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}
