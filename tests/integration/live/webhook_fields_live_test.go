//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	neturl "net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// secretCanary and passwordCanary are what a leak looks like when it happens.
//
// Distinct strings, because the two credentials leaked through different paths
// and a single canary could not tell which one a failure had found.
const (
	secretCanary   = "SECRETCANARY7t4h"
	passwordCanary = "PASSWORDCANARY9k2v"
)

// executeLiveCLISplit runs the CLI with stdout and stderr kept apart.
//
// The shared helper merges them, which is fine when the question is what the
// command did. It is not fine when the question is which stream a credential
// warning went to, because merging is precisely the mistake being checked for.
func executeLiveCLISplit(t *testing.T, stdin string, args ...string) (string, string, error) {
	t.Helper()

	command := cli.NewRootCommandWithOverrides(liveCLIOverrides(t))
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetIn(strings.NewReader(stdin))
	command.SetArgs(withLiveRepoContext(t, command, args))

	err := command.Execute()

	return stdout.String(), stderr.String(), err
}

// seedWebhookWithCredentials creates a webhook carrying both credentials,
// through the API rather than through bb, so the test does not depend on the
// code it is checking to have put them there.
//
// It reads them back the same way. Bitbucket answers 201 to a property it
// drops, and a credential it dropped would leave every leak check built on this
// webhook with nothing to find.
func seedWebhookWithCredentials(t *testing.T, ctx context.Context, harness *liveHarness, projectKey, slug, url string) string {
	t.Helper()

	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/webhooks", projectKey, slug)
	created, err := harness.liveJSON(ctx, http.MethodPost, path, map[string]any{
		"name":   "canary",
		"url":    url,
		"events": []string{"repo:refs_changed"},
		// Bitbucket's default, so no read tells it from a drop; the deliveries
		// TestLiveWebhookEndpointPasswordSurvivesAnUpdate waits for are its effect.
		"active":                  true,
		"sslVerificationRequired": false,
		"configuration":           map[string]any{"secret": secretCanary},
		"credentials":             map[string]any{"username": "hookuser", "password": passwordCanary},
	})
	if err != nil {
		t.Fatalf("seed the webhook: %v", err)
	}
	id := fmt.Sprintf("%v", created["id"])

	stored, err := harness.liveJSON(ctx, http.MethodGet, path+"/"+id, nil)
	if err != nil {
		t.Fatalf("read the seeded webhook back: %v", err)
	}
	configuration, _ := stored["configuration"].(map[string]any)
	credentials, _ := stored["credentials"].(map[string]any)
	if stored["name"] != "canary" || stored["url"] != url || !slices.Equal(webhookEventsOf(stored), []string{"repo:refs_changed"}) ||
		stored["sslVerificationRequired"] != false || configuration["secret"] != secretCanary || credentials["username"] != "hookuser" {
		t.Fatalf("the seeded webhook was not stored as sent: %#v", stored)
	}

	// The password never comes back on a read. A test ping's delivery record
	// says what Bitbucket sends, and this ping goes to Bitbucket's own status
	// page, so a receiver the webhook points at sees nothing of it.
	query := neturl.Values{"webhookId": {id}, "url": {"http://localhost:7990/status"}}
	ping, err := harness.liveJSON(ctx, http.MethodPost, path+"/test?"+query.Encode(), map[string]any{})
	if err != nil {
		t.Fatalf("ping the seeded webhook: %v", err)
	}
	request, _ := ping["request"].(map[string]any)
	headers, _ := request["headers"].(map[string]any)
	if want := webhookBasicAuthorization("hookuser", passwordCanary); headers["Authorization"] != want {
		t.Fatalf("a ping from the seeded webhook carried Authorization %v, want the seeded credentials", headers["Authorization"])
	}

	return id
}

