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
		return apperrors.New(
			apperrors.KindValidation,
			fmt.Sprintf("%s only applies when the target is named explicitly; pass %s to confirm", request.Flag, request.Resource),
			nil,
		)
	}
	if request.Yes {
		return nil
	}

	if err := gate(request, "delete "+request.Resource); err != nil {
		return err
	}

	return confirmDeletion(request.In, request.Out, request.Resource)
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
func confirmDeletion(in io.Reader, out io.Writer, resource string) error {
	// A caller that leaves this blank turns the confirmation into a bare return.
	if strings.TrimSpace(resource) == "" {
		return apperrors.New(apperrors.KindInternal, "refusing to confirm a deletion with no named target", nil)
	}

	fmt.Fprintf(out, "Type %q to confirm deletion: ", resource)

	reader := bufio.NewReader(in)
	line, err := reader.ReadString('\n')
	if err != nil && !(err == io.EOF && line != "") {
		return apperrors.New(apperrors.KindValidation, "could not read the confirmation", err)
	}

	if strings.TrimSpace(line) != resource {
		return apperrors.New(
			apperrors.KindValidation,
			fmt.Sprintf("confirmation did not match %q; nothing was deleted", resource),
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
}

// FillMissing asks for each absent value, or refuses naming every flag at once.
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
		flags := make([]string, 0, len(absent))
		for _, item := range absent {
			flags = append(flags, item.Flag)
		}
		return apperrors.New(
			apperrors.KindValidation,
			fmt.Sprintf("required flag(s) %s not set (%s, so there is nobody to ask)", strings.Join(flags, ", "), decision.Reason),
			nil,
		)
	}

	reader := bufio.NewReader(request.In)
	for _, item := range absent {
		fmt.Fprintf(request.Out, "%s: ", item.Question)

		line, err := reader.ReadString('\n')
		if err != nil && !(err == io.EOF && line != "") {
			return apperrors.New(apperrors.KindValidation, "could not read "+item.Flag, err)
		}

		answer := strings.TrimSpace(line)
		if answer == "" {
			// An empty answer is not a value. Substituting a default here is
			// the "refusing to ask is not permission to guess" failure with an
			// extra step.
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
