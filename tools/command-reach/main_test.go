package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

func writeLiveTestFile(t *testing.T, source string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sample_live_test.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write sample: %v", err)
	}

	return dir
}

func TestDiscoverLiveInvocationsSplitsAssertedFromMasked(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestAsserted(t *testing.T) {
	output, err := executeLiveCLI(t, "--json", "tag", "list")
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	_ = output
}

func TestMasked(t *testing.T) {
	output, err := executeLiveCLI(t, "--json", "pr", "task", "list", prID, "--state", "all")
	if err != nil {
		if strings.Contains(err.Error(), "not_found") {
			t.Skipf("endpoint unavailable: %v", err)
		}
		t.Fatalf("failed: %v", err)
	}
	_ = output
}

func TestEnvironmentSkipIsNotMasking(t *testing.T) {
	if os.Getenv("BITBUCKET_URL") == "" {
		t.Skip("live environment required")
	}
	if _, err := executeLiveCLI(t, "repo", "list"); err != nil {
		t.Fatalf("failed: %v", err)
	}
}
`)

	found, err := discoverLiveInvocations(dir, testValueFlags())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	if _, ok := found.asserted["tag list"]; !ok {
		t.Fatalf("expected %q to be asserted, got %#v", "tag list", found.asserted)
	}
	if _, ok := found.masked["pr task list"]; !ok {
		t.Fatalf("expected %q to be masked by the skip-on-error branch, got %#v", "pr task list", found.masked)
	}
	if _, ok := found.asserted["pr task list"]; ok {
		t.Fatalf("a command behind a skip-on-error test must not count as asserted")
	}
	// Skipping because the environment is not configured says nothing about
	// whether the command works, so it must not mask anything.
	if _, ok := found.asserted["repo list"]; !ok {
		t.Fatalf("an environment precondition skip must not mask coverage, got %#v", found.masked)
	}
}

// A command asserted by one test and skipped by another is genuinely covered:
// the asserting test still fails when it breaks.
func TestAssertedElsewhereWins(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestSkips(t *testing.T) {
	if _, err := executeLiveCLI(t, "pr", "get", prID); err != nil {
		t.Skipf("unavailable: %v", err)
	}
}

func TestAsserts(t *testing.T) {
	if _, err := executeLiveCLI(t, "pr", "get", prID); err != nil {
		t.Fatalf("failed: %v", err)
	}
}
`)

	found, err := discoverLiveInvocations(dir, testValueFlags())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	if _, ok := found.asserted["pr get"]; !ok {
		t.Fatalf("expected the asserting test to win, got asserted=%#v", found.asserted)
	}
	if _, ok := found.masked["pr get"]; ok {
		t.Fatalf("expected %q not to remain masked", "pr get")
	}
}

