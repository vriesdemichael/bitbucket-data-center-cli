package prcmd

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestPrCreateNamesEveryMissingFlagAtOnce is the half of ADR-073 that is easy
// to get wrong.
//
// bb pr create used MarkFlagRequired, so Cobra refused before RunE and a person
// at a terminal could never be asked. Removing that must not weaken the
// non-interactive contract: with nobody to ask, the command still refuses, and
// it names every absent flag in one message rather than one per round trip.
func TestPrCreateNamesEveryMissingFlagAtOnce(t *testing.T) {
	// A URL, not a server: every case here is refused for missing flags before a
	// request exists, so a listener could only hide the refusal not happening.
	const serverURL = "http://bitbucket.invalid"

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

// TestPrCreateDoesNotAskWhenItHasEverything guards against the prompt firing on
// a complete invocation, which would hang every scripted caller.
func TestPrCreateDoesNotAskWhenItHasEverything(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"id":1,"version":0,"title":"x","state":"OPEN"}`))
			return
		}
		_, _ = w.Write([]byte(`{"isLastPage":true,"size":0,"values":[]}`))
	}))
	defer server.Close()

	_, err := executePr(t, server.URL,
		"create", "--repo", "PRJ/repo", "--from-ref", "feature", "--to-ref", "main", "--title", "x")
	// Asserting only that the error is not about missing flags would let any
	// unrelated failure pass while looking like a guard. A complete invocation
	// against this stub succeeds, so that is what is asserted.
	if err != nil {
		t.Fatalf("a complete invocation failed: %v", err)
	}
}
