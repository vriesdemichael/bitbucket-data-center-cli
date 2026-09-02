// Command command-reach reports which CLI commands the live integration
// suite actually proves work against a real Bitbucket.
//
// The project treats live tests as the primary correctness gate, but two holes
// let `bb pr task *` keep calling a REST endpoint Atlassian removed in
// Bitbucket 8.0:
//
//  1. Many commands are never invoked by a live test at all, so a mock that
//     still serves a retired endpoint is the only thing they are checked
//     against.
//  2. Worse, a command can be invoked by a live test that skips itself when the
//     call fails. The pull request task tests do exactly that — they call
//     t.Skipf when the endpoint returns not_found — so the suite stayed green
//     for years while the command was broken. A skipped test looks the same as
//     a passing one in CI output.
//
// Commands come from the Cobra tree; invocations and error-conditioned skips
// come from parsing the live test sources. A command whose only live coverage
// can skip itself on error is reported as masked, not covered, because it
// proves nothing.
//
// A full sweep is not realistic to demand at once, so the committed report
// doubles as a baseline. Verification fails when coverage regresses, when a new
// command arrives with no live coverage, or when a new masked command appears.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

// reportVersion is bumped when the on-disk shape changes.
const reportVersion = 1

type report struct {
	Version int `json:"version"`
	Summary struct {
		Runnable  int     `json:"runnable_commands"`
		Covered   int     `json:"covered_commands"`
		Percent   float64 `json:"covered_percent"`
		Masked    int     `json:"masked_commands"`
		Uncovered int     `json:"uncovered_commands"`
	} `json:"summary"`
	// Covered lists commands a live test invokes and asserts on. Regressions
	// against this set fail verification.
	Covered []string `json:"covered"`
	// KnownMasked lists commands whose only live coverage comes from a test that
	// skips itself when the call fails, so the suite stays green whether or not
	// the command works. These are the most dangerous entries in the report: they
	// look covered in CI output and are not.
	KnownMasked []string `json:"known_masked"`
	// KnownUncovered is the accepted backlog of commands no live test runs.
	// A command may only appear here if it is already in the committed report;
	// anything new must come with live coverage.
	KnownUncovered []string `json:"known_uncovered"`
}

func main() {
	reportPath := flag.String("report-file", "docs/quality/command-reach.json", "Path to the committed command reach report")
	liveDir := flag.String("live-dir", "tests/integration/live", "Directory holding the live integration tests")
	write := flag.Bool("write", false, "Write the report to disk")
	verify := flag.Bool("verify", false, "Fail when live coverage regresses against the committed report")
	flag.Parse()

	root := cli.NewRootCommand()
	runnable := collectRunnableCommands(root)
	invoked, err := discoverLiveInvocations(*liveDir, globalValueFlags(root))
	if err != nil {
		fail("failed to scan live tests: %v", err)
	}

	current := buildReport(runnable, invoked)
	printSummary(current)

	if *write {
		if err := writeReport(*reportPath, current); err != nil {
			fail("failed to write report: %v", err)
		}
		fmt.Printf("Wrote command reach report: %s\n", *reportPath)
	}

	if *verify {
		committed, err := readReport(*reportPath)
		if err != nil {
			fail("failed to read committed report: %v", err)
		}
		if problems := compareReports(committed, current); len(problems) > 0 {
			for _, problem := range problems {
				fmt.Fprintln(os.Stderr, "FAIL: "+problem)
			}
			fmt.Fprintln(os.Stderr, "\nEvery command needs at least one live test that runs it against a real Bitbucket.")
			fmt.Fprintln(os.Stderr, "A test that calls t.Skip on error does not count: it passes whether or not the command works.")
			fmt.Fprintln(os.Stderr, "Add or fix a test in tests/integration/live, then run: task quality:command-reach:update")
			os.Exit(1)
		}
		fmt.Println("Verified command reach: no regressions")
	}
}

// collectRunnableCommands returns the paths of commands that actually do
// something. Groups that only hold subcommands are skipped: running `bb pr`
// prints help, so demanding live coverage for it would be noise.
func collectRunnableCommands(root *cobra.Command) []string {
	paths := []string{}

	var walk func(command *cobra.Command)
	walk = func(command *cobra.Command) {
		for _, child := range command.Commands() {
			if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			if child.Runnable() {
				paths = append(paths, commandPathWithoutRoot(child))
			}
			walk(child)
		}
	}
	walk(root)

	sort.Strings(paths)
	return paths
}

// commandPathWithoutRoot renders "pr comment list" rather than "bb pr comment
// list", matching the argument slices the live tests pass.
func commandPathWithoutRoot(command *cobra.Command) string {
	parts := []string{}
	for current := command; current != nil && current.HasParent(); current = current.Parent() {
		parts = append([]string{current.Name()}, parts...)
	}

	return strings.Join(parts, " ")
}

