package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/deprecation"
)

// deprecatedFlagRuns is, for each deprecated flag, an invocation that passes
// it. The rest of each is what the command's own example passes.
var deprecatedFlagRuns = map[string][]string{
	"bb ai mcp tools --safe-only": {"ai", "mcp", "tools", "--safe-only"},

	"bb branch restriction create --matcher-display": {
		"branch", "restriction", "create", "--repo", "PROJ/repo", "--type", "no-deletes",
		"--matcher-id", "refs/heads/main", "--matcher-display", "main",
	},
	"bb branch restriction update --matcher-display": {
		"branch", "restriction", "update", "7", "--repo", "PROJ/repo", "--type", "no-deletes",
		"--matcher-type", "BRANCH", "--matcher-id", "refs/heads/main", "--matcher-display", "main",
	},
	"bb project branch-restriction create --matcher-display": {
		"project", "branch-restriction", "create", "PROJ", "--type", "no-deletes",
		"--matcher-id", "refs/heads/main", "--matcher-display", "main",
	},
	"bb project branch-restriction update --matcher-display": {
		"project", "branch-restriction", "update", "PROJ", "7", "--type", "no-deletes",
		"--matcher-type", "BRANCH", "--matcher-id", "refs/heads/main", "--matcher-display", "main",
	},

	"bb build required delete --limit":       {"build", "required", "delete", "5", "--repo", "PROJ/repo", "--limit", "1"},
	"bb build required delete --all":         {"build", "required", "delete", "5", "--repo", "PROJ/repo", "--all"},
	"bb build status stats --include-unique": {"build", "status", "stats", "a1b2c3d", "--include-unique"},
	"bb build status get --include-unique":   {"build", "status", "get", "a1b2c3d", "--repo", "PROJ/repo", "--include-unique"},
	"bb build status set --include-unique": {
		"build", "status", "set", "a1b2c3d", "--repo", "PROJ/repo", "--key", "ci", "--state", "INPROGRESS",
		"--url", "https://ci.example.com/builds/128", "--include-unique",
	},
}

// deprecatedFlagsThatAreNotRunHere serve until stopped, so the package that
// owns them runs them to the point where they would start.
var deprecatedFlagsThatAreNotRunHere = map[string]bool{
	"bb ai mcp serve --yolo":         true,
	"bb ai mcp serve --allow-writes": true,
}

// TestEveryDeprecatedFlagIsStillTakenAndSaysSo holds each flag the registry
// deprecates to what ADR-084 promises for it: the command still takes it, it
// is out of the help, and passing it prints the registered warning on stderr
// and leaves the document on stdout alone.
//
// The registry is only a list. What makes a deprecation real is the flag still
// being there, which a deletion would end without a test noticing, and the
// warning being printed, which takes a call in the command that nothing else
// requires.
func TestEveryDeprecatedFlagIsStillTakenAndSaysSo(t *testing.T) {
	sealEnvironment(t)

	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")
	t.Setenv("BITBUCKET_TOKEN", "not-a-token")

	checked := 0
	for _, entry := range deprecation.Entries {
		path, flag, isFlag := strings.Cut(entry.Name, " --")
		if !isFlag || strings.Contains(flag, " ") {
			// A deprecated output field: "bb update --json field staged".
			continue
		}
		checked++

		target, _, err := cli.NewRootCommand().Find(strings.Fields(path)[1:])
		if err != nil || target.CommandPath() != path {
			t.Errorf("%s: the registry deprecates a flag of a command bb does not have", entry.Name)
			continue
		}

		declared := target.Flag(flag)
		if declared == nil {
			t.Errorf("%s: the flag is gone, and it has to be taken until v%d.0.0", entry.Name, removalMajor(t, entry))
			continue
		}
		if !declared.Hidden {
			t.Errorf("%s: the flag is still in the help", entry.Name)
		}
		if !strings.HasPrefix(declared.Usage, "Deprecated") {
			t.Errorf("%s: the flag's description is %q, want it to say the flag is deprecated", entry.Name, declared.Usage)
		}

		invocation, run := deprecatedFlagRuns[entry.Name]
		if !run {
			if !deprecatedFlagsThatAreNotRunHere[entry.Name] {
				t.Errorf("%s: no invocation here passes it, so nothing shows that it warns", entry.Name)
			}
			continue
		}
		if !contains(invocation, "--"+flag) {
			t.Errorf("%s: its invocation does not pass --%s", entry.Name, flag)
			continue
		}

		t.Run(entry.Name, func(t *testing.T) {
			t.Parallel()

			with, stderr := runForDeprecation(invocation)
			if !strings.Contains(stderr, entry.Warning()) {
				t.Errorf("stderr does not carry the registered warning %q:\n%s", entry.Warning(), stderr)
			}
			if strings.Contains(with, "deprecated") {
				t.Errorf("the warning is on stdout, where a script reads the result:\n%s", with)
			}

			if !json.Valid([]byte(with)) {
				t.Fatalf("stdout is not one JSON document:\n%s", with)
			}
			if strings.Contains(with, `"kind": "validation"`) {
				t.Errorf("bb refuses the invocation, so the flag no longer works:\n%s", with)
			}

			// Without the flag the command says nothing about it.
			_, quiet := runForDeprecation(without(invocation, "--"+flag))
			if strings.Contains(quiet, "deprecated") {
				t.Errorf("the warning is printed without the flag:\n%s", quiet)
			}
		})
	}

	if checked < 10 {
		t.Fatalf("checked only %d deprecated flags; the registry's names no longer read as flags", checked)
	}
}

// runForDeprecation runs bb under --dry-run and returns what it wrote to each
// stream.
func runForDeprecation(invocation []string) (string, string) {
	args := append(append([]string{}, invocation...), "--dry-run", "--json", "--no-input")

	stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}
	root := cli.NewRootCommand()
	root.SetArgs(args)
	root.SetErr(stderr)
	executeRootCommand(root, args, stdout, stderr)

	return stdout.String(), stderr.String()
}

// without returns the invocation with the flag, and the value after it when it
// has one, taken out.
func without(invocation []string, flag string) []string {
	position := indexOf(invocation, flag)
	kept := append([]string{}, invocation[:position]...)

	rest := invocation[position+1:]
	if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		rest = rest[1:]
	}

	return append(kept, rest...)
}

func removalMajor(t *testing.T, entry deprecation.Entry) int {
	t.Helper()

	major, err := entry.RemoveIn()
	if err != nil {
		t.Fatalf("%s: %v", entry.Name, err)
	}

	return major
}
