//go:build live

package live_test

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"
)

// webhookJSONAttempts and webhookTextAttempts are how many webhooks each create
// path makes, and each update path then updates, per output mode.
//
// Bitbucket's answer to a create carries the shared secret on some calls and
// not on others, and load decides the share: 8 of 200 sequential creates
// against an idle instance carried it, 303 of 800 made eight at a time. A
// command that published the answer passed every attempt whose answer happened
// to carry the secret, so a single attempt proves little. The text rendering
// reads the same webhook as --json, which is why it gets fewer.
const (
	webhookJSONAttempts = 10
	webhookTextAttempts = 3
)

// readBackWebhookURL is the instance's own status page, so nothing these
// webhooks could deliver reaches anything.
const readBackWebhookURL = "http://localhost:7990/status"

// TestLiveWebhookWritesPublishTheSecretTheyStored is the guard on
// `bb webhook create --json` reporting secretConfigured false for a webhook
// created with a shared secret.
//
// The command published Bitbucket's answer to the create, which is not a
// reliable account of the configuration
// (TestLiveWebhookCreateResponseIsNotAReliableSourceForTheSecret). Every write
// here is given a secret, the published webhook has to say one is configured
// -- in both renderings, after the create and after an update that leaves the
// secret alone -- and the API, read directly, has to agree.
func TestLiveWebhookWritesPublishTheSecretTheyStored(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/webhooks", seeded.Key, repo.Slug)

	t.Run("--json", func(t *testing.T) {
		for attempt := range webhookJSONAttempts {
			name := fmt.Sprintf("json-%d", attempt)
			output, err := executeLiveCLIWithStdin(t, secretCanary,
				"--json", "webhook", "create", name, readBackWebhookURL, "--secret-stdin")
			if err != nil {
				t.Fatalf("create %d failed: %v\noutput: %s", attempt, err, output)
			}
			id := expectSecretPublished(t, output, name)
			expectSecretStored(t, ctx, harness, path, id, name)

			renamed := name + "-renamed"
			output, err = executeLiveCLI(t, "--json", "webhook", "update", id, "--name", renamed)
			if err != nil {
				t.Fatalf("update %d failed: %v\noutput: %s", attempt, err, output)
			}
			expectSecretPublished(t, output, renamed)
			expectSecretStored(t, ctx, harness, path, id, renamed)
		}
	})

	t.Run("text", func(t *testing.T) {
		for attempt := range webhookTextAttempts {
			name := fmt.Sprintf("text-%d", attempt)
			output, err := executeLiveCLIWithStdin(t, secretCanary,
				"webhook", "create", name, readBackWebhookURL, "--secret-stdin")
			if err != nil {
				t.Fatalf("create %d failed: %v\noutput: %s", attempt, err, output)
			}
			id := expectSecretShown(t, output, name)
			expectSecretStored(t, ctx, harness, path, id, name)

			renamed := name + "-renamed"
			output, err = executeLiveCLI(t, "webhook", "update", id, "--name", renamed)
			if err != nil {
				t.Fatalf("update %d failed: %v\noutput: %s", attempt, err, output)
			}
			expectSecretShown(t, output, renamed)
			expectSecretStored(t, ctx, harness, path, id, renamed)
		}
	})
}

// TestLiveProjectWebhookWritesPublishTheSecretTheyStored is the same guard on
// `bb project webhook`, which configures the same object through the project
// route and published its answers the same way.
func TestLiveProjectWebhookWritesPublishTheSecretTheyStored(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)
	path := fmt.Sprintf("/rest/api/latest/projects/%s/webhooks", seeded.Key)

	t.Run("--json", func(t *testing.T) {
		for attempt := range webhookJSONAttempts {
			name := fmt.Sprintf("json-%d", attempt)
			output, err := executeLiveCLIWithStdin(t, secretCanary,
				"--json", "project", "webhook", "create", seeded.Key, name, readBackWebhookURL, "--secret-stdin")
			if err != nil {
				t.Fatalf("create %d failed: %v\noutput: %s", attempt, err, output)
			}
			id := expectSecretPublished(t, output, name)
			expectSecretStored(t, ctx, harness, path, id, name)

			renamed := name + "-renamed"
			output, err = executeLiveCLI(t, "--json", "project", "webhook", "update", seeded.Key, id, "--name", renamed)
			if err != nil {
				t.Fatalf("update %d failed: %v\noutput: %s", attempt, err, output)
			}
			expectSecretPublished(t, output, renamed)
			expectSecretStored(t, ctx, harness, path, id, renamed)
		}
	})

	t.Run("text", func(t *testing.T) {
		for attempt := range webhookTextAttempts {
			name := fmt.Sprintf("text-%d", attempt)
			output, err := executeLiveCLIWithStdin(t, secretCanary,
				"project", "webhook", "create", seeded.Key, name, readBackWebhookURL, "--secret-stdin")
			if err != nil {
				t.Fatalf("create %d failed: %v\noutput: %s", attempt, err, output)
			}
			id := expectSecretShown(t, output, name)
			expectSecretStored(t, ctx, harness, path, id, name)

			renamed := name + "-renamed"
			output, err = executeLiveCLI(t, "project", "webhook", "update", seeded.Key, id, "--name", renamed)
			if err != nil {
				t.Fatalf("update %d failed: %v\noutput: %s", attempt, err, output)
			}
			expectSecretShown(t, output, renamed)
			expectSecretStored(t, ctx, harness, path, id, renamed)
		}
	})
}