// invocations records, per command path, whether every live test that runs it
// can skip itself when the call fails.
type invocations struct {
	// asserted holds commands invoked by at least one test that cannot skip on
	// error, so a failure there really does fail CI.
	asserted map[string]struct{}
	// masked holds commands invoked only by tests that skip on error.
	masked map[string]struct{}
}

// discoverLiveInvocations parses the live tests and records the command paths
// passed to executeLiveCLI, split by whether the enclosing test can skip itself
// when a call fails.
//
// Attribution is per test function: if a function contains an error-conditioned
// skip, every command it invokes is treated as masked, because any of those
// calls failing can end the test as "skipped" rather than "failed".
func discoverLiveInvocations(dir string, valueFlags map[string]bool) (invocations, error) {
	found := invocations{asserted: map[string]struct{}{}, masked: map[string]struct{}{}}
	fileSet := token.NewFileSet()

	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}

		node, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		for _, declaration := range node.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}

			commands := commandsInvokedBy(function, valueFlags)
			if len(commands) == 0 {
				continue
			}

			target := found.asserted
			if skipsOnError(function) {
				target = found.masked
			}
			for command := range commands {
				target[command] = struct{}{}
			}
		}

		return nil
	})
	if err != nil {
		return invocations{}, err
	}

	// A command asserted anywhere is genuinely covered, even if some other test
	// also invokes it behind a skip.
	for command := range found.asserted {
		delete(found.masked, command)
	}

	return found, nil
}

func commandsInvokedBy(function *ast.FuncDecl, valueFlags map[string]bool) map[string]struct{} {
	commands := map[string]struct{}{}

	ast.Inspect(function.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		// The stdin variant takes the input as an extra argument before the
		// command words. Recognising only the plain helper reported commands
		// that are driven through stdin -- the credential helper protocol, for
		// one -- as never invoked, when they were covered all along.
		leadingArgs := 1
		switch identifier.Name {
		case "executeLiveCLI":
		case "executeLiveCLIWithStdin":
			leadingArgs = 2
		case "executeLiveMCPServer":
			// The MCP server is a conversation, not an invoke-and-assert: it
			// takes a callback that drives the protocol while the command runs.
			// Like the stdin variant, the command words follow that argument.
			leadingArgs = 2
		default:
			return true
		}
		if len(call.Args) < leadingArgs {
			return true
		}
		if path := commandPathFromArgs(call.Args[leadingArgs-1:], valueFlags); path != "" {
			commands[path] = struct{}{}
		}
		return true
	})

	return commands
}

// skipsOnError reports whether a test can end itself as skipped because a call
// failed, as opposed to skipping on an environment precondition such as missing
// credentials. The signal is a t.Skip/t.Skipf inside an if whose condition
// mentions an error value.
func skipsOnError(function *ast.FuncDecl) bool {
	skips := false

	ast.Inspect(function.Body, func(n ast.Node) bool {
		ifStatement, ok := n.(*ast.IfStmt)
		if !ok || ifStatement.Cond == nil {
			return true
		}
		if !mentionsError(ifStatement.Cond) || !containsSkip(ifStatement.Body) {
			return true
		}

		skips = true
		return false
	})

	return skips
}

func mentionsError(node ast.Node) bool {
	found := false

	ast.Inspect(node, func(n ast.Node) bool {
		identifier, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		name := strings.ToLower(identifier.Name)
		if name == "err" || strings.HasSuffix(name, "err") || strings.HasSuffix(name, "error") {
			found = true
			return false
		}
		return true
	})

	return found
}

func containsSkip(node ast.Node) bool {
	found := false

	ast.Inspect(node, func(n ast.Node) bool {
		selector, ok := n.(*ast.SelectorExpr)
		if !ok || selector.Sel == nil {
			return true
		}
		if strings.HasPrefix(selector.Sel.Name, "Skip") {
			found = true
			return false
		}
		return true
	})

	return found
}

// commandPathFromArgs turns the literal prefix of an executeLiveCLI argument
// list into a command path, skipping the testing.T receiver and any global
// flags that precede the command itself.
//
// valueFlags names the root persistent flags that consume the following
// argument, so that `executeLiveCLI(t, "--ca-file", "ca.pem", "pr", "get", id)`
// yields "pr get" rather than treating the certificate path as the command.
func commandPathFromArgs(args []ast.Expr, valueFlags map[string]bool) string {
	parts := []string{}
	skipNext := false

	for index, arg := range args {
		if index == 0 {
			// The testing.T receiver.
			continue
		}

		literal, ok := arg.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			break
		}

		value := strings.Trim(literal.Value, "\"")

		if skipNext {
			// The value belonging to the preceding global flag.
			skipNext = false
			continue
		}

		if strings.HasPrefix(value, "-") {
			if len(parts) > 0 {
				// Once the command words have started, a flag ends them.
				// Anything after it belongs to the command, not the path.
				break
			}
			// A global flag preceding the command. `--flag=value` carries its
			// own value; otherwise the next argument is the value.
			name, _, inline := strings.Cut(value, "=")
			skipNext = !inline && valueFlags[name]
			continue
		}

		parts = append(parts, value)
	}

	return strings.Join(parts, " ")
}