func TestCommandPathIgnoresFlagsAndStopsAtVariables(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestFlags(t *testing.T) {
	_, _ = executeLiveCLI(t, "--json", "--dry-run", "repo", "settings", "security", "permissions", "users", "list", "--limit", "200")
	_, _ = executeLiveCLI(t, "branch", "create", branchName, "--from", "main")
}
`)

	found, err := discoverLiveInvocations(dir, testValueFlags())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	// The first invocation carries --dry-run and both discard their results, so
	// neither counts as coverage. What is under test here is only that the
	// leading flags were skipped to find the command.
	if _, ok := found.dryRun["repo settings security permissions users list"]; !ok {
		t.Fatalf("expected leading flags to be skipped, got %#v", found.dryRun)
	}
	// Collection stops at the first non-literal argument, so a trailing flag
	// after a variable is not folded into the command path.
	if _, ok := found.masked["branch create"]; !ok {
		t.Fatalf("expected collection to stop at the variable argument, got %#v", found.masked)
	}
}

func TestBuildReportClassifies(t *testing.T) {
	runnable := []string{"pr get", "pr task list", "repo list", "tag list"}
	invoked := invocations{
		asserted: map[string]struct{}{"pr get": {}, "tag list": {}},
		masked:   map[string]struct{}{"pr task list": {}},
	}

	result := buildReport(runnable, invoked)

	if got := strings.Join(result.Covered, ","); got != "pr get,tag list" {
		t.Fatalf("covered = %q", got)
	}
	if got := strings.Join(result.KnownMasked, ","); got != "pr task list" {
		t.Fatalf("masked = %q", got)
	}
	if got := strings.Join(result.KnownUncovered, ","); got != "repo list" {
		t.Fatalf("uncovered = %q", got)
	}
	if result.Summary.Runnable != 4 || result.Summary.Covered != 2 || result.Summary.Masked != 1 || result.Summary.Uncovered != 1 {
		t.Fatalf("unexpected summary: %#v", result.Summary)
	}
	if result.Summary.Percent != 50 {
		t.Fatalf("expected 50%% coverage, got %v", result.Summary.Percent)
	}
}

// An invocation carries trailing positional arguments, so it resolves to the
// longest known command that prefixes it — never to an ancestor group and never
// to a partial word.
func TestResolveInvocationsPicksTheLongestMatch(t *testing.T) {
	runnable := []string{"pr", "pr get", "tag view", "repo list"}
	invoked := map[string]struct{}{
		"pr get 42":         {},
		"tag view v1 extra": {},
		"unknown thing":     {},
	}

	resolved := resolveInvocations(runnable, invoked)

	if _, ok := resolved["pr get"]; !ok {
		t.Fatalf("expected %q to resolve from its positional invocation, got %#v", "pr get", resolved)
	}
	if _, ok := resolved["pr"]; ok {
		t.Fatal("a parent group must not be credited by a child invocation")
	}
	if _, ok := resolved["tag view"]; !ok {
		t.Fatalf("expected %q to resolve, got %#v", "tag view", resolved)
	}
	if _, ok := resolved["repo list"]; ok {
		t.Fatal("a command nothing invoked must not resolve")
	}
	if len(resolved) != 2 {
		t.Fatalf("an unknown invocation must resolve to nothing, got %#v", resolved)
	}
}

// A command that is itself runnable and also has subcommands is only covered by
// an invocation of the command itself.
func TestResolveInvocationsCreditsRunnableParentOnDirectInvocation(t *testing.T) {
	runnable := []string{"pr", "pr get"}

	resolved := resolveInvocations(runnable, map[string]struct{}{"pr": {}})

	if _, ok := resolved["pr"]; !ok {
		t.Fatalf("expected a direct invocation to credit the command, got %#v", resolved)
	}
	if _, ok := resolved["pr get"]; ok {
		t.Fatal("invoking a group must not credit its subcommands")
	}
}

func TestCompareReportsFlagsRegressions(t *testing.T) {
	committed := report{
		Covered:        []string{"pr get", "tag list"},
		KnownMasked:    []string{"pr task list"},
		KnownUncovered: []string{"repo list"},
	}

	t.Run("no change passes", func(t *testing.T) {
		if problems := compareReports(committed, committed); len(problems) != 0 {
			t.Fatalf("expected no problems, got %v", problems)
		}
	})

	t.Run("lost coverage fails", func(t *testing.T) {
		current := report{Covered: []string{"pr get"}, KnownUncovered: []string{"tag list"}}
		problems := compareReports(committed, current)
		if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), "lost its live coverage") {
			t.Fatalf("expected a lost-coverage failure, got %v", problems)
		}
	})

	t.Run("new uncovered command fails", func(t *testing.T) {
		current := report{
			Covered:        committed.Covered,
			KnownMasked:    committed.KnownMasked,
			KnownUncovered: []string{"repo list", "brand new"},
		}
		problems := compareReports(committed, current)
		if len(problems) != 1 || !strings.Contains(problems[0], "no live test invoking it") {
			t.Fatalf("expected exactly one new-uncovered failure, got %v", problems)
		}
	})

	t.Run("new masked command fails", func(t *testing.T) {
		current := report{
			Covered:        []string{"pr get"},
			KnownMasked:    []string{"pr task list", "tag list"},
			KnownUncovered: committed.KnownUncovered,
		}
		problems := compareReports(committed, current)
		joined := strings.Join(problems, "\n")
		if !strings.Contains(joined, "skips itself on error") {
			t.Fatalf("expected a newly masked command to fail, got %v", problems)
		}
	})

	t.Run("clearing the backlog passes", func(t *testing.T) {
		current := report{
			Covered:     []string{"pr get", "tag list", "pr task list", "repo list"},
			KnownMasked: []string{},
		}
		if problems := compareReports(committed, current); len(problems) != 0 {
			t.Fatalf("improving coverage must not fail, got %v", problems)
		}
	})
}

// The real command tree must produce sane paths; a regression here would make
// every command look uncovered.
func TestCollectRunnableCommandsUsesRootRelativePaths(t *testing.T) {
	paths := collectRunnableCommands(newRootForTest())

	if len(paths) == 0 {
		t.Fatal("expected runnable commands")
	}

	seen := map[string]struct{}{}
	for _, path := range paths {
		if strings.HasPrefix(path, "bb ") {
			t.Fatalf("paths must be root-relative, got %q", path)
		}
		if _, duplicate := seen[path]; duplicate {
			t.Fatalf("duplicate command path %q", path)
		}
		seen[path] = struct{}{}
	}

	for _, expected := range []string{"pr get", "pr comment list", "tag list"} {
		if _, ok := seen[expected]; !ok {
			t.Fatalf("expected %q in the command tree", expected)
		}
	}
	if _, ok := seen["help"]; ok {
		t.Fatal("help must be excluded")
	}
}

func newRootForTest() *cobra.Command {
	return cli.NewRootCommand()
}

// testValueFlags mirrors the root persistent flags that consume a value.
func testValueFlags() map[string]bool {
	return globalValueFlags(newRootForTest())
}

// A value-taking global flag before the command used to swallow the command:
// the flag was skipped but its value was then read as the first command word,
// so the whole invocation resolved to nothing and the command looked uncovered.
func TestCommandPathSkipsGlobalFlagValues(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestValueFlags(t *testing.T) {
	_, _ = executeLiveCLI(t, "--ca-file", "ca.pem", "pr", "get", prID)
	_, _ = executeLiveCLI(t, "--log-level", "debug", "--json", "tag", "list")
	_, _ = executeLiveCLI(t, "--request-timeout=30s", "repo", "list")
	_, _ = executeLiveCLI(t, "--json", "--dry-run", "branch", "create", name)
}
`)

	found, err := discoverLiveInvocations(dir, testValueFlags())
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	// These fixtures discard their results, so they are discovered as masked:
	// the command ran and nothing was checked.
	for _, expected := range []string{"pr get", "tag list", "repo list"} {
		if _, ok := found.masked[expected]; !ok {
			t.Fatalf("expected %q to be discovered, got %#v", expected, found.masked)
		}
	}
	// The branch create call is a dry run, so it is discovered as one.
	if _, ok := found.dryRun["branch create"]; !ok {
		t.Fatalf("expected %q to be discovered as a dry run, got %#v", "branch create", found.dryRun)
	}
	// The flag values must never be mistaken for command words.
	for _, unexpected := range []string{"ca.pem", "debug", "ca.pem pr get"} {
		if _, ok := found.masked[unexpected]; ok {
			t.Fatalf("flag value %q leaked into the command path", unexpected)
		}
	}
}

