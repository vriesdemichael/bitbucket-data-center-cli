package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"gopkg.in/yaml.v3"
)

// adrFlagPattern matches a long flag as documentation writes one.
var adrFlagPattern = regexp.MustCompile(`--[a-z][a-z0-9-]*`)

// foreignFlags are flags of other programs that a decision record has reason to
// name exactly.
//
// A record explaining a release step quotes the command that performs it, and
// rewording it around the flag would make the record less useful than the
// false positive it avoids. The list is exhaustive on purpose: anything not on
// it is required to be a flag bb actually has.
var foreignFlags = map[string]bool{
	// sha256sum, in ADR-057's account of how the release verifies unversioned
	// download aliases.
	"--ignore-missing": true,
}

// TestADRDoesNotNameFlagsThatDoNotExist guards the drift that left ADR-047
// asserting the opposite of what bb does.
//
// ADR-047 decided that --token and --password "remain for compatibility and
// warn on stderr when used". v4 removed them, and the record went on saying
// otherwise while marked accepted -- as did ADR-039, which described a --token
// flag on the MCP server, and ADR-022, whose agent instruction told an agent to
// log in with a flag that no longer parses. An accepted ADR that contradicts
// the binary is worse than no ADR: this project points agents at these records,
// and an agent has no way to tell a stale one from a live one.
//
// Only the normative fields are checked. `decision` says what bb does and
// `agent_instructions` says what to do about it, so both are claims about the
// current binary. `rationale` and `rejected_alternatives` argue about roads not
// taken, and naming a flag that never existed or no longer does is the whole
// point of them.
//
// A record linked into an amendment or supersession chain is exempt, in either
// direction. It is either the record whose decision was changed -- which must
// keep describing what it decided, or the history is lost -- or the record that
// changed it, which cannot explain the change without naming what went away.
// ADR-047 and ADR-083 are now one such pair. A record with no link claims to
// describe the current world with nothing qualifying it, and is held to that.
func TestADRDoesNotNameFlagsThatDoNotExist(t *testing.T) {
	t.Parallel()

	known := knownFlagNames()

	records, err := filepath.Glob(filepath.Join("..", "..", "docs", "decisions", "*.yaml"))
	if err != nil {
		t.Fatalf("glob decision records: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("no decision records found; has the directory moved?")
	}

	var offenders []string

	for _, path := range records {
		contents, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}

		var record struct {
			Status            string `yaml:"status"`
			SupersededBy      *int   `yaml:"superseded_by"`
			Supersedes        any    `yaml:"supersedes"`
			AmendedBy         *int   `yaml:"amended_by"`
			Amends            any    `yaml:"amends"`
			Decision          string `yaml:"decision"`
			AgentInstructions string `yaml:"agent_instructions"`
		}
		if err := yaml.Unmarshal(contents, &record); err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		// A record that is not in force describes a world that is not the
		// current one, which is what its status already says.
		if status := strings.TrimSpace(record.Status); status != "" && status != "accepted" {
			continue
		}
		if record.SupersededBy != nil || record.AmendedBy != nil || record.Supersedes != nil || record.Amends != nil {
			continue
		}

		for _, field := range []string{record.Decision, record.AgentInstructions} {
			for _, flag := range adrFlagPattern.FindAllString(field, -1) {
				if known[flag] || foreignFlags[flag] {
					continue
				}
				offenders = append(offenders, filepath.Base(path)+": "+flag)
			}
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		offenders = uniqueStrings(offenders)
		t.Fatalf(
			"accepted decision records name %d flag(s) bb does not have:\n  %s\n\n"+
				"A decision that changed is recorded, not edited: add a new ADR that amends this one "+
				"(see ADR-083 amending ADR-047), which exempts both ends and keeps the history. "+
				"Correct an agent_instruction in place only when it tells an agent to run something that no longer works.",
			len(offenders), strings.Join(offenders, "\n  "))
	}
}

// knownFlagNames collects every long flag the command tree defines.
//
// Built from the tree rather than from the generated reference, so it cannot
// disagree with the binary and does not depend on that file being current.
func knownFlagNames() map[string]bool {
	known := map[string]bool{
		// Cobra registers these during Execute, which this never calls.
		"--help":    true,
		"--version": true,
	}

	var walk func(command *cobra.Command)
	walk = func(command *cobra.Command) {
		collect := func(flag *pflag.Flag) { known["--"+flag.Name] = true }
		command.Flags().VisitAll(collect)
		command.PersistentFlags().VisitAll(collect)
		command.InheritedFlags().VisitAll(collect)

		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(NewRootCommand())

	return known
}

func uniqueStrings(values []string) []string {
	unique := values[:0]
	var previous string
	for index, value := range values {
		if index > 0 && value == previous {
			continue
		}
		unique = append(unique, value)
		previous = value
	}

	return unique
}
