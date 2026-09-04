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
const reportVersion = 2

type report struct {
	Version int `json:"version"`
	Summary struct {
		Runnable   int     `json:"runnable_commands"`
		Covered    int     `json:"covered_commands"`
		Percent    float64 `json:"covered_percent"`
		Masked     int     `json:"masked_commands"`
		DryRunOnly int     `json:"dry_run_only_commands"`
		Uncovered  int     `json:"uncovered_commands"`
	} `json:"summary"`
	// Covered lists commands a live test invokes and asserts on. Regressions
	// against this set fail verification.
	Covered []string `json:"covered"`
	// KnownMasked lists commands whose only live coverage comes from a test that
	// skips itself when the call fails, so the suite stays green whether or not
	// the command works. These are the most dangerous entries in the report: they
	// look covered in CI output and are not.
	KnownMasked []string `json:"known_masked"`
	// KnownDryRunOnly lists commands whose only live coverage runs them with
	// --dry-run. The planning path is exercised and the mutation never is, so
	// the command has never been shown to work against a server -- which is
	// how #511, #505, #506 and #503 all shipped green. Treat these as
	// uncovered; they are listed apart only so the backlog says why.
	KnownDryRunOnly []string `json:"known_dry_run_only"`
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

	if len(invoked.unreadable) > 0 {
		for _, site := range invoked.unreadable {
			fmt.Fprintln(os.Stderr, "FAIL: "+site)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "A command reached through an unreadable call is missing from the report,")
		fmt.Fprintln(os.Stderr, "which lowers reach without saying so. Keep the command words in the slice")
		fmt.Fprintln(os.Stderr, "literal the call spreads, or teach stringSliceLiterals the new shape.")
		os.Exit(1)
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
	// asserted holds commands invoked for real by at least one test that cannot
	// skip on error, so a failure there really does fail CI.
	asserted map[string]struct{}
	// masked holds commands invoked for real only by tests that skip on error.
	masked map[string]struct{}
	// unreadable holds call sites whose command words the scanner could not
	// read, so a blind spot is reported rather than silently lowering reach.
	unreadable []string
	// dryRun holds commands only ever invoked with --dry-run.
	//
	// A --dry-run invocation exercises the planning path and deliberately does
	// not send the mutation, so it says nothing about whether the command works.
	// Counting it as reach is what let four defects ship in #530: `pr update`,
	// `pr decline` and the rest were reported as covered while their mutating
	// path had never once run against a server (#532).
	dryRun map[string]struct{}
}

// discoverLiveInvocations parses the live tests and records the command paths
// passed to executeLiveCLI, split by whether the enclosing test can skip itself
// when a call fails.
//
// Attribution is per test function: if a function contains an error-conditioned
// skip, every command it invokes is treated as masked, because any of those
// calls failing can end the test as "skipped" rather than "failed".
func discoverLiveInvocations(dir string, valueFlags map[string]bool) (invocations, error) {
	found := invocations{
		asserted: map[string]struct{}{},
		masked:   map[string]struct{}{},
		dryRun:   map[string]struct{}{},
	}
	fileSet := token.NewFileSet()

	invokers, dryRunInvokers, err := resolveInvokers(dir, fileSet)
	if err != nil {
		return invocations{}, err
	}

	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
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

			real, dryRun, unreadable := commandsInvokedBy(function, valueFlags, invokers, dryRunInvokers, fileSet, path)
			found.unreadable = append(found.unreadable, unreadable...)
			if len(real) == 0 && len(dryRun) == 0 {
				continue
			}

			// A dry run proves nothing about the mutating path whether or not
			// the test can skip, so it is recorded the same way either way.
			for command := range dryRun {
				found.dryRun[command] = struct{}{}
			}

			target := found.asserted
			if skipsOnError(function) {
				target = found.masked
			}
			for command := range real {
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

	// "Dry-run only" means exactly that: any real invocation, asserted or
	// masked, takes the command out of the bucket.
	for command := range found.asserted {
		delete(found.dryRun, command)
	}
	for command := range found.masked {
		delete(found.dryRun, command)
	}

	return found, nil
}

// baseInvokers are the helpers that actually run the CLI. The stdin and MCP
// variants take an extra argument before the command words: the input, and the
// callback that drives the protocol.
var baseInvokers = map[string]int{
	"executeLiveCLI":          1,
	"executeLiveCLIWithStdin": 2,
	"executeLiveMCPServer":    2,
}

// resolveInvokers returns every function that runs the CLI, keyed by name, with
// the number of arguments that precede the command words.
//
// A test may call the CLI through a local helper -- `mustLiveCLI(t, "repo",
// "permissions", "grant", ...)` -- rather than executeLiveCLI directly.
// Recognising only the base helpers made those invocations invisible, so eight
// commands stayed listed as dry-run-only while a test was mutating them for
// real. That is the same silent loss of coverage #532 exists to stop, so a
// wrapper is followed rather than requiring every test to inline the call.
//
// A wrapper qualifies when it takes a variadic ...string and its body calls a
// known invoker. The pass repeats until it finds nothing new, so a wrapper of a
// wrapper is found too.
func resolveInvokers(dir string, fileSet *token.FileSet) (map[string]int, map[string]bool, error) {
	invokers := map[string]int{}
	// Wrappers that pass --dry-run themselves, so every call to them is a dry
	// run however the call site is spelled.
	alwaysDryRun := map[string]bool{}
	for name, leading := range baseInvokers {
		invokers[name] = leading
	}

	type candidate struct {
		name    string
		leading int
		body    *ast.BlockStmt
	}
	candidates := []candidate{}

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
			if !ok || function.Body == nil || function.Recv != nil {
				continue
			}
			if _, known := invokers[function.Name.Name]; known {
				continue
			}
			leading, variadic := variadicStringParameter(function.Type)
			if !variadic {
				continue
			}
			candidates = append(candidates, candidate{name: function.Name.Name, leading: leading, body: function.Body})
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	for {
		added := false
		for _, entry := range candidates {
			if _, known := invokers[entry.name]; known {
				continue
			}
			if !callsAnyOf(entry.body, invokers) {
				continue
			}
			invokers[entry.name] = entry.leading
			if mentionsDryRun(entry.body) {
				alwaysDryRun[entry.name] = true
			}
			added = true
		}
		if !added {
			break
		}
	}

	return invokers, alwaysDryRun, nil
}

// mentionsDryRun reports whether a function body passes --dry-run anywhere, so
// a helper that supplies it is not mistaken for one that runs the mutation.
func mentionsDryRun(body *ast.BlockStmt) bool {
	found := false

	ast.Inspect(body, func(n ast.Node) bool {
		literal, ok := n.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return true
		}
		if name, _, _ := strings.Cut(strings.Trim(literal.Value, "\""), "="); name == "--dry-run" {
			found = true
		}

		return !found
	})

	return found
}

// variadicStringParameter reports whether a function ends in a ...string, and
// how many arguments precede it.
func variadicStringParameter(signature *ast.FuncType) (leading int, ok bool) {
	if signature.Params == nil || len(signature.Params.List) == 0 {
		return 0, false
	}

	for index, field := range signature.Params.List {
		// A field can name several parameters at once: `a, b string`.
		names := len(field.Names)
		if names == 0 {
			names = 1
		}

		if index == len(signature.Params.List)-1 {
			ellipsis, isVariadic := field.Type.(*ast.Ellipsis)
			if !isVariadic {
				return 0, false
			}
			identifier, isIdent := ellipsis.Elt.(*ast.Ident)
			if !isIdent || identifier.Name != "string" {
				return 0, false
			}

			return leading, true
		}

		leading += names
	}

	return 0, false
}

func callsAnyOf(body *ast.BlockStmt, invokers map[string]int) bool {
	found := false

	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if identifier, ok := call.Fun.(*ast.Ident); ok {
			if _, known := invokers[identifier.Name]; known {
				found = true
			}
		}

		return !found
	})

	return found
}

