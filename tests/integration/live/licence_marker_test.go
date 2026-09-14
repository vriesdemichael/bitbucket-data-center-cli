//go:build live

package live_test

import (
	"testing"
	"time"
)

// TestParseInstanceMarker pins how the suite reads an instance's licence age and
// the age at which it stops itself. Like TestLicenceExpiryHint it needs no
// Bitbucket, so a broken reading fails in the live job rather than only when an
// instance is about to stop.
func TestParseInstanceMarker(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)
	const stopAge = 10680 * time.Second

	for _, testCase := range []struct {
		name   string
		marker string
		age    time.Duration
		stop   time.Duration
		ok     bool
	}{
		{name: "a fresh instance", marker: "1000000 10680\n", age: 0, stop: stopAge, ok: true},
		{name: "2h40m in", marker: "990400 10680", age: 9600 * time.Second, stop: stopAge, ok: true},
		{name: "past the age it stops at", marker: "989000 10680", age: 11000 * time.Second, stop: stopAge, ok: true},
		{name: "an image without the setting", marker: "1000000 \n", ok: false},
		{name: "no marker at all", marker: "", ok: false},
		{name: "not numbers", marker: "soon later", ok: false},
		{name: "a zero stop age", marker: "1000000 0", ok: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			age, stop, ok := parseInstanceMarker(testCase.marker, now)
			if ok != testCase.ok || age != testCase.age || stop != testCase.stop {
				t.Errorf("parseInstanceMarker(%q) = %s, %s, %t; want %s, %s, %t",
					testCase.marker, age, stop, ok, testCase.age, testCase.stop, testCase.ok)
			}
		})
	}
}

// TestFormatAge keeps the ages in the suite's messages in the form the stop and
// the restart are described in: 2h40m, not 2h40m0s.
func TestFormatAge(t *testing.T) {
	t.Parallel()

	for age, want := range map[time.Duration]string{
		2*time.Hour + 40*time.Minute:                  "2h40m",
		2*time.Hour + 45*time.Minute + 20*time.Second: "2h45m",
		2*time.Hour + 57*time.Minute + 40*time.Second: "2h58m",
		45 * time.Second:                              "0h01m",
	} {
		if got := formatAge(age); got != want {
			t.Errorf("formatAge(%s) = %q, want %q", age, got, want)
		}
	}
}

// TestOnlyThisCheckoutsInstanceIsJudged keeps this checkout's container, stopped
// or running, from failing a run that is pointed at another server, another
// checkout's instance included.
func TestOnlyThisCheckoutsInstanceIsJudged(t *testing.T) {
	t.Parallel()

	const instance = "http://localhost:32769"

	for _, testCase := range []struct {
		name       string
		configured string
		instance   string
		want       bool
	}{
		{name: "this checkout's instance", configured: instance, instance: instance, want: true},
		{name: "with a trailing slash", configured: instance + "/", instance: instance, want: true},
		{name: "another checkout's instance", configured: "http://localhost:7990", instance: instance, want: false},
		{name: "another server", configured: "https://bitbucket.corp.example", instance: instance, want: false},
		{name: "no instance file", configured: instance, instance: "", want: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			if got := judgesStackInstance(testCase.configured, testCase.instance); got != testCase.want {
				t.Errorf("judgesStackInstance(%q, %q) = %t, want %t", testCase.configured, testCase.instance, got, testCase.want)
			}
		})
	}
}
