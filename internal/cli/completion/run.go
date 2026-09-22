package completion

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// maxCandidates is how many suggestions a slot may return.
//
// An instance with ten thousand repositories can answer a prefix query with
// hundreds, and a shell that prints hundreds has told the user nothing they
// could not have got by typing another letter. The cap is applied after the
// prefix filter so the first letters still narrow.
const maxCandidates = 100

// maxDescription is where a description is cut. Longer ones push the value
// itself off a narrow terminal, and a pull request title is the usual offender.
const maxDescription = 72

// slot is one place a value can be typed, resolved to what it accepts.
type slot struct {
	kind Kind
	// flagName is empty for a positional argument.
	flagName string
	// position is the index of the positional, -1 for a flag.
	position int
	// multi marks a flag that takes a comma-separated list, where the value
	// being completed is the element after the last comma.
	multi bool
}

// answer runs the source for a slot and turns the result into the shell
// protocol.
//
// Every failure ends the same way -- no candidates, and no file names either,
// because falling back to the working directory for a slot that wanted a
// branch is worse than offering nothing. Where the reason is something the
// user can act on, it goes out as Active Help rather than as a candidate; bash
// and zsh print it under the prompt, and the other shells ask Cobra to leave
// it out.
func answer(
	ctx context.Context,
	dependencies Dependencies,
	target slot,
	command *cobra.Command,
	bindings map[Kind]string,
	args []string,
	toComplete string,
) ([]cobra.Completion, cobra.ShellCompDirective) {
	source, known := sources[target.kind]
	if !known || target.kind == KindFree {
		debugf("kind %q has no source", target.kind)

		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	chosen, prefix, word := splitList(toComplete, target.multi)

	environment := &Environment{
		dependencies: dependencies,
		command:      command,
		bindings:     bindings,
	}

	request := Request{
		Command:    command,
		Args:       args,
		ToComplete: word,
		Flag:       target.flagName,
		Position:   target.position,
	}

	result, err := runWithin(ctx, source, environment, request)
	if err != nil {
		debugf("%s: %v", target.kind, err)

		return activeHelpFor(err), cobra.ShellCompDirectiveNoFileComp
	}

	directive := cobra.ShellCompDirectiveNoFileComp
	if result.NoSpace || (target.multi && len(result.Candidates) > 0) {
		directive |= cobra.ShellCompDirectiveNoSpace
	}
	if result.KeepOrder {
		directive |= cobra.ShellCompDirectiveKeepOrder
	}

	return format(result.Candidates, word, prefix, chosen), directive
}

// runWithin bounds the whole answer, not just its requests.
//
// A source reads configuration before it reads anything else, and that reads
// the keyring, which takes no context and can block on a locked collection. So
// the deadline is enforced here, by not waiting for the goroutine rather than
// by asking it to stop: the process ends as soon as the answer is printed, and
// what it was doing ends with it.
func runWithin(ctx context.Context, source Source, environment *Environment, request Request) (Result, error) {
	bounded, cancel := context.WithTimeout(ctx, deadline())
	defer cancel()

	type outcome struct {
		result Result
		err    error
	}

	answered := make(chan outcome, 1)

	go func() {
		defer func() {
			// A source that panics would otherwise take the process down with
			// a stack trace the shell has nowhere to put.
			if recovered := recover(); recovered != nil {
				answered <- outcome{err: apperrors.New(apperrors.KindInternal, "completion source failed", nil)}
			}
		}()

		result, err := source(bounded, environment, request)
		answered <- outcome{result: result, err: err}
	}()

	select {
	case <-bounded.Done():
		return Result{}, bounded.Err()
	case done := <-answered:
		return done.result, done.err
	}
}

// format applies what every source would otherwise each have to remember.
//
// The prefix filter is here because Bitbucket's own filters match anywhere in
// a value rather than at the start, and a shell only offers what starts with
// what was typed -- so a substring match the server returned is a candidate
// the user will never see. Matching case-insensitively keeps PROJ reachable
// from proj in the shells that can show it.
func format(candidates []Candidate, word, prefix string, chosen []string) []cobra.Completion {
	seen := make(map[string]bool, len(candidates))
	for _, value := range chosen {
		seen[value] = true
	}

	lowered := strings.ToLower(word)
	formatted := make([]cobra.Completion, 0, len(candidates))

	for _, candidate := range candidates {
		value := strings.TrimSpace(candidate.Value)
		if value == "" || strings.ContainsAny(value, "\t\n\r") {
			continue
		}
		if !strings.HasPrefix(strings.ToLower(value), lowered) {
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true

		entry := prefix + value
		if description := describe(candidate.Description); description != "" {
			entry += "\t" + description
		}

		formatted = append(formatted, entry)

		if len(formatted) == maxCandidates {
			break
		}
	}

	return formatted
}

func describe(description string) string {
	cleaned := strings.Map(func(character rune) rune {
		if character == '\t' || character == '\n' || character == '\r' {
			return ' '
		}

		return character
	}, strings.TrimSpace(description))

	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if len(cleaned) > maxDescription {
		cleaned = strings.TrimSpace(cleaned[:maxDescription-1]) + "…"
	}

	return cleaned
}

// splitList separates a comma-separated flag's finished elements from the one
// being typed.
//
// --reviewers alice,bo<tab> asks about "bo". The finished elements are not
// offered again, and the completion has to carry them back: a shell replaces
// the whole word, so returning "bob" would leave the line reading --reviewers
// bob.
func splitList(toComplete string, multi bool) (chosen []string, prefix, word string) {
	if !multi {
		return nil, "", toComplete
	}

	cut := strings.LastIndex(toComplete, ",")
	if cut < 0 {
		return nil, "", toComplete
	}

	prefix = toComplete[:cut+1]
	word = toComplete[cut+1:]

	for _, value := range strings.Split(toComplete[:cut], ",") {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			chosen = append(chosen, trimmed)
		}
	}

	return chosen, prefix, word
}

// debug says why a completion came back empty, when asked to.
//
// Silence is right for a shell and wrong for whoever is working on a source:
// every generated script sends stderr to /dev/null, so a failure looks
// identical to a kind with nothing to offer and to a slot that was never
// declared. With BB_COMPLETION_DEBUG set, running `bb __complete ...` by hand
// prints the reason; inside a shell it stays hidden either way. Cobra's own
// scripts take the same approach with BASH_COMP_DEBUG_FILE.
func debugf(format string, args ...any) {
	if os.Getenv("BB_COMPLETION_DEBUG") == "" {
		return
	}

	fmt.Fprintf(os.Stderr, "completion: "+format+"\n", args...)
}

// activeHelpFor explains a silence worth explaining.
//
// Not every failure is: a timeout or an unreachable server says nothing the
// user can act on mid-word, and a line about it under every prompt would be
// noise. A missing credential or a repository nothing could resolve is
// different -- it will not fix itself, and it is the reason completion looks
// broken.
func activeHelpFor(err error) []cobra.Completion {
	switch apperrors.KindOf(err) {
	case apperrors.KindValidation, apperrors.KindAuthentication, apperrors.KindAuthorization:
		return cobra.AppendActiveHelp(nil, err.Error())
	default:
		return nil
	}
}
