//go:build live

package live_test

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The SDK issues a licence valid for three hours from process start, recorded
// by docker/harness/start-bitbucket.sh in /tmp/licence-issued-at. The compose
// healthcheck retires the container at 2h45m; this preflight uses the same
// marker so the suite and the healthcheck cannot disagree about what expired
// means.
const (
	licenceLifetime      = 3 * time.Hour
	licenceContainerName = "bb-bitbucket"
	// licenceMinimumRemaining is the margin below which starting a run is not
	// worth it. A full live run takes about five minutes; anything under this
	// is likely to expire partway through and produce exactly the confusing
	// mid-run failure this check exists to prevent.
	licenceMinimumRemaining = 10 * time.Minute
)

const licenceRemedy = "run 'task stack:restart' to reissue it (about three minutes with the Maven cache warm)"

// licenceExpiryHint recognises a failure caused by an expired SDK licence and
// returns the remedy, or an empty string when the failure is something else.
//
// This exists because the failure does not look like what it is. Bitbucket
// keeps reporting RUNNING on an expired licence and only refuses writes, so the
// first symptom is a git push failing partway through seeding — which reads as
// a broken test or a broken change, and sends you debugging the wrong thing.
func licenceExpiryHint(message string) string {
	lowered := strings.ToLower(message)

	// Both phrasings appear depending on which limit trips first: the licence
	// going invalid, or the user count it permits dropping to zero.
	for _, marker := range []string{
		"license has expired",
		"licence has expired",
		"license limit exceeded",
		"licence limit exceeded",
		"license is not valid",
		"brought back into compliance",
	} {
		if strings.Contains(lowered, marker) {
			return "the local Bitbucket SDK licence has expired; " + licenceRemedy
		}
	}

	return ""
}

// licenceRemaining reports how long the container's SDK licence has left.
//
// Reported separately from the error so the caller can tell "expired" from
// "could not tell", which are different situations: the second happens
// whenever the suite runs against something that is not the local docker
// stack, and must not fail anything.
func licenceRemaining() (time.Duration, bool) {
	if _, err := exec.LookPath("docker"); err != nil {
		return 0, false
	}

	output, err := exec.Command("docker", "exec", licenceContainerName, "cat", "/tmp/licence-issued-at").Output()
	if err != nil {
		return 0, false
	}

	issuedAtUnix, err := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
	if err != nil {
		return 0, false
	}

	return time.Until(time.Unix(issuedAtUnix, 0).Add(licenceLifetime)), true
}

// requireUsableLicence stops the run before it starts when the local stack's
// licence has expired or is about to.
//
// Failing here costs one clear line. Not failing here costs a confusing
// mid-run error, and the time spent debugging the wrong thing — which is the
// damage this is meant to prevent, not the failure itself.
func requireUsableLicence(t *testing.T) {
	t.Helper()

	remaining, known := licenceRemaining()
	if !known {
		// Not the local docker stack, or docker is not available. Nothing to
		// say, and nothing to fail: the suite runs against other instances too.
		return
	}

	if remaining <= 0 {
		t.Fatalf("the local Bitbucket SDK licence expired %s ago; %s", remaining.Abs().Round(time.Minute), licenceRemedy)
	}

	if remaining < licenceMinimumRemaining {
		t.Fatalf("the local Bitbucket SDK licence expires in %s, which is less than a full live run takes; %s", remaining.Round(time.Minute), licenceRemedy)
	}
}