// webhookSecretAsStored reads a webhook's shared secret back through bb webhook
// get --reveal-secret, the one read that hands it over.
func webhookSecretAsStored(t *testing.T, id string) string {
	t.Helper()

	stdout, stderr, err := executeLiveCLISplit(t, "", "--json", "webhook", "get", id, "--reveal-secret")
	if err != nil {
		t.Fatalf("webhook get --reveal-secret failed: %v\n%s%s", err, stdout, stderr)
	}
	hook, ok := decodeJSONMap(t, stdout)["webhook"].(map[string]any)
	if !ok {
		t.Fatalf("no webhook in the get output: %s", stdout)
	}
	secret, _ := hook["secret"].(string)

	return secret
}

// webhookCredentialsAsDelivered is the Authorization header a test ping from a
// webhook carries, empty when it carries none.
//
// Bitbucket never returns the endpoint password. The delivery record bb webhook
// test --reveal-secret publishes is where it says what it sends.
func webhookCredentialsAsDelivered(t *testing.T, id string) string {
	t.Helper()

	stdout, stderr, err := executeLiveCLISplit(t, "", "--json", "webhook", "test", id, "--reveal-secret")
	if err != nil {
		t.Fatalf("webhook test --reveal-secret failed: %v\n%s%s", err, stdout, stderr)
	}
	request, _ := decodeJSONMap(t, stdout)["request"].(map[string]any)
	headers, ok := request["headers"].(map[string]any)
	if !ok {
		t.Fatalf("no request headers in the delivery record: %s", stdout)
	}
	authorization, _ := headers["Authorization"].(string)

	return authorization
}

// webhooksNamedInListing returns every webhook with a name in a --json listing.
// Every one, because Bitbucket accepts identical webhooks.
func webhooksNamedInListing(t *testing.T, output, name string) []map[string]any {
	t.Helper()

	named := []map[string]any{}
	for _, hook := range webhooksInListing(t, output) {
		if hook["name"] == name {
			named = append(named, hook)
		}
	}

	return named
}

// TestLiveWebhookCredentialsNeverReachStdout is the guard on #522's real
// finding.
//
// Bitbucket hands the shared secret back in plaintext on every read of a
// webhook, and puts the endpoint's basic-auth credentials in the Authorization
// header of the delivery record `webhook test` publishes. Two bb commands
// forwarded what they were given, so reading a webhook wrote a credential to
// stdout -- under --json, into the machine contract; in CI, into a log.
//
// Checking the base64 form as well as the plaintext one is the point. The first
// version of this check passed against a command that was publishing the
// password, because the password was inside `Basic aG9va3VzZXI6...` and base64
// is not encryption.
func TestLiveWebhookCredentialsNeverReachStdout(t *testing.T) {
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

	id := seedWebhookWithCredentials(t, ctx, harness, seeded.Key, repo.Slug, "http://localhost:7990/status")

	// What the password looks like once Bitbucket has encoded it for the wire.
	encodedCredentials := base64.StdEncoding.EncodeToString([]byte("hookuser:" + passwordCanary))

	for _, readPath := range []struct {
		name string
		args []string
	}{
		{"webhook get", []string{"webhook", "get", id}},
		{"webhook get --json", []string{"--json", "webhook", "get", id}},
		{"webhook list", []string{"webhook", "list"}},
		{"webhook list --json", []string{"--json", "webhook", "list"}},
		{"webhook test --json", []string{"--json", "webhook", "test", id}},
		{"project webhook list --json", []string{"--json", "project", "webhook", "list", seeded.Key}},
		{"repo settings webhooks list", []string{"repo", "settings", "workflow", "webhooks", "list"}},
		{"repo settings webhooks list --json", []string{"--json", "repo", "settings", "workflow", "webhooks", "list"}},
	} {
		t.Run(readPath.name, func(t *testing.T) {
			stdout, _, err := executeLiveCLISplit(t, "", readPath.args...)
			if err != nil {
				t.Fatalf("%s failed: %v\noutput: %s", readPath.name, err, stdout)
			}
			if strings.Contains(stdout, secretCanary) {
				t.Errorf("the shared secret was written to stdout:\n%s", stdout)
			}
			if strings.Contains(stdout, passwordCanary) {
				t.Errorf("the endpoint password was written to stdout:\n%s", stdout)
			}
			if strings.Contains(stdout, encodedCredentials) {
				t.Errorf("the endpoint password was written to stdout base64-encoded:\n%s", stdout)
			}
		})
	}

	// The read paths still have to say a secret is there, or redacting them
	// would have made the commands useless rather than safe.
	t.Run("a configured secret is still reported as configured", func(t *testing.T) {
		output := mustLiveCLI(t, "--json", "webhook", "get", id)
		hook, _ := decodeJSONMap(t, output)["webhook"].(map[string]any)
		if configured, _ := hook["secretConfigured"].(bool); !configured {
			t.Errorf("secretConfigured is false for a webhook that has one: %s", output)
		}
		if username, _ := hook["credentialsUsername"].(string); username != "hookuser" {
			t.Errorf("credentialsUsername = %q, want hookuser: %s", username, output)
		}
	})
}

