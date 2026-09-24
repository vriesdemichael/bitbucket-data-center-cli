package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"

	resultpkg "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// runDescribe invokes a command with --describe and returns its stdout.
func runDescribe(t *testing.T, arguments ...string) string {
	t.Helper()

	return runIsolated(t, append(arguments, "--describe")...)
}

// runIsolated runs bb with no configuration and no server, and returns stdout.
func runIsolated(t *testing.T, arguments ...string) string {
	t.Helper()

	directory := t.TempDir()
	t.Chdir(directory)
	t.Setenv("BB_CONFIG_PATH", directory+"/config.yaml")
	t.Setenv("BB_URL", "")
	t.Setenv("BB_TOKEN", "")
	t.Setenv("BITBUCKET_USERNAME", "")
	t.Setenv("BITBUCKET_PASSWORD", "")

	root := NewRootCommand()
	out := &bytes.Buffer{}
	root.SetOut(out)
	root.SetErr(&bytes.Buffer{})
	root.SetArgs(arguments)

	if err := root.Execute(); err != nil {
		t.Fatalf("bb %v failed: %v\n%s", arguments, err, out.String())
	}

	return out.String()
}

// describeDocument runs bb <command> --describe --json and returns the
// description member.
func describeDocument(t *testing.T, command ...string) map[string]any {
	t.Helper()

	output := runDescribe(t, append(command, "--json")...)

	var document map[string]any
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatalf("--describe --json did not emit JSON: %v\n%s", err, output)
	}
	if len(document) != 2 || document["meta"] == nil {
		t.Fatalf("members = %v, want description and meta", document)
	}
	description, ok := document["description"].(map[string]any)
	if !ok {
		t.Fatalf("no description member:\n%s", output)
	}

	return description
}

// TestDescribeGivesTheWholeDocumentForEachMode is ADR-097: the schema of the
// whole document a run writes -- data and meta, or error and meta -- and of the
// one --dry-run writes, rather than of data alone.
func TestDescribeGivesTheWholeDocumentForEachMode(t *testing.T) {
	description := describeDocument(t, "pr", "get")

	run := schemaAt(t, description, "run", "outputSchema")
	branches := run["oneOf"].([]any)
	if len(branches) != 2 {
		t.Fatalf("run: %d shapes, want data and error", len(branches))
	}
	data := branches[0].(map[string]any)["properties"].(map[string]any)["data"]
	if _, hasError := branches[1].(map[string]any)["properties"].(map[string]any)["error"]; !hasError {
		t.Fatalf("run: the second shape is not the error document")
	}

	// The data schema is the one the command declares, not a summary of it:
	// both come from the same type, so --describe cannot drift from the payload.
	declared, ok := resultpkg.SchemaFor("pr get")
	if !ok {
		t.Fatal("pr get declares no schema")
	}
	if !reflect.DeepEqual(data, schemaJSON(t, declared)) {
		t.Errorf("run.data is not the schema pr get declares")
	}

	dryRun := description["dryRun"].(map[string]any)
	if dryRun["behaviour"] != "runs" || dryRun["tier"] != "server-validated" {
		t.Errorf("dryRun = %v, want a read that runs", dryRun)
	}
	preview := schemaAt(t, description, "dryRun", "outputSchema")["oneOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["preview"].(map[string]any)
	if !reflect.DeepEqual(preview["properties"].(map[string]any)["data"], schemaJSON(t, declared)) {
		t.Errorf("a read's preview.data is not its declared data")
	}
}

// TestTheDescribedSchemasValidateRealOutput is what makes the schemas worth
// having: a document the command really writes validates against the schema
// --describe gives for it, in each mode.
func TestTheDescribedSchemasValidateRealOutput(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		command []string
		mode    string
		args    []string
	}{
		{"a read", []string{"auth", "server", "list"}, "run", []string{"--json", "auth", "server", "list"}},
		{"a read under --dry-run", []string{"auth", "server", "list"}, "dryRun", []string{"--json", "--dry-run", "auth", "server", "list"}},
		{"a change to this machine under --dry-run", []string{"auth", "logout"}, "dryRun", []string{"--json", "--dry-run", "auth", "logout"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			schema := schemaAt(t, describeDocument(t, testCase.command...), testCase.mode, "outputSchema")
			compiled := compileSchema(t, schema)

			var document any
			output := runIsolated(t, testCase.args...)
			if err := json.Unmarshal([]byte(output), &document); err != nil {
				t.Fatalf("not JSON: %v\n%s", err, output)
			}
			if err := compiled.Validate(document); err != nil {
				t.Fatalf("the real document does not validate against --describe's schema: %v\n%s", err, output)
			}
		})
	}
}

// TestEveryDescriptionIsAValidSchema compiles what --describe gives for every
// command, so no command publishes a schema a validator rejects.
func TestEveryDescriptionIsAValidSchema(t *testing.T) {
	t.Parallel()

	root := NewRootCommand()
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			walk(child)
		}
		if !cmd.Runnable() {
			return
		}

		path := commandPathWithoutRoot(cmd)
		description := schemaJSON(t, DescribeCommand(path))
		for _, mode := range []string{"run", "dryRun"} {
			if section, ok := description[mode].(map[string]any); ok && section["outputSchema"] != nil {
				compileSchema(t, section["outputSchema"].(map[string]any))
			}
		}
	}
	walk(root)
}

