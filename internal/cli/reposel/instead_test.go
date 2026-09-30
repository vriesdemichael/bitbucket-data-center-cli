package reposel

import (
	"testing"

	"github.com/spf13/pflag"
)

func TestNamedInsteadOfRepo(t *testing.T) {
	t.Parallel()

	flagSet := func() *pflag.FlagSet {
		flags := pflag.NewFlagSet("reviewer-group", pflag.ContinueOnError)
		flags.String("project", "", "")
		flags.String("repo", "", "")
		flags.String("users", "", "")
		MarkInsteadOfRepo(flags, "project")

		return flags
	}

	for name, testCase := range map[string]struct {
		args []string
		want string
	}{
		"nothing given":            {args: nil, want: ""},
		"the marked flag given":    {args: []string{"--project", "PRJ"}, want: "project"},
		"only an unmarked flag":    {args: []string{"--users", "alice"}, want: ""},
		"the marked flag as empty": {args: []string{"--project="}, want: ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			flags := flagSet()
			if err := flags.Parse(testCase.args); err != nil {
				t.Fatalf("parse %v: %v", testCase.args, err)
			}
			if got := NamedInsteadOfRepo(flags); got != testCase.want {
				t.Errorf("NamedInsteadOfRepo after %v = %q, want %q", testCase.args, got, testCase.want)
			}
		})
	}
}

// A mark on a flag the command does not have would declare nothing, and the
// inference it was meant to stop would go on as before without a word.
func TestMarkInsteadOfRepoRefusesAMissingFlag(t *testing.T) {
	t.Parallel()

	defer func() {
		if recover() == nil {
			t.Error("marking a flag that does not exist was accepted")
		}
	}()

	MarkInsteadOfRepo(pflag.NewFlagSet("search prs", pflag.ContinueOnError), "role")
}
