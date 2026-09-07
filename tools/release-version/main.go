// Command release-version decides whether a push to main cuts a release, and
// what the version is.
//
// It reads the commits since the last tag through the conventionalcommits
// package -- the same reading the release-flow gate applies to a pull request
// into main -- so the gate cannot allow something in that this then cuts a
// major release from (ADR-065). Its answers go to GITHUB_OUTPUT for the rest of
// the release workflow.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	cc "github.com/vriesdemichael/bitbucket-data-center-cli/tools/conventionalcommits"
)

// tagPattern is the glob git describe matches, and semverPattern is what a
// manually requested version must look like.
const tagPattern = "v[0-9]*.[0-9]*.[0-9]*"

var (
	semverPattern  = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:[.-][0-9A-Za-z.-]+)?$`)
	numericPattern = regexp.MustCompile(`^v(\d+)\.(\d+)\.(\d+)$`)
)

// decision is what the workflow reads back.
type decision struct {
	shouldRelease bool
	version       string
	previousTag   string
}

// compute answers whether to release and with which version.
//
// tagExists says whether a tag is already present, which is how a version
// somebody else published first is detected: releasing over it would move a tag
// onto a different commit from the artifacts built under it.
func compute(previousTag, manualVersion string, commits []cc.Commit, tagExists func(string) bool) (decision, error) {
	if manualVersion != "" {
		if !semverPattern.MatchString(manualVersion) {
			return decision{}, fmt.Errorf("manual version %q must look like vMAJOR.MINOR.PATCH (optionally with prerelease/build suffix)", manualVersion)
		}

		return decision{shouldRelease: true, version: manualVersion, previousTag: previousTag}, nil
	}

	held := decision{previousTag: previousTag}

	level := cc.BumpLevel(commits)
	if !cc.HasConventional(commits) || level == cc.BumpNone {
		return held, nil
	}

	major, minor, patch, err := parseBase(previousTag)
	if err != nil {
		return decision{}, err
	}

	switch level {
	case cc.BumpMajor:
		major, minor, patch = major+1, 0, 0
	case cc.BumpMinor:
		minor, patch = minor+1, 0
	default:
		patch++
	}

	next := fmt.Sprintf("v%d.%d.%d", major, minor, patch)
	if tagExists(next) {
		return held, nil
	}

	return decision{shouldRelease: true, version: next, previousTag: previousTag}, nil
}

// parseBase reads the version the bump starts from. No tag yet means the first
// release counts up from 0.0.0.
func parseBase(previousTag string) (major, minor, patch int, err error) {
	if previousTag == "" {
		return 0, 0, 0, nil
	}

	match := numericPattern.FindStringSubmatch(previousTag)
	if match == nil {
		return 0, 0, 0, fmt.Errorf("previous tag %q is not vMAJOR.MINOR.PATCH, so there is no version to count up from", previousTag)
	}

	major, _ = strconv.Atoi(match[1])
	minor, _ = strconv.Atoi(match[2])
	patch, _ = strconv.Atoi(match[3])

	return major, minor, patch, nil
}

func git(args ...string) (string, error) {
	output, err := exec.Command("git", args...).Output()

	return strings.TrimSpace(string(output)), err
}

func writeOutputs(path string, result decision) (err error) {
	file, openErr := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if openErr != nil {
		return openErr
	}
	// The write is only durable once the close succeeds, so a close failure is
	// the step failing rather than a tidy-up detail.
	defer func() {
		if closeErr := file.Close(); err == nil {
			err = closeErr
		}
	}()

	_, err = fmt.Fprintf(file, "should_release=%t\nversion=%s\nprevious_tag=%s\n",
		result.shouldRelease, result.version, result.previousTag)

	return err
}

func main() {
	// An absent tag is the first release, not a failure, so the error is the
	// answer rather than a stop.
	previousTag, _ := git("describe", "--tags", "--abbrev=0", "--match", tagPattern)

	rangeSpec := "HEAD"
	if previousTag != "" {
		rangeSpec = previousTag + "..HEAD"
	}

	raw, err := git(cc.LogArgs(rangeSpec)...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read the commits in %s: %v\n", rangeSpec, err)
		os.Exit(1)
	}

	result, err := compute(previousTag, strings.TrimSpace(os.Getenv("MANUAL_VERSION")), cc.ParseLog(raw), func(tag string) bool {
		_, err := git("rev-parse", "--verify", "--quiet", tag)

		return err == nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if result.shouldRelease {
		fmt.Printf("Releasing %s (previous tag: %s)\n", result.version, orNone(result.previousTag))
	} else {
		fmt.Printf("Nothing to release since %s.\n", orNone(result.previousTag))
	}

	outputPath := os.Getenv("GITHUB_OUTPUT")
	if outputPath == "" {
		fmt.Fprintln(os.Stderr, "GITHUB_OUTPUT is not set; there is nowhere to report the version.")
		os.Exit(1)
	}

	if err := writeOutputs(outputPath, result); err != nil {
		fmt.Fprintf(os.Stderr, "write the outputs: %v\n", err)
		os.Exit(1)
	}
}

func orNone(tag string) string {
	if tag == "" {
		return "none"
	}

	return tag
}
