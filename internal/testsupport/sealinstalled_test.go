package testsupport_test

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// configLoadPattern matches a test source line that loads the CLI's
// configuration. Both spellings count: internal/config's own tests call it
// unqualified, and a detector that only knew the qualified form would excuse
// the package with the most to inherit.
//
// The leading boundary is what keeps `func TestLoadFromEnvSystemCAFile(` from
// reading as a call.
var configLoadPattern = regexp.MustCompile(`\b(?:config\.)?Load(?:FromEnv|WithOverrides)\(`)

// sealPattern matches a package that seals its process. SealedMain is the whole
// of it; SealAmbientEnvironment is for a package that also installs something
// else in TestMain, which the ones that guard git configuration do.
var sealPattern = regexp.MustCompile(`testsupport\.Seal(?:edMain|AmbientEnvironment)\(`)

// liveBuildTag matches the constraint that marks the integration suite.
var liveBuildTag = regexp.MustCompile(`(?m)^//go:build .*\blive\b`)

// TestTheSealIsInstalledWhereTestsLoadTheConfiguration is the guard on the seal.
//
// ADR-082 says a unit test process inherits nothing. The enforcement is per
// package -- a TestMain has to install it -- and a package without one is
// unsealed while looking no different from the outside.
//
// That gap was not theoretical. internal/services/pullrequest had no seal, so
// seven of its tests read the developer's stored credentials and failed on any
// machine where someone had run `bb auth login`. CI never saw it: hosted
// runners have no stored config, so the failure existed only for the people who
// use the tool they are developing, and the pre-commit hook meant they could
// not commit. internal/config had two more of the same, for the same reason.
//
// Naming the sealed packages somewhere would not have caught that -- ADR-071's
// equivalent guard exists because a list in AGENTS.md said three when the answer
// was four. So this computes the set: a package whose tests load the
// configuration must seal.
func TestTheSealIsInstalledWhereTestsLoadTheConfiguration(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	loadsConfig := map[string]bool{}
	hasSeal := map[string]bool{}

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// A directory that vanished mid-walk is a test writing into the
			// checkout and cleaning up after itself, which is what ADR-071
			// prohibits. Say so, rather than reporting the bare open error and
			// leaving the next reader to work out which test did it.
			if os.IsNotExist(err) {
				return fmt.Errorf(
					"%s disappeared during the walk, which means a test created it inside the "+
						"checkout and removed it again. Give that test a t.TempDir(); ADR-071 explains why: %w",
					path, err,
				)
			}
			return err
		}

		if entry.IsDir() {
			// Dotted directories hold agent worktrees with whole other branches
			// of this repository in them (ADR-065).
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

		// The live suite is not a unit test. It exists to reach a real server
		// with real credentials, so the seal is the opposite of what it wants.
		// Keyed off the build tag rather than the path, because the tag is what
		// actually decides.
		if liveBuildTag.MatchString(source) {
			return nil
		}

		directory := filepath.ToSlash(filepath.Dir(path))
		if configLoadPattern.MatchString(source) {
			loadsConfig[directory] = true
		}
		if sealPattern.MatchString(source) {
			hasSeal[directory] = true
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}

	// A detector that stopped matching would report perfect compliance, which
	// is the failure mode ADR-067 exists to catch. Several packages are known
	// to load configuration in their tests; finding almost none means the
	// pattern broke rather than that the repository improved.
	if len(loadsConfig) < 4 {
		t.Fatalf(
			"expected several packages whose tests load configuration, found %d: %v\nThe detector is probably broken, not the repository.",
			len(loadsConfig), sortedKeys(loadsConfig),
		)
	}

	var offenders []string
	for directory := range loadsConfig {
		if !hasSeal[directory] {
			offenders = append(offenders, directory)
		}
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf(
			"%d package(s) load configuration in their tests without the ADR-082 seal:\n  %s\n\n"+
				"Add: func TestMain(m *testing.M) { os.Exit(testsupport.SealedMain(m)) }\n"+
				"Without it the tests read whatever the developer is logged into, so they pass on\n"+
				"CI and fail on the machines of people who use bb. A test that means to read a\n"+
				"stored config writes its own and points BB_CONFIG_PATH at it.",
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
