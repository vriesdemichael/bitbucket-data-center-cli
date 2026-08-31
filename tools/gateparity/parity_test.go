package gateparity

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This test asserts ADR-065's rule: every quality gate that a git hook can run
// runs both locally and in CI.
//
// "That a git hook can run" is a statement about cost, not capability. It used
// to be about capability: before ADR-043 the live suite needed a licensed
// Bitbucket that CI could not provision, so "CI-safe" meant "runs without one".
// CI provisions its own instance now, on forks included, so every check can run
// anywhere. What is left is that booting Bitbucket costs minutes, and a
// contributor who has not started the stack should still be able to push.
//
// The two lists had drifted in both directions before anyone compared them.
// Three gates ran only in the pre-push hook and two only in CI.
//
// The CI-only ones merely waste a round trip. The local-only ones are worse: a
// gate running in one place is checked by nobody. openapi:operation-paths:verify
// reported 468 stale operations on a clean main and was believed, because CI did
// not run it and so could not contradict it -- the tool was in fact reading Go
// files out of gitignored agent worktrees. Parity is what makes a gate
// falsifiable.
//
// It compares transitive closures rather than literal lines, because the two
// sides legitimately differ in shape. Locally the entry points are lefthook
// commands that fan out through Taskfile references; in CI they are steps
// spread over four jobs at whatever granularity reads well in the workflow UI.
// Closing over Taskfile references makes `task quality:verify` on one side and
// eleven enumerated steps on the other compare equal, which is the property
// worth asserting.
//
// The alternative was to have CI run `task quality:verify` as a single step,
// making drift impossible by construction. ADR-065 rejects that: it collapses
// eleven named steps into one in the workflow UI, and losing which gate failed
// at a glance is a cost paid on every failure.
const (
	taskfilePath   = "Taskfile.yml"
	lefthookPath   = "lefthook.yml"
	ciWorkflowPath = ".github/workflows/ci.yml"

	// liveJob boots Bitbucket. Its gates could run in a hook -- nothing stops
	// them since ADR-043 -- but a full stack boot on every push is a cost
	// nobody wants, so they are CI-only by choice and excluded from both sides.
	liveJob = "live-tests"
)

// exemptFromParity are gates that legitimately run on only one side, each with
// the reason. Anything else in one closure and not the other fails.
//
// Keep this as close to empty as the facts allow. Every entry is either a gate
// a developer cannot run before pushing -- a round trip to CI someone pays for
// -- or one CI does not enforce, which makes it advisory.
var exemptFromParity = map[string]string{
	// The docs job renders a changelog from the releases API before building,
	// which a developer has no token for and does not need.
	"docs:build": "CI renders the changelog from the releases API before building",
	// The same tests over the same packages, twice, differing only in whether a
	// coverage profile is written. The hook skips the profile because nothing
	// local reads it and instrumentation costs time on every commit; CI writes
	// it because the coverage gates consume it.
	"test:go:safe":       "the pre-commit form of test:unit:coverage, without the profile",
	"test:unit:coverage": "the CI form of test:go:safe, which also writes the profile the gates read",
}

func TestEveryHookRunnableGateRunsOnBothSides(t *testing.T) {
	root := repositoryRoot(t)
	references := taskReferences(t, filepath.Join(root, taskfilePath))

	local := closure(localEntryPoints(t, filepath.Join(root, lefthookPath)), references)
	ci := closure(ciEntryPoints(t, filepath.Join(root, ciWorkflowPath)), references)

	// A parser that has stopped matching would report perfect agreement, which
	// is the failure mode this whole test exists to avoid.
	if len(local) < 5 {
		t.Fatalf("expected the local hooks to reach at least 5 gates, got %v; the %s parser has probably stopped matching", local, lefthookPath)
	}
	if len(ci) < 5 {
		t.Fatalf("expected the CI jobs to reach at least 5 gates, got %v; the %s parser has probably stopped matching", ci, ciWorkflowPath)
	}

	for _, gate := range difference(local, ci) {
		if reason, ok := exemptFromParity[gate]; ok {
			t.Logf("%s is local-only by design: %s", gate, reason)
			continue
		}
		t.Errorf(
			"gate %q runs locally but not in CI.\n"+
				"A gate that runs only in a git hook is advisory: nothing stops a branch that skipped the hook.\n"+
				"Add it to a non-live job in %s, or record why not in exemptFromParity.",
			gate, ciWorkflowPath,
		)
	}

	for _, gate := range difference(ci, local) {
		if reason, ok := exemptFromParity[gate]; ok {
			t.Logf("%s is CI-only by design: %s", gate, reason)
			continue
		}
		t.Errorf(
			"gate %q runs in CI but not locally.\n"+
				"That makes a pull request the first place a developer discovers the failure.\n"+
				"Add it to a task the lefthook hooks reach in %s, or record why not in exemptFromParity.",
			gate, taskfilePath,
		)
	}
}

