package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"reflect"

	resultpkg "github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// runDescribe invokes a command with --describe and returns its stdout.
func runDescribe(t *testing.T, arguments ...string) string {
	t.Helper()

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("BB_CONFIG_PATH", directory+"/config.yaml")
	t.Setenv("BB_URL", "")
	t.Setenv("BB_TOKEN", "")

	root := NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(append(arguments, "--describe"))

	if err := root.Execute(); err != nil {
		t.Fatalf("%v --describe failed: %v\n%s", arguments, err, out.String())
	}

	return out.String()
}

// TestDescribeReturnsThePublishedSchemaForACommand is the point of #485: the
// binary answers what its own output looks like, without a network round trip
// to a docs site whose version may not match the installed binary.
func TestDescribeReturnsThePublishedSchemaForACommand(t *testing.T) {
	output := runDescribe(t, "pr", "get")

	var result DescribeResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("--describe did not emit JSON: %v\n%s", err, output)
	}

	if result.Command != "pr get" {
		t.Errorf("command = %q, want %q", result.Command, "pr get")
	}
	if !result.Described {
		t.Fatalf("pr get has a published schema but was reported undescribed: %+v", result)
	}
	if result.Schema["type"] != "object" || result.Schema["properties"] == nil {
		t.Errorf("the returned document is not a JSON Schema: %v", result.Schema)
	}

	// The document must be the schema the command declares, not a summary of
	// it. Comparing against the declaration is what makes --describe unable to
	// drift from the payload: both come from the same type.
	declared, ok := resultpkg.SchemaFor("pr get")
	if !ok {
		t.Fatal("pr get declares no schema")
	}
	encoded, err := json.Marshal(declared)
	if err != nil {
		t.Fatalf("encode declared schema: %v", err)
	}
	var expected map[string]any
	if err := json.Unmarshal(encoded, &expected); err != nil {
		t.Fatalf("decode declared schema: %v", err)
	}
	if !reflect.DeepEqual(result.Schema, expected) {
		t.Errorf("--describe returned a different document than the command declares\ngot:  %v\nwant: %v", result.Schema, expected)
	}
}

// TestDescribeSaysSoWhenACommandHasNoSchema keeps the answer truthful.
//
// Most commands have no schema, and a caller is better served by "not described
// yet" than by an empty schema that appears to guarantee a shape.
func TestDescribeSaysSoWhenACommandHasNoSchema(t *testing.T) {
	output := runDescribe(t, "repo", "create")

	var result DescribeResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("--describe did not emit JSON: %v\n%s", err, output)
	}

	if result.Described {
		t.Errorf("repo create has no published schema but reported one: %+v", result)
	}
	if result.Schema != nil {
		t.Errorf("an undescribed command returned a schema: %v", result.Schema)
	}
	if strings.TrimSpace(result.Reason) == "" {
		t.Error("an undescribed command gave no reason")
	}
}

// TestDescribeDistinguishesNoSchemaFromNoDataPayload covers the third answer.
//
// `bb api` will never have a schema, because it streams the upstream body. A
// caller that cannot tell that from "not written yet" would wait for a contract
// that is never coming.
func TestDescribeDistinguishesNoSchemaFromNoDataPayload(t *testing.T) {
	output := runDescribe(t, "api")

	var result DescribeResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("--describe did not emit JSON: %v\n%s", err, output)
	}

	if result.Described {
		t.Errorf("api reported a schema: %+v", result)
	}
	if !strings.Contains(result.Reason, "does not return a data payload") {
		t.Errorf("reason = %q, want it to say the command returns no data", result.Reason)
	}
}

// TestDescribeNeedsNoArgumentsFlagsOrConfiguration is the usability property.
//
// Cobra validates arguments and required flags before RunE, so without the
// wrappers `bb pr get --describe` would fail for a missing pull request id and
// `bb repo create --describe` for missing --name and --project. Asking what a
// command returns must not require knowing what it takes -- and must not need a
// server or a configuration file, since the schemas are compiled in.
func TestDescribeNeedsNoArgumentsFlagsOrConfiguration(t *testing.T) {
	for _, command := range [][]string{
		{"pr", "get"},                      // positional argument required
		{"repo", "create"},                 // required flags
		{"tag", "list"},                    // needs a repository and a server
		{"auth", "status"},                 // needs configuration
		{"bulk", "apply"},                  // required --from-plan
		{"project", "permissions", "list"}, // deeply nested, needs a server
	} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			output := runDescribe(t, command...)

			var result DescribeResult
			if err := json.Unmarshal([]byte(output), &result); err != nil {
				t.Fatalf("--describe did not emit JSON: %v\n%s", err, output)
			}
			if result.Command != strings.Join(command, " ") {
				t.Errorf("command = %q, want %q", result.Command, strings.Join(command, " "))
			}
		})
	}
}

