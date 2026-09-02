package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outputschemas"
)

// commandsThatDoNotEmitJSON aliases the one exemption list, which lives in
// outputschemas because the coverage report needs it too.
//
// A command absent from it that prints something other than JSON under --json
// is a defect: the caller cannot tell "this command does not do JSON" from
// "this command failed to", which is how ai skill install and remove went
// unnoticed while 229 of 233 commands were correct.
var commandsThatDoNotEmitJSON = outputschemas.CommandsWithoutDataContract

// TestEveryJSONExemptionIsARealCommandWithAReason checks the exemption list
// itself.
//
// It is only the list that is checked here. The contract the list carves out of
// -- every other command emitting an envelope under --json -- is enforced by
// walking and invoking the tree, in
// TestEveryLeafCommandUnderJSONWritesAnEnvelopeOrNothing.
func TestEveryJSONExemptionIsARealCommandWithAReason(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()

	for path, reason := range commandsThatDoNotEmitJSON {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%q is exempt with no reason; the reason is the part that gets reviewed", path)
		}
		if findCommandByPath(root, path) == nil {
			t.Errorf("%q is exempt but is not a command; the list has outlived what it describes", path)
		}
	}
}

// TestTheJSONExemptionListNamesRealCommands is the sabotage, kept as a test.
//
// The list is only worth having if something checks it. A name that no longer
// resolves is the ADR-039 failure with a different filename.
func TestTheJSONExemptionListNamesRealCommands(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()

	if findCommandByPath(root, "ai skill show") == nil {
		t.Fatal("the resolver cannot find a command that exists; it has stopped matching")
	}
	if findCommandByPath(root, "ai skill nonexistent") != nil {
		t.Error("the resolver found a command that does not exist")
	}
}

// findCommandByPath resolves a space-separated command path against the tree.
func findCommandByPath(root *cobra.Command, path string) *cobra.Command {
	current := root
	for _, name := range strings.Fields(path) {
		var next *cobra.Command
		for _, candidate := range current.Commands() {
			if candidate.Name() == name {
				next = candidate
				break
			}
		}
		if next == nil {
			return nil
		}
		current = next
	}
	return current
}

// TestSkillInstallAndRemoveEmitAnEnvelope covers the two commands that did not.
//
// They report an outcome and a resolved path, which is a data payload: the path
// is chosen by bb -- project scope resolves against the working directory,
// --global against the home directory -- so a caller that wants to know where
// the file landed had to scrape an English sentence with a hardcoded prefix.
func TestSkillInstallAndRemoveEmitAnEnvelope(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)

	for _, arguments := range [][]string{
		{"--json", "ai", "skill", "install"},
		{"--json", "ai", "skill", "remove"},
		{"--json", "ai", "skill", "remove"}, // again: already absent
	} {
		root := NewRootCommand()
		out := &bytes.Buffer{}
		root.SetOut(out)
		root.SetErr(out)
		root.SetArgs(arguments)

		if err := root.Execute(); err != nil {
			t.Fatalf("%v failed: %v", arguments, err)
		}

		var envelope struct {
			Data struct {
				Status string `json:"status"`
				Path   string `json:"path"`
				Scope  string `json:"scope"`
				Skill  string `json:"skill"`
			} `json:"data"`
			Meta struct {
				BBVersion string `json:"bbVersion"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatalf("%v did not emit JSON: %v\n%s", arguments, err, out.String())
		}

		if envelope.Meta.BBVersion == "" {
			t.Errorf("%v carries no meta.bbVersion", arguments)
		}
		if envelope.Data.Path == "" {
			t.Errorf("%v carried no path, which is the value a caller cannot compute itself", arguments)
		}
		if envelope.Data.Scope != "project" {
			t.Errorf("%v scope = %q, want project", arguments, envelope.Data.Scope)
		}
		if envelope.Data.Skill != "bb" {
			t.Errorf("%v skill = %q, want bb", arguments, envelope.Data.Skill)
		}
	}
}