// TestLiveWebhookRevealSecretIsDeliberate covers the escape hatch.
//
// Redacting by default only works if there is a way to get the value back --
// otherwise an operator who lost the secret has to go to the database. The
// requirement is that recovering it is an act: a flag that has to be typed, and
// a warning on stderr saying a credential just went through stdout.
func TestLiveWebhookRevealSecretIsDeliberate(t *testing.T) {
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

	id := seedWebhookWithCredentials(t, ctx, harness, seeded.Key, repo.Slug, "http://localhost:7990/status")

	t.Run("the shared secret comes back when it is asked for", func(t *testing.T) {
		stdout, stderr, err := executeLiveCLISplit(t, "", "--json", "webhook", "get", id, "--reveal-secret")
		if err != nil {
			t.Fatalf("webhook get --reveal-secret failed: %v\n%s", err, stdout)
		}
		hook, _ := decodeJSONMap(t, stdout)["webhook"].(map[string]any)
		if secret, _ := hook["secret"].(string); secret != secretCanary {
			t.Errorf("secret = %q, want the configured one: %s", secret, stdout)
		}
		// The warning is the audit trail. On stderr, because stdout is the
		// machine contract and prose there makes the envelope unparseable.
		if !strings.Contains(stderr, "--reveal-secret") {
			t.Errorf("nothing on stderr said a credential had been printed: %q", stderr)
		}
		if strings.Contains(stderr, secretCanary) {
			t.Errorf("the warning repeated the secret it was warning about: %q", stderr)
		}
	})

	t.Run("the endpoint credentials come back when they are asked for", func(t *testing.T) {
		encoded := base64.StdEncoding.EncodeToString([]byte("hookuser:" + passwordCanary))
		stdout, stderr, err := executeLiveCLISplit(t, "", "--json", "webhook", "test", id, "--reveal-secret")
		if err != nil {
			t.Fatalf("webhook test --reveal-secret failed: %v\n%s", err, stdout)
		}
		if !strings.Contains(stdout, encoded) {
			t.Errorf("the delivery record was still redacted with --reveal-secret:\n%s", stdout)
		}
		request, _ := decodeJSONMap(t, stdout)["request"].(map[string]any)
		headers, _ := request["headers"].(map[string]any)
		if headers["Authorization"] != "Basic "+encoded {
			t.Errorf("the delivery record's Authorization = %v, want the stored credentials:\n%s", headers["Authorization"], stdout)
		}
		if !strings.Contains(stderr, "--reveal-secret") {
			t.Errorf("nothing on stderr said a credential had been printed: %q", stderr)
		}
	})
}

