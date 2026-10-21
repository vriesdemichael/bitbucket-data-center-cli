package prcmd

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
)

// Every command that reads one pull request takes it optionally, and no more
// than one (#766): the Use line says so, which the help text and completion
// read, and the arity holds it.
func TestTheReadCommandsTakeThePullRequestOptionally(t *testing.T) {
	t.Parallel()

	root := New(Dependencies{JSONEnabled: func() bool { return false }, WriteJSON: jsonoutput.Write, WriteJSONList: jsonoutput.WriteList})
	for _, path := range commandsReadingTheCurrentPullRequest {
		command, _, err := root.Find(strings.Fields(path))
		if err != nil || command == root {
			t.Errorf("pr %s: not found", path)
			continue
		}
		if !strings.HasSuffix(command.Use, " [pr-id]") {
			t.Errorf("pr %s: Use is %q, want the pull request optional", path, command.Use)
		}
		for count, want := range map[int]bool{0: true, 1: true, 2: false} {
			args := make([]string, count)
			if accepted := command.Args(command, args) == nil; accepted != want {
				t.Errorf("pr %s with %d arguments: accepted %v, want %v", path, count, accepted, want)
			}
		}
	}
}

// A path that is not a command taking <pr-id> alone is a wiring error, and
// stops the tree from building rather than leaving a command half changed.
func TestTakingTheCurrentPullRequestRefusesAnotherCommand(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"create", "comment get", "no-such-command"} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("pr %s was taken as a command reading one pull request", path)
				}
			}()
			root := New(Dependencies{JSONEnabled: func() bool { return false }, WriteJSON: jsonoutput.Write, WriteJSONList: jsonoutput.WriteList})
			repository := ""
			takeTheCurrentPullRequest(root, path, Dependencies{}, &repository)
		}()
	}
}
