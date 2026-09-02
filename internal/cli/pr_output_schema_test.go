package cli

import (
	"encoding/json"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// validateAgainstDeclaredSchema compiles the schema a command declares -- the
// same one --describe publishes -- and validates a real invocation's payload
// against it.
//
// This is the assertion the whole result package exists to make possible. A
// schema that merely looks right is worth little; pinning it to what the
// command actually emits is what stops the two drifting apart, and here they
// cannot drift silently because the schema is derived from the type the command
// fills in.
func validateAgainstDeclaredSchema(t *testing.T, commandPath string, output string) {
	t.Helper()

	declared, ok := result.SchemaFor(commandPath)
	if !ok {
		t.Fatalf("no schema is declared for %q", commandPath)
	}

	encoded, err := json.Marshal(declared)
	if err != nil {
		t.Fatalf("encode declared schema: %v", err)
	}
	var schemaMap map[string]any
	if err := json.Unmarshal(encoded, &schemaMap); err != nil {
		t.Fatalf("decode declared schema: %v", err)
	}

	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(commandPath, schemaMap); err != nil {
		t.Fatalf("add schema resource: %v", err)
	}
	schema, err := compiler.Compile(commandPath)
	if err != nil {
		t.Fatalf("compile schema: %v", err)
	}

	var envelope struct {
		Data any `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("decode command output: %v\noutput: %s", err, output)
	}

	if err := schema.Validate(envelope.Data); err != nil {
		t.Fatalf("%s output does not match its declared schema: %v\noutput: %s", commandPath, err, output)
	}
}

func TestPRGetMatchesDeclaredSchema(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "get", "7")
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	validateAgainstDeclaredSchema(t, "pr get", output)
}

// The unmeasured case omits every count, so it exercises a different shape than
// the fully populated summary.
func TestPRGetUnmeasuredMatchesDeclaredSchema(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	output, err := executeTestCLI(t, "--json", "pr", "get", "7", "--no-review-summary")
	if err != nil {
		t.Fatalf("pr get failed: %v\noutput: %s", err, output)
	}

	validateAgainstDeclaredSchema(t, "pr get", output)
}

func TestPRCommentListMatchesDeclaredSchema(t *testing.T) {
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
		{name: "full comment list", args: []string{"--full"}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := append([]string{"--json", "pr", "comment", "list", "7"}, testCase.args...)
			output, err := executeTestCLI(t, args...)
			if err != nil {
				t.Fatalf("pr comment list failed: %v\noutput: %s", err, output)
			}

			validateAgainstDeclaredSchema(t, "pr comment list", output)
		})
	}
}

// --full adds the ungrouped comment list; it does not swap one payload for
// another. The previous contract made the two mutually exclusive, so one
// command had two shapes and a consumer needed to know which flag had been
// passed to know what it was reading.
func TestPRCommentListFullAddsCommentsWithoutRemovingThreads(t *testing.T) {
	server := newReviewVisibilityServer(t)
	configureDryRunEnv(t, server.URL, "TEST", "demo")

	full, err := executeTestCLI(t, "--json", "pr", "comment", "list", "7", "--full")
	if err != nil {
		t.Fatalf("pr comment list --full failed: %v\noutput: %s", err, full)
	}

	var envelope struct {
		Data struct {
			Threads  []any `json:"threads"`
			Comments []any `json:"comments"`
			Summary  struct {
				TotalThreads int `json:"totalThreads"`
			} `json:"summary"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(full), &envelope); err != nil {
		t.Fatalf("decode command output: %v\noutput: %s", err, full)
	}

	if len(envelope.Data.Comments) == 0 {
		t.Fatalf("expected --full to carry the ungrouped comments, got: %s", full)
	}
	if len(envelope.Data.Threads) == 0 {
		t.Fatalf("expected --full to keep the thread view, got: %s", full)
	}
	if envelope.Data.Summary.TotalThreads == 0 {
		t.Fatalf("expected --full to keep the summary, got: %s", full)
	}
}