// TestLiveWebhookFieldsAreSettableAndPublished walks the four fields bb could
// not set through a create, a read, an update and a removal.
//
// One test rather than four because the interesting part is the sequence: the
// update endpoint replaces the whole webhook, so what proves the read-modify-
// write is that changing one field leaves the others where they were.
func TestLiveWebhookFieldsAreSettableAndPublished(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	name := testsupport.UniqueName("live-fields-")

	// The secret on stdin, the endpoint password in the environment: the two
	// routes ADR-047 leaves open, and the combination automation actually needs
	// because there is only one stdin.
	t.Setenv("BB_WEBHOOK_PASSWORD", passwordCanary)
	createOutput, _, err := executeLiveCLISplit(t, secretCanary,
		"--json", "webhook", "create", name, "http://localhost:7990/status",
		"--ssl-verification=false", "--secret-stdin", "--credentials-username", "hookuser")
	if err != nil {
		t.Fatalf("webhook create failed: %v\noutput: %s", err, createOutput)
	}

	created, _ := decodeJSONMap(t, createOutput)["webhook"].(map[string]any)
	id := fmt.Sprintf("%d", int(created["id"].(float64)))
	defer func() {
		_, _ = executeLiveCLI(t, "webhook", "delete", id, "--yes")
	}()

	readBack := func(t *testing.T) map[string]any {
		t.Helper()
		output := mustLiveCLI(t, "--json", "webhook", "get", id)
		hook, ok := decodeJSONMap(t, output)["webhook"].(map[string]any)
		if !ok {
			t.Fatalf("no webhook in the output: %s", output)
		}

		return hook
	}

	t.Run("create sets all four", func(t *testing.T) {
		hook := readBack(t)
		if verification, ok := hook["sslVerificationRequired"].(bool); !ok || verification {
			t.Errorf("sslVerificationRequired = %v, want false: %#v", hook["sslVerificationRequired"], hook)
		}
		if scope, _ := hook["scopeType"].(string); scope != "repository" {
			t.Errorf("scopeType = %q, want repository", scope)
		}
		if configured, _ := hook["secretConfigured"].(bool); !configured {
			t.Error("secretConfigured is false after creating with --secret-stdin")
		}
		if username, _ := hook["credentialsUsername"].(string); username != "hookuser" {
			t.Errorf("credentialsUsername = %q, want hookuser", username)
		}
		expectWebhookStoredAsSent(t, hook,
			sentWebhook{name: name, url: "http://localhost:7990/status", events: []string{"repo:refs_changed"}})
		// The read above says whether a secret is there. Which secret, and the
		// password no read returns, come from the two paths that hand a
		// credential over when asked.
		if secret := webhookSecretAsStored(t, id); secret != secretCanary {
			t.Errorf("secret = %q, want the one given on stdin", secret)
		}
		if delivered := webhookCredentialsAsDelivered(t, id); delivered != webhookBasicAuthorization("hookuser", passwordCanary) {
			t.Errorf("a test ping carried Authorization %q, want hookuser and the password from BB_WEBHOOK_PASSWORD", delivered)
		}
	})

	t.Run("an update that mentions one field leaves the others alone", func(t *testing.T) {
		// The shape of defect this release is full of: the endpoint replaces
		// the object, so a partial update is only partial because bb reads
		// first. Bitbucket clears the secret outright when an update arrives
		// without a configuration object.
		if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--ssl-verification=true"); err != nil {
			t.Fatalf("webhook update failed: %v\noutput: %s", err, output)
		}

		hook := readBack(t)
		if verification, _ := hook["sslVerificationRequired"].(bool); !verification {
			t.Error("sslVerificationRequired was not changed to true")
		}
		if configured, _ := hook["secretConfigured"].(bool); !configured {
			t.Error("the shared secret was lost by an update that did not mention it")
		}
		if username, _ := hook["credentialsUsername"].(string); username != "hookuser" {
			t.Errorf("the endpoint credentials were lost by an update that did not mention them: %q", username)
		}
		if secret := webhookSecretAsStored(t, id); secret != secretCanary {
			t.Errorf("an update that did not mention the shared secret left %q", secret)
		}
		if delivered := webhookCredentialsAsDelivered(t, id); delivered != webhookBasicAuthorization("hookuser", passwordCanary) {
			t.Errorf("a test ping carried Authorization %q after an update that did not mention the password", delivered)
		}

		// And back to false. True is what Bitbucket stores when an update
		// leaves the field out, so only false shows the update carried it.
		if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--ssl-verification=false"); err != nil {
			t.Fatalf("webhook update --ssl-verification=false failed: %v\noutput: %s", err, output)
		}
		if verification, ok := readBack(t)["sslVerificationRequired"].(bool); !ok || verification {
			t.Error("sslVerificationRequired was not changed back to false")
		}
	})

	t.Run("the endpoint username can be changed on its own", func(t *testing.T) {
		// Without a password, which is the only form available for an edit:
		// Bitbucket never returns the password, so bb cannot re-send one it
		// was not given.
		if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--credentials-username", "otheruser"); err != nil {
			t.Fatalf("webhook update --credentials-username failed: %v\noutput: %s", err, output)
		}
		if username, _ := readBack(t)["credentialsUsername"].(string); username != "otheruser" {
			t.Errorf("credentialsUsername = %q, want otheruser", username)
		}
		// On its own: the password stays, and a delivery is the only place it shows.
		if delivered := webhookCredentialsAsDelivered(t, id); delivered != webhookBasicAuthorization("otheruser", passwordCanary) {
			t.Errorf("a test ping carried Authorization %q, want otheruser with the password it already had", delivered)
		}
	})

	t.Run("removing the endpoint credentials takes a flag of its own", func(t *testing.T) {
		if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--no-credentials"); err != nil {
			t.Fatalf("webhook update --no-credentials failed: %v\noutput: %s", err, output)
		}
		if username, _ := readBack(t)["credentialsUsername"].(string); username != "" {
			t.Errorf("--no-credentials left credentials in place: %q", username)
		}
		// The password with them, or deliveries would still authenticate.
		if delivered := webhookCredentialsAsDelivered(t, id); delivered != "" {
			t.Errorf("a test ping still carried Authorization %q after --no-credentials", delivered)
		}
	})

	t.Run("removing a secret takes a flag of its own", func(t *testing.T) {
		if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--no-secret"); err != nil {
			t.Fatalf("webhook update --no-secret failed: %v\noutput: %s", err, output)
		}
		if configured, _ := readBack(t)["secretConfigured"].(bool); configured {
			t.Error("--no-secret left the shared secret in place")
		}
	})

	t.Run("setting and removing the same thing is refused", func(t *testing.T) {
		_, _, err := executeLiveCLISplit(t, secretCanary,
			"--json", "webhook", "update", id, "--no-secret", "--secret-stdin")
		if err == nil {
			t.Error("--no-secret with --secret-stdin was accepted; one of the two has to win and neither should")
		}
		// Refused rather than sent: the secret removed above is still absent.
		if configured, _ := readBack(t)["secretConfigured"].(bool); configured {
			t.Error("the refused update set the shared secret anyway")
		}
	})

	t.Run("two secrets cannot share one stdin", func(t *testing.T) {
		_, _, err := executeLiveCLISplit(t, secretCanary,
			"--json", "webhook", "update", id, "--secret-stdin", "--credentials-password-stdin")
		if err == nil {
			t.Error("both --*-stdin flags were accepted, and there is only one stdin")
		}
		if configured, _ := readBack(t)["secretConfigured"].(bool); configured {
			t.Error("the refused update set the shared secret anyway")
		}
	})
}