func commandsInvokedBy(function *ast.FuncDecl, valueFlags map[string]bool, invokers map[string]int, dryRunInvokers map[string]bool, fileSet *token.FileSet, path string) (real, dryRun map[string]struct{}, unreadable []string) {
	real = map[string]struct{}{}
	dryRun = map[string]struct{}{}
	argumentSlices := stringSliceLiterals(function)
	tableRows := rangedTableRows(function)

	ast.Inspect(function.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		identifier, ok := call.Fun.(*ast.Ident)
		if !ok {
			return true
		}
		leadingArgs, ok := invokers[identifier.Name]
		if !ok {
			return true
		}
		if len(call.Args) < leadingArgs {
			return true
		}
		// `executeLiveCLI(t, args...)` carries the words in a variable, not in
		// the call. Reading only the literal arguments reported those commands
		// as never invoked -- and while every invocation counted the same that
		// went unnoticed, because a --dry-run call elsewhere covered for them.
		// The words themselves: the receiver, and the stdin or callback argument
		// where there is one, are already behind leadingArgs.
		candidates := [][]ast.Expr{call.Args[leadingArgs:]}
		if call.Ellipsis.IsValid() && len(call.Args[leadingArgs:]) == 1 {
			// A spread whose words cannot be read is reported rather than
			// skipped. Skipping is what made `repo comment update` look
			// uncovered, and the mistake was invisible: the command simply did
			// not appear, and a --dry-run call elsewhere covered for it.
			//
			// Resolving arbitrary Go is not the job here. Saying plainly that
			// this call could not be read is, so the author either keeps the
			// words in the literal or teaches the scanner the new shape.
			resolved, reason := spreadElements(call.Args[leadingArgs], argumentSlices, tableRows)
			if reason != "" {
				unreadable = append(unreadable, describeCall(fileSet, path, call, reason))

				return true
			}
			candidates = resolved
		}

		for _, args := range candidates {
			commandPath, isDryRun := commandPathFromArgs(args, valueFlags)
			// A helper that supplies --dry-run itself makes every one of its
			// callers a dry run, however the call site reads.
			isDryRun = isDryRun || dryRunInvokers[identifier.Name]
			if commandPath == "" {
				continue
			}
			if isDryRun {
				dryRun[commandPath] = struct{}{}

				continue
			}
			real[commandPath] = struct{}{}
		}

		return true
	})

	return real, dryRun, unreadable
}

