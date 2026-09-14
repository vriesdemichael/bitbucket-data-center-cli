//go:build live

package live_test

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

// The SDK issues a licence valid for three hours from process start, recorded
// by docker/harness/start-bitbucket.sh in /tmp/licence-issued-at. Every limit
// here is an age measured from that moment:
//
//   - 2h58m, BB_LICENCE_RETIRE_SECONDS: the container stops itself, two minutes
//     before the licence runs out. The compose healthcheck reads the same value.
//   - 2h40m: scripts/stack.sh up, which task test:live runs first, starts an
//     instance at least this old again with a new licence.
//   - 2h45m, licenceLatestStartAge: the latest a run may start. Five minutes
//     after the restart age, the time task test:live spends building the suite
//     after up, and thirteen minutes before the stop, three times a full run.
//
// Which container is this checkout's comes from the file scripts/stack.sh
// writes (stack_instance_test.go).
const licenceLatestStartAge = 2*time.Hour + 45*time.Minute

const (
	licenceRemedy = "run 'task stack:restart' to reissue it (about three minutes with the Maven cache warm)"
	startRemedy   = "run 'task stack:up', which starts it again with a new licence, or 'task test:live', which runs that first"
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
// it is, how old its licence is and the age at which it stops itself.
//
// Unknown is kept apart from stopped because it is a different situation: it is
// what the suite sees whenever it has no local instance to look at, and it must
// not fail anything.
func localInstance() (instanceState, time.Duration, time.Duration) {
	container := strings.TrimSpace(os.Getenv(stackInstanceContainerVariable))
	if container == "" {
		return instanceUnknown, 0, 0
	}
	if _, err := exec.LookPath("docker"); err != nil {
		return instanceUnknown, 0, 0
	}

	running, err := exec.Command("docker", "inspect", "--format", "{{.State.Running}}", container).Output()
	if err != nil {
		return instanceUnknown, 0, 0
	}
	if strings.TrimSpace(string(running)) != "true" {
		return instanceStopped, 0, 0
	}

	marker, err := exec.Command("docker", "exec", container, "sh", "-c",
		`echo "$(cat /tmp/licence-issued-at) ${BB_LICENCE_RETIRE_SECONDS}"`).Output()
	if err != nil {
		return instanceUnknown, 0, 0
	}

	age, stopAge, ok := parseInstanceMarker(string(marker), time.Now())
	if !ok {
		return instanceUnknown, 0, 0
	}

	return instanceRunning, age, stopAge
}

// parseInstanceMarker reads "<licence issued, unix seconds> <age in seconds at
// which the instance stops itself>" and returns the licence's age and that stop
// age.
func parseInstanceMarker(marker string, now time.Time) (time.Duration, time.Duration, bool) {
	fields := strings.Fields(marker)
	if len(fields) != 2 {
		return 0, 0, false
	}

	issuedAt, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, 0, false
	}
	stopAfter, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || stopAfter <= 0 {
		return 0, 0, false
	}

	return now.Sub(time.Unix(issuedAt, 0)), time.Duration(stopAfter) * time.Second, true
}

// formatAge writes an age in hours and minutes, the way the stop and the
// restart are described.
func formatAge(age time.Duration) string {
	minutes := int(age.Round(time.Minute) / time.Minute)

	return fmt.Sprintf("%dh%02dm", minutes/60, minutes%60)
}

// judgesStackInstance reports whether the suite is pointed at this checkout's
// own instance, the only one whose container it knows. A run against anything
// else, another checkout's instance included, has nothing to learn from it.
func judgesStackInstance(bitbucketURL, instanceURL string) bool {
	instance := strings.TrimRight(strings.TrimSpace(instanceURL), "/")

	return instance != "" && strings.EqualFold(strings.TrimRight(strings.TrimSpace(bitbucketURL), "/"), instance)
}

// requireUsableLicence stops the run before it starts when this checkout's
// instance is stopped, or too old for a run to finish before it stops itself.
//
// Failing here costs one clear line. Not failing here costs a confusing
// mid-run error, and the time spent debugging the wrong thing — which is the
// damage this is meant to prevent, not the failure itself.
func requireUsableLicence(t *testing.T, bitbucketURL string) {
	t.Helper()

	if !judgesStackInstance(bitbucketURL, os.Getenv(stackInstanceURLVariable)) {
		return
	}

	state, age, stopAge := localInstance()
	switch {
	case state == instanceUnknown:
		return
	case state == instanceStopped:
		t.Fatalf("this checkout's Bitbucket is stopped, as it is once its licence is nearly out; %s", startRemedy)
	case age >= stopAge:
		t.Fatalf("this checkout's Bitbucket is %s into its licence, past the %s at which it stops itself; %s",
			formatAge(age), formatAge(stopAge), startRemedy)
	case age >= licenceLatestStartAge:
		t.Fatalf("this checkout's Bitbucket is %s into its licence, and a run starts no later than %s in; %s",
			formatAge(age), formatAge(licenceLatestStartAge), startRemedy)
	}
}