// globalValueFlags returns the root persistent flags that take a value, keyed by
// the spelling a caller would use. Booleans are excluded because they never
// consume the following argument.
func globalValueFlags(root *cobra.Command) map[string]bool {
	flags := map[string]bool{}

	root.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		if flag.Value.Type() == "bool" {
			return
		}
		flags["--"+flag.Name] = true
		if flag.Shorthand != "" {
			flags["-"+flag.Shorthand] = true
		}
	})

	return flags
}

// buildReport classifies every runnable command as covered, masked by a
// skip-on-error test, or never invoked live.
func buildReport(runnable []string, invoked invocations) report {
	asserted := resolveInvocations(runnable, invoked.asserted)
	masked := resolveInvocations(runnable, invoked.masked)

	coveredCommands := []string{}
	maskedCommands := []string{}
	uncovered := []string{}

	for _, command := range runnable {
		_, isAsserted := asserted[command]
		_, isMasked := masked[command]

		switch {
		case isAsserted:
			coveredCommands = append(coveredCommands, command)
		case isMasked:
			maskedCommands = append(maskedCommands, command)
		default:
			uncovered = append(uncovered, command)
		}
	}

	sort.Strings(coveredCommands)
	sort.Strings(maskedCommands)
	sort.Strings(uncovered)

	result := report{
		Version:        reportVersion,
		Covered:        coveredCommands,
		KnownMasked:    maskedCommands,
		KnownUncovered: uncovered,
	}
	result.Summary.Runnable = len(runnable)
	result.Summary.Covered = len(coveredCommands)
	result.Summary.Masked = len(maskedCommands)
	result.Summary.Uncovered = len(uncovered)
	if len(runnable) > 0 {
		result.Summary.Percent = float64(len(coveredCommands)) / float64(len(runnable)) * 100
	}

	return result
}

// resolveInvocations maps each invocation onto the command it actually ran.
//
// An invocation carries trailing positional arguments — "pr get 42" — so the
// command is the longest known path that prefixes it. Matching on any prefix
// would credit "pr get 42" to the "pr" group as well, which proves nothing
// about the group itself.
func resolveInvocations(runnable []string, invoked map[string]struct{}) map[string]struct{} {
	resolved := map[string]struct{}{}
	known := toSet(runnable)

	for invocation := range invoked {
		words := strings.Fields(invocation)
		for length := len(words); length > 0; length-- {
			candidate := strings.Join(words[:length], " ")
			if _, ok := known[candidate]; ok {
				resolved[candidate] = struct{}{}
				break
			}
		}
	}

	return resolved
}

func compareReports(committed report, current report) []string {
	problems := []string{}

	currentCovered := toSet(current.Covered)
	knownMasked := toSet(committed.KnownMasked)
	knownUncovered := toSet(committed.KnownUncovered)

	// A command that was covered must stay covered.
	for _, command := range committed.Covered {
		if _, ok := currentCovered[command]; !ok {
			problems = append(problems, fmt.Sprintf("%q lost its live coverage", command))
		}
	}

	// A newly masked command means a test was just given an escape hatch that
	// lets it pass whether or not the command works.
	for _, command := range current.KnownMasked {
		if _, ok := knownMasked[command]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%q is only covered by a live test that skips itself on error, so the suite passes whether or not it works",
			command,
		))
	}

	// Anything newly uncovered must not simply be appended to the backlog.
	for _, command := range current.KnownUncovered {
		if _, ok := knownUncovered[command]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf("%q has no live test invoking it", command))
	}

	sort.Strings(problems)
	return problems
}

func toSet(values []string) map[string]struct{} {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	return set
}

func printSummary(current report) {
	fmt.Printf(
		"command reach: %.2f%% (%d/%d runnable commands asserted by a live test)\n",
		current.Summary.Percent,
		current.Summary.Covered,
		current.Summary.Runnable,
	)
	if current.Summary.Masked > 0 {
		fmt.Printf("  masked by a skip-on-error test: %d\n", current.Summary.Masked)
	}
	if current.Summary.Uncovered > 0 {
		fmt.Printf("  never invoked live:             %d\n", current.Summary.Uncovered)
	}
}

func writeReport(path string, current report) error {
	encoded, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func readReport(path string) (report, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return report{}, err
	}

	committed := report{}
	if err := json.Unmarshal(content, &committed); err != nil {
		return report{}, err
	}

	return committed, nil
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