// Boolean globals take no value, so the argument after them is the command.
func TestGlobalValueFlagsExcludesBooleans(t *testing.T) {
	flags := testValueFlags()

	for _, boolean := range []string{"--json", "--dry-run", "--no-color", "--insecure-skip-verify"} {
		if flags[boolean] {
			t.Fatalf("%s is a boolean and must not consume the next argument", boolean)
		}
	}
	for _, valued := range []string{"--ca-file", "--log-level", "--request-timeout", "--retry-backoff"} {
		if !flags[valued] {
			t.Fatalf("%s takes a value and must consume the next argument", valued)
		}
	}
}

// TestDryRunInvocationsAreNotCoverage is #532.
//
// --dry-run is a root persistent bool, so the path scan drops it either side of
// the command words and every dry run counted as reach. The report therefore
// read 100% while 16 mutating commands had never once run against a server,
// which is how #503, #505, #506 and #511 all shipped green.
func TestDryRunInvocationsAreNotCoverage(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestDryRunBeforeTheCommand(t *testing.T) {
	if _, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "approve", prID); err != nil {
		t.Fatalf("failed: %v", err)
	}
}

func TestDryRunAfterTheCommand(t *testing.T) {
	if _, err := executeLiveCLI(t, "--json", "branch", "default", "set", "main", "--dry-run"); err != nil {
		t.Fatalf("failed: %v", err)
	}
}

