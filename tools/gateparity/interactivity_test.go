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

// stdinReadAllowed are the files permitted to read standard input.
//
// Each reads it only when the caller asked, with a `-` argument or an explicit
// flag value. That is the whole rule (ADR-073): stdin held open with no data is
// indistinguishable from stdin about to deliver data, so an implicit fallback
// blocks forever on a CI runner and returns nothing under an agent, which
// silently writes an empty file over a real one.
var stdinReadAllowed = map[string]string{
	filepath.Join("internal", "cli", "cmd", "api", "api.go"):           "--input -",
	filepath.Join("internal", "cli", "cmd", "repo", "misc.go"):         "--content -",
	filepath.Join("internal", "cli", "cmd", "reviewer", "reviewer.go"): "a - positional",
	filepath.Join("internal", "cli", "prompt", "prompt.go"):            "the confirmation itself, behind interactive.Detect",
}

// TestOnlyExplicitlyRequestedStdinIsRead fails when a new site reads standard
// input.
//
// The check is on the read rather than on the guard, because the guard is the
// part that gets forgotten: three of these had none, and ADR-054 did not catch
// them because it forbade prompts and said nothing about reads.
func TestOnlyExplicitlyRequestedStdinIsRead(t *testing.T) {
	root := repositoryRoot(t)

	found := stdinReaders(t, root)
	if len(found) == 0 {
		t.Fatal("found no stdin reads at all; the scan has stopped matching")
	}

	for _, path := range found {
		if _, ok := stdinReadAllowed[path]; !ok {
			t.Errorf(
				"%s reads standard input.\n"+
					"ADR-073 permits that only when the caller asked for it, with a - argument\n"+
					"or a named flag value. An implicit fallback blocks forever when stdin is an\n"+
					"open pipe with nothing coming, which is what a CI runner provides.\n"+
					"Gate the read, then add this file to stdinReadAllowed with the token that\n"+
					"requests it.",
				path,
			)
		}
	}
}

// stdinReaders finds every file reading the command's standard input.
func stdinReaders(t *testing.T, root string) []string {
	t.Helper()

	readers := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if path != root && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		for _, line := range readLines(t, path) {
			if !readsStandardInput(line) {
				continue
			}
			relative, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			readers = append(readers, relative)
			break
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	return normalise(readers)
}

// readsStandardInput reports whether a line consumes standard input.
//
// Passing the stream on is not reading it: a command handing cmd.InOrStdin() to
// the prompter has delegated the decision, which is the arrangement ADR-073
// wants. What counts is the call that blocks -- ReadAll, a buffered read, or a
// scan -- applied to one of the two spellings of standard input.
func readsStandardInput(line string) bool {
	// fmt.Scanln and friends always read the process's stdin, whatever the
	// command was given, which is what made gpg-key clear hang.
	for _, scan := range []string{"fmt.Scanln(", "fmt.Scan(", "fmt.Fscan"} {
		if strings.Contains(line, scan) {
			return true
		}
	}

	stream := strings.Contains(line, "InOrStdin()") || strings.Contains(line, "os.Stdin")
	if !stream {
		return false
	}
	for _, consume := range []string{"io.ReadAll(", "bufio.NewReader(", "bufio.NewScanner(", "term.ReadPassword("} {
		if strings.Contains(line, consume) {
			return true
		}
	}
	return false
}
