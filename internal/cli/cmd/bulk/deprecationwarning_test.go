package bulkcmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// TestEverySubcommandWarnsThatBulkIsGoing covers the warning ADR-084 requires,
// by running it.
//
// The command finds its entry by matching a name in the registry, which is a
// string comparison that can stop matching: rename the entry, or the command,
// and the loop finds nothing, prints nothing, and every test stays green. The
// live suite cannot see it either -- it discards stderr.
//
// So each subcommand is run, and each has to say it on stderr. The invocations
// fail on purpose: the warning is a PersistentPreRun and has to arrive before
// whatever the command was going to do, including before it fails.
func TestEverySubcommandWarnsThatBulkIsGoing(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "absent.yaml")

	for _, args := range [][]string{
		{"plan", "-f", missing},
		{"apply", "--from-plan", missing},
		{"status", "operation-that-does-not-exist"},
	} {
		t.Run(args[0], func(t *testing.T) {
			t.Parallel()

			stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
			cmd := New(testDependencies("http://localhost"))
			cmd.SetOut(stdout)
			cmd.SetErr(stderr)
			cmd.SetArgs(args)
			_ = cmd.Execute()

			warning := stderr.String()
			for _, part := range []string{"bb bulk", "deprecated", "v5"} {
				if !strings.Contains(warning, part) {
					t.Errorf("the deprecation warning does not mention %q: %q", part, warning)
				}
			}

			// --json output is a contract, and these dependencies ask for it.
			if strings.Contains(stdout.String(), "deprecated") {
				t.Errorf("the warning reached stdout: %q", stdout.String())
			}

			if count := strings.Count(warning, "bb bulk"); count != 1 {
				t.Errorf("the warning arrived %d times, want once per invocation: %q", count, warning)
			}
		})
	}
}
