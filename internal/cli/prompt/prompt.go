// Package prompt asks a person a question, when there is one to ask.
//
// It is the only place bb prompts. Deciding whether to ask belongs to
// internal/cli/interactive (ADR-072); deciding what happens when the answer
// cannot be had belongs here, because that is the half ADR-054 left unsaid and
// the half that makes prompting safe: a refused prompt names the flag that
// would have supplied the value, and never substitutes a default.
//
// See ADR-073.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/interactive"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// noInputFlag is the per-invocation refusal, registered on the root command.
const noInputFlag = "no-input"

// Request is one destructive command's confirmation, in the terms ADR-073
// states it.
type Request struct {
	In  io.Reader
	Out io.Writer

	// Notice is where FillMissing says which values it inferred when nobody
	// was there to be asked: the error stream, so a command's output stays
	// what the command wrote. Nil says nothing.
	Notice io.Writer

	// Disabled is the --no-input flag: an explicit per-invocation refusal.
	Disabled bool

	// Yes is the --yes flag. It is honoured only when TargetExplicit is true.
	Yes bool

	// TargetExplicit reports whether the caller named the target rather than
	// letting it be inferred. A safety flag that works on an inferred target is
	// not a safety flag: bb repo delete deleted the repository you were
	// standing in, with no arguments at all.
	TargetExplicit bool

	// Resource is what will be destroyed, as the person must type it back.
	Resource string

	// Flag is the escape hatch to name when there is nobody to ask.
	Flag string

	// Verb is what the command does to the resource: delete, remove, revoke or
	// clear. Empty means delete, which is what every caller meant before the
	// other three were guarded and told a person revoking a token that they
	// were about to delete one.
	Verb string

	// MachineOutput suppresses prompting the way --json does.
	MachineOutput bool

	// Lookup is injected by tests; nil means the real environment.
	Lookup func(string) (string, bool)
}

// ConfirmDestructive returns nil when the command may proceed.
//
// The three outcomes are the whole of ADR-073's destructive rule:
//   - the target was named and --yes was given: proceed without asking.
//   - a person is present: ask them to type the resource name.
//   - nobody is present: refuse, and say which flag was missing.
//
// A caller that ignores the error proceeds with a deletion nobody confirmed,
// so the error is the only return: there is no boolean to misread.
func ConfirmDestructive(request Request) error {
	if request.Yes && !request.TargetExplicit {
		// gh reaches the same answer for the same reason: --yes on an inferred
		// target is the accident it was meant to prevent.
		//
		// The remedy is what to do, not what was going to happen. It named the
		// resource -- "pass PROJ/repo branch feature to confirm" -- which is
		// not something that can be passed to anything, and a pipeline running
		// inside a checkout is exactly where this is read.
		return apperrors.New(
			apperrors.KindValidation,
			fmt.Sprintf(
				"%s only applies when the target is named explicitly, and bb took the repository from the git remote; name it with --repo PROJECT/slug, or set BITBUCKET_PROJECT_KEY and BITBUCKET_REPO_SLUG",
				request.Flag,
			),
			nil,
		)
	}
	if request.Yes {
		return nil
	}

	if err := gate(request, request.verb()+" "+request.Resource); err != nil {
		return err
	}

	return confirmDeletion(request.In, request.Out, request.Resource, request.destruction())
}

// verb is what the command does, for the question and the refusal to say.
func (request Request) verb() string {
	if strings.TrimSpace(request.Verb) == "" {
		return "delete"
	}

	return strings.TrimSpace(request.Verb)
}

// destruction is the verb in the two other forms the question needs.
type destruction struct {
	// noun completes "Type X to confirm ...".
	noun string
	// past completes "nothing was ...".
	past string
}

// destructions are the four verbs ADR-073 guards.
//
// A verb that is not one of them falls back to wording that needs no noun at
// all, so a fifth destructive command reads correctly on the day it is added
// rather than on the day somebody remembers this map.
var destructions = map[string]destruction{
	"delete": {noun: "deletion", past: "deleted"},
	"remove": {noun: "removal", past: "removed"},
	"revoke": {noun: "revocation", past: "revoked"},
	"clear":  {noun: "clearing", past: "cleared"},
}

func (request Request) destruction() destruction {
	if words, known := destructions[request.verb()]; known {
		return words
	}

	return destruction{past: "changed"}
}

// ConfirmAction is ConfirmDestructive for something that has no single target
// to type back: clearing every key, disabling a whole feature.
//
// It asks a yes-or-no question, which is weaker on purpose. The typed-name form
// exists because a stray keystroke should not destroy a named resource; where
// there is no name to type, the flag and the refusal carry the safety instead.
func ConfirmAction(request Request, action string) error {
	if err := gate(request, action); err != nil {
		return err
	}
	if request.Yes {
		return nil
	}

	return confirmYesNo(request.In, request.Out, action)
}

