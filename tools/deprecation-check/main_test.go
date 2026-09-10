package main

import (
	"testing"

	cc "github.com/vriesdemichael/bitbucket-data-center-cli/tools/conventionalcommits"
)

// Only a breaking change moves the major, so only a breaking change makes a
// deprecation due. A branch accumulating fixes leaves everything alone.
func TestPendingMajorMovesOnlyForBreakingChanges(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		subjects []string
		want     int
	}{
		{name: "no commits", subjects: nil, want: 4},
		{name: "fixes only", subjects: []string{"fix: a", "fix: b"}, want: 4},
		{name: "a feature", subjects: []string{"feat: a"}, want: 4},
		{name: "a breaking change", subjects: []string{"fix: a", "feat!: b"}, want: 5},
		{name: "docs and chores", subjects: []string{"docs: a", "chore: b"}, want: 4},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			commits := make([]cc.Commit, 0, len(testCase.subjects))
			for _, subject := range testCase.subjects {
				commits = append(commits, cc.Classify("0000000", subject, ""))
			}

			got, err := pendingMajorOf("v4.3.1", commits)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("pending major %d, want %d", got, testCase.want)
			}
		})
	}
}
