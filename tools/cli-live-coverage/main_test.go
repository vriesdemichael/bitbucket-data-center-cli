package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli"
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

	found, err := discoverLiveInvocations(dir)
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

	found, err := discoverLiveInvocations(dir)
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

	found, err := discoverLiveInvocations(dir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}

	if _, ok := found.asserted["repo settings security permissions users list"]; !ok {
		t.Fatalf("expected leading flags to be skipped, got %#v", found.asserted)
	}
	// Collection stops at the first non-literal argument, so a trailing flag
	// after a variable is not folded into the command path.
	if _, ok := found.asserted["branch create"]; !ok {
		t.Fatalf("expected collection to stop at the variable argument, got %#v", found.asserted)
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