// TestParityComparisonDetectsDrift is the sabotage, recorded as a test.
//
// ADR-065 asks for governance tests whose failure has been confirmed rather
// than assumed: a guard that has quietly stopped guarding is worse than a
// missing one, because it occupies the slot and reports success. This drives
// the comparison with lists that are deliberately wrong and asserts it objects.
func TestParityComparisonDetectsDrift(t *testing.T) {
	references := map[string][]string{
		"quality:verify": {"quality:format:verify", "docs:lint"},
	}

	expanded := closure([]string{"quality:verify"}, references)
	for _, want := range []string{"quality:format:verify", "docs:lint"} {
		if len(difference([]string{want}, expanded)) != 0 {
			t.Fatalf("closure did not follow the reference to %q: got %v", want, expanded)
		}
	}

	if got := difference(expanded, []string{"quality:format:verify"}); len(got) != 1 || got[0] != "docs:lint" {
		t.Fatalf("expected the missing gate to be reported, got %v", got)
	}
	if got := difference(expanded, expanded); len(got) != 0 {
		t.Fatalf("identical closures must produce no difference, got %v", got)
	}
}

// taskReferences maps each Taskfile task to the tasks it delegates to via
// `- task: X`.
//
// Read as text rather than parsed as YAML on purpose: a structural parse needs
// the Taskfile schema, and what is being asserted is a flat list of names a
// person edits by hand.
func taskReferences(t *testing.T, path string) map[string][]string {
	t.Helper()

	header := regexp.MustCompile(`^  ([a-z0-9:._-]+):\s*$`)
	reference := regexp.MustCompile(`^\s+- task:\s*([a-z0-9:._-]+)\s*$`)

	references := map[string][]string{}
	current := ""
	for _, line := range readLines(t, path) {
		if match := header.FindStringSubmatch(line); match != nil {
			current = match[1]
			continue
		}
		if current == "" {
			continue
		}
		if match := reference.FindStringSubmatch(line); match != nil {
			references[current] = append(references[current], match[1])
		}
	}

	return references
}

// localEntryPoints reads the `task X` commands the git hooks run.
func localEntryPoints(t *testing.T, path string) []string {
	t.Helper()
	return taskInvocations(readLines(t, path))
}

// ciEntryPoints reads the `task X` invocations in every job except the live one.
func ciEntryPoints(t *testing.T, path string) []string {
	t.Helper()

	jobHeader := regexp.MustCompile(`^  ([a-z0-9_-]+):\s*$`)

	included := []string{}
	inLiveJob := false
	for _, line := range readLines(t, path) {
		if match := jobHeader.FindStringSubmatch(line); match != nil {
			inLiveJob = match[1] == liveJob
			continue
		}
		if inLiveJob {
			continue
		}
		included = append(included, line)
	}

	return taskInvocations(included)
}

func taskInvocations(lines []string) []string {
	invocation := regexp.MustCompile(`\btask ([a-z0-9:._-]+)`)

	names := []string{}
	for _, line := range lines {
		// Comments name tasks in prose -- lefthook.yml suggests running
		// `task quality:coverage:origin-main` by hand -- and matching those
		// invents gates that neither side runs. Strip them before matching.
		if index := strings.Index(line, "#"); index >= 0 {
			line = line[:index]
		}
		// `go install github.com/go-task/task/v3/cmd/task@v3.48.0` installs the
		// runner; it is not a gate.
		if strings.Contains(line, "go install") {
			continue
		}
		for _, match := range invocation.FindAllStringSubmatch(line, -1) {
			names = append(names, match[1])
		}
	}
	return normalise(names)
}