// TestLiveWebhookDryRunNamesTheSecretWithoutPrintingIt checks the preview.
//
// A dry run is written to stdout and is the thing an operator pastes into a
// ticket when it looks wrong, so it is exactly where a secret must not appear.
// What it has to say instead is where the value will come from, because the
// mistake a plan makes is reading the wrong variable.
func TestLiveWebhookDryRunNamesTheSecretWithoutPrintingIt(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	t.Setenv("BB_WEBHOOK_SECRET", secretCanary)
	output, _, err := executeLiveCLISplit(t, "",
		"--json", "--dry-run", "webhook", "create", "preview", "http://localhost:7990/status")
	if err != nil {
		t.Fatalf("dry-run create failed: %v\noutput: %s", err, output)
	}

	if strings.Contains(output, secretCanary) {
		t.Errorf("the dry run printed the secret it was going to set:\n%s", output)
	}
	if !strings.Contains(output, "BB_WEBHOOK_SECRET") {
		t.Errorf("the dry run did not say which variable the secret would come from:\n%s", output)
	}

	// And the preview was a preview: nothing was created.
	listing := mustLiveCLI(t, "--json", "webhook", "list")
	if strings.Contains(listing, "preview") {
		t.Errorf("the dry run created the webhook:\n%s", listing)
	}
	// The repository is this test's own and had no webhook before the dry run.
	if hooks := webhooksInListing(t, listing); len(hooks) != 0 {
		t.Errorf("the repository holds %d webhooks after a dry run: %v", len(hooks), hooks)
	}
}

