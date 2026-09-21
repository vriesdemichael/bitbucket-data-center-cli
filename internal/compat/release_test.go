package compat

import (
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestParseReleaseReadsWhatBitbucketReports(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]Release{
		"10.4.3":      {Major: 10, Minor: 4, Patch: 3},
		"9.2.1":       {Major: 9, Minor: 2, Patch: 1},
		" 9.4.24 ":    {Major: 9, Minor: 4, Patch: 24},
		"10.0":        {Major: 10, Minor: 0, Patch: 0},
		"10.5.0-rc1":  {Major: 10, Minor: 5, Patch: 0},
		"10.5.0-m123": {Major: 10, Minor: 5, Patch: 0},
	} {
		got, err := ParseRelease(version)
		if err != nil || got != want {
			t.Errorf("ParseRelease(%q) = %v, %v; want %v", version, got, err, want)
		}
	}

	for _, version := range []string{"", "10", "ten.four", "v10.4.3"} {
		if _, err := ParseRelease(version); apperrors.KindOf(err) != apperrors.KindPermanent {
			t.Errorf("ParseRelease(%q) = %v, want a permanent failure", version, err)
		}
	}
}

func TestReleaseOrdersByEachNumberInTurn(t *testing.T) {
	t.Parallel()

	ordered := []Release{{9, 2, 1}, {9, 4, 24}, {9, 10, 0}, {10, 0, 2}, {10, 1, 5}, {10, 2, 0}, {10, 2, 7}}
	for index := range ordered {
		for other := range ordered {
			if got, want := ordered[index].Before(ordered[other]), index < other; got != want {
				t.Errorf("%s.Before(%s) = %t, want %t", ordered[index], ordered[other], got, want)
			}
		}
	}
}

func TestDifferenceRefusesOnlyReleasesBeforeIt(t *testing.T) {
	t.Parallel()

	difference := Difference{What: "requiring a build for the merge queue", Since: Release{10, 2, 0}}

	if !difference.In(Release{10, 2, 0}) || !difference.In(Release{10, 4, 3}) {
		t.Error("a release from Since on lacks the capability")
	}
	if difference.In(Release{10, 1, 5}) {
		t.Error("a release before Since has the capability")
	}

	err := difference.Unsupported(Release{10, 1, 5})
	if apperrors.KindOf(err) != apperrors.KindUnsupported || apperrors.ExitCode(err) != 14 {
		t.Fatalf("the refusal is %v (exit %d), want unsupported, exit 14", err, apperrors.ExitCode(err))
	}
	// The sentence a caller reads names the capability, the release they are on
	// and the one they need.
	for _, want := range []string{"requiring a build for the merge queue", "not supported by this Bitbucket version", "10.1.5", "10.2.0 or later"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal %q does not say %q", err, want)
		}
	}
}
