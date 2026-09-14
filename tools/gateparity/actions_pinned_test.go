package gateparity

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const workflowsDirectory = ".github/workflows"

// actionUse matches a `uses:` line and captures the reference and whatever
// trails it.
var actionUse = regexp.MustCompile(`^\s*(?:-\s+)?uses:\s*([^\s#]+)\s*(#.*)?$`)

// pinnedAction is a remote action pinned to a full commit, with the release it
// is in written in the comment Dependabot reads and rewrites.
var pinnedAction = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_./-]+@[0-9a-f]{40}$`)

var releaseComment = regexp.MustCompile(`^#\s*v?[0-9]`)

// TestEveryActionIsPinnedToACommit keeps every workflow's actions on a commit
// rather than a tag (ADR-069).
//
// A tag is a pointer its owner can move, and the release workflow runs with a
// token that publishes signed binaries to five package managers, so an action
// whose tag is moved runs someone else's code with that token. A commit cannot
// move. The comment beside it names the release, which is what Dependabot reads
// to propose the next one and rewrites along with the commit, so pinning does
// not take an action out of the update stream.
func TestEveryActionIsPinnedToACommit(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, workflowsDirectory))
	if err != nil {
		t.Fatalf("read %s: %v", workflowsDirectory, err)
	}

	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}

		lines := readLines(t, filepath.Join(root, workflowsDirectory, name))
		checked += len(actionUses(lines))
		for _, problem := range unpinnedActions(lines) {
			t.Errorf("%s/%s: %s", workflowsDirectory, name, problem)
		}
	}

	// A scanner that stopped matching would report every workflow as pinned.
	if checked < 10 {
		t.Fatalf("expected the workflows to use at least ten actions, found %d; the uses: scanner has probably stopped matching", checked)
	}
}

// TestTheActionPinScannerFindsAFloatingTag is the sabotage, recorded as a test:
// the scanner is fed the lines it exists to catch.
func TestTheActionPinScannerFindsAFloatingTag(t *testing.T) {
	t.Parallel()

	lines := []string{
		"      - uses: actions/checkout@v7",
		"        uses: actions/setup-go@3d3c42e5aac5ba805825da76410c181273ba90b1",
		"        uses: actions/cache@3d3c42e5 # v6.1.0",
		"        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1",
		"        uses: ./.github/actions/local",
		"        uses: docker://alpine:3.20",
	}

	problems := unpinnedActions(lines)
	if len(problems) != 4 {
		t.Fatalf("expected four problems (a tag, a commit with no release comment, a short sha, a docker tag), got %d: %v", len(problems), problems)
	}
	for _, want := range []string{"actions/checkout@v7", "actions/setup-go@", "actions/cache@3d3c42e5", "docker://alpine:3.20"} {
		found := false
		for _, problem := range problems {
			if strings.Contains(problem, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no problem names %q: %v", want, problems)
		}
	}
}

// actionUses returns the reference of every `uses:` line.
func actionUses(lines []string) []string {
	uses := []string{}
	for _, line := range lines {
		if match := actionUse.FindStringSubmatch(line); match != nil {
			uses = append(uses, match[1])
		}
	}

	return uses
}

// unpinnedActions describes every `uses:` line that does not pin its action.
//
// A local action (./...) is part of this repository and needs no pin. A docker
// image must be pinned by digest, for the same reason as an action.
func unpinnedActions(lines []string) []string {
	problems := []string{}
	for number, line := range lines {
		match := actionUse.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		reference, comment := match[1], strings.TrimSpace(match[2])

		switch {
		case strings.HasPrefix(reference, "./"):
			continue
		case strings.HasPrefix(reference, "docker://"):
			if !strings.Contains(reference, "@sha256:") {
				problems = append(problems, lineProblem(number, reference, "pin the image by its @sha256: digest"))
			}
		case !pinnedAction.MatchString(reference):
			problems = append(problems, lineProblem(number, reference, "pin it to the full 40-character commit of a release"))
		case !releaseComment.MatchString(comment):
			problems = append(problems, lineProblem(number, reference, "name the release in a comment after the commit, as '# v1.2.3'"))
		}
	}

	return problems
}

func lineProblem(index int, reference, fix string) string {
	return "line " + itoa(index+1) + ": " + reference + ": " + fix
}

func itoa(value int) string {
	digits := []byte{}
	if value == 0 {
		return "0"
	}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}

	return string(digits)
}
