package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

// TestRunSetsUpWhatMainDoes drives run with a real root command: the version,
// the interrupt handling around the command, and the exit code it returns.
//
// Not parallel. run stamps the release version, which is process-wide, and a
// sequential test finishes before the parallel ones in this package start.
func TestRunSetsUpWhatMainDoes(t *testing.T) {
	stdout := &bytes.Buffer{}

	if code := run([]string{"--version"}, stdout, io.Discard); code != 0 {
		t.Fatalf("bb --version exited %d", code)
	}
	if !strings.Contains(stdout.String(), Version) {
		t.Fatalf("bb --version did not print the version %q: %q", Version, stdout.String())
	}

	if code := run([]string{"--no-such-flag"}, io.Discard, io.Discard); code != 2 {
		t.Fatalf("an unknown flag exited %d, want 2", code)
	}
}