// closure expands entry points through Taskfile references down to the tasks
// that actually do work.
//
// A task that delegates is dropped from the result: only its leaves matter,
// which is what lets `task quality:verify` on one side compare equal to the
// same gates enumerated as separate steps on the other.
func closure(entryPoints []string, references map[string][]string) []string {
	seen := map[string]struct{}{}
	leaves := []string{}

	var visit func(string)
	visit = func(name string) {
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}

		delegates, ok := references[name]
		if !ok || len(delegates) == 0 {
			leaves = append(leaves, name)
			return
		}
		for _, delegate := range delegates {
			visit(delegate)
		}
	}

	for _, entryPoint := range entryPoints {
		visit(entryPoint)
	}

	return normalise(leaves)
}

func readLines(t *testing.T, path string) []string {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n")
}

func normalise(values []string) []string {
	seen := map[string]struct{}{}
	unique := []string{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	sort.Strings(unique)
	return unique
}

func difference(from, against []string) []string {
	present := map[string]struct{}{}
	for _, value := range against {
		present[value] = struct{}{}
	}

	missing := []string{}
	for _, value := range from {
		if _, ok := present[value]; !ok {
			missing = append(missing, value)
		}
	}
	return missing
}

// repositoryRoot walks up from the package directory until it finds the
// Taskfile, so the test does not depend on where `go test` was invoked.
func repositoryRoot(t *testing.T) string {
	t.Helper()

	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}

	for {
		if _, err := os.Stat(filepath.Join(directory, taskfilePath)); err == nil {
			return directory
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatalf("could not find %s above the test's working directory", taskfilePath)
		}
		directory = parent
	}
}

// gateNamePattern matches the tasks that look like checks rather than actions.
//
// A heuristic, and deliberately a loose one: a task whose name says verify,
// lint, vulncheck or ensure is claiming to enforce something, and a claim
// nothing runs is worse than no claim.
var gateNamePattern = regexp.MustCompile(`(verify|lint|vulncheck|ensure)`)

// TestNoGateIsDefinedAndNeverRun closes the hole the parity test above leaves.
//
// That test compares the local and CI lists to each other. A gate present in
// neither satisfies it perfectly: both sides agree, and the check runs nowhere.
// Adding a task and forgetting to wire it up is the easiest mistake to make
// here and the only one that produces no signal at all -- the task exists, it
// passes when run by hand, and nothing ever runs it.
func TestNoGateIsDefinedAndNeverRun(t *testing.T) {
	root := repositoryRoot(t)

	defined := gateShapedTasks(t, filepath.Join(root, taskfilePath))
	if len(defined) < 5 {
		t.Fatalf("expected several gate-shaped tasks, found %v; the parser has stopped matching", defined)
	}

	references := taskReferences(t, filepath.Join(root, taskfilePath))
	reachable := map[string]struct{}{}
	for _, name := range closure(localEntryPoints(t, filepath.Join(root, lefthookPath)), references) {
		reachable[name] = struct{}{}
	}
	for _, name := range closure(ciEntryPoints(t, filepath.Join(root, ciWorkflowPath)), references) {
		reachable[name] = struct{}{}
	}
	// A task that only delegates is reachable through the tasks it delegates
	// to, which the closure already flattened away.
	for name := range references {
		reachable[name] = struct{}{}
	}

	for _, gate := range defined {
		if _, ok := reachable[gate]; ok {
			continue
		}
		if reason, exempt := exemptFromParity[gate]; exempt {
			t.Logf("%s is not wired up, by design: %s", gate, reason)
			continue
		}
		t.Errorf(
			"task %q is named like a gate and nothing runs it.\n"+
				"Add it to quality:verify, or to a CI job if it needs a Bitbucket instance,\n"+
				"or rename it if it is not a check.",
			gate,
		)
	}
}

// gateShapedTasks lists the Taskfile tasks whose names claim to check something.
func gateShapedTasks(t *testing.T, path string) []string {
	t.Helper()

	header := regexp.MustCompile(`^  ([a-z0-9:._-]+):\s*$`)

	names := []string{}
	for _, line := range readLines(t, path) {
		match := header.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		if gateNamePattern.MatchString(match[1]) {
			names = append(names, match[1])
		}
	}

	return normalise(names)
}
