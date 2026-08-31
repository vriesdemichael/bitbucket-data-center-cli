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
)

// dependabotConfig is the subset of .github/dependabot.yml this test reads.
type dependabotConfig struct {
	Updates []struct {
		Ecosystem string `yaml:"package-ecosystem"`
		Directory string `yaml:"directory"`
		Ignore    []struct {
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
