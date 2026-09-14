//go:build live

package live_test

import (
	"testing"
	"time"
)

// TestParseInstanceMarker pins how the suite reads the time an instance has
// before it stops itself. Like TestLicenceExpiryHint it needs no Bitbucket, so
// a broken reading fails in the live job rather than only when an instance is
// about to stop.
func TestParseInstanceMarker(t *testing.T) {
	t.Parallel()

	now := time.Unix(1_000_000, 0)

	for _, testCase := range []struct {
		name   string
		marker string
		want   time.Duration
		ok     bool
	}{
		{name: "a fresh instance", marker: "1000000 9900\n", want: 9900 * time.Second, ok: true},
		{name: "an hour in", marker: "996400 9900", want: 6300 * time.Second, ok: true},
		{name: "past the age it stops at", marker: "990000 9900", want: -100 * time.Second, ok: true},
		{name: "an image without the setting", marker: "1000000 \n", ok: false},
		{name: "no marker at all", marker: "", ok: false},
		{name: "not numbers", marker: "soon later", ok: false},
		{name: "a zero age", marker: "1000000 0", ok: false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseInstanceMarker(testCase.marker, now)
			if ok != testCase.ok || got != testCase.want {
				t.Errorf("parseInstanceMarker(%q) = %s, %t; want %s, %t", testCase.marker, got, ok, testCase.want, testCase.ok)
			}
		})
	}
}

// TestOnlyTheLocalStackIsJudged keeps a local container, stopped or running,
// from failing a run that is pointed somewhere else.
func TestOnlyTheLocalStackIsJudged(t *testing.T) {
	t.Parallel()

	for bitbucketURL, want := range map[string]bool{
		"http://localhost:7990":          true,
		"http://127.0.0.1:7990":          true,
		"http://[::1]:7990":              true,
		"http://LOCALHOST:17990":         true,
		"https://bitbucket.corp.example": false,
		"http://localhost.corp.example":  false,
		"":                               false,
		"://not a url":                   false,
	} {
		if got := targetsLocalStack(bitbucketURL); got != want {
			t.Errorf("targetsLocalStack(%q) = %t, want %t", bitbucketURL, got, want)
		}
	}
}
