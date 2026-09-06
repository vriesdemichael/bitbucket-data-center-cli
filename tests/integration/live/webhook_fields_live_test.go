//go:build live

package live_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
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

	command := cli.NewRootCommand()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	command.SetOut(stdout)
	command.SetErr(stderr)
	command.SetIn(strings.NewReader(stdin))
	command.SetArgs(args)

	err := command.Execute()

	return stdout.String(), stderr.String(), err
}

// seedWebhookWithCredentials creates a webhook carrying both credentials,
// through the API rather than through bb, so the test does not depend on the
// code it is checking to have put them there.
func seedWebhookWithCredentials(t *testing.T, ctx context.Context, harness *liveHarness, projectKey, slug, url string) string {
	t.Helper()

	path := fmt.Sprintf("/rest/api/latest/projects/%s/repos/%s/webhooks", projectKey, slug)
	created, err := harness.liveJSON(ctx, http.MethodPost, path, map[string]any{
		"name":                    "canary",
		"url":                     url,
		"events":                  []string{"repo:refs_changed"},
		"active":                  true,
		"sslVerificationRequired": false,
		"configuration":           map[string]any{"secret": secretCanary},
		"credentials":             map[string]any{"username": "hookuser", "password": passwordCanary},
	})
	if err != nil {
		t.Fatalf("seed the webhook: %v", err)
	}

	return fmt.Sprintf("%v", created["id"])
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
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	name := fmt.Sprintf("live-fields-%d", time.Now().UnixNano()%100000)

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
		_, _ = executeLiveCLI(t, "webhook", "delete", id)
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
	})

	t.Run("removing the endpoint credentials takes a flag of its own", func(t *testing.T) {
		if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--no-credentials"); err != nil {
			t.Fatalf("webhook update --no-credentials failed: %v\noutput: %s", err, output)
		}
		if username, _ := readBack(t)["credentialsUsername"].(string); username != "" {
			t.Errorf("--no-credentials left credentials in place: %q", username)
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
	})

	t.Run("two secrets cannot share one stdin", func(t *testing.T) {
		_, _, err := executeLiveCLISplit(t, secretCanary,
			"--json", "webhook", "update", id, "--secret-stdin", "--credentials-password-stdin")
		if err == nil {
			t.Error("both --*-stdin flags were accepted, and there is only one stdin")
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

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
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
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	delivered := make(chan string, 8)
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	receiver := &httptest.Server{
		Listener: listener,
		Config: &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case delivered <- r.Header.Get("Authorization"):
			default:
			}
			w.WriteHeader(http.StatusOK)
		})},
	}
	receiver.Start()
	defer receiver.Close()

	// The instance runs in a container, so it reaches the host by name.
	target := fmt.Sprintf("http://host.docker.internal:%d/hook", listener.Addr().(*net.TCPAddr).Port)
	id := seedWebhookWithCredentials(t, ctx, harness, seeded.Key, repo.Slug, target)

	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte("hookuser:"+passwordCanary))

	push := func(t *testing.T, branch, file string) string {
		t.Helper()
		if err := harness.pushFileOnBranch(seeded.Key, repo.Slug, branch, file, "x\n"); err != nil {
			t.Fatalf("push %s: %v", branch, err)
		}
		select {
		case header := <-delivered:
			return header
		case <-time.After(30 * time.Second):
			t.Skipf("no delivery arrived at %s; this host cannot receive from the instance", target)
			return ""
		}
	}

	if header := push(t, "creds/before", "before.txt"); header != expected {
		t.Fatalf("Authorization before the update = %q, want the seeded credentials", header)
	}

	if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--name", "renamed"); err != nil {
		t.Fatalf("webhook update failed: %v\noutput: %s", err, output)
	}

	if header := push(t, "creds/after", "after.txt"); header != expected {
		t.Errorf("Authorization after the update = %q, want the seeded credentials; "+
			"the update dropped the endpoint password", header)
	}

	// And the case that actually reaches the credential-merging code: an
	// update that names the username without a password. bb sends the
	// credentials object back with the username alone, which is all it can do
	// -- and the delivery is where it shows that Bitbucket keeps the password
	// it already had.
	if output, err := executeLiveCLI(t, "--json", "webhook", "update", id, "--credentials-username", "hookuser"); err != nil {
		t.Fatalf("webhook update --credentials-username failed: %v\noutput: %s", err, output)
	}

	if header := push(t, "creds/username-only", "username-only.txt"); header != expected {
		t.Errorf("Authorization after a username-only credentials update = %q, want the seeded credentials; "+
			"naming the username dropped the password that went with it", header)
	}
}

