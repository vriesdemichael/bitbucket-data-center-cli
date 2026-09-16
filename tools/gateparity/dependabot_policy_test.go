package gateparity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	dependabotConfigPath    = ".github/dependabot.yml"
	dependabotAutomergePath = ".github/workflows/dependabot-automerge.yml"

	// harnessDirectory is the Dependabot directory that watches the Dockerfile
	// carrying the Bitbucket base image tag (ADR-042).
	harnessDirectory = "/docker/harness"

	// dependabotTargetBranch is the branch every update is proposed against: the
	// integration branch, like any other change (ADR-066).
	dependabotTargetBranch = "next"
)

// dependabotConfig is the subset of .github/dependabot.yml these tests read.
type dependabotConfig struct {
	Updates []struct {
		Ecosystem    string `yaml:"package-ecosystem"`
		Directory    string `yaml:"directory"`
		TargetBranch string `yaml:"target-branch"`
		Ignore       []struct {
			DependencyName string `yaml:"dependency-name"`
		} `yaml:"ignore"`
	} `yaml:"updates"`
}

// TestBitbucketImageIsProposedButNotAutoMerged asserts the two halves of
// ADR-042's upgrade path, which are stated in two different files and are only
// correct together.
//
// The proposal half: .github/dependabot.yml must watch /docker/harness and must
// not ignore atlassian/bitbucket. An ignore was there once, added while the test
// licence capped the version the stack could provision, and removed when ADR-043
// made the Plugin SDK issue a licence per run. Its absence looks accidental in
// the history -- it was reported as an accidental regression before it was
// established as deliberate -- and "restoring" it would silently stop the
// project ever learning that a new release exists, because an ignore suppresses
// the pull request and with it the CI run that would have judged the release.
//
// The adoption half: dependabot-automerge.yml must still decline to merge the
// product image, whatever the update type (ADR-069). Without the hold, a
// Bitbucket bump becomes an unattended change to a user-facing claim.
//
// What this proves and does not: it reads the workflow as text, so it asserts
// that the hold is still written, not that the surrounding script still reaches
// it. That is the weaker half deliberately -- the strong evidence that a bare
// bump cannot land is openapi:verify in the quality gate, which fails when the
// vendored reference and the image tag part ways. This guards the policy; the
// gate guards the outcome.
func TestBitbucketImageIsProposedButNotAutoMerged(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	t.Run("dependabot proposes every bitbucket release", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(root, dependabotConfigPath))
		if err != nil {
			t.Fatalf("read %s: %v", dependabotConfigPath, err)
		}

		var config dependabotConfig
		if err := yaml.Unmarshal(raw, &config); err != nil {
			t.Fatalf("parse %s: %v", dependabotConfigPath, err)
		}

		watched := false
		for _, update := range config.Updates {
			if update.Directory != harnessDirectory {
				continue
			}
			watched = true

			for _, ignored := range update.Ignore {
				if strings.Contains(strings.ToLower(ignored.DependencyName), "atlassian/bitbucket") {
					t.Errorf(
						"%s ignores %q for %s.\n"+
							"ADR-042 requires every published Bitbucket release to be proposed: an ignore\n"+
							"suppresses the pull request, so the live suite never judges the release and\n"+
							"nobody learns whether the project could move. The merge is held in %s\n"+
							"instead, which keeps the decision human and keeps the evidence.",
						dependabotConfigPath, ignored.DependencyName, harnessDirectory, dependabotAutomergePath,
					)
				}
			}
		}

		if !watched {
			t.Errorf(
				"%s declares no update entry for %s.\n"+
					"That directory holds the harness Dockerfile whose base image tag is the single\n"+
					"place the Bitbucket version under test is recorded (ADR-042). Unwatched, the\n"+
					"version stops moving and nothing says so.",
				dependabotConfigPath, harnessDirectory,
			)
		}
	})

	t.Run("auto-merge holds the product image", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(root, dependabotAutomergePath))
		if err != nil {
			t.Fatalf("read %s: %v", dependabotAutomergePath, err)
		}
		workflow := string(raw)

		productImage := regexp.MustCompile(`atlassian\\?/bitbucket`)
		if !productImage.MatchString(workflow) {
			t.Fatalf(
				"%s no longer mentions the Bitbucket product image.\n"+
					"ADR-042 and ADR-069 require it to be held from auto-merge whatever the update\n"+
					"type, because its tag is a user-facing claim about the supported version.",
				dependabotAutomergePath,
			)
		}

		// The hold is only a hold if it sets should_merge false. A mention that
		// no longer refuses the merge is the failure this looks for.
		held := false
		lines := readLines(t, filepath.Join(root, dependabotAutomergePath))
		for index, line := range lines {
			if !productImage.MatchString(line) {
				continue
			}
			for _, following := range lines[index:min(index+12, len(lines))] {
				if strings.Contains(following, "should_merge") && strings.Contains(following, "'false'") {
					held = true
				}
			}
		}

		if !held {
			t.Errorf(
				"%s names the Bitbucket product image but no should_merge 'false' follows it.\n"+
					"ADR-069 holds it whatever the update type; a mention that no longer declines the\n"+
					"merge leaves a Bitbucket bump to land unattended.",
				dependabotAutomergePath,
			)
		}
	})
}

