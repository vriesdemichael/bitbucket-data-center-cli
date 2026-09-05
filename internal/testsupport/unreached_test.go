package testsupport_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// recordingReporter stands in for *testing.T so a failure can be observed
// rather than suffered.
type recordingReporter struct {
	helped   bool
	failures []string
}

func (r *recordingReporter) Helper() { r.helped = true }

func (r *recordingReporter) Errorf(format string, args ...any) {
	r.failures = append(r.failures, fmt.Sprintf(format, args...))
}

// TestUnreachedHandlerFailsWhenItIsReached is the sabotage AGENTS.md asks for,
// written down as a test.
//
// Thirty-nine mocks in this tree are this handler, and every one of them is
// asserting a negative: no request was made. A handler that had stopped
// reporting would leave all of them passing and none of them checking, and
// nothing about reading them would say so -- an unreached guard and a broken
// one look identical from the outside, because neither ever fires.
//
// The only way to tell them apart is to reach it on purpose.
func TestUnreachedHandlerFailsWhenItIsReached(t *testing.T) {
	reporter := &recordingReporter{}

	server := httptest.NewServer(testsupport.UnreachedHandler(reporter))
	t.Cleanup(server.Close)

	if !reporter.helped {
		t.Error("the handler did not mark itself a helper, so a failure would point at this file")
	}
	if len(reporter.failures) != 0 {
		t.Fatalf("building the handler already failed the test: %v", reporter.failures)
	}

	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, server.URL+"/rest/api/latest/projects/PRJ", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = response.Body.Close()

	if len(reporter.failures) != 1 {
		t.Fatalf("a request reached the handler and it reported %d failures, want 1: %v", len(reporter.failures), reporter.failures)
	}

	// The method and path are in the message, because a suite with dozens of
	// these needs to say which request arrived, not that one did.
	failure := reporter.failures[0]
	for _, want := range []string{http.MethodDelete, "/rest/api/latest/projects/PRJ"} {
		if !strings.Contains(failure, want) {
			t.Errorf("the failure does not name %q: %s", want, failure)
		}
	}
}
