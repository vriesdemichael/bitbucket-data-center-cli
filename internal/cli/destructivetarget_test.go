package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestTheTargetNamesWhatIsGoing covers what a person is asked to type back.
//
// The target was built from the repository, the parent command and the
// positional arguments, which is everything except the part that says which
// one on a command that takes it as a flag: `bb pr reviewer remove 1 --user
// bob` asked to confirm "PROJ/demo reviewer 1", naming the pull request that
// stays and not the reviewer that goes. A confirmation that cannot tell two
// invocations apart is a keystroke with extra steps.
func TestTheTargetNamesWhatIsGoing(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		group   string
		leaf    string
		flags   []string
		args    []string
		want    []string
		unnamed []string
	}{
		{
			name:  "a flag says which reviewer",
			group: "reviewer",
			leaf:  "remove",
			flags: []string{"--repo", "PROJ/demo", "--user", "bob"},
			args:  []string{"1"},
			want:  []string{"PROJ/demo", "reviewer", "1", "bob"},
		},
		{
			name:  "a command whose whole target is a flag",
			group: "comment",
			leaf:  "delete",
			flags: []string{"--repo", "PROJ/demo", "--id", "7"},
			want:  []string{"PROJ/demo", "comment", "7"},
		},
		{
			// How the command runs is not what it destroys, and --yes reading
			// back as "true" in the middle of the name would be worse than the
			// omission it replaced.
			name:    "control flags are not part of the target",
			group:   "branch",
			leaf:    "delete",
			flags:   []string{"--repo", "PROJ/demo", "--yes", "--json"},
			args:    []string{"feature/x"},
			want:    []string{"PROJ/demo", "branch", "feature/x"},
			unnamed: []string{"true", "yes", "json"},
		},
		{
			// A flag left at its default was not a choice the caller made.
			name:    "an unset flag says nothing",
			group:   "reviewer",
			leaf:    "remove",
			flags:   []string{"--repo", "PROJ/demo"},
			args:    []string{"1"},
			want:    []string{"PROJ/demo", "reviewer", "1"},
			unnamed: []string{"bob"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			root := &cobra.Command{Use: "bb"}
			group := &cobra.Command{Use: testCase.group}
			leaf := &cobra.Command{Use: testCase.leaf, RunE: func(*cobra.Command, []string) error { return nil }}
			root.AddCommand(group)
			group.AddCommand(leaf)

			leaf.Flags().String("repo", "", "")
			leaf.Flags().String("user", "bob", "")
			leaf.Flags().String("id", "", "")
			leaf.Flags().Bool("yes", false, "")
			leaf.Flags().Bool("json", false, "")

			if err := leaf.ParseFlags(testCase.flags); err != nil {
				t.Fatalf("could not parse %v: %v", testCase.flags, err)
			}

			target := destructiveTarget(leaf, testCase.args)

			for _, part := range testCase.want {
				if !strings.Contains(target, part) {
					t.Errorf("the confirmation does not name %q: %q", part, target)
				}
			}
			for _, part := range testCase.unnamed {
				if strings.Contains(target, part) {
					t.Errorf("the confirmation names %q, which is not part of the target: %q", part, target)
				}
			}
		})
	}
}