func TestDryRunAsAnAssignedValue(t *testing.T) {
	if _, err := executeLiveCLI(t, "--json", "repo", "archive", "--dry-run=true"); err != nil {
		t.Fatalf("failed: %v", err)
	}
}

func TestRealInvocation(t *testing.T) {
	if _, err := executeLiveCLI(t, "--json", "tag", "list"); err != nil {
		t.Fatalf("failed: %v", err)
	}
}
`)

	found, err := discoverLiveInvocations(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	// An invocation keeps its trailing positionals -- "branch default set main"
	// -- so the check goes through the same resolution the report uses.
	runnable := []string{"pr review approve", "branch default set", "repo archive", "tag list"}
	dryRunOnly := resolveInvocations(runnable, found.dryRun)
	asserted := resolveInvocations(runnable, found.asserted)

	// Every spelling of the flag has to be recognised. Missing one would put
	// the command back in "covered" on the strength of a call that never
	// mutates anything.
	for _, command := range []string{
		"pr review approve",  // --dry-run before the command words
		"branch default set", // --dry-run after them
		"repo archive",       // --dry-run=true
	} {
		if _, ok := dryRunOnly[command]; !ok {
			t.Errorf("%q was not recognised as dry-run-only; found %v", command, keysOf(found.dryRun))
		}
		if _, ok := asserted[command]; ok {
			t.Errorf("%q counted as asserted coverage on the strength of a --dry-run call", command)
		}
	}

	if _, ok := asserted["tag list"]; !ok {
		t.Errorf("a real invocation stopped counting; asserted=%v", keysOf(found.asserted))
	}
}

// A command run both ways is covered: the dry run is extra, not a downgrade.
func TestARealInvocationBeatsADryRun(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestPreviewFirst(t *testing.T) {
	if _, err := executeLiveCLI(t, "--json", "--dry-run", "pr", "review", "approve", prID); err != nil {
		t.Fatalf("failed: %v", err)
	}
}

func TestThenForReal(t *testing.T) {
	if _, err := executeLiveCLI(t, "--json", "pr", "review", "approve", prID); err != nil {
		t.Fatalf("failed: %v", err)
	}
}
`)

	found, err := discoverLiveInvocations(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	if _, ok := found.asserted["pr review approve"]; !ok {
		t.Error("a real invocation must count even when a dry run of the same command exists")
	}
	if _, ok := found.dryRun["pr review approve"]; ok {
		t.Error("a command invoked for real must not also be reported as dry-run-only")
	}
}

