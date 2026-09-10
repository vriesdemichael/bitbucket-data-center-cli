//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"
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

	receivedPing := make(chan bool, 1)
	_, target := newContainerReachableReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		select {
		case receivedPing <- true:
		default:
		}
	})

	webhookName := fmt.Sprintf("live-ping-test-%d", time.Now().UnixNano()%100000)
	createOutput, err := executeLiveCLI(t, "--json", "repo", "settings", "workflow", "webhooks", "create",
		webhookName, target, "--event", "repo:refs_changed")
	if err != nil {
		t.Fatalf("create webhook for ping delivery test failed: %v\noutput: %s", err, createOutput)
	}

	webhookID, ok := webhookIDFromCreateOutput(createOutput)
	if !ok {
		t.Fatalf("expected valid webhook ID in create output: %s", createOutput)
	}
	defer func() {
		_, _ = executeLiveCLI(t, "repo", "settings", "workflow", "webhooks", "delete", webhookID)
	}()

	// Execute webhook test ping via bb CLI
	testOutput, err := executeLiveCLI(t, "--json", "webhook", "test", webhookID)
	if err != nil {
		t.Fatalf("webhook test call failed: %v\noutput: %s", err, testOutput)
	}

	// The ping has to arrive. bb reporting a 200 says the instance accepted the
	// request, not that it delivered anything.
	select {
	case <-receivedPing:
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
