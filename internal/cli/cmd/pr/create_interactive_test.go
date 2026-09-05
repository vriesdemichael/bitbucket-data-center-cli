package prcmd

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestPrCreateNamesEveryMissingFlagAtOnce is the half of ADR-073 that is easy
// to get wrong.
//
// bb pr create used MarkFlagRequired, so Cobra refused before RunE and a person
// at a terminal could never be asked. Removing that must not weaken the
// non-interactive contract: with nobody to ask, the command still refuses, and
// it names every absent flag in one message rather than one per round trip.
func TestPrCreateNamesEveryMissingFlagAtOnce(t *testing.T) {
	// A listener that fails the test if it is reached, which is the
	// assertion: every case here is refused before a request exists.
	guard := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(guard.Close)
	serverURL := guard.URL

	cases := []struct {
		name     string
		args     []string
		wants    []string
		notWants []string
	}{
		{
			name:  "nothing given",
			args:  []string{"create", "--repo", "PRJ/repo"},
			wants: []string{"--from-ref", "--to-ref", "--title"},
		},
		{
			name:     "only the title given",
			args:     []string{"create", "--repo", "PRJ/repo", "--title", "x"},
			wants:    []string{"--from-ref", "--to-ref"},
			notWants: []string{"--title"},
		},
		{
			name:     "only the refs given",
			args:     []string{"create", "--repo", "PRJ/repo", "--from-ref", "a", "--to-ref", "b"},
			wants:    []string{"--title"},
			notWants: []string{"--from-ref", "--to-ref"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := executePr(t, serverURL, testCase.args...)
			if err == nil {
				t.Fatal("the pull request was created with required values missing")
			}

			message := err.Error()
			for _, flag := range testCase.wants {
				if !strings.Contains(message, flag) {
					t.Errorf("error = %q, want it to name %q", message, flag)
				}
			}
			// The claim is that it names exactly the absent set. A message
			// listing a flag the caller did supply would pass the check above.
			for _, flag := range testCase.notWants {
				if strings.Contains(message, flag) {
					t.Errorf("error = %q, should not name %q, which was supplied", message, flag)
				}
			}
		})
	}
}

// TestPrCreateDoesNotAskWhenItHasEverything is live now, and not as its own
// test: every `pr create` in the live suite is a complete invocation, run
// without a terminal, and a prompt firing there would hang or refuse rather
// than return a pull request. The version here proved the same thing against
// a stub that accepted any POST.
