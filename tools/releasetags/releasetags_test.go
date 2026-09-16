package releasetags_test

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/tools/releasetags"
)

// TestTheNewestTagIsNotAlwaysTheLastRelease is the whole reason this package
// exists.
//
// `git describe --match v[0-9]*.[0-9]*.[0-9]*` matches v4.1.0-rc.1: the glob's
// trailing star swallows the suffix. Whatever asked would then count the next
// release up from a tag that is not a version, report deprecations against the
// wrong major, or ask the documentation to name a release candidate -- so
// publishing one prerelease broke the next ordinary release.
func TestTheNewestTagIsNotAlwaysTheLastRelease(t *testing.T) {
	repository := t.TempDir()
	run := func(args ...string) string {
		t.Helper()

		command := exec.Command("git", args...)
		command.Dir = repository
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
		}

		return strings.TrimSpace(string(output))
	}

	run("init", "--quiet", "-b", "main", ".")
	commit := func(message string) {
		t.Helper()
		run("-c", "user.email=test@example.com", "-c", "user.name=test", "commit", "--quiet", "--allow-empty", "-m", message)
	}

	commit("the release")
	run("tag", "v1.0.0")
	commit("the candidate")
	run("tag", "v1.1.0-rc.1")
	commit("work since")

	// The fixture is only a fixture if git really does hand back the candidate
	// without the exclusions.
	loose := exec.Command("git", "describe", "--tags", "--abbrev=0", "--match", releasetags.Glob)
	loose.Dir = repository
	if output, err := loose.Output(); err != nil || strings.TrimSpace(string(output)) != "v1.1.0-rc.1" {
		t.Fatalf("expected the bare glob to match the prerelease, got %q (%v)", strings.TrimSpace(string(output)), err)
	}

	described := exec.Command("git", releasetags.DescribeArgs()...)
	described.Dir = repository
	output, err := described.Output()
	if err != nil {
		t.Fatalf("describe with the release arguments: %v", err)
	}
	if got := strings.TrimSpace(string(output)); got != "v1.0.0" {
		t.Fatalf("the last release is %q, want v1.0.0", got)
	}
}

func TestOnlyAVersionIsARelease(t *testing.T) {
	t.Parallel()

	for tag, want := range map[string]bool{
		"v1.0.0":         true,
		"v10.20.30":      true,
		" v1.0.0 ":       true,
		"v1.1.0-rc.1":    false,
		"v1.0.0+build.7": false,
		"v1.0.0.1":       false,
		"1.0.0":          false,
		"v1.0":           false,
		"":               false,
	} {
		if got := releasetags.IsRelease(tag); got != want {
			t.Errorf("IsRelease(%q) = %v, want %v", tag, got, want)
		}
	}
}
