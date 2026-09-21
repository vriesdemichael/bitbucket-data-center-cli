// Package compat is how bb serves the Bitbucket Data Center releases older than
// the one it is generated against.
//
// Every supported release behaves like the newest except where a Difference
// here says otherwise. A call that differs asks for the instance's release and
// either adapts, so that the older release answers the way the newest would, or
// refuses with an unsupported error when nothing bb can do makes it work.
// docs/site/reference/bitbucket-versions.md catalogues the differences.
package compat

import (
	"fmt"
	"strconv"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// Release is a Bitbucket Data Center release.
type Release struct {
	Major, Minor, Patch int
}

// ParseRelease reads the version Bitbucket reports for itself, such as 10.4.3.
//
// Anything after the patch number -- a milestone suffix, a build qualifier -- is
// not part of what a difference is keyed on and is ignored.
func ParseRelease(version string) (Release, error) {
	parts := strings.SplitN(strings.TrimSpace(version), ".", 3)
	if len(parts) < 2 {
		return Release{}, apperrors.New(apperrors.KindPermanent, fmt.Sprintf("Bitbucket reported a version bb cannot read: %q", version), nil)
	}

	numbers := make([]int, 3)
	for index, part := range parts {
		digits := part
		if end := strings.IndexFunc(part, func(r rune) bool { return r < '0' || r > '9' }); end >= 0 {
			digits = part[:end]
		}
		number, err := strconv.Atoi(digits)
		if err != nil {
			return Release{}, apperrors.New(apperrors.KindPermanent, fmt.Sprintf("Bitbucket reported a version bb cannot read: %q", version), err)
		}
		numbers[index] = number
	}

	return Release{Major: numbers[0], Minor: numbers[1], Patch: numbers[2]}, nil
}

// Before reports whether release came out before other.
func (release Release) Before(other Release) bool {
	if release.Major != other.Major {
		return release.Major < other.Major
	}
	if release.Minor != other.Minor {
		return release.Minor < other.Minor
	}

	return release.Patch < other.Patch
}

func (release Release) String() string {
	return fmt.Sprintf("%d.%d.%d", release.Major, release.Minor, release.Patch)
}

// Difference is a capability Bitbucket gained in a release. Every release before
// Since lacks it, in the way the catalogue describes.
type Difference struct {
	// What names the capability as a refusal states it: "requiring a build for
	// the merge queue".
	What string
	// Since is the first release that has it.
	Since Release
}

// In reports whether release has the capability.
func (difference Difference) In(release Release) bool {
	return !release.Before(difference.Since)
}

// Unsupported is the refusal for a capability the instance's release lacks and
// bb cannot make up for.
func (difference Difference) Unsupported(release Release) error {
	return apperrors.New(apperrors.KindUnsupported,
		fmt.Sprintf("%s is not supported by this Bitbucket version (%s); it needs %s or later", difference.What, release, difference.Since), nil)
}