// rangedTableRows maps each range variable over a table literal to the word
// lists its rows carry.
//
// A table-driven test spreads one row per iteration -- `executeLiveCLI(t,
// testCase.args...)` over a []struct{args []string} literal, or `args...` over
// a [][]string one. The call site is a single expression standing for several
// invocations, so every []string{...} inside the table counts as one.
func rangedTableRows(function *ast.FuncDecl) map[string][][]ast.Expr {
	tables := map[string][][]ast.Expr{}

	// Tables assigned to a name first, so `range tests` can find them.
	literals := map[string]*ast.CompositeLit{}
	ast.Inspect(function.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		name, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		if literal, ok := assign.Rhs[0].(*ast.CompositeLit); ok {
			literals[name.Name] = literal
		}

		return true
	})

	ast.Inspect(function.Body, func(n ast.Node) bool {
		loop, ok := n.(*ast.RangeStmt)
		if !ok || loop.Value == nil {
			return true
		}
		variable, ok := loop.Value.(*ast.Ident)
		if !ok || variable.Name == "_" {
			return true
		}

		var table *ast.CompositeLit
		switch source := loop.X.(type) {
		case *ast.CompositeLit:
			table = source
		case *ast.Ident:
			table = literals[source.Name]
		}
		if table == nil {
			return true
		}

		rows := [][]ast.Expr{}
		ast.Inspect(table, func(inner ast.Node) bool {
			literal, ok := inner.(*ast.CompositeLit)
			if !ok {
				return true
			}
			// Inside a [][]string the element type is elided, so a row reads as
			// {"browse", "--wiki"} with no type at all. Missing that case left
			// the whole table unreadable.
			if !isStringSliceType(literal.Type) && !(literal.Type == nil && allStringLiterals(literal.Elts)) {
				return true
			}
			rows = append(rows, literal.Elts)

			return true
		})
		if len(rows) > 0 {
			tables[variable.Name] = rows
		}

		return true
	})

	return tables
}

// allStringLiterals reports whether every element is a string constant, which
// is what an elided row of a [][]string looks like.
func allStringLiterals(elements []ast.Expr) bool {
	if len(elements) == 0 {
		return false
	}

	for _, element := range elements {
		literal, ok := element.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			return false
		}
	}

	return true
}

// stringSliceLiterals maps each local []string{...} in a function to its
// elements, so a call that spreads one can be read as if the words were written
// out at the call site.
func stringSliceLiterals(function *ast.FuncDecl) map[string][]ast.Expr {
	found := map[string][]ast.Expr{}

	ast.Inspect(function.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		name, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}

		switch value := assign.Rhs[0].(type) {
		case *ast.CompositeLit:
			if isStringSliceType(value.Type) {
				found[name.Name] = value.Elts
			}
		case *ast.CallExpr:
			// `args := append([]string{"--json", "repo", ...}, extra...)`: the
			// command words are in the first argument, and the rest are flags
			// that follow them, which the path scan stops at anyway.
			if identifier, ok := value.Fun.(*ast.Ident); !ok || identifier.Name != "append" || len(value.Args) == 0 {
				return true
			}
			if literal, ok := value.Args[0].(*ast.CompositeLit); ok && isStringSliceType(literal.Type) {
				found[name.Name] = literal.Elts
			}
		}

		return true
	})

	return found
}

func isStringSliceType(expr ast.Expr) bool {
	arrayType, ok := expr.(*ast.ArrayType)
	if !ok || arrayType.Len != nil {
		return false
	}
	identifier, ok := arrayType.Elt.(*ast.Ident)

	return ok && identifier.Name == "string"
}