// TestDependabotProposesUpdatesAgainstNext asserts that dependency updates reach
// next and not main, in both files that decide it.
//
// The proposal half: every entry in .github/dependabot.yml sets target-branch to
// next. Without it Dependabot opens its pull requests against the default
// branch, and an update merged into main moves main ahead of next. next can then
// be promoted only after every commit on it is rebased, where the same update
// rebased onto next would have cost nothing.
//
// The adoption half: dependabot-automerge.yml declines a pull request based on
// any other branch. Dependabot opens security updates against the default
// branch whatever its configuration says, so the configuration alone cannot keep
// them off main. Like the product-image hold above, this reads the workflow as
// text: it proves the hold is written, not that the script still reaches it.
func TestDependabotProposesUpdatesAgainstNext(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)

	t.Run("every update targets next", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join(root, dependabotConfigPath))
		if err != nil {
			t.Fatalf("read %s: %v", dependabotConfigPath, err)
		}

		var config dependabotConfig
		if err := yaml.Unmarshal(raw, &config); err != nil {
			t.Fatalf("parse %s: %v", dependabotConfigPath, err)
		}
		if len(config.Updates) == 0 {
			t.Fatalf("%s declares no updates", dependabotConfigPath)
		}

		for _, update := range config.Updates {
			if update.TargetBranch == dependabotTargetBranch {
				continue
			}
			t.Errorf(
				"%s: the %s update for %s has target-branch %q, not %q.\n"+
					"Without it Dependabot opens pull requests against main. Merged there, an update\n"+
					"moves main ahead of next, and next cannot fast-forward onto main again until\n"+
					"every commit on it is rebased (ADR-066).",
				dependabotConfigPath, update.Ecosystem, update.Directory, update.TargetBranch, dependabotTargetBranch,
			)
		}
	})

	t.Run("auto-merge holds a pull request based elsewhere", func(t *testing.T) {
		lines := readLines(t, filepath.Join(root, dependabotAutomergePath))

		if !holdsBaseOtherThan(lines, dependabotTargetBranch) {
			t.Errorf(
				"%s does not decline a pull request whose base is not %s.\n"+
					"Dependabot opens security updates against main whatever %s says, and the\n"+
					"workflow would merge them there, moving main ahead of next (ADR-066, ADR-069).",
				dependabotAutomergePath, dependabotTargetBranch, dependabotConfigPath,
			)
		}
	})
}

// holdsBaseOtherThan reports whether the workflow declines a pull request based
// on any branch but this one.
//
// The whole guard is matched, not the comparison inside it. A comparison is
// text that can still be there while nothing acts on it: `if (false && pr.base
// ?.ref !== 'next')` kept an earlier version of this test green, which is the
// failure ADR-067 exists to catch. The block it guards has to refuse the merge
// and return, because setting should_merge false and carrying on would be
// overwritten by the eligible line below it.
func holdsBaseOtherThan(lines []string, branch string) bool {
	guard := regexp.MustCompile(`^\s*if \(pr\.base\??\.ref !== '` + regexp.QuoteMeta(branch) + `'\) \{\s*$`)

	for index, line := range lines {
		if !guard.MatchString(line) {
			continue
		}

		refused, returned := false, false
		for _, following := range lines[index:min(index+12, len(lines))] {
			if strings.Contains(following, "should_merge") && strings.Contains(following, "'false'") {
				refused = true
			}
			if refused && strings.TrimSpace(following) == "return;" {
				returned = true
			}
			if strings.TrimSpace(following) == "}" && refused {
				break
			}
		}

		if refused && returned {
			return true
		}
	}

	return false
}

// TestTheBaseHoldScannerFindsAHoldThatDoesNotHold breaks the guard the way a
// careless edit would, because a governance test nobody has seen fail is a
// governance test nobody knows works (ADR-067).
func TestTheBaseHoldScannerFindsAHoldThatDoesNotHold(t *testing.T) {
	t.Parallel()

	held := []string{
		"            if (pr.base?.ref !== 'next') {",
		"              core.notice(`PR #${pr.number} is based on ${pr.base?.ref}, not next.`);",
		"              core.setOutput('should_merge', 'false');",
		"              return;",
		"            }",
	}
	if !holdsBaseOtherThan(held, "next") {
		t.Error("the scanner does not recognise the hold this repository ships")
	}

	for name, sabotaged := range map[string][]string{
		"dead code": {
			"            if (false && pr.base?.ref !== 'next') {",
			"              core.setOutput('should_merge', 'false');",
			"              return;",
			"            }",
		},
		"another branch": {
			"            if (pr.base?.ref !== 'main') {",
			"              core.setOutput('should_merge', 'false');",
			"              return;",
			"            }",
		},
		"noticed but not refused": {
			"            if (pr.base?.ref !== 'next') {",
			"              core.notice(`PR #${pr.number} is based elsewhere.`);",
			"            }",
		},
		"refused but not returned": {
			"            if (pr.base?.ref !== 'next') {",
			"              core.setOutput('should_merge', 'false');",
			"            }",
			"            core.setOutput('should_merge', 'true');",
		},
		"removed entirely": {
			"            const isDependabot = pr.user?.login === 'dependabot[bot]';",
		},
	} {
		if holdsBaseOtherThan(sabotaged, "next") {
			t.Errorf("the scanner accepted a hold sabotaged by %q", name)
		}
	}
}