// confirmYesNo asks a yes-or-no question and defaults to no.
//
// Split from ConfirmAction so it can be tested directly. Under `go test` no
// stream is a terminal, so a test going through ConfirmAction never reaches
// this and would assert nothing while appearing to cover it.
func confirmYesNo(in io.Reader, out io.Writer, action string) error {
	fmt.Fprintf(out, "%s? (y/N): ", action)

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !(err == io.EOF && line != "") {
		return apperrors.New(apperrors.KindValidation, "could not read the confirmation", err)
	}

	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return nil
	}
	return apperrors.New(apperrors.KindValidation, "cancelled", nil)
}

// decide is a seam. Under `go test` no stream is a terminal, so every path past
// the gate is unreachable without one, and the prompting half of this package
// would be untestable -- which is also how it would rot.
var decide = interactive.Detect

// gate is the shared half: when nobody can answer, say which flag was missing.
//
// Refusing to prompt is not permission to proceed, and it is not permission to
// stay quiet either. ADR-054 said only the first half, which is how a command
// ended up reading stdin with no guard at all.
func gate(request Request, action string) error {
	if request.Yes {
		return nil
	}

	decision := decide(interactive.Options{
		Stdin:         request.In,
		Stdout:        request.Out,
		Disabled:      request.Disabled,
		MachineOutput: request.MachineOutput,
		Lookup:        request.Lookup,
	})
	if decision.Allowed {
		return nil
	}

	return apperrors.New(
		apperrors.KindValidation,
		fmt.Sprintf("%s is required to %s (%s, so there is nobody to confirm)", request.Flag, action, decision.Reason),
		nil,
	)
}

// confirmDeletion asks the person to type what will be destroyed.
//
// A keystroke is the wrong unit for something irreversible: y is one character
// away from every other answer, and a person who has already typed the wrong
// command will type y to it. Naming the resource makes the confirmation carry
// the same information as the command.
func confirmDeletion(in io.Reader, out io.Writer, resource string, words destruction) error {
	// A caller that leaves this blank turns the confirmation into a bare return.
	if strings.TrimSpace(resource) == "" {
		return apperrors.New(apperrors.KindInternal, "refusing to confirm a deletion with no named target", nil)
	}

	// In the command's own word: somebody revoking a token was asked to confirm
	// a deletion and told afterwards that nothing was deleted, which describes
	// neither what they typed nor what would have happened.
	if words.noun == "" {
		fmt.Fprintf(out, "Type %q to confirm: ", resource)
	} else {
		fmt.Fprintf(out, "Type %q to confirm %s: ", resource, words.noun)
	}

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !(err == io.EOF && line != "") {
		return apperrors.New(apperrors.KindValidation, "could not read the confirmation", err)
	}

	if strings.TrimSpace(line) != resource {
		return apperrors.New(
			apperrors.KindValidation,
			fmt.Sprintf("confirmation did not match %q; nothing was %s", resource, words.past),
			nil,
		)
	}
	return nil
}

// RequestFor builds a Request from the command.
//
// It reads the persistent --no-input flag, so a call site cannot forget it.
// That flag was declared in ADR-072 and documented on this struct before it was
// registered anywhere, which meant a refusal could name a flag nobody could
// pass. Reading it here rather than threading it through every Dependencies
// struct keeps the one place that must not be forgotten down to one place.
func RequestFor(cmd *cobra.Command, machineOutput bool) Request {
	// A missing flag is not an error worth surfacing: GetBool reports false for
	// a command that somehow lacks it, which is the same as not passing it.
	disabled, _ := cmd.Flags().GetBool(noInputFlag)

	return Request{
		In:            cmd.InOrStdin(),
		Out:           cmd.OutOrStdout(),
		Notice:        cmd.ErrOrStderr(),
		Disabled:      disabled,
		MachineOutput: machineOutput,
	}
}

// Missing is one value a command needs and does not have.
type Missing struct {
	// Flag is what would have supplied it, named in the refusal.
	Flag string
	// Question is what to ask a person who is there.
	Question string
	// Value receives the answer.
	Value *string
	// Infer works out the value from where the command is run, when the flag
	// was not passed. It is called after the values before it are settled, so
	// one inference can rest on another. Nil infers nothing.
	Infer func() Inferred
}

// Inferred is a value worked out rather than given.
type Inferred struct {
	Value string
	// Source says where it came from, as "the checked-out branch".
	Source string
	// Unattended is whether the value may stand in for the flag when nobody is
	// there to see it. A person is always shown an inferred value and can
	// change it; without one it is used only where the command says the
	// inference leaves nothing to choose.
	Unattended bool
}

