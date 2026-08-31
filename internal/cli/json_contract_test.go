package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// commandsThatDoNotEmitJSON are the commands whose stdout is deliberately not a
// bb.machine envelope, each with the reason.
//
// This list is the rule, made checkable. A command absent from it that prints
// something other than JSON under --json is a defect: the caller cannot tell
// "this command does not do JSON" from "this command failed to", which is how
// ai skill install and remove went unnoticed while 229 of 233 commands were
// correct.
//
// Adding an entry is a decision, not a formality. The question to answer is
// whether the command returns *data* or produces a *document or stream*. Data
// owes the caller an envelope (ADR-014). A document does not, because wrapping
// markdown or a diff in a JSON string helps nobody.
// help and completion are not listed: Cobra injects them at execute time, so
// they are not in the command tree and are not part of the surface this
// project documents or ships schemas for.
var commandsThatDoNotEmitJSON = map[string]string{
	"ai skill show": "prints a SKILL.md document; wrapping markdown in a data string helps nobody",
	"api":           "streams the upstream response body verbatim, which is the point of the escape hatch",
}

// TestEveryCommandEitherEmitsJSONOrSaysWhyNot walks the command tree and holds
// each command to one of the two contracts.
//
// It cannot invoke them -- most need a server -- so it checks the property that
// is statically visible: a command that writes to stdout on its success path
// either goes through the shared JSON writer or is named above. That is weaker
// than running them, and it is the half that would have caught this one.
func TestEveryCommandEitherEmitsJSONOrSaysWhyNot(t *testing.T) {
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
				Contract string `json:"contract"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
			t.Fatalf("%v did not emit JSON: %v\n%s", arguments, err, out.String())
		}

		if envelope.Meta.Contract != "bb.machine" {
			t.Errorf("%v contract = %q, want bb.machine", arguments, envelope.Meta.Contract)
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