// TestDescribeUnderJSONIsAnEnvelope keeps it consistent with everything else the
// CLI emits under --json: one bb.machine document, data carrying the payload.
func TestDescribeUnderJSONIsAnEnvelope(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("BB_CONFIG_PATH", directory+"/config.yaml")

	root := NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"--json", "tag", "list", "--describe"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute failed: %v", err)
	}

	var envelope struct {
		Data DescribeResult `json:"data"`
		Meta struct {
			BBVersion string `json:"bbVersion"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
		t.Fatalf("not an envelope: %v\n%s", err, out.String())
	}
	if envelope.Meta.BBVersion == "" {
		t.Error("the envelope carries no meta.bbVersion")
	}
	if envelope.Data.Command != "tag list" {
		t.Errorf("command = %q, want tag list", envelope.Data.Command)
	}
	if !envelope.Data.Described {
		t.Errorf("tag list has a published schema but was reported undescribed")
	}
}

// TestDescribeStillRunsTheRealCommand is the guard on the wrapping.
//
// installDescribe replaces every runnable command's RunE. A wrapper that
// swallowed the original would leave commands appearing to succeed while doing
// nothing -- and validation tests cannot see that, because argument and
// required-flag checks fail before RunE is ever reached. This runs a command
// that does its work without a server, and checks the work happened.
func TestDescribeStillRunsTheRealCommand(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("BB_CONFIG_PATH", directory+"/config.yaml")

	root := NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"ai", "skill", "show"})

	if err := root.Execute(); err != nil {
		t.Fatalf("ai skill show failed: %v", err)
	}
	if !strings.Contains(out.String(), "name: bb") {
		t.Fatalf("the command produced no output, so the wrapper swallowed its RunE:\n%q", out.String())
	}
}

// TestDescribeDoesNotRelaxValidationForOrdinaryInvocations is the other half.
//
// The wrapper also replaces Args, and skips validation when --describe is set.
// Without --describe the original validation must still run, or a mistyped
// command would silently succeed.
func TestDescribeDoesNotRelaxValidationForOrdinaryInvocations(t *testing.T) {
	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("BB_CONFIG_PATH", directory+"/config.yaml")

	for _, testCase := range []struct {
		name      string
		arguments []string
		expect    string
	}{
		{
			name:      "required flags are still enforced",
			arguments: []string{"repo", "create"},
			expect:    "required flag",
		},
		{
			name:      "positional arguments are still enforced",
			arguments: []string{"bulk", "status"},
			expect:    "arg",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := NewRootCommand()
			out := &bytes.Buffer{}
			root.SetOut(out)
			root.SetErr(out)
			root.SetArgs(testCase.arguments)

			err := root.Execute()
			if err == nil {
				t.Fatalf("%v succeeded with no validation error", testCase.arguments)
			}
			if !strings.Contains(strings.ToLower(err.Error()), testCase.expect) {
				t.Errorf("error = %q, want it to mention %q", err, testCase.expect)
			}
		})
	}
}

// TestEveryRunnableCommandAnswersDescribe walks the tree, because a command
// added later must answer too.
func TestEveryRunnableCommandAnswersDescribe(t *testing.T) {
	root := NewRootCommand()

	missing := []string{}

	var walk func(command *cobra.Command)
	walk = func(command *cobra.Command) {
		for _, child := range command.Commands() {
			if child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			walk(child)
		}
		if !command.Runnable() || command == root {
			return
		}
		if command.Flags().Lookup(describeFlag) == nil && command.InheritedFlags().Lookup(describeFlag) == nil {
			missing = append(missing, commandPathWithoutRoot(command))
		}
	}
	walk(root)

	if len(missing) > 0 {
		t.Errorf("%d commands do not accept --describe: %v", len(missing), missing)
	}
}
