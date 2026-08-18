//go:build live

package live_test

import (
	"strings"
	"testing"
)

// TestLicenceExpiryHint pins the recognition against the message the issue
// actually recorded, and against failures that must not be mistaken for it.
//
// Deliberately a pure test: it needs no Bitbucket, so it fails fast in the live
// job rather than only when a licence happens to lapse. The whole point of the
// classifier is that it fires on a rare condition, which is exactly the kind
// that goes untested and quietly stops working.
func TestLicenceExpiryHint(t *testing.T) {
	recognised := []struct {
		name    string
		message string
	}{
		{
			// Verbatim from the failure recorded in issue #347.
			name: "git push refused, as observed",
			message: "git push failed: exit status 128: fatal: remote error: License limit exceeded\n" +
				"Your license has expired. Pushing has been disabled until the license is\n" +
				"brought back into compliance.",
		},
		{name: "british spelling", message: "remote error: Your licence has expired"},
		{name: "limit exceeded alone", message: "fatal: remote error: License limit exceeded"},
		{name: "not valid phrasing", message: "The license is not valid for this instance"},
		{name: "mixed case", message: "LICENSE HAS EXPIRED"},
	}

	for _, testCase := range recognised {
		t.Run(testCase.name, func(t *testing.T) {
			hint := licenceExpiryHint(testCase.message)
			if hint == "" {
				t.Fatalf("expected a licence hint for %q", testCase.message)
			}
			// The remedy is the reason the hint exists. A hint naming the cause
			// without the fix would still leave the reader looking things up.
			if !strings.Contains(hint, "task stack:restart") {
				t.Fatalf("expected the remedy in the hint, got %q", hint)
			}
		})
	}

	ignored := []struct {
		name    string
		message string
	}{
		{name: "ordinary rejection", message: "fatal: remote error: pre-receive hook declined"},
		{name: "authentication", message: "fatal: Authentication failed for 'http://localhost:7990'"},
		{name: "network", message: "fatal: unable to access: Could not resolve host"},
		{name: "empty", message: ""},
		// A licence mentioned in passing is not a licence failure. Matching it
		// would send a reader chasing a restart that fixes nothing.
		{name: "unrelated mention of a licence file", message: "error: cannot stat 'LICENSE.txt'"},
	}

	for _, testCase := range ignored {
		t.Run(testCase.name, func(t *testing.T) {
			if hint := licenceExpiryHint(testCase.message); hint != "" {
				t.Fatalf("expected no hint for %q, got %q", testCase.message, hint)
			}
		})
	}
}
