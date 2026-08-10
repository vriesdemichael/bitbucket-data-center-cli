package cli

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/outputschemas"
)

// validateAgainstOutputSchema compiles a published output schema and validates a
// real command's JSON against it. A schema that merely looks right is worth
// little; this pins it to what the command actually emits, so the two cannot
// drift apart silently.
func validateAgainstOutputSchema(t *testing.T, schemaName string, output string) {
	t.Helper()

	schemaMap, ok := outputschemas.Schemas()[schemaName]
	if !ok {
		t.Fatalf("no published schema named %q", schemaName)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(schemaName, schemaMap); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile(schemaName)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	var decoded any
	if err := json.Unmarshal([]byte(output), &decoded); err != nil {
		t.Fatalf("decode command output: %v\noutput: %s", err, output)
	}

	if err := schema.Validate(decoded); err != nil {
		t.Fatalf("%s output does not match its published schema: %v\noutput: %s", schemaName, err, output)
	}
}

func TestPRGetMatchesPublishedSchema(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "get", "7")
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	validateAgainstOutputSchema(t, "output.pr.get.schema.json", output)
}

// The unmeasured case omits every count, so it exercises a different shape than
// the fully populated summary.
func TestPRGetUnmeasuredMatchesPublishedSchema(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "get", "7", "--no-review-summary")
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	validateAgainstOutputSchema(t, "output.pr.get.schema.json", output)
}

func TestPRCommentListMatchesPublishedSchema(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	cases := []struct {
		name string
		args []string
	}{
		{name: "default thread view", args: nil},
		{name: "unresolved filter", args: []string{"--unresolved"}},
		{name: "with replies", args: []string{"--with-replies"}},
		{name: "tasks only", args: []string{"--tasks-only"}},
		{name: "path scoped", args: []string{"--path", "internal/cli/root.go"}},
		{name: "raw payload", args: []string{"--full"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := append([]string{"--json", "pr", "comment", "list", "7"}, testCase.args...)
			output, err := executeTestCLI(t, args...)
			if err != nil {
				t.Fatalf("pr comment list failed: %v\noutput: %s", err, output)
			}

			validateAgainstOutputSchema(t, "output.pr.comment.list.schema.json", output)
		})
	}
}

// The schema declares the two payload shapes as mutually exclusive, so a
// document carrying both threads and comments must be rejected. This guards the
// oneOf rather than the command.
func TestPRCommentListSchemaRejectsMixedPayload(t *testing.T) {
	schemaMap := outputschemas.Schemas()["output.pr.comment.list.schema.json"]

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("mixed", schemaMap); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile("mixed")
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	var mixed any
	if err := json.Unmarshal([]byte(`{
      "version": "v2",
      "meta": {"contract": "bb.machine"},
      "data": {
        "repository": {"project_key": "TEST", "slug": "demo"},
        "pull_request_id": "7",
        "source": "activities",
        "path": "",
        "state": "all",
        "summary": {"total_threads":0,"unresolved":0,"resolved":0,"pending":0,"open_tasks":0,"resolved_tasks":0},
        "threads": [],
        "comments": []
      }
    }`), &mixed); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	if err := schema.Validate(mixed); err == nil {
		t.Fatal("expected a payload carrying both threads and comments to be rejected")
	}
}
