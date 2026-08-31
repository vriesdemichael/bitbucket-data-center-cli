package gittest

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// gitSubprocessPattern matches a test source line that starts a real git
// process: exec.Command / exec.CommandContext naming git, or a construction of
// the exec-based backend, which does the same thing one layer down.
var gitSubprocessPattern = regexp.MustCompile(`exec\.Command(?:Context)?\([^)]*"git"|execgit\.New\(`)

// guardPattern matches a package that installs the ambient-config guard: a
// TestMain, and the snapshot call that makes it one.
var (
	testMainPattern = regexp.MustCompile(`func TestMain\(`)
	snapshotPattern = regexp.MustCompile(`gittest\.SnapshotAmbientConfig\(`)
)

// TestAmbientGitConfigGuardIsInstalledWhereTestsShellOutToGit is the guard on
// the guard.
//
// ADR-071 records the constraint -- a test that shells out to git operates on a
// directory it created -- and the snapshot comparison that enforces it. The
// enforcement is per package: a TestMain has to install it, and a package
// without one is unprotected while looking no different from the outside.
//
// That is not a theoretical gap. The guard was installed on internal/cli and
// internal/git/execgit, and the setup-git tests in internal/cli/cmd/auth, which
// write git configuration by design, had none. They used t.Chdir, which moves
// the working directory for the whole test binary, and wrote core.bare=true
// into this project's own configuration. The guard existed, worked, and was not
// there.
//
// Listing the guarded packages in AGENTS.md did not prevent that either -- it
// listed three when there were four. So this computes the set instead: any
// package whose tests start a git process must install the guard.
func TestAmbientGitConfigGuardIsInstalledWhereTestsShellOutToGit(t *testing.T) {
	root := filepath.Join("..", "..", "..")

	shellsOutToGit := map[string]bool{}
	hasTestMain := map[string]bool{}
	hasSnapshot := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			// Skip every dotted directory. .git is the obvious one; .claude
			// holds agent worktrees containing whole other branches of this
			// repository, and a tool that walked into them once already
			// produced a confidently wrong answer that was believed and
			// committed (ADR-065).
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		source := string(contents)
		directory := filepath.ToSlash(filepath.Dir(path))

		if gitSubprocessPattern.MatchString(source) {
			shellsOutToGit[directory] = true
		}
		if testMainPattern.MatchString(source) {
			hasTestMain[directory] = true
		}
		if snapshotPattern.MatchString(source) {
			hasSnapshot[directory] = true
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}

	// A detector that stopped matching would report perfect compliance, which
	// is the failure mode ADR-067 exists to catch. Several packages are known
	// to start git processes in their tests; finding almost none means the
	// pattern broke rather than that the repository improved.
	if len(shellsOutToGit) < 3 {
		t.Fatalf(
			"expected several packages whose tests start git, found %d: %v\nThe detector is probably broken, not the repository.",
			len(shellsOutToGit), sortedKeys(shellsOutToGit),
		)
	}

	var offenders []string
	for directory := range shellsOutToGit {
		if !hasTestMain[directory] || !hasSnapshot[directory] {
			offenders = append(offenders, directory)
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf(
			"%d package(s) start git in their tests without the ambient-config guard:\n  %s\n\n"+
				"Add a TestMain that calls gittest.SnapshotAmbientConfig before m.Run, compares with\n"+
				"gittest.Diff afterwards, and fails the package on any difference. Copy the one in\n"+
				"internal/cli/main_test.go. ADR-071 explains why.",
			len(offenders), strings.Join(offenders, "\n  "),
		)
	}
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
