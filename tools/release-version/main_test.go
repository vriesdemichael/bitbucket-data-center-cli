package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	cc "github.com/vriesdemichael/bitbucket-data-center-cli/tools/conventionalcommits"
)

func built(pairs ...[2]string) []cc.Commit {
	commits := make([]cc.Commit, 0, len(pairs))
	for _, pair := range pairs {
		commits = append(commits, cc.Classify("0123456789abcdef", pair[0], pair[1]))
	}

	return commits
}

func subjects(list ...string) [][2]string {
	pairs := make([][2]string, 0, len(list))
	for _, subject := range list {
		pairs = append(pairs, [2]string{subject, ""})
	}

	return pairs
}

func noTags(string) bool { return false }

func TestComputeMovesTheVersionByTheHighestBump(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		previousTag string
		commits     []cc.Commit
		want        string
	}{
		"a fix is a patch":        {"v3.4.5", built(subjects("fix: a thing")...), "v3.4.6"},
		"a feature is a minor":    {"v3.4.5", built(subjects("feat: a thing")...), "v3.5.0"},
		"a break is a major":      {"v3.4.5", built(subjects("feat!: a thing")...), "v4.0.0"},
		"the highest of a batch":  {"v3.4.5", built(subjects("fix: a", "feat: b", "chore: c")...), "v3.5.0"},
		"a footer breaks too":     {"v3.4.5", built([2]string{"fix: a", "BREAKING CHANGE: gone"}), "v4.0.0"},
		"no tag starts at zero":   {"", built(subjects("feat: a thing")...), "v0.1.0"},
		"a major from no tag too": {"", built(subjects("feat!: a thing")...), "v1.0.0"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			result, err := compute(testCase.previousTag, "", testCase.commits, noTags)
			if err != nil {
				t.Fatalf("compute: %v", err)
			}
			if !result.shouldRelease {
				t.Fatal("expected a release")
			}
			if result.version != testCase.want {
				t.Errorf("version: got %s, want %s", result.version, testCase.want)
			}
			if result.previousTag != testCase.previousTag {
				t.Errorf("previous tag: got %q, want %q", result.previousTag, testCase.previousTag)
			}
		})
	}
}

// A run of well-formed commits that change nothing for a user releases nothing.
// This is the rule that took the release rate down from 95 in six months, and
// it is not the same as "no conventional commits".
func TestComputeReleasesNothingForNonReleasingTypes(t *testing.T) {
	t.Parallel()

	result, err := compute("v3.4.5", "", built(subjects("chore: a", "docs: b", "ci: c")...), noTags)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if result.shouldRelease {
		t.Fatalf("expected no release, got %s", result.version)
	}
	if result.version != "" {
		t.Errorf("version: got %q, want empty", result.version)
	}
	if result.previousTag != "v3.4.5" {
		t.Errorf("the previous tag should still be reported, got %q", result.previousTag)
	}
}

func TestComputeReleasesNothingWhenNoCommitIsConventional(t *testing.T) {
	t.Parallel()

	result, err := compute("v3.4.5", "", built(subjects("Merge pull request #1", "wip")...), noTags)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if result.shouldRelease {
		t.Fatalf("expected no release, got %s", result.version)
	}
}

// Another run published this version first. Tagging over it would move the tag
// onto a different commit from the artifacts built under it, so this run stands
// down and the next release sweeps these commits up.
func TestComputeStandsDownWhenTheVersionIsAlreadyTagged(t *testing.T) {
	t.Parallel()

	taken := func(tag string) bool { return tag == "v3.5.0" }

	result, err := compute("v3.4.5", "", built(subjects("feat: a thing")...), taken)
	if err != nil {
		t.Fatalf("compute: %v", err)
	}
	if result.shouldRelease {
		t.Fatalf("expected to stand down, got %s", result.version)
	}
}

func TestComputeAcceptsAManualVersionWithoutReadingCommits(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"v4.0.0", "v4.0.0-rc1", "v4.0.0.1"} {
		result, err := compute("v3.4.5", version, nil, noTags)
		if err != nil {
			t.Fatalf("%s: compute: %v", version, err)
		}
		if !result.shouldRelease || result.version != version {
			t.Errorf("%s: got %+v", version, result)
		}
	}
}

func TestComputeRefusesAManualVersionThatIsNotSemver(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"4.0.0", "v4.0", "release-four", "v4.0.0 "} {
		if _, err := compute("v3.4.5", version, nil, noTags); err == nil {
			t.Errorf("%q: expected a refusal", version)
		}
	}
}

// A tag the bump cannot count up from stops the release rather than producing a
// version from a partly parsed one.
func TestComputeRefusesAPreviousTagItCannotCountFrom(t *testing.T) {
	t.Parallel()

	if _, err := compute("v3.4.5-rc1", "", built(subjects("fix: a thing")...), noTags); err == nil {
		t.Fatal("expected a refusal")
	}
}

func TestWriteOutputsAppendsTheThreeKeys(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "outputs")
	if err := writeOutputs(path, decision{shouldRelease: true, version: "v4.0.0", previousTag: "v3.9.1"}); err != nil {
		t.Fatalf("writeOutputs: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	for _, want := range []string{"should_release=true", "version=v4.0.0", "previous_tag=v3.9.1"} {
		if !strings.Contains(string(raw), want+"\n") {
			t.Errorf("missing %q in:\n%s", want, raw)
		}
	}
}

// GITHUB_OUTPUT is appended to, not written: other steps put their own values
// in the same file.
func TestWriteOutputsKeepsWhatWasThereAlready(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "outputs")
	if err := os.WriteFile(path, []byte("existing=value\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := writeOutputs(path, decision{}); err != nil {
		t.Fatalf("writeOutputs: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.HasPrefix(string(raw), "existing=value\n") {
		t.Errorf("the earlier value was lost:\n%s", raw)
	}
	if !strings.Contains(string(raw), "should_release=false") {
		t.Errorf("missing the decision:\n%s", raw)
	}
}