// spreadElements reads the command words out of whatever a call spreads, or
// says why it could not.
//
// Three shapes appear in the live tests, and all three put the words in a
// literal: the variable, the literal written at the call site, and an append
// onto a literal -- `executeLiveCLI(t, append([]string{"--json", "pr", ...},
// args...)...)`. The trailing arguments of an append are flags that follow the
// command, which the path scan stops at anyway.
func spreadElements(expr ast.Expr, argumentSlices map[string][]ast.Expr, tableRows map[string][][]ast.Expr) (candidates [][]ast.Expr, reason string) {
	switch value := expr.(type) {
	case *ast.Ident:
		if rows, known := tableRows[value.Name]; known {
			return rows, ""
		}
		found, known := argumentSlices[value.Name]
		if !known {
			return nil, fmt.Sprintf("%s is not assigned a []string{...} literal in this function", value.Name)
		}

		return [][]ast.Expr{found}, ""

	case *ast.SelectorExpr:
		// `testCase.args` in a table-driven test: the rows carry the words in a
		// field, and the range variable is what names them.
		receiver, ok := value.X.(*ast.Ident)
		if !ok {
			return nil, "the spread argument is a field of something this scanner cannot read"
		}
		rows, known := tableRows[receiver.Name]
		if !known {
			return nil, fmt.Sprintf("%s is not a range variable over a table literal in this function", receiver.Name)
		}

		return rows, ""

	case *ast.CompositeLit:
		if !isStringSliceType(value.Type) {
			return nil, "the spread literal is not a []string"
		}

		return [][]ast.Expr{value.Elts}, ""

	case *ast.CallExpr:
		identifier, ok := value.Fun.(*ast.Ident)
		if !ok || identifier.Name != "append" || len(value.Args) == 0 {
			return nil, "the spread argument is a call this scanner cannot read"
		}

		return spreadElements(value.Args[0], argumentSlices, tableRows)

	default:
		return nil, "the spread argument is neither a variable nor a slice literal"
	}
}

// describeCall names a call site the scanner could not read, so the report says
// where to look rather than quietly leaving a command out.
func describeCall(fileSet *token.FileSet, path string, call *ast.CallExpr, reason string) string {
	return fmt.Sprintf("%s:%d: cannot read the command words (%s)", path, fileSet.Position(call.Pos()).Line, reason)
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
// commandPathFromArgs returns the command an invocation runs, and whether it
// runs it as a dry run.
//
// --dry-run is a root persistent bool, so it is dropped by the loop below
// either side of the command words: before them it looks like any global flag,
// after them it ends the path. Dropping it is right for identifying the
// command and wrong for deciding what was tested, so it is picked out
// separately here rather than silently vanishing (#532).
func commandPathFromArgs(args []ast.Expr, valueFlags map[string]bool) (string, bool) {
	parts := []string{}
	skipNext := false
	dryRun := false

	for _, arg := range args {
		literal, ok := arg.(*ast.BasicLit)
		if !ok || literal.Kind != token.STRING {
			continue
		}
		if name, _, _ := strings.Cut(strings.Trim(literal.Value, "\""), "="); name == "--dry-run" {
			dryRun = true

			break
		}
	}

	for _, arg := range args {
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

	return strings.Join(parts, " "), dryRun
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
	dryRun := resolveInvocations(runnable, invoked.dryRun)

	coveredCommands := []string{}
	maskedCommands := []string{}
	dryRunOnly := []string{}
	uncovered := []string{}

	for _, command := range runnable {
		_, isAsserted := asserted[command]
		_, isMasked := masked[command]
		_, isDryRunOnly := dryRun[command]

		switch {
		case isAsserted:
			coveredCommands = append(coveredCommands, command)
		case isMasked:
			maskedCommands = append(maskedCommands, command)
		case isDryRunOnly:
			dryRunOnly = append(dryRunOnly, command)
		default:
			uncovered = append(uncovered, command)
		}
	}

	sort.Strings(coveredCommands)
	sort.Strings(maskedCommands)
	sort.Strings(dryRunOnly)
	sort.Strings(uncovered)

	result := report{
		Version:         reportVersion,
		Covered:         coveredCommands,
		KnownMasked:     maskedCommands,
		KnownDryRunOnly: dryRunOnly,
		KnownUncovered:  uncovered,
	}
	result.Summary.Runnable = len(runnable)
	result.Summary.Covered = len(coveredCommands)
	result.Summary.Masked = len(maskedCommands)
	result.Summary.DryRunOnly = len(dryRunOnly)
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
	knownDryRunOnly := toSet(committed.KnownDryRunOnly)

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

	// A command that slips back to dry-run-only has lost its mutating coverage
	// while still looking exercised in the test output, which is the failure
	// mode #532 exists to stop.
	for _, command := range current.KnownDryRunOnly {
		if _, ok := knownDryRunOnly[command]; ok {
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%q is only invoked live with --dry-run, so its mutating path never runs against a server",
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
	if current.Summary.DryRunOnly > 0 {
		fmt.Printf("  only ever run with --dry-run:   %d\n", current.Summary.DryRunOnly)
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
