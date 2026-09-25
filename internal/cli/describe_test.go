package cli

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/spf13/cobra"

	updatecmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/update"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
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
		{"a request bb api would send", []string{"api"}, "dryRun", []string{"--json", "--dry-run", "api", "-X", "POST", "/rest/api/latest/projects"}},
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

// TestTheDescribedSchemasRejectWhatIsNotTheDocument is the other half: a schema
// that accepted anything would pass the test above. Each variant starts from a
// real document and changes one thing, so the change is what the schema judges.
func TestTheDescribedSchemasRejectWhatIsNotTheDocument(t *testing.T) {
	run := compileSchema(t, schemaAt(t, describeDocument(t, "auth", "server", "list"), "run", "outputSchema"))
	runDocument := decodeDocument(t, runIsolated(t, "--json", "auth", "server", "list"))
	dryRun := compileSchema(t, schemaAt(t, describeDocument(t, "auth", "logout"), "dryRun", "outputSchema"))
	dryRunDocument := decodeDocument(t, runIsolated(t, "--json", "--dry-run", "auth", "logout"))

	failure := func(kind string, exitCode int) map[string]any {
		return map[string]any{"kind": kind, "message": "it failed", "exitCode": exitCode}
	}

	for _, testCase := range []struct {
		name   string
		schema *jsonschema.Schema
		base   map[string]any
		change func(document map[string]any)
		valid  bool
	}{
		{"a member the document does not have", run, runDocument, func(document map[string]any) {
			document["warnings"] = []any{}
		}, false},
		{"no meta", run, runDocument, func(document map[string]any) {
			delete(document, "meta")
		}, false},
		{"data and error together", run, runDocument, func(document map[string]any) {
			document["error"] = failure("internal", 1)
		}, false},
		{"data of another shape", run, runDocument, func(document map[string]any) {
			document["data"] = "servers"
		}, false},
		{"an error of a kind bb does not have", run, runDocument, func(document map[string]any) {
			delete(document, "data")
			document["error"] = failure("mystery", 1)
		}, false},
		{"an error", run, runDocument, func(document map[string]any) {
			delete(document, "data")
			document["error"] = failure("not_found", 4)
		}, true},
		// meta may gain fields in a minor release, so a consumer validating
		// with this schema keeps working when it does.
		{"a meta field a later release adds", run, runDocument, func(document map[string]any) {
			document["meta"].(map[string]any)["elapsed"] = 12
		}, true},
		// Under --dry-run a failure the check finds is a verdict, in the
		// preview; a top-level error means no verdict was reached (ADR-096).
		{"a verdict as a top-level error", dryRun, dryRunDocument, func(document map[string]any) {
			delete(document, "preview")
			document["error"] = failure("conflict", 5)
		}, false},
		{"no verdict reached", dryRun, dryRunDocument, func(document map[string]any) {
			delete(document, "preview")
			document["error"] = failure("transient", 10)
		}, true},
		{"an outcome a dry run does not have", dryRun, dryRunDocument, func(document map[string]any) {
			effects := document["preview"].(map[string]any)["effects"].([]any)
			effects[0].(map[string]any)["outcome"] = "maybe"
		}, false},
		// A command that changes something does not run under --dry-run, so
		// there is no data to carry.
		{"data in the preview of a change", dryRun, dryRunDocument, func(document map[string]any) {
			document["preview"].(map[string]any)["data"] = map[string]any{}
		}, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			document := schemaJSON(t, testCase.base)
			testCase.change(document)

			err := testCase.schema.Validate(any(document))
			if testCase.valid && err != nil {
				t.Errorf("the schema rejects it: %v\n%v", err, document)
			}
			if !testCase.valid && err == nil {
				t.Errorf("the schema accepts it:\n%v", document)
			}
		})
	}
}

// TestUpdatesDryRunSchemaHoldsItsReport: bb update changes something, yet its
// dry run carries the report of the release it checked in preview.data. The
// dry run needs a release source, so the document is built as bb update builds
// it rather than run: the schema accepts its report, and no other data.
func TestUpdatesDryRunSchemaHoldsItsReport(t *testing.T) {
	schema := compileSchema(t, schemaAt(t, describeDocument(t, "update"), "dryRun", "outputSchema"))

	document := func(data any) any {
		return schemaJSON(t, jsonoutput.PreviewEnvelope{
			Preview: jsonoutput.Preview{
				Tier: jsonoutput.TierServerValidated,
				Effects: []jsonoutput.Effect{{
					Action:  "update",
					Target:  map[string]any{"binary": "/usr/local/bin/bb", "version": "v5.0.0"},
					Outcome: jsonoutput.OutcomeWouldApply,
					Reasons: []string{"v5.0.0 would replace v4.1.0"},
				}},
				Data: data,
			},
			Meta: jsonoutput.EnvelopeMeta{Command: "update", BBVersion: "4.1.0"},
		})
	}

	report := updatecmd.Update{CurrentVersion: "v4.1.0", LatestVersion: "v5.0.0", UpdateAvailable: true, DryRun: true}
	if err := schema.Validate(document(report)); err != nil {
		t.Errorf("the schema rejects bb update's report: %v", err)
	}
	if err := schema.Validate(document("not a report")); err == nil {
		t.Error("the schema accepts any data in bb update's preview")
	}
}

// decodeDocument decodes a document bb wrote.
func decodeDocument(t *testing.T, output string) map[string]any {
	t.Helper()

	var document map[string]any
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, output)
	}

	return document
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

	// The prose around the fields wraps at the outline's width: pr merge's
	// fields all fit in 120 columns, and bb api's answer is prose alone.
	for _, command := range [][]string{{"pr", "merge"}, {"api"}} {
		for line := range strings.SplitSeq(runDescribe(t, command...), "\n") {
			if width := utf8.RuneCountInString(line); width > 120 {
				t.Errorf("bb %s --describe: a line of %d columns:\n%s", strings.Join(command, " "), width, line)
			}
		}
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
	catalogue := everything["description"].(map[string]any)["commands"].(map[string]any)
	if count := len(catalogue); count < 200 {
		t.Errorf("bb --describe lists %d commands; it covers the whole tool", count)
	}

	// The behaviour and the tier say the same thing twice, so they must
	// agree: a read runs and is Bitbucket's answer, a preview that checks
	// what the change depends on verifies, and one that does not predicts.
	for path, entry := range catalogue {
		dryRun, takes := entry.(map[string]any)["dryRun"].(map[string]any)
		if !takes {
			continue
		}
		behaviour, tier := dryRun["behaviour"], dryRun["tier"]
		switch {
		case behaviour == "runs" && tier == "server-validated":
		case behaviour == "verifies" && (tier == "server-validated" || tier == "preconditions-checked"):
		case behaviour == "predicts" && tier == "predicted":
		default:
			t.Errorf("%s: --dry-run %v at tier %v", path, behaviour, tier)
		}
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
