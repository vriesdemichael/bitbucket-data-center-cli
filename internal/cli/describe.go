package cli

import (
	"sort"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outputschemas"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// describeFlag is the persistent flag that makes a command say what it returns
// instead of running.
const describeFlag = "describe"

// What --dry-run does for a command, as the description and the catalogue name
// it.
const (
	// dryRunRuns is a command that only reads: it runs, and its data is in the
	// preview.
	dryRunRuns = "runs"
	// dryRunVerifies is a command whose preview checks something: Bitbucket's
	// answer, or the permission and state the change depends on.
	dryRunVerifies = "verifies"
	// dryRunPredicts is a command whose preview checks nothing first.
	dryRunPredicts = "predicts"
)

// dryRunBehaviourOverrides are commands whose --dry-run is not what their
// classification alone says.
var dryRunBehaviourOverrides = map[string]DryRunBehaviour{
	// bb update honours --dry-run itself: it checks the release it would
	// install -- signature, checksum -- and installs nothing.
	"update": {Behaviour: dryRunVerifies, Tier: jsonoutput.TierServerValidated},
	// bb api runs a GET or HEAD, which only reads, and answers in
	// preview.data; any other request is shown as what it would send.
	"api": {Behaviour: dryRunRuns, Tier: jsonoutput.TierServerValidated},
}

// Description is what --describe answers (ADR-097): what a command returns,
// for a run and for a dry run, or a group's catalogue.
type Description struct {
	Run      *RunOutput                `json:"run,omitempty"`
	DryRun   *DryRunOutput             `json:"dryRun,omitempty"`
	Commands map[string]CatalogueEntry `json:"commands,omitempty"`
}

// RunOutput is the document a run writes.
type RunOutput struct {
	// OutputSchema is the JSON Schema of the whole document: data and meta, or
	// error and meta.
	OutputSchema *jsonschema.Schema `json:"outputSchema,omitempty"`
	// Reason says why a command has no schema, or why its data promises no
	// shape.
	Reason string `json:"reason,omitempty"`
}

// DryRunBehaviour is what --dry-run does for a command.
type DryRunBehaviour struct {
	Behaviour string          `json:"behaviour"`
	Tier      jsonoutput.Tier `json:"tier"`
}

// DryRunOutput is what --dry-run does for a command, and the document it
// writes.
type DryRunOutput struct {
	DryRunBehaviour
	OutputSchema *jsonschema.Schema `json:"outputSchema,omitempty"`
}

// CatalogueEntry is one command in a group's catalogue. DryRun is absent for a
// command that does not take the flag.
type CatalogueEntry struct {
	DryRun *DryRunBehaviour `json:"dryRun,omitempty"`
}

// installDescribe makes every command answer --describe.
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
	// prints its help. The help function is where Cobra sends it, so it is
	// where the group's catalogue goes -- unless the group was handed a
	// subcommand it does not have, which is no group to describe but a path
	// naming no command, and main reports it as the error it is.
	help := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if *describe && !cmd.Runnable() {
			if unconsumed := cmd.Flags().Args(); len(unconsumed) > 0 && !helpWasAskedFor(cmd, unconsumed) {
				return
			}
			// Nothing to return but the catalogue, and an error here would only
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

// writeDescription answers --describe for one command or group: the document
// under --json or --yaml, and an outline for a person otherwise.
func writeDescription(cmd *cobra.Command) error {
	description := catalogueOf(cmd)
	if cmd.Runnable() {
		description = DescribeCommand(commandPathWithoutRoot(cmd))
	}

	// A group reaches here through the help function, before PersistentPreRunE
	// has bound the output settings, so they are read from the flags.
	settings := OutputSettingsFromFlags(cmd.Root(), cmd)
	if settings.Machine {
		return jsonoutput.WriteDescription(jsonoutput.Bind(cmd.OutOrStdout(), settings), description)
	}

	return writeDescriptionText(cmd.OutOrStdout(), cmd, description)
}

// DescribeCommand is what --describe answers for a command path.
func DescribeCommand(path string) Description {
	data, reason, written := dataContract(path)

	description := Description{Run: &RunOutput{Reason: reason}}
	if written {
		description.Run.OutputSchema = runDocumentSchema(data)
	}

	if behaviour, ok := dryRunBehaviourOf(path); ok {
		description.DryRun = &DryRunOutput{
			DryRunBehaviour: behaviour,
			OutputSchema:    dryRunDocumentSchema(behaviour.Behaviour, data),
		}
	}

	return description
}

// DataSchema is the schema a command declares for its data, or false with the
// reason it declares none.
//
// Exported for tools/docs-lint, which checks a documented output example
// against the schema the command declares rather than against other
// documentation, so an example and --describe cannot disagree about what a
// command emits.
func DataSchema(path string) (*jsonschema.Schema, string, bool) {
	data, reason, written := dataContract(path)
	if !written || reason != "" {
		return nil, reason, false
	}

	return data, "", true
}

// dataContract looks up the data a command declares.
//
// Three answers, and which one a caller gets is itself information: a schema
// derived from the result type the command fills in; a statement that the
// command writes no document of its own; or a document whose data has no shape
// bb can promise, described with its data left open and the reason beside it.
// An empty schema is never offered as a declaration on its own -- it would look
// like a guarantee of nothing rather than an absence of one.
func dataContract(path string) (data *jsonschema.Schema, reason string, written bool) {
	if why := outputschemas.CommandsWithoutDataContract[path]; why != "" {
		return &jsonschema.Schema{}, "this command writes no document of its own: " + why, false
	}

	if why := outputschemas.CommandsWithoutDeclarableShape[path]; why != "" {
		return &jsonschema.Schema{}, "its data has no shape bb can promise: " + why, true
	}

	// A schema derived from the command's own result type. It cannot drift from
	// what the command emits, because it is the same declaration (#521).
	if schema, ok := result.SchemaFor(path); ok {
		return schema, "", true
	}

	return &jsonschema.Schema{}, "no output schema is published for this command yet; the payload shape is not guaranteed", false
}

// dryRunBehaviourOf is what --dry-run does for a command, from its
// classification and its declared tier, or false for one that does not take
// the flag.
func dryRunBehaviourOf(path string) (DryRunBehaviour, bool) {
	if behaviour, ok := dryRunBehaviourOverrides[path]; ok {
		return behaviour, true
	}

	switch classifyCommand(path) {
	case classificationMutating:
		tier, _ := DeclaredDryRunTier(path)
		if dryRunProfiles[path].Stateful {
			return DryRunBehaviour{Behaviour: dryRunVerifies, Tier: tier}, true
		}
		return DryRunBehaviour{Behaviour: dryRunPredicts, Tier: tier}, true
	case classificationLocalMutating:
		return DryRunBehaviour{Behaviour: dryRunPredicts, Tier: jsonoutput.TierPredicted}, true
	case classificationReadOnly, classificationLocal:
		return DryRunBehaviour{Behaviour: dryRunRuns, Tier: jsonoutput.TierServerValidated}, true
	default:
		return DryRunBehaviour{}, false
	}
}

// catalogueOf lists every command beneath a group, or beneath bb itself, with
// what --dry-run does for each, so one call covers the whole tool (ADR-097).
func catalogueOf(group *cobra.Command) Description {
	commands := map[string]CatalogueEntry{}

	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		for _, child := range cmd.Commands() {
			walk(child)
		}
		if !cmd.Runnable() || cmd.Hidden {
			return
		}

		path := commandPathWithoutRoot(cmd)
		entry := CatalogueEntry{}
		if behaviour, ok := dryRunBehaviourOf(path); ok {
			entry.DryRun = &behaviour
		}
		commands[path] = entry
	}
	walk(group)

	return Description{Commands: commands}
}

// sortedCommands is a catalogue's command paths in order.
func sortedCommands(commands map[string]CatalogueEntry) []string {
	paths := make([]string, 0, len(commands))
	for path := range commands {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	return paths
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
