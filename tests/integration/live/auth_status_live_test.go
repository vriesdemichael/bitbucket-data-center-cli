//go:build live

package live_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestLiveAuthStatusVerifiesRatherThanReports is the point of the change: the
// command used to print the configured target without checking any of it, so a
// dead credential and a working one produced the same confident output.
//
// Only a real Bitbucket can establish that the probe actually probes. A stub
// can show the plumbing carries a result; it cannot show the result is true.
func TestLiveAuthStatusVerifiesRatherThanReports(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	output, err := executeLiveCLI(t, "--json", "auth", "status")
	if err != nil {
		t.Fatalf("auth status failed: %v\noutput: %s", err, output)
	}

	payload := decodeJSONMap(t, output)
	data, ok := payload["data"].(map[string]any)
	if !ok {
		data = payload
	}

	// CI authenticates from the environment and never pushes through the git
	// credential helper, so that check legitimately misses here. It is advisory
	// precisely so an API-only setup like this one still reports healthy —
	// counting it would fail every CI pipeline that never runs git.
	if data["ok"] != true {
		t.Fatalf("expected an API-only setup to still report ok, got: %s", output)
	}

	checks, ok := data["checks"].([]any)
	if !ok || len(checks) == 0 {
		t.Fatalf("expected checks in the payload, got: %s", output)
	}

	// The authentication check is the one that proves reachability, TLS and the
	// credential all at once, so it must both exist and pass here.
	var sawAuthentication bool
	for _, raw := range checks {
		check, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("expected check objects, got: %s", output)
		}
		if asString(check["name"]) != "authentication" {
			continue
		}
		sawAuthentication = true
		if check["ok"] != true {
			t.Fatalf("expected authentication to pass against the live stack, got: %s", output)
		}
		if strings.TrimSpace(asString(check["detail"])) == "" {
			t.Fatalf("expected the authenticated identity in the detail, got: %s", output)
		}
	}
	if !sawAuthentication {
		t.Fatalf("expected an authentication check, got: %s", output)
	}

	// --check must agree with the payload: exit zero when everything passed.
	checkOutput, err := executeLiveCLI(t, "auth", "status", "--check")
	if err != nil {
		t.Fatalf("auth status --check should succeed on a healthy setup: %v\noutput: %s", err, checkOutput)
	}
}

// TestLiveAuthStatusFailsOnABadCredential establishes the half that matters for
// CI: --check has to actually fail, or it is decoration.
func TestLiveAuthStatusFailsOnABadCredential(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	// A wrong password against a reachable host: the failure is authentication,
	// not connectivity, and the remedy should say so.
	t.Setenv("BITBUCKET_PASSWORD", "definitely-not-the-password")
	t.Setenv("ADMIN_PASSWORD", "definitely-not-the-password")

	output, err := executeLiveCLI(t, "auth", "status", "--check")
	if err == nil {
		t.Fatalf("expected --check to fail on a rejected credential, got: %s", output)
	}

	combined := output + err.Error()
	if !strings.Contains(combined, "bb auth login") {
		t.Fatalf("expected the remedy to name bb auth login, got: %s", combined)
	}

	// Without --check the exit stays zero, which is what keeps this
	// non-breaking for callers that only want the report.
	reportOutput, reportErr := executeLiveCLI(t, "auth", "status")
	if reportErr != nil {
		t.Fatalf("auth status without --check must not fail: %v\noutput: %s", reportErr, reportOutput)
	}
}

// TestLiveAuthStatusTreatsTheGitHelperAsAdvisory pins the distinction that a
// CI run exposed: the live job authenticates from the environment and never
// pushes through the helper, so the check misses — and the setup is fine.
//
// Counting it as a failure would report a broken setup to every pipeline and
// every agent that only calls the API, which is the exact failure mode this
// command exists to remove.
func TestLiveAuthStatusTreatsTheGitHelperAsAdvisory(t *testing.T) {
	harness := newLiveHarness(t)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	seeded, err := harness.seedProjectWithRepositories(ctx, 1, 1)
	if err != nil {
		t.Fatalf("seed project failed: %v", err)
	}
	configureLiveCLIEnv(t, harness, seeded.Key, seeded.Repos[0].Slug)

	output, err := executeLiveCLI(t, "--json", "auth", "status")
	if err != nil {
		t.Fatalf("auth status failed: %v\noutput: %s", err, output)
	}

	payload := decodeJSONMap(t, output)
	data, ok := payload["data"].(map[string]any)
	if !ok {
		data = payload
	}

	checks, ok := data["checks"].([]any)
	if !ok {
		t.Fatalf("expected checks in the payload, got: %s", output)
	}

	var sawHelper bool
	for _, raw := range checks {
		check, ok := raw.(map[string]any)
		if !ok || asString(check["name"]) != "git credential helper" {
			continue
		}
		sawHelper = true
		if check["advisory"] != true {
			t.Fatalf("expected the git helper check to be advisory, got: %s", output)
		}
	}
	if !sawHelper {
		t.Fatalf("expected a git credential helper check, got: %s", output)
	}

	// The verdict, and --check with it, must ignore an advisory miss.
	if data["ok"] != true {
		t.Fatalf("an advisory miss must not make the setup unhealthy, got: %s", output)
	}
	if checkOutput, err := executeLiveCLI(t, "auth", "status", "--check"); err != nil {
		t.Fatalf("--check must pass despite an advisory miss: %v\noutput: %s", err, checkOutput)
	}
}
