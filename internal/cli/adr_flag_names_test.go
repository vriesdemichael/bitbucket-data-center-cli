package cli

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/adr"
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
// otherwise while it was in force -- as did ADR-039, which described a --token
// flag on the MCP server, and ADR-022, which told an agent to log in with a
// flag that no longer parses. A record that contradicts the binary is worse
// than no record: this project points agents at these records, and an agent
// has no way to tell a stale one from a live one.
//
// The title and the rule are checked, because both are claims about the
// current binary. An alternative that was not chosen may name a flag that
// never existed; that is the point of it.
//
// A record with a standing line is exempt: it is either one that another
// changed in part, or the one that changed it, and neither can say what
// changed without naming what went away. Folding each such pair into one
// record that states the rule as it now is ends the exemption.
func TestADRDoesNotNameFlagsThatDoNotExist(t *testing.T) {
	t.Parallel()

	known := knownFlagNames()

	records, err := adr.Load(filepath.Join("..", "..", adr.Directory))
	if err != nil {
		t.Fatalf("load the decision records: %v", err)
	}
	if len(records) == 0 {
		t.Fatal("no decision records found; has the directory moved?")
	}

	var offenders []string

	for _, record := range records {
		if record.Standing != "" {
			continue
		}

		for _, text := range []string{record.Title, record.Body} {
			for _, flag := range adrFlagPattern.FindAllString(text, -1) {
				if known[flag] || foreignFlags[flag] {
					continue
				}
				offenders = append(offenders, filepath.Base(record.Path)+": "+flag)
			}
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		offenders = uniqueStrings(offenders)
		t.Fatalf(
			"decision records name %d flag(s) bb does not have:\n  %s\n\n"+
				"A record states the rule as it holds now. Change the record to say what bb does; "+
				"how it came to change is in git.",
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