// TestLiveBulkWebhookSecretTravelsAsAVariableName covers the plan file.
//
// A bulk plan is written to disk, committed, attached to a change request and
// read by whoever reviews it. A literal secret in it is a secret in version
// control, which is the same reason ADR-047 keeps one off the command line. So
// the plan names the variable and the apply reads it.
func TestLiveBulkWebhookSecretTravelsAsAVariableName(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	tempDir := t.TempDir()
	policyPath := filepath.Join(tempDir, "policy.yaml")
	planPath := filepath.Join(tempDir, "plan.json")
	writePolicy := func(t *testing.T, body string) {
		t.Helper()
		if err := os.WriteFile(policyPath, []byte(body), 0o600); err != nil {
			t.Fatalf("write policy: %v", err)
		}
	}

	writePolicy(t, strings.Join([]string{
		"apiVersion: bb.io/v1alpha1",
		"selector:",
		"  projectKey: " + seeded.Key,
		"  repositories:",
		"    - " + repo.Slug,
		"operations:",
		"  - type: repo.webhook.create",
		"    name: bulk-hook",
		"    url: http://localhost:7990/status",
		"    sslVerificationRequired: false",
		"    secretEnv: BB_WEBHOOK_SECRET",
	}, "\n"))

	t.Run("a literal secret is refused where a variable name belongs", func(t *testing.T) {
		// The mistake the field invites: reading "secretEnv" as "the secret".
		writePolicy(t, strings.Join([]string{
			"apiVersion: bb.io/v1alpha1",
			"selector:",
			"  projectKey: " + seeded.Key,
			"operations:",
			"  - type: repo.webhook.create",
			"    name: bulk-hook",
			"    url: http://localhost:7990/status",
			"    secretEnv: " + secretCanary + "/not-a-name=",
		}, "\n"))

		output, err := executeLiveCLI(t, "--json", "bulk", "plan", "-f", policyPath, "-o", planPath)
		if err == nil {
			t.Fatalf("a plan naming a value rather than a variable was accepted:\n%s", output)
		}
	})

	writePolicy(t, strings.Join([]string{
		"apiVersion: bb.io/v1alpha1",
		"selector:",
		"  projectKey: " + seeded.Key,
		"  repositories:",
		"    - " + repo.Slug,
		"operations:",
		"  - type: repo.webhook.create",
		"    name: bulk-hook",
		"    url: http://localhost:7990/status",
		"    sslVerificationRequired: false",
		"    secretEnv: BB_WEBHOOK_SECRET",
	}, "\n"))

	planOutput, err := executeLiveCLI(t, "--json", "bulk", "plan", "-f", policyPath, "-o", planPath)
	if err != nil {
		t.Fatalf("bulk plan failed: %v\noutput: %s", err, planOutput)
	}

	t.Run("neither the plan on stdout nor the plan on disk carries the secret", func(t *testing.T) {
		t.Setenv("BB_WEBHOOK_SECRET", secretCanary)
		if strings.Contains(planOutput, secretCanary) {
			t.Errorf("the planned output carried the secret:\n%s", planOutput)
		}
		onDisk, err := os.ReadFile(planPath)
		if err != nil {
			t.Fatalf("read the plan: %v", err)
		}
		if strings.Contains(string(onDisk), secretCanary) {
			t.Error("the plan file carried the secret, which is the file that gets committed")
		}
		if !strings.Contains(string(onDisk), "BB_WEBHOOK_SECRET") {
			t.Errorf("the plan file did not name the variable to read:\n%s", onDisk)
		}
	})

	t.Run("an apply with the variable unset is refused rather than run without it", func(t *testing.T) {
		t.Setenv("BB_WEBHOOK_SECRET", "")
		output, err := executeLiveCLI(t, "--json", "bulk", "apply", "--from-plan", planPath)
		if err == nil {
			t.Fatalf("the plan applied without the secret it said it needed:\n%s", output)
		}

		// A failed apply writes nothing to stdout and leaves the operation id
		// in the error, so the reason lives in the saved status document.
		// Reading it is what tells "refused because the variable is unset"
		// from "refused because the server disliked something else" -- and the
		// first version of this check could not, so it passed against a build
		// that had sent an empty secret and been rejected for it.
		operationID := regexp.MustCompile(`op-[0-9a-f]+`).FindString(err.Error())
		if operationID == "" {
			t.Fatalf("the failure named no operation to inspect: %v", err)
		}

		status, statusErr := executeLiveCLI(t, "--json", "bulk", "status", operationID)
		if statusErr != nil {
			t.Fatalf("bulk status %s failed: %v\n%s", operationID, statusErr, status)
		}
		if !strings.Contains(status, "BB_WEBHOOK_SECRET") {
			t.Errorf("the recorded failure did not name the variable that was missing:\n%s", status)
		}
	})

	t.Run("an apply with the variable set configures the secret", func(t *testing.T) {
		t.Setenv("BB_WEBHOOK_SECRET", secretCanary)
		output, err := executeLiveCLI(t, "--json", "bulk", "apply", "--from-plan", planPath)
		if err != nil {
			t.Fatalf("bulk apply failed: %v\noutput: %s", err, output)
		}
		// The apply status is printed here and written to the status store on
		// disk, and it publishes the webhook Bitbucket answered with rather
		// than a model of it -- so what it must not carry is the payload's
		// configuration object, whatever that object happened to contain.
		if strings.Contains(output, secretCanary) || strings.Contains(output, `"configuration"`) {
			t.Errorf("the apply status carried the webhook's credentials:\n%s", output)
		}
		if !strings.Contains(output, `"secretConfigured": true`) {
			t.Errorf("the apply status did not report that a secret had been configured:\n%s", output)
		}

		listing := mustLiveCLI(t, "--json", "webhook", "list")
		if strings.Contains(listing, secretCanary) {
			t.Errorf("the listing carried the secret:\n%s", listing)
		}
		if !strings.Contains(listing, `"secretConfigured": true`) {
			t.Errorf("no webhook came out of the apply with a secret configured:\n%s", listing)
		}
	})
}

// TestLiveWebhookListingsAreUsable covers the two listing defects #522 collected
// while the fields were being counted.
func TestLiveWebhookListingsAreUsable(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project with repositories failed: %v", err)
	}
	repo := seeded.Repos[0]
	configureLiveCLIEnv(t, harness, seeded.Key, repo.Slug)

	for index := range 2 {
		name := fmt.Sprintf("listing-%d-%d", index, time.Now().UnixNano()%100000)
		if output, err := executeLiveCLI(t, "--json", "webhook", "create", name, "http://localhost:7990/status"); err != nil {
			t.Fatalf("create webhook %d failed: %v\noutput: %s", index, err, output)
		}
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
