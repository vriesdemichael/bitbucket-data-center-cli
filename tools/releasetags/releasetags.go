// Package releasetags answers one question, in one place: which git tag is the
// last release.
//
// Three tools ask it -- the version the next release cuts, the deprecations due
// in it, and the version the documentation names -- and each used to ask git
// itself with the same glob. That was fine while every tag was a release. A
// prerelease tag is not: `git describe` hands back v4.1.0-rc.1 as the newest
// match, and from there the next real release cannot be counted up from a tag
// that is not vMAJOR.MINOR.PATCH, the deprecation report reads the wrong major,
// and docs-lint asks the documentation to name a release candidate.
//
// So the rule lives here (ADR-065): a release tag is vMAJOR.MINOR.PATCH and
// nothing else. Anything carrying a prerelease, a build suffix or a fourth
// component is a tag, not a release.
package releasetags

import (
	"os/exec"
	"regexp"
	"strings"
)

// Glob is what git matches to find a candidate tag. It is deliberately loose:
// git's globs cannot say "three numeric components and nothing else", so the
// excludes below and IsRelease do the rest.
const Glob = "v[0-9]*.[0-9]*.[0-9]*"

// excludes drop what the glob lets through that is not a release: v4.1.0-rc.1,
// v4.1.0+build.7, v4.1.0.1.
var excludes = []string{"*-*", "*+*", "v*.*.*.*"}

// releasePattern is the same rule, for a tag that arrives as text rather than
// through git's matching.
var releasePattern = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)

// DescribeArgs are the git arguments that name the newest release tag reachable
// from HEAD, or nothing when there is none.
//
// Reachable, rather than newest overall: a tag on another branch is not part of
// the history this one releases from.
func DescribeArgs() []string {
	args := []string{"describe", "--tags", "--abbrev=0", "--match", Glob}
	for _, exclude := range excludes {
		args = append(args, "--exclude", exclude)
	}

	return args
}

// IsRelease reports whether a tag names a release.
func IsRelease(tag string) bool {
	return releasePattern.MatchString(strings.TrimSpace(tag))
}

// Latest is the newest release tag reachable from HEAD, or "" when there is
// none and when git cannot be asked: an absent tag is the first release rather
// than a failure.
func Latest() string {
	output, err := exec.Command("git", DescribeArgs()...).Output()
	if err != nil {
		return ""
	}

	return strings.TrimSpace(string(output))
}
