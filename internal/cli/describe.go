package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/outputschemas"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// describeFlag is the persistent flag that makes a command print its own output
// contract instead of running.
const describeFlag = "describe"

// DescribeResult is the payload `--describe` emits.
//
// It is a contract of its own, so it has a fixed shape rather than being
// whatever the lookup happened to return. Described says whether Schema is
// present; when it is not, Reason says why in a form a human can read and an
// agent can log. A caller checks Described before reading Schema, the same way
// it checks error before data on the envelope.
type DescribeResult struct {
	// Schema is any rather than a map because the two sources produce
	// different Go values -- a derived *jsonschema.Schema and a decoded
	// map -- and both encode to the same JSON. Narrowing the field forced the
	// derived one through a marshal and an unmarshal to become a map that was
	// then marshalled again.
	Command   string `json:"command"`
	Described bool   `json:"described"`
	Schema    any    `json:"schema,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

// installDescribe makes every runnable command answer --describe.
//
// It wraps each command rather than using PersistentPreRunE because Cobra
// validates arguments before it runs the hooks: `bb pr get --describe` would
// otherwise fail for a missing pull request id, which is exactly the thing the
// caller is asking about rather than supplying. Argument validation is skipped
// when --describe is set, for the same reason.
func installDescribe(root *cobra.Command, describe *bool) {
	// Cobra adds help and completion on first execution, which is after this
	// walk. Adding them now is what puts them under the same rule as every
	// other command instead of leaving two that answer --describe with help
	// text.
	root.InitDefaultHelpCmd()
	root.InitDefaultCompletionCmd()

	// A group is not runnable, so the walk below never reaches it and Cobra
	// prints its help. --describe answering with a page of human text on
	// stdout and exit 0 is the worst of both: a caller parsing the answer sees
	// a success it cannot read. The help function is where Cobra sends a
	// non-runnable command, so it is where the JSON answer goes.
	help := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if *describe && !cmd.Runnable() {
			// Nothing to return but the reason, and an error here would only
			// be printed after the document a caller came for.
			_ = writeDescription(cmd)
			return
		}
		help(cmd, args)
	})

	var walk func(command *cobra.Command)
	walk = func(command *cobra.Command) {
		for _, child := range command.Commands() {
			walk(child)
		}

		if !command.Runnable() {
			return
		}

		originalArgs := command.Args
		command.Args = func(cmd *cobra.Command, args []string) error {
			if *describe {
				// Cobra validates required flags after this point but before
				// RunE, so clearing them has to happen here. Asking what a
				// command returns must not require knowing what it takes:
				// `bb repo create --describe` otherwise fails for a missing
				// --name and --project, which is the opposite of helpful.
				relaxRequiredFlags(cmd)
				return nil
			}
			if originalArgs == nil {
				return nil
			}
			return originalArgs(cmd, args)
		}

		originalRunE, originalRun := command.RunE, command.Run
		command.Run = nil
		command.RunE = func(cmd *cobra.Command, args []string) error {
			if *describe {
				return writeDescription(cmd)
			}
			if originalRunE != nil {
				return originalRunE(cmd, args)
			}
			if originalRun != nil {
				originalRun(cmd, args)
			}
			return nil
		}
	}

	walk(root)
}

// writeDescription emits the output contract for one command.
func writeDescription(cmd *cobra.Command) error {
	path := commandPathWithoutRoot(cmd)

	// A group holds commands rather than returning anything, so it has no output
	// to describe -- and neither does bb itself, which reaches here with an
	// empty path. Saying that is the answer; falling through to the lookup would
	// report "no schema published yet", which reads as an omission rather than a
	// category.
	described := DescribeResult{
		Command: path,
		Reason:  "this is a command group, not a command; describe one of its subcommands",
	}
	if cmd.Runnable() {
		described = describeCommand(path)
	}

	jsonRequested, _ := cmd.Root().PersistentFlags().GetBool("json")
	if jsonRequested {
		return jsonoutput.Write(cmd.OutOrStdout(), described)
	}

	// Without --json the schema is still a JSON document, so it is printed as
	// one. There is no table form of a JSON Schema worth having.
	encoded, err := jsonoutput.MarshalIndent(described)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), string(encoded))

	return err
}

// describeCommand looks up the schema a command declares.
//
// Three answers, and which one a caller gets is itself information: a schema
// derived from the result type the command fills in, a statement that the
// command returns no data payload at all, or a statement that it returns one
// whose shape bb cannot promise. An empty schema is not among them -- it would
// look like a guarantee of nothing rather than an absence of one.
//
// A fourth answer, for a command group, is decided by writeDescription: a group
// never reaches here, because it has no output to look up.
func describeCommand(path string) DescribeResult {
	described := DescribeResult{Command: path}

	if reason := outputschemas.CommandsWithoutDataContract[path]; reason != "" {
		described.Reason = "this command does not return a data payload: " + reason
		return described
	}

	if reason := outputschemas.CommandsWithoutDeclarableShape[path]; reason != "" {
		described.Reason = "this command returns a payload with no shape bb can promise: " + reason
		return described
	}

	// A schema derived from the command's own result type wins. It cannot drift
	// from what the command emits, because it is the same declaration; the
	// hand-written fallback is what is being retired (#521), and every command
	// that moves across stops being able to disagree with itself.
	if schema, ok := result.SchemaFor(path); ok {
		described.Described = true
		described.Schema = schema

		return described
	}

	fileName := "output." + strings.ReplaceAll(path, " ", ".") + ".schema.json"
	schema, ok := outputschemas.Schemas()[fileName]
	if !ok {
		described.Reason = "no output schema is published for this command yet; the payload shape is not guaranteed"
		return described
	}

	// The hand-written schemas describe the whole bb.machine envelope, while a
	// derived one describes the data payload. --describe answers one question,
	// so it answers at one level: the payload. Serving both levels under one
	// field meant a consumer validating envelope.data passed for a declared
	// command and rejected every document from a fallback one, or the reverse.
	described.Described = true
	described.Schema = dataSchemaOf(schema)

	return described
}

// dataSchemaOf unwraps an envelope schema to the part that describes the
// payload.
//
// The published files also carry an $id pointing into a schema directory this
// project no longer publishes, so it is dropped with the rest of the envelope
// rather than handed to a validator that would try to resolve it.
func dataSchemaOf(schema map[string]any) map[string]any {
	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return schema
	}
	data, ok := properties["data"].(map[string]any)
	if !ok {
		return schema
	}

	return data
}

// commandPathWithoutRoot renders the command path the way the rest of the
// project names commands: space separated, without the binary name.
func commandPathWithoutRoot(command *cobra.Command) string {
	return strings.TrimSpace(strings.TrimPrefix(command.CommandPath(), command.Root().Name()))
}

// relaxRequiredFlags clears the required annotation on a command's flags.
//
// Only ever called when --describe is set, and only on the command being
// described, so it cannot affect a real invocation: the tree is rebuilt for
// every process.
func relaxRequiredFlags(cmd *cobra.Command) {
	clear := func(flag *pflag.Flag) {
		if flag.Annotations == nil {
			return
		}
		delete(flag.Annotations, cobra.BashCompOneRequiredFlag)
	}

	cmd.Flags().VisitAll(clear)
	cmd.PersistentFlags().VisitAll(clear)
}