// TestLiveWebhookEndpointPasswordSurvivesAnUpdate answers a question the API
// cannot be asked directly.
//
// Bitbucket never returns the endpoint password, so bb's read-modify-write
// cannot carry it forward the way it carries the shared secret; it sends the
// credentials object back with the username alone. Whether the stored password
// survives that is not visible in any response -- the only place it shows is on
// the wire, in the Authorization header of a real delivery.
//
// So this stands up a listener the container can reach, pushes twice, and reads
// the header. It is the difference between "bb probably preserves it" and
// knowing.
func TestLiveWebhookEndpointPasswordSurvivesAnUpdate(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	// Each delivery with the branch its push created, so a push reads the
	// delivery it caused. Taking whichever came next read one push's
	// credentials as another's whenever Bitbucket delivered an event twice.
	type delivery struct {
		authorization string
		branches      []string
	}
	delivered := make(chan delivery, 16)
	_, target := newContainerReachableReceiver(t, func(w http.ResponseWriter, r *http.Request) {
		var event struct {
			Changes []struct {
				Ref struct {
					DisplayID string `json:"displayId"`
				} `json:"ref"`
			} `json:"changes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&event)

		arrived := delivery{authorization: r.Header.Get("Authorization")}
		for _, change := range event.Changes {
			arrived.branches = append(arrived.branches, change.Ref.DisplayID)
		}

		select {
		case delivered <- arrived:
		default:
		}
		w.WriteHeader(http.StatusOK)
	})
	id := seedWebhookWithCredentials(t, ctx, harness, seeded.Key, repo.Slug, target)

	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("hookuser:"+passwordCanary))

	push := func(t *testing.T, branch, file string) string {
		t.Helper()
		if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, file, "x\n"); err != nil {
			t.Fatalf("push %s: %v", branch, err)
		}

		deadline := time.After(30 * time.Second)
		others := []string{}
		for {
			select {
			case arrived := <-delivered:
				if slices.Contains(arrived.branches, branch) {
					return arrived.authorization
				}
				// Another push's event, delivered again or late: not this
				// push's answer.
				others = append(others, strings.Join(arrived.branches, ","))
			case <-deadline:
				if len(others) > 0 {
					t.Fatalf("no delivery for %s arrived within 30s, though deliveries for %q did", branch, others)
				}

				// A failure, not a skip. This used to skip, so an instance that
				// could not reach the host produced a green run with the
				// assertion never made -- which is what it did on every CI run
				// until the extra_hosts entry in docker/compose.yml gave the
				// container a route back. An undeliverable webhook is now the
				// test's answer.
				t.Fatalf(
					"no delivery arrived at %s within 30s. The instance could not reach the receiver: check that "+
						"docker/compose.yml still maps host.docker.internal and that the listener is bound where the "+
						"container can reach it (webhookReceiverAddress).",
					target,
				)

				return ""
			}
		}
	}

	if header := push(t, "creds/before", "before.txt"); header != expected {
		t.Fatalf("Authorization before the update = %q, want the seeded credentials", header)
	}

	if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--name", "renamed"); err != nil {
		t.Fatalf("webhook update failed: %v\noutput: %s", err, output)
	}
	if renamed := webhookAsStored(t, id)["name"]; renamed != "renamed" {
		t.Errorf("name = %v after the update, want renamed", renamed)
	}

	if header := push(t, "creds/after", "after.txt"); header != expected {
		t.Errorf("Authorization after the update = %q, want the seeded credentials; "+
			"the update dropped the endpoint password", header)
	}

	// And the case that actually reaches the credential-merging code: an
	// update that names the username without a password. bb sends the
	// credentials object back with the username alone, which is all it can do
	// -- and the delivery is where it shows that Bitbucket keeps the password
	// it already had. Another username, so the delivery also shows the update
	// arrived: the same one would read the same if it had not.
	if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--credentials-username", "otheruser"); err != nil {
		t.Fatalf("webhook update --credentials-username failed: %v\noutput: %s", err, output)
	}
	if username := webhookAsStored(t, id)["credentialsUsername"]; username != "otheruser" {
		t.Errorf("credentialsUsername = %v after the update, want otheruser", username)
	}

	if header := push(t, "creds/username-only", "username-only.txt"); header != webhookBasicAuthorization("otheruser", passwordCanary) {
		t.Errorf("Authorization after a username-only credentials update = %q, want otheruser with the seeded password; "+
			"naming the username dropped the password that went with it", header)
	}
}

// TestLiveWebhookCreateResponseIsNotAReliableSourceForTheSecret records why
// whether a webhook has a shared secret is a question for a read, never for the
// answer to a create.
//
// Bitbucket answers identical creates inconsistently: some carry
// configuration.secret in full, some carry an empty object. Every read carries
// it. Ten attempts, because two produced both shapes.
//
// It is a race, and load decides it. Bitbucket serialises the create response
// while the configuration it echoes is still being changed: 200 sequential
// creates against an idle instance echoed the secret 8 times, 800 creates eight
// at a time 303 times, and 6,400 thirty-two at a time 5,539 times. Once in a
// while the serialiser trips over the change outright and the create answers
// 400 with a ConcurrentModificationException through
// RestWebhook["configuration"] -- seen once in CI, not reproduced in those
// 7,400 creates. That is the same race lost, not a failed create: the response
// is written after the webhook is stored, so the read below still has to find
// it.
func TestLiveWebhookCreateResponseIsNotAReliableSourceForTheSecret(t *testing.T) {
	t.Parallel()

	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedIsolatedProject(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]

	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/webhooks", seeded.Key, repo.Slug)
	echoed, empty, raced := 0, 0, 0
	for attempt := range 10 {
		name := fmt.Sprintf("echo-probe-%d", attempt)
		created, err := harness.liveJSON(ctx, http.MethodPost, path, map[string]any{
			"name":   name,
			"url":    "http://localhost:7990/status",
			"events": []string{"repo:refs_changed"},
			// Bitbucket's default, so no read tells it from a drop; nothing here depends on it.
			"active":        true,
			"configuration": map[string]any{"secret": secretCanary},
		})
		switch {
		case err == nil:
			configuration, _ := created["configuration"].(map[string]any)
			if secret, _ := configuration["secret"].(string); secret != "" {
				echoed++
			} else {
				empty++
			}
		case strings.Contains(err.Error(), "ConcurrentModificationException"):
			raced++
			created = liveWebhookNamed(t, ctx, harness, path, name)
		default:
			t.Fatalf("create %d: %v", attempt, err)
		}

		// The read, by contrast, always answers.
		got, err := harness.liveJSON(ctx, http.MethodGet, fmt.Sprintf("%s/%v", path, created["id"]), nil)
		if err != nil {
			t.Fatalf("get %d: %v", attempt, err)
		}
		readConfiguration, _ := got["configuration"].(map[string]any)
		if secret, _ := readConfiguration["secret"].(string); secret != secretCanary {
			t.Fatalf("a read did not return the secret it was created with, so the "+
				"asymmetry this test records has changed: %#v", got["configuration"])
		}
		expectWebhookStoredAsSent(t, got,
			sentWebhook{name: name, url: "http://localhost:7990/status", events: []string{"repo:refs_changed"}})
	}

	if echoed == 0 && empty == 0 && raced == 0 {
		t.Fatal("no creates were observed at all")
	}
	t.Logf("create responses carrying the secret: %d, without it: %d, lost to the race: %d", echoed, empty, raced)
}

// liveWebhookNamed finds a webhook by name, for a create whose response was
// lost to Bitbucket's serialisation race. Not finding it fails the test: that
// would mean the race can lose the webhook too, which is not what was observed.
func liveWebhookNamed(t *testing.T, ctx context.Context, harness *liveHarness, path, name string) map[string]any {
	t.Helper()

	listing, err := harness.liveJSON(ctx, http.MethodGet, path+"?limit=1000", nil)
	if err != nil {
		t.Fatalf("list webhooks to find %s after its create lost the response: %v", name, err)
	}
	values, _ := listing["values"].([]any)
	for _, value := range values {
		if hook, ok := value.(map[string]any); ok && hook["name"] == name {
			return hook
		}
	}

	t.Fatalf("a create answered with a ConcurrentModificationException and %s does not exist, "+
		"so the race loses webhooks and not only their responses", name)

	return nil
}

// TestLiveWebhookListingsAreUsable covers the two listing defects #522 collected
// while the fields were being counted.
func TestLiveWebhookListingsAreUsable(t *testing.T) {
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

	names := make([]string, 0, 2)
	for index := range 2 {
		name := fmt.Sprintf("listing-%d-%s", index, testsupport.UniqueSuffix())
		if output, err := executeLiveCLI(t, "--json", "webhook", "create", name, "http://localhost:7990/status"); err != nil {
			t.Fatalf("create webhook %d failed: %v\noutput: %s", index, err, output)
		}
		names = append(names, name)
	}

	// Both stored as created, before either listing is judged on them.
	created := mustLiveCLI(t, "webhook", "list", "--limit", "50")
	for _, name := range names {
		hooks := webhooksNamedInListing(t, created, name)
		if len(hooks) != 1 {
			t.Fatalf("%d webhooks named %s, want 1:\n%s", len(hooks), name, created)
		}
		expectWebhookStoredAsSent(t, hooks[0],
			sentWebhook{name: name, url: "http://localhost:7990/status", events: []string{"repo:refs_changed"}})
	}

	t.Run("a truncated listing says so", func(t *testing.T) {
		// The listing paged correctly and published a bare envelope, so a
		// caller reading meta could not tell a complete answer from a cut one.
		output := mustLiveCLI(t, "--json", "webhook", "list", "--limit", "1")
		if !strings.Contains(output, `"limitReached": true`) {
			t.Errorf("a listing cut to one of two carried no meta.limitReached:\n%s", output)
		}

		full := mustLiveCLI(t, "--json", "webhook", "list", "--limit", "50")
		if !strings.Contains(full, `"limitReached": false`) {
			t.Errorf("a complete listing did not say so:\n%s", full)
		}

		// A person reading the text listing has the same question, and is
		// answered on stderr. executeLiveCLI rather than mustLiveCLI, which
		// adds --json.
		if text, err := executeLiveCLI(t, "webhook", "list", "--limit", "1"); err != nil || !strings.Contains(text, "Stopped at the limit of 1") {
			t.Errorf("a text listing cut to one of two did not say so (err: %v):\n%s", err, text)
		}
		if text, err := executeLiveCLI(t, "webhook", "list", "--limit", "50"); err != nil || strings.Contains(text, "Stopped at the limit") {
			t.Errorf("a complete text listing said it was cut (err: %v):\n%s", err, text)
		}
	})

	t.Run("the settings listing renders the webhooks rather than counting them", func(t *testing.T) {
		// It printed "Webhooks configured: 2" while --json returned both, so
		// the id that `webhooks delete` takes could not be obtained from the
		// command that lists them.
		output := mustLiveCLI(t, "repo", "settings", "workflow", "webhooks", "list")
		if !strings.Contains(output, "listing-0") || !strings.Contains(output, "listing-1") {
			t.Errorf("the listing did not name the webhooks:\n%s", output)
		}
		if strings.Contains(output, "Webhooks configured:") {
			t.Errorf("the listing still answers with a count:\n%s", output)
		}
	})
}