// TestLiveSettingsWebhookCreatePublishesTheSecretItStored is the guard on the
// third create path, `bb repo settings workflow webhooks create`, which has no
// update beside it.
func TestLiveSettingsWebhookCreatePublishesTheSecretItStored(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)
	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/webhooks", seeded.Key, repo.Slug)

	t.Run("--json", func(t *testing.T) {
		for attempt := range webhookJSONAttempts {
			name := fmt.Sprintf("json-%d", attempt)
			output, err := executeLiveCLIWithStdin(t, secretCanary,
				"--json", "repo", "settings", "workflow", "webhooks", "create", name, readBackWebhookURL, "--secret-stdin")
			if err != nil {
				t.Fatalf("create %d failed: %v\noutput: %s", attempt, err, output)
			}
			expectSecretStored(t, ctx, harness, path, expectSecretPublished(t, output, name), name)
		}
	})

	t.Run("text", func(t *testing.T) {
		for attempt := range webhookTextAttempts {
			name := fmt.Sprintf("text-%d", attempt)
			output, err := executeLiveCLIWithStdin(t, secretCanary,
				"repo", "settings", "workflow", "webhooks", "create", name, readBackWebhookURL, "--secret-stdin")
			if err != nil {
				t.Fatalf("create %d failed: %v\noutput: %s", attempt, err, output)
			}
			expectSecretStored(t, ctx, harness, path, expectSecretShown(t, output, name), name)
		}
	})
}

// expectSecretPublished checks the webhook a --json create or update
// published: named name, with its shared secret reported as configured and not
// written out. It returns the webhook's id.
//
// A secret reported as not configured is an error rather than a fatal one, so a
// failing run says how many of its attempts the answer decided.
func expectSecretPublished(t *testing.T, output, name string) string {
	t.Helper()

	if strings.Contains(output, secretCanary) {
		t.Fatalf("the shared secret was written to the output:\n%s", output)
	}
	hook, ok := decodeJSONMap(t, output)["webhook"].(map[string]any)
	if !ok {
		t.Fatalf("no webhook in the output: %s", output)
	}
	id, ok := numericOrStringID(hook["id"])
	if !ok || hook["name"] != name {
		t.Fatalf("published webhook %v named %v, want an id and the name %s:\n%s", hook["id"], hook["name"], name, output)
	}
	if configured, _ := hook["secretConfigured"].(bool); !configured {
		t.Errorf("webhook %s was written with a shared secret and published secretConfigured %v:\n%s",
			id, hook["secretConfigured"], output)
	}

	return id
}

// expectSecretShown is expectSecretPublished for the text rendering.
func expectSecretShown(t *testing.T, output, name string) string {
	t.Helper()

	if strings.Contains(output, secretCanary) {
		t.Fatalf("the shared secret was written to the output:\n%s", output)
	}
	id, _ := webhookDetailField(output, "ID")
	if shown, _ := webhookDetailField(output, "Name"); id == "" || shown != name {
		t.Fatalf("shown webhook %q named %q, want an id and the name %s:\n%s", id, shown, name, output)
	}
	if configured, _ := webhookDetailField(output, "Shared secret configured"); configured != "true" {
		t.Errorf("webhook %s was written with a shared secret and shown as %q configured:\n%s", id, configured, output)
	}

	return id
}

// expectSecretStored reads a webhook from the API -- not through bb, whose
// output is what is being judged -- and fails unless it is stored under name
// holding the shared secret it was given.
func expectSecretStored(t *testing.T, ctx context.Context, harness *liveHarness, path, id, name string) {
	t.Helper()

	stored, err := harness.liveJSON(ctx, http.MethodGet, path+"/"+id, nil)
	if err != nil {
		t.Fatalf("read webhook %s from the API: %v", id, err)
	}
	configuration, _ := stored["configuration"].(map[string]any)
	if holds := configuration["secret"] == secretCanary; stored["name"] != name || !holds {
		t.Fatalf("webhook %s is stored named %v, holding the secret it was given: %t; want %s, holding it",
			id, stored["name"], holds, name)
	}
}

// terminalEscape is the colour a run on a terminal puts around a label.
var terminalEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// webhookDetailField reads the value of one "Label: value" line of a webhook
// shown as text, and whether there was such a line.
func webhookDetailField(output, label string) (string, bool) {
	for _, line := range strings.Split(terminalEscape.ReplaceAllString(output, ""), "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), label+":"); found {
			return strings.TrimSpace(value), true
		}
	}

	return "", false
}