// The words may live in a variable rather than at the call site. Reading only
// the literal arguments reported `repo comment update` as never invoked for
// real, and the mistake was invisible while a --dry-run call covered for it.
func TestCommandWordsInASliceVariableAreFound(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestSpreadSlice(t *testing.T) {
	updateArgs := []string{"--json", "repo", "comment", "update", "--pr", prID, "--text", "x"}
	if _, err := executeLiveCLI(t, updateArgs...); err != nil {
		t.Fatalf("failed: %v", err)
	}
}

func TestSpreadAppend(t *testing.T) {
	args := append([]string{"--json", "branch", "model", "update"}, extra...)
	if _, err := executeLiveCLI(t, args...); err != nil {
		t.Fatalf("failed: %v", err)
	}
}
`)

	found, err := discoverLiveInvocations(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	for _, command := range []string{"repo comment update", "branch model update"} {
		if _, ok := found.asserted[command]; !ok {
			t.Errorf("%q was not found in a spread slice; asserted=%v", command, keysOf(found.asserted))
		}
	}
}

func keysOf(set map[string]struct{}) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}

	return names
}

// A test may run the CLI through a local helper rather than executeLiveCLI.
// Recognising only the base helpers made those invocations invisible, so eight
// commands stayed listed as dry-run-only while a test was mutating them for
// real -- the same silent loss of coverage #532 exists to stop.
func TestCommandsReachedThroughAWrapperCount(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func mustLiveCLI(t *testing.T, args ...string) string {
	output, err := executeLiveCLI(t, append([]string{"--json"}, args...)...)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	return output
}

func alsoWrapped(t *testing.T, args ...string) string {
	return mustLiveCLI(t, args...)
}

func TestGrant(t *testing.T) {
	mustLiveCLI(t, "project", "permissions", "grant", key, name, "PROJECT_READ")
	alsoWrapped(t, "repo", "permissions", "revoke", name)
}
`)

	found, err := discoverLiveInvocations(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	// The second proves a wrapper of a wrapper is followed too.
	for _, command := range []string{"project permissions grant", "repo permissions revoke"} {
		resolved := resolveInvocations([]string{command}, found.asserted)
		if _, ok := resolved[command]; !ok {
			t.Errorf("%q invoked through a helper was not counted; asserted=%v", command, keysOf(found.asserted))
		}
	}
}

// The hole a wrapper opens: if the helper supplies --dry-run itself, its
// callers look like real invocations while nothing is ever mutated. That is the
// original defect wearing a different hat, so it is closed explicitly.
func TestAWrapperThatSuppliesDryRunIsStillADryRun(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func previewOnly(t *testing.T, args ...string) string {
	output, err := executeLiveCLI(t, append([]string{"--json", "--dry-run"}, args...)...)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	return output
}

func TestPreview(t *testing.T) {
	previewOnly(t, "branch", "default", "set", "main")
}
`)

	found, err := discoverLiveInvocations(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	runnable := []string{"branch default set"}
	if _, ok := resolveInvocations(runnable, found.asserted)[runnable[0]]; ok {
		t.Error("a helper that passes --dry-run must not give its callers real coverage")
	}
	if _, ok := resolveInvocations(runnable, found.dryRun)[runnable[0]]; !ok {
		t.Errorf("expected the wrapped call to be recorded as a dry run; dryRun=%v", keysOf(found.dryRun))
	}
}

// The scanner used to skip a call whose words it could not read, which is how
// `repo comment update` looked uncovered: the command simply did not appear,
// and a --dry-run call elsewhere covered for it. A blind spot that lowers reach
// without saying so is the failure this tool exists to prevent, so an
// unreadable call is now reported.
func TestAnUnreadableCallIsReportedRatherThanSkipped(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestBuiltUpAcrossStatements(t *testing.T) {
	var args []string
	args = append(args, "repo", "delete")
	if _, err := executeLiveCLI(t, args...); err != nil {
		t.Fatalf("failed: %v", err)
	}
}
`)

	found, err := discoverLiveInvocations(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	if len(found.unreadable) == 0 {
		t.Fatal("a call whose words cannot be read must be reported, not skipped")
	}
	if !strings.Contains(found.unreadable[0], "sample_live_test.go:6") {
		t.Errorf("the report must name the call site, got: %s", found.unreadable[0])
	}
}

