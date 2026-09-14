//go:build live

package live_test

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The SDK issues a licence valid for three hours from process start, recorded
// by docker/harness/start-bitbucket.sh in /tmp/licence-issued-at. The container
// stops itself BB_LICENCE_RETIRE_SECONDS after that, and the compose
// healthcheck judges the same two values, so the suite, the healthcheck and the
// stop cannot disagree about how long an instance has left. Which container is
// this checkout's comes from the file scripts/stack.sh writes
// (stack_instance_test.go).

// licenceMinimumRemaining is the margin below which starting a run is not worth
// it. A full live run takes about five minutes; anything under this is likely
// to be stopped partway through and produce exactly the confusing mid-run
// failure this check exists to prevent. scripts/stack.sh up restarts an
// instance this close, so a run through task test:live does not meet it.
const licenceMinimumRemaining = 10 * time.Minute

const (
	licenceRemedy = "run 'task stack:restart' to reissue it (about three minutes with the Maven cache warm)"
	startRemedy   = "run 'task stack:up' to start it with a new licence, or 'task test:live', which starts it first"
)

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

// instanceState is what the suite can tell about this checkout's instance.
type instanceState int

const (
	// instanceUnknown is nothing to judge: no instance file, no docker, or no
	// container by the recorded name.
	instanceUnknown instanceState = iota
	instanceStopped
	instanceRunning
)

// localInstance reports whether this checkout's instance is running and, when
// it is, how long it has before it stops itself.
//
// Unknown is kept apart from stopped because it is a different situation: it is
// what the suite sees whenever it has no local instance to look at, and it must
// not fail anything.
func localInstance() (instanceState, time.Duration) {
	container := strings.TrimSpace(os.Getenv(stackInstanceContainerVariable))
	if container == "" {
		return instanceUnknown, 0
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return instanceUnknown, 0
	}

	running, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", container).Output()
	if err != nil {
		return instanceUnknown, 0
	}
	if strings.TrimSpace(string(running)) != "true" {
		return instanceStopped, 0
	}

	marker, err := exec.Command("docker", "exec", container, "sh", "-c",
		`echo "$(cat /tmp/licence-issued-at) ${BB_LICENCE_RETIRE_SECONDS}"`).Output()
	if err != nil {
		return instanceUnknown, 0
	}

	remaining, ok := parseInstanceMarker(string(marker), time.Now())
	if !ok {
		return instanceUnknown, 0
	}

	return instanceRunning, remaining
}

// parseInstanceMarker reads "<licence issued, unix seconds> <seconds until the
// instance stops itself>" and returns how long is left before it stops.
func parseInstanceMarker(marker string, now time.Time) (time.Duration, bool) {
	fields := strings.Fields(marker)
	if len(fields) != 2 {
		return 0, false
	}

	issuedAt, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, false
	}
	retireAfter, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || retireAfter <= 0 {
		return 0, false
	}

	return time.Unix(issuedAt, 0).Add(time.Duration(retireAfter) * time.Second).Sub(now), true
}

// judgesStackInstance reports whether the suite is pointed at this checkout's
// own instance, the only one whose container it knows. A run against anything
// else, another checkout's instance included, has nothing to learn from it.
func judgesStackInstance(bitbucketURL, instanceURL string) bool {
	instance := strings.TrimRight(strings.TrimSpace(instanceURL), "/")

	return instance != "" && strings.EqualFold(strings.TrimRight(strings.TrimSpace(bitbucketURL), "/"), instance)
}

// requireUsableLicence stops the run before it starts when this checkout's
// instance is stopped, or about to stop itself.
//
// Failing here costs one clear line. Not failing here costs a confusing
// mid-run error, and the time spent debugging the wrong thing — which is the
// damage this is meant to prevent, not the failure itself.
func requireUsableLicence(t *testing.T, bitbucketURL string) {
	t.Helper()

	if !judgesStackInstance(bitbucketURL, os.Getenv(stackInstanceURLVariable)) {
		return
	}

	state, remaining := localInstance()
	switch {
	case state == instanceUnknown:
		return
	case state == instanceStopped:
		t.Fatalf("this checkout's Bitbucket is stopped, as it is once its licence ages out; %s", startRemedy)
	case remaining <= 0:
		t.Fatalf("this checkout's Bitbucket is %s past the age at which it stops itself; %s", remaining.Abs().Round(time.Minute), licenceRemedy)
	case remaining < licenceMinimumRemaining:
		t.Fatalf("this checkout's Bitbucket stops itself in %s, which is less than a full live run takes; %s", remaining.Round(time.Minute), licenceRemedy)
	}
}