// TestDescribeSaysWhyWhenDataHasNoShape keeps the answer truthful.
//
// `webhook test` hands back whatever the endpoint answered, untyped, so its
// data has no shape bb can promise: the document is described with data left
// open, and the reason beside it.
func TestDescribeSaysWhyWhenDataHasNoShape(t *testing.T) {
	description := describeDocument(t, "webhook", "test")

	run := description["run"].(map[string]any)
	if !strings.Contains(run["reason"].(string), "no shape bb can promise") {
		t.Errorf("reason = %v", run["reason"])
	}
	if run["outputSchema"] == nil {
		t.Error("the document around the open data is still described")
	}
}

// TestDescribeSaysSoOfACommandWithNoDocument covers bb api, which streams the
// upstream body and writes no document of its own. A caller that cannot tell
// that from "not written yet" would wait for a contract that is never coming.
func TestDescribeSaysSoOfACommandWithNoDocument(t *testing.T) {
	description := describeDocument(t, "api")

	run := description["run"].(map[string]any)
	if run["outputSchema"] != nil || !strings.Contains(run["reason"].(string), "writes no document of its own") {
		t.Errorf("run = %v", run)
	}
	if description["dryRun"] == nil {
		t.Error("bb api takes --dry-run, so its description says what that does")
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
		{"project", "permissions", "list"}, // deeply nested, needs a server
	} {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			if description := describeDocument(t, command...); description["run"] == nil {
				t.Errorf("no run description: %v", description)
			}
			if text := runDescribe(t, command...); !strings.Contains(text, "bb "+strings.Join(command, " ")+" --json") {
				t.Errorf("the outline does not name the command:\n%s", text)
			}
		})
	}
}

// TestDescribeWithoutJSONIsAnOutline is ADR-097's text form: the document's
// fields for a person, one sentence each, and what --dry-run does.
func TestDescribeWithoutJSONIsAnOutline(t *testing.T) {
	text := runDescribe(t, "pr", "merge")

	for _, want := range []string{
		"bb pr merge --json prints data, or error when it fails:",
		"\ndata\n",
		"    title               string",
		"\nmeta\n",
		"? marks a field that can be absent.",
		"--dry-run: ",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the outline lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `"type"`) {
		t.Errorf("the outline prints JSON Schema:\n%s", text)
	}
}

// TestDescribeOfAGroupIsItsCatalogue: a group, or bb itself, lists its commands
// with what --dry-run does for each, so one call covers the whole tool.
func TestDescribeOfAGroupIsItsCatalogue(t *testing.T) {
	commands := describeDocument(t, "pr")["commands"].(map[string]any)
	merge, ok := commands["pr merge"].(map[string]any)
	if !ok || merge["dryRun"].(map[string]any)["behaviour"] != "verifies" {
		t.Fatalf("pr merge in the catalogue = %v", commands["pr merge"])
	}
	if list := commands["pr list"].(map[string]any)["dryRun"].(map[string]any); list["behaviour"] != "runs" {
		t.Errorf("pr list = %v, want a read that runs", list)
	}
	if serve, ok := describeDocument(t, "ai")["commands"].(map[string]any)["ai mcp serve"].(map[string]any); !ok || serve["dryRun"] != nil {
		t.Errorf("ai mcp serve does not take --dry-run, so its entry names no behaviour: %v", serve)
	}

	var everything map[string]any
	if err := json.Unmarshal([]byte(runIsolated(t, "--describe", "--json")), &everything); err != nil {
		t.Fatalf("bb --describe --json: %v", err)
	}
	if count := len(everything["description"].(map[string]any)["commands"].(map[string]any)); count < 200 {
		t.Errorf("bb --describe lists %d commands; it covers the whole tool", count)
	}

	text := runDescribe(t, "pr")
	if !strings.Contains(text, "Commands in bb pr") || !strings.Contains(text, "pr merge") || strings.Contains(text, "Usage:") {
		t.Errorf("the catalogue as text:\n%s", text)
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
			arguments: []string{"project", "get"},
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
	t.Parallel()

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

// schemaJSON is a value as the JSON a caller receives: every assertion about a
// schema belongs on the encoded form, which is the only form anything outside
// bb sees.
func schemaJSON(t *testing.T, value any) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode: %v", err)
	}

	return document
}

// schemaAt reads description[mode][key] as a schema.
func schemaAt(t *testing.T, description map[string]any, mode, key string) map[string]any {
	t.Helper()

	section, ok := description[mode].(map[string]any)
	if !ok {
		t.Fatalf("no %s in %v", mode, description)
	}
	schema, ok := section[key].(map[string]any)
	if !ok {
		t.Fatalf("no %s.%s in %v", mode, key, section)
	}

	return schema
}

// compileSchema compiles a published schema, failing the test if a validator
// would reject it.
func compileSchema(t *testing.T, schema map[string]any) *jsonschema.Schema {
	t.Helper()

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("describe.json", schema); err != nil {
		t.Fatalf("add schema: %v", err)
	}
	compiled, err := compiler.Compile("describe.json")
	if err != nil {
		t.Fatalf("the schema does not compile: %v", err)
	}

	return compiled
}
