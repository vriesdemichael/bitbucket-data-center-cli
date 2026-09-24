package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outputschemas"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// TestEveryDeclaredResultNamesARealCommand keeps the declarations honest.
//
// A declaration is keyed by command path, so a typo or a renamed command leaves
// a schema describing nothing while `--describe` quietly falls back to "not
// described yet". That is the failure two published schemas already had: they
// described `bb branch get-default`, which has never existed, and were served
// for months because nothing compared the name to the tree.
func TestEveryDeclaredResultNamesARealCommand(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()

	for _, path := range result.DeclaredPaths() {
		command := findCommandByPath(root, path)
		if command == nil {
			t.Errorf("a result is declared for %q, which is not a command", path)
			continue
		}
		if !command.Runnable() {
			t.Errorf("a result is declared for %q, which is a group rather than a command", path)
		}
	}
}

// TestDeclaredResultsAreReachableThroughDescribe checks the wiring end to end.
//
// Declaring a schema and having --describe answer with it are two different
// things, and the second is the one a caller experiences.
func TestDeclaredResultsAreReachableThroughDescribe(t *testing.T) {
	t.Parallel()

	declared := result.DeclaredPaths()
	if len(declared) == 0 {
		t.Fatal("no results are declared; the mechanism is wired to nothing")
	}

	for _, path := range declared {
		schema, reason, described := DataSchema(path)
		if !described {
			t.Errorf("%q has a declared result but --describe reports no schema for it: %s", path, reason)
			continue
		}
		if len(schemaJSON(t, schema)) == 0 {
			t.Errorf("%q described with an empty schema", path)
		}
	}
}

// TestEveryCommandIsModelled is the gate this whole change exists to pass.
//
// Every runnable command must answer --describe with something true: a schema
// derived from the result type it fills in, or a stated reason it has no data
// payload bb can describe. Nothing may simply have been forgotten -- that was
// the state this replaced, where most of the surface published no contract and
// nothing said so.
//
// A new command fails this test until its author decides which of the three it
// is. That decision is the point.
func TestEveryCommandIsModelled(t *testing.T) {
	t.Parallel()

	declared := map[string]bool{}
	for _, path := range result.DeclaredPaths() {
		declared[path] = true
	}

	root := NewRootCommand()

	var walk func(command *cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Runnable() && command != root {
			path := commandPathWithoutRoot(command)
			switch {
			case declared[path]:
			case outputschemas.CommandsWithoutDataContract[path] != "":
			case outputschemas.CommandsWithoutDeclarableShape[path] != "":
			default:
				t.Errorf("%q neither declares a result type nor says why it has none", path)
			}
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)
}

// TestUnmodelledCommandsSaySoRatherThanGuessing covers the third answer.
//
// A command whose payload has no shape bb can promise must say that, rather than
// reporting an empty schema that looks like a guarantee.
func TestUnmodelledCommandsSaySoRatherThanGuessing(t *testing.T) {
	t.Parallel()

	_, reason, described := DataSchema("webhook test")
	if described {
		t.Fatal("webhook test has no data shape but reported a schema")
	}
	if !strings.Contains(reason, "no shape bb can promise") {
		t.Errorf("reason = %q, want it to say the payload has no shape bb can promise", reason)
	}
}

// TestDescribeAnswersForGroupsRatherThanPrintingHelp covers the answer a
// non-runnable command gives.
//
// `bb pr --describe` used to print pr's help page on stdout and exit 0, which a
// caller parsing the answer reads as a success it cannot decode. A group has no
// output to describe, and saying so is the answer.
func TestDescribeAnswersForGroupsRatherThanPrintingHelp(t *testing.T) {
	t.Parallel()

	output := &bytes.Buffer{}
	root := NewRootCommand()
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs([]string{"pr", "--describe"})

	if err := root.Execute(); err != nil {
		t.Fatalf("bb pr --describe: %v", err)
	}

	if !strings.Contains(output.String(), "Commands in bb pr") || strings.Contains(output.String(), "Usage:") {
		t.Fatalf("bb pr --describe printed something other than its catalogue:\n%s", output)
	}
}

// TestDescribeAnswersForHelpAndCompletion covers Cobra's own commands.
//
// They are added at execute time, so they used to slip past the --describe
// wrapper entirely and answer with the very text a caller was trying to avoid.
func TestDescribeAnswersForHelpAndCompletion(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"help", "completion bash"} {
		_, reason, described := DataSchema(path)
		if described {
			t.Errorf("%q reported a schema for a document it does not write", path)
		}
		if !strings.Contains(reason, "writes no document of its own") {
			t.Errorf("%q reason = %q", path, reason)
		}
	}
}

// TestExemptionsNameRealCommandsAndDoNotOverlapDeclarations keeps the two
// exemption maps from going stale.
//
// Nothing checked either one. An entry naming a command that was since renamed
// sat there exempting nothing, and an entry for a command that later grew a
// declared result type silently won over it -- describeCommand consults the maps
// first, so the command would have gone on reporting that it has no contract
// while holding one.
func TestExemptionsNameRealCommandsAndDoNotOverlapDeclarations(t *testing.T) {
	t.Parallel()

	runnable := map[string]bool{}
	root := NewRootCommand()
	var walk func(command *cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Runnable() && command != root {
			runnable[commandPathWithoutRoot(command)] = true
		}
		for _, child := range command.Commands() {
			walk(child)
		}
	}
	walk(root)

	declared := map[string]bool{}
	for _, path := range result.DeclaredPaths() {
		declared[path] = true
	}

	exemptions := map[string]map[string]string{
		"CommandsWithoutDataContract":    outputschemas.CommandsWithoutDataContract,
		"CommandsWithoutDeclarableShape": outputschemas.CommandsWithoutDeclarableShape,
	}
	for name, exempt := range exemptions {
		for path, reason := range exempt {
			if !runnable[path] {
				t.Errorf("%s exempts %q, which is not a runnable command", name, path)
			}
			if strings.TrimSpace(reason) == "" {
				t.Errorf("%s exempts %q with no reason", name, path)
			}
			if declared[path] {
				t.Errorf("%s exempts %q, but it declares a result type; the exemption would win and hide the contract", name, path)
			}
		}
	}

	for path := range outputschemas.CommandsWithoutDataContract {
		if outputschemas.CommandsWithoutDeclarableShape[path] != "" {
			t.Errorf("%q is in both exemption maps, which answer different questions", path)
		}
	}
}

// TestEveryDeclarationResolves is where the startup panic went.
//
// Declarations are lazy, so a bad enum path -- a property the type does not
// have, usually left behind by a rename -- no longer blows up at init. Reading
// every declaration here restores the guard, and puts it in a test run rather
// than in a user's terminal.
func TestEveryDeclarationResolves(t *testing.T) {
	t.Parallel()

	for _, path := range result.DeclaredPaths() {
		schema, ok := result.SchemaFor(path)
		if !ok {
			t.Errorf("%q is a declared path with no declaration", path)
			continue
		}
		if schema == nil {
			t.Errorf("%q derived a nil schema", path)
		}
	}
}