// Three shapes put the words somewhere the literal-only scan could not see, and
// all three appear in the live suite. Each was found by the report above rather
// than by anyone noticing a command had gone missing.
func TestTheShapesTheScannerCanRead(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestInlineAppend(t *testing.T) {
	if _, err := executeLiveCLI(t, append([]string{"--json", "pr", "comment", "add", prID}, extra...)...); err != nil {
		t.Fatalf("failed: %v", err)
	}
}

func TestTableOfStructs(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "list", args: []string{"tag", "list"}},
		{name: "get", args: []string{"repo", "archive"}},
	}
	for _, testCase := range tests {
		if _, err := executeLiveCLI(t, testCase.args...); err != nil {
			t.Fatalf("failed: %v", err)
		}
	}
}

func TestTableOfSlices(t *testing.T) {
	unsupported := [][]string{
		{"browse", "--wiki"},
		{"branch", "list"},
	}
	for _, args := range unsupported {
		if _, err := executeLiveCLI(t, args...); err != nil {
			t.Fatalf("failed: %v", err)
		}
	}
}
`)

	found, err := discoverLiveInvocations(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if len(found.unreadable) > 0 {
		t.Fatalf("these shapes must all be readable, got: %v", found.unreadable)
	}

	runnable := []string{"pr comment add", "tag list", "repo archive", "browse", "branch list"}
	resolved := resolveInvocations(runnable, found.asserted)

	for _, command := range runnable {
		if _, ok := resolved[command]; !ok {
			t.Errorf("%q was not found; asserted=%v", command, keysOf(found.asserted))
		}
	}
}

// A command whose result is thrown away is not covered by it.
//
// `_, _ = executeLiveCLI(...)` runs the command and checks nothing: it would
// pass identically if the command returned the wrong answer or failed outright.
// Four commands were counted as covered on exactly that basis, alongside a test
// that logged its failure and returned.
func TestAnInvocationThatChecksNothingIsMasked(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func TestDiscarded(t *testing.T) {
	_, _ = executeLiveCLI(t, "--json", "reviewer", "condition", "delete", id)
}

func TestLoggedAndReturned(t *testing.T) {
	output, err := executeLiveCLI(t, "--json", "reviewer", "condition", "create", body)
	if err != nil {
		t.Logf("attempt output: %s (err: %v)", output, err)
		return
	}
}

func TestActuallyChecked(t *testing.T) {
	if _, err := executeLiveCLI(t, "--json", "tag", "list"); err != nil {
		t.Fatalf("failed: %v", err)
	}
}
`)

	found, err := discoverLiveInvocations(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	for _, command := range []string{"reviewer condition delete", "reviewer condition create"} {
		resolved := resolveInvocations([]string{command}, found.masked)
		if _, ok := resolved[command]; !ok {
			t.Errorf("%q checks nothing but was not masked; masked=%v asserted=%v",
				command, keysOf(found.masked), keysOf(found.asserted))
		}
	}

	if _, ok := found.asserted["tag list"]; !ok {
		t.Error("a test that fails on error must still count as coverage")
	}
}

// A wrapper forwarding its own variadic parameter is not an invocation, it is
// the forwarding. Reporting it as unreadable demands a literal that cannot
// exist -- the command words are at its callers, which are read separately.
//
// mustLiveCLI hid this because it wraps with append([]string{"--json"}, ...),
// which resolves to a literal that yields no command path and is skipped
// silently. A helper that forwards args bare made it visible.
func TestAWrapperForwardingItsOwnArgsIsNotUnreadable(t *testing.T) {
	dir := writeLiveTestFile(t, `package live_test

func mustLiveHumanCLI(t *testing.T, args ...string) string {
	output, err := executeLiveCLI(t, args...)
	if err != nil {
		t.Fatalf("failed: %v", err)
	}
	return output
}

func TestUsesIt(t *testing.T) {
	mustLiveHumanCLI(t, "pr", "list", "--state", "open")
}
`)

	found, err := discoverLiveInvocations(dir, map[string]bool{})
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	if len(found.unreadable) > 0 {
		t.Errorf("the wrapper's own forwarding call was reported: %v", found.unreadable)
	}
	if _, ok := resolveInvocations([]string{"pr list"}, found.asserted)["pr list"]; !ok {
		t.Errorf("the caller's command was not found; asserted=%v", keysOf(found.asserted))
	}
}