func (missing Missing) infer() Inferred {
	if missing.Infer == nil {
		return Inferred{}
	}

	inferred := missing.Infer()
	inferred.Value = strings.TrimSpace(inferred.Value)

	return inferred
}

// FillMissing settles each absent value: by asking, with an inferred value
// offered as the answer, or without a person by taking the inferences that may
// stand in unattended and refusing the rest, naming every flag at once.
//
// Naming them all matters: a caller told about --title, corrected, and then
// told about --to-ref has spent two round trips learning what one message
// could have said. gh names the whole set for the same reason.
func FillMissing(request Request, missing []Missing) error {
	absent := []Missing{}
	for _, item := range missing {
		if strings.TrimSpace(*item.Value) == "" {
			absent = append(absent, item)
		}
	}
	if len(absent) == 0 {
		return nil
	}

	decision := decide(interactive.Options{
		Stdin:         request.In,
		Stdout:        request.Out,
		Disabled:      request.Disabled,
		MachineOutput: request.MachineOutput,
		Lookup:        request.Lookup,
	})
	if !decision.Allowed {
		return fillUnattended(request, absent, decision.Reason)
	}

	reader := bufio.NewReader(request.In)
	for _, item := range absent {
		inferred := item.infer()
		if inferred.Value == "" {
			fmt.Fprintf(request.Out, "%s: ", item.Question)
		} else {
			fmt.Fprintf(request.Out, "%s [%s]: ", item.Question, inferred.Value)
		}

		line, err := reader.ReadString('\n')
		if err != nil && !(err == io.EOF && line != "") {
			return apperrors.New(apperrors.KindValidation, "could not read "+item.Flag, err)
		}

		answer := strings.TrimSpace(line)
		if answer == "" {
			// Accepting what was shown is an answer. With nothing shown, an
			// empty one is not a value, and substituting a default here is the
			// "refusing to ask is not permission to guess" failure with an
			// extra step.
			answer = inferred.Value
		}
		if answer == "" {
			return apperrors.New(
				apperrors.KindValidation,
				fmt.Sprintf("%s cannot be empty; pass %s or answer the question", item.Question, item.Flag),
				nil,
			)
		}
		*item.Value = answer
	}
	return nil
}

// fillUnattended settles what it can with nobody there. An inference that may
// stand in unattended does, and is reported on Notice so that a person reading
// the run afterwards can see what was decided for them; every value left
// without one is named in the refusal.
func fillUnattended(request Request, absent []Missing, reason string) error {
	unfilled := []string{}
	for _, item := range absent {
		inferred := item.infer()
		if inferred.Value == "" || !inferred.Unattended {
			unfilled = append(unfilled, item.Flag)
			continue
		}

		*item.Value = inferred.Value
		if request.Notice != nil && !request.MachineOutput {
			fmt.Fprintf(request.Notice, "Using %s %s (%s)\n", item.Flag, inferred.Value, inferred.Source)
		}
	}
	if len(unfilled) == 0 {
		return nil
	}

	return apperrors.New(
		apperrors.KindValidation,
		fmt.Sprintf("required flag(s) %s not set (%s, so there is nobody to ask)", strings.Join(unfilled, ", "), reason),
		nil,
	)
}

// ConfirmDeleteOf is ADR-073 for a command whose target is one named resource.
//
// The rule was implemented once, correctly, on repo delete, and stayed there:
// thirty-four other destructive commands had no confirmation and no --yes at
// all. Copying seven lines to thirty-four call sites is how the copies drift,
// so the seven lines live here and the call sites pass what differs.
func ConfirmDeleteOf(cmd *cobra.Command, machineOutput, yes, targetExplicit bool, resource string) error {
	request := RequestFor(cmd, machineOutput)
	request.Yes = yes
	request.TargetExplicit = targetExplicit
	request.Resource = resource
	request.Flag = "--yes"
	if cmd != nil {
		request.Verb = cmd.Name()
	}

	return ConfirmDestructive(request)
}

// TargetNamed reports whether the caller named the repository, rather than
// having it filled in from the git remote.
//
// Changed alone is not "the caller named it": inference sets --repo and marks
// it Changed so every command can resolve a target, which silently made an
// inferred repository count as explicit and let --yes apply to the one you
// happened to be standing in (#472). A branch named on the command line does
// not rescue that -- `bb branch delete main` names the branch and infers the
// repository, which is how a probe deleted main.
func TargetNamed(cmd *cobra.Command, repositoryWasInferred func() bool) bool {
	if cmd == nil || !cmd.Flags().Changed("repo") {
		return false
	}

	return repositoryWasInferred == nil || !repositoryWasInferred()
}
