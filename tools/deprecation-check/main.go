// Command deprecation-check reports deprecations that are due for removal in
// the release the current commits would cut.
//
// It never fails the build. A breaking change can land on next weeks before the
// major ships, and a gate that goes red at that moment would fail every
// unrelated pull request for the rest of the integration window -- which is how
// a team learns to ignore a red build. So this warns, twice: as a GitHub
// annotation on next, where it is seen often, and in release:promote:check,
// which is the last place a person looks before pushing next onto main.
//
// The pending major is read the same way the release itself reads it, through
// tools/conventionalcommits, so this cannot disagree with what gets published.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/deprecation"
	cc "github.com/vriesdemichael/bitbucket-data-center-cli/tools/conventionalcommits"
)

const tagPattern = "v[0-9]*.[0-9]*.[0-9]*"

var numericPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)

func main() {
	annotate := flag.Bool("annotate", false, "Emit GitHub Actions warning annotations instead of plain lines")
	flag.Parse()

	previousTag, _ := git("describe", "--tags", "--abbrev=0", "--match", tagPattern)

	rangeSpec := "HEAD"
	if previousTag != "" {
		rangeSpec = previousTag + "..HEAD"
	}

	raw, err := git(cc.LogArgs(rangeSpec)...)
	if err != nil {
		// No history to read is not a finding. This runs in shallow checkouts
		// and in source trees with no repository at all.
		return
	}

	pendingMajor, err := pendingMajorOf(previousTag, cc.ParseLog(raw))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	outstanding, err := deprecation.Outstanding(pendingMajor)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if len(outstanding) == 0 {
		return
	}

	for _, entry := range outstanding {
		removeIn, _ := entry.RemoveIn()
		message := fmt.Sprintf(
			"%s was deprecated in %s and is due for removal in v%d.0.0, which is what these commits would cut. Remove it, or extend it by moving DeprecatedIn in internal/deprecation.",
			entry.Name, entry.DeprecatedIn, removeIn,
		)
		if *annotate {
			fmt.Printf("::warning title=Deprecation due for removal::%s\n", message)
			continue
		}
		fmt.Printf("  %s\n", message)
	}
}

// pendingMajorOf is the major the accumulated commits would release.
//
// A patch or minor bump leaves the major where it is, so nothing is due; only a
// breaking change moves it, and that is the release a deprecation is written
// against.
func pendingMajorOf(previousTag string, commits []cc.Commit) (int, error) {
	major := 0
	if previousTag != "" {
		match := numericPattern.FindStringSubmatch(previousTag)
		if match == nil {
			return 0, fmt.Errorf("previous tag %q is not vMAJOR.MINOR.PATCH", previousTag)
		}
		major, _ = strconv.Atoi(match[1])
	}

	if cc.BumpLevel(commits) == cc.BumpMajor {
		return major + 1, nil
	}

	return major, nil
}

func git(args ...string) (string, error) {
	output, err := exec.Command("git", args...).Output()

	return strings.TrimSpace(string(output)), err
}
