package gateparity

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// terminalCheckAllowed are the files permitted to ask whether a stream is a
// terminal.
//
// internal/cli/interactive is where the question is answered for the whole
// programme. clone.go is the one legitimate second caller: once prompting has
// been permitted it needs the descriptor to turn terminal echo off while a
// token is typed, which is a question about how to read rather than whether to
// ask. Every other call site would be a command deciding interactivity for
// itself, which is the drift ADR-072 exists to prevent.
var terminalCheckAllowed = map[string]string{
	filepath.Join("internal", "cli", "interactive", "interactive.go"): "the shared decision",
	filepath.Join("internal", "cli", "cmd", "repo", "clone.go"):       "suppressing echo while a token is typed",
}

// TestOnlyTheSharedHelperDecidesInteractivity fails when a new call site asks
// whether a stream is a terminal.
//
// The check is deliberately about the call rather than about prompting,
// because a terminal check is the thing that reads like enforcement while
// being wrong: bb repo clone gated a password prompt on isatty(stdin) alone
// for months, which permitted prompting into a pipeline and was weaker than
// the rule gh applies. A guard on the prompt would not have caught it; a guard
// on the question does.
//
// Adding a file here is allowed and is the point. It forces the decision to be
// made once, in review, rather than in a handler.
func TestOnlyTheSharedHelperDecidesInteractivity(t *testing.T) {
	root := repositoryRoot(t)

	found := terminalCheckCallers(t, root)
	if len(found) == 0 {
		t.Fatal("found no terminal checks at all; the scan or the import name has changed")
	}

	for _, path := range found {
		if _, ok := terminalCheckAllowed[path]; !ok {
			t.Errorf(
				"%s asks whether a stream is a terminal.\n"+
					"That decision belongs to internal/cli/interactive, which ADR-072 records:\n"+
					"terminal attachment is a necessary condition for prompting and not a\n"+
					"sufficient one, and a per-command check cannot see the environment rules.\n"+
					"Call interactive.Detect, or add this file to terminalCheckAllowed with a\n"+
					"reason if it needs the descriptor for something other than deciding.",
				path,
			)
		}
	}
}

// TestTheInteractivityGuardDetectsANewCaller is the sabotage, kept as a test.
//
// ADR-067 requires breaking a guard before trusting it. The real tree has only
// permitted callers, so nothing would notice if the scan stopped scanning.
func TestTheInteractivityGuardDetectsANewCaller(t *testing.T) {
	unlisted := filepath.Join("internal", "cli", "cmd", "somewhere", "new.go")
	if _, ok := terminalCheckAllowed[unlisted]; ok {
		t.Fatalf("%s is listed; pick a path that is not", unlisted)
	}

	// The guard is the membership test above, so this asserts the thing that
	// would actually have to fail: a caller the map does not name.
	found := append(terminalCheckCallers(t, repositoryRoot(t)), unlisted)

	offenders := 0
	for _, path := range found {
		if _, ok := terminalCheckAllowed[path]; !ok {
			offenders++
		}
	}
	if offenders != 1 {
		t.Errorf("planted one unlisted caller, the guard would report %d", offenders)
	}
}

// terminalCheckCallers finds every file asking whether a stream is a terminal.
func terminalCheckCallers(t *testing.T, root string) []string {
	t.Helper()

	callers := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			// Agent tooling keeps checkouts of other branches under dot
			// directories; their call sites are not this tree's.
			if path != root && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		for _, line := range readLines(t, path) {
			if strings.Contains(line, "term.IsTerminal(") {
				relative, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return relErr
				}
				callers = append(callers, relative)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	return normalise(callers)
}
