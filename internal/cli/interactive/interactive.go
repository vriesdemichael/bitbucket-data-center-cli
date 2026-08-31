// Package interactive decides whether bb may prompt a person.
//
// The decision exists in one place because it cannot be made correctly at each
// call site. Terminal attachment is not evidence that anyone will answer: a
// prompt was measured to block under a real pseudo-terminal whose input side
// nobody was writing to, and to return an empty string instantly under a
// harness that allocates no terminal at all. isatty is therefore a necessary
// condition and not a sufficient one, which is why an escape hatch that costs
// nothing per invocation is part of the design rather than a fallback.
//
// See ADR-072 for the rules and, more importantly, for what they do and do not
// guarantee.
package interactive

import (
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// Decision is the answer, with the reason kept for the caller's error message.
//
// A refusal is worth explaining: the difference between "no terminal" and "CI
// is set" is the difference between two different fixes, and a command that
// declines to prompt should be able to say which applies.
type Decision struct {
	Allowed bool
	Reason  string
}

// Options are the inputs to the decision.
//
// Streams are typed as interfaces rather than *os.File so a test can pass a
// buffer, which is also how every non-terminal case is exercised.
type Options struct {
	Stdin  io.Reader
	Stdout io.Writer

	// Disabled is the --no-input flag: an explicit per-invocation refusal.
	Disabled bool

	// MachineOutput is --json. A machine reading structured output is not a
	// person who can answer a question.
	MachineOutput bool

	// Lookup defaults to os.LookupEnv. Injected so tests do not need t.Setenv,
	// which cannot be combined with t.Parallel.
	Lookup func(string) (string, bool)
}

// disableVariable turns prompting off for every command in the process.
const disableVariable = "BB_NO_PROMPT"

// extensionVariable names further variables that mean the same thing.
//
// This is the part that ages well. Harnesses appear faster than this project
// releases, and each one announces itself differently or not at all, so the
// built-in list below is a convenience rather than a contract. An operator who
// meets a harness bb has never heard of sets BB_NO_PROMPT_VARS to the variable
// that harness does set, once, in the environment -- rather than waiting for a
// release, or paying a flag on every call.
const extensionVariable = "BB_NO_PROMPT_VARS"

// knownNonInteractive are variables whose presence means no one is watching.
//
// The first four are conventions with real reach: CI is set by every hosted
// runner worth naming, DEBIAN_FRONTEND is Debian packaging, NONINTERACTIVE is
// Homebrew, and TERM=dumb is how a terminal says it cannot do better. The rest
// are coding harnesses, which is the population this project actually serves
// (ADR-003).
//
// TERM is handled separately below: only the value "dumb" means anything.
// A set TERM proves nothing -- it was measured as xterm-256color in a harness
// with no terminal on any descriptor.
//
// NO_COLOR is deliberately absent. It is a rendering preference, not a
// statement about who is present, and internal/cli/style already reads it for
// the thing it actually means.
var knownNonInteractive = []string{
	"CI",
	"DEBIAN_FRONTEND",
	"NONINTERACTIVE",

	// Coding harnesses, best effort. Several announce nothing at all, which is
	// the reason extensionVariable exists.
	"CLAUDECODE",
	"AI_AGENT",
	"CURSOR_TRACE_ID",
	"CODEX_THREAD_ID",
	"REPLIT_ENVIRONMENT",
	"AIDER_CHAT",
}

// Detect answers whether bb may prompt.
//
// The order is precedence, and every rule can only refuse. Nothing below can
// re-enable prompting that something above turned off, so adding a rule cannot
// widen the set of situations in which bb prompts.
func Detect(options Options) Decision {
	lookup := options.Lookup
	if lookup == nil {
		lookup = os.LookupEnv
	}

	if options.Disabled {
		return Decision{Reason: "--no-input was given"}
	}
	if options.MachineOutput {
		return Decision{Reason: "machine-readable output was requested"}
	}
	if truthy(lookup, disableVariable) {
		return Decision{Reason: disableVariable + " is set"}
	}
	if name, ok := firstSet(lookup, extensionNames(lookup)); ok {
		return Decision{Reason: name + " is set, named by " + extensionVariable}
	}
	if name, ok := firstSet(lookup, knownNonInteractive); ok {
		return Decision{Reason: name + " is set"}
	}
	if value, ok := lookup("TERM"); ok && strings.EqualFold(value, "dumb") {
		return Decision{Reason: "TERM is dumb"}
	}

	// Both streams, not just stdin. A command whose output is piped is feeding
	// something that is not reading the question, even when a keyboard is
	// attached to the input side.
	if !terminalCheck(options.Stdin) {
		return Decision{Reason: "standard input is not a terminal"}
	}
	if !terminalCheck(options.Stdout) {
		return Decision{Reason: "standard output is not a terminal"}
	}

	return Decision{Allowed: true}
}

// extensionNames reads the operator's additional variable names.
func extensionNames(lookup func(string) (string, bool)) []string {
	raw, ok := lookup(extensionVariable)
	if !ok {
		return nil
	}

	names := []string{}
	for _, field := range strings.Split(raw, ",") {
		if name := strings.TrimSpace(field); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// firstSet returns the first of names that is set to a meaningful value.
func firstSet(lookup func(string) (string, bool), names []string) (string, bool) {
	for _, name := range names {
		if truthy(lookup, name) {
			return name, true
		}
	}
	return "", false
}

// truthy reports whether a variable is set to something that means yes.
//
// Empty, "0" and "false" are treated as unset. CI=false is a thing people
// write, and reading it as "this is CI" would disable prompting for exactly
// the person who took the trouble to say otherwise.
func truthy(lookup func(string) (string, bool), name string) bool {
	value, ok := lookup(name)
	if !ok {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0", "false":
		return false
	}
	return true
}

// terminalCheck is a seam. Under `go test` no stream is a terminal, so the
// stdout branch is unreachable without one; the repository uses this shape
// elsewhere for the same reason.
var terminalCheck = isTerminal

// isTerminal reports whether a stream is attached to a terminal.
//
// Anything that is not an *os.File cannot be one, which is what makes a test
// using a bytes.Buffer take the non-interactive path without ceremony.
func isTerminal(stream any) bool {
	file, ok := stream.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(file.Fd()))
}
