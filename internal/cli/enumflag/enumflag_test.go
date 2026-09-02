package enumflag_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
)

func newFlagSet(target *string, allowed []string) *pflag.FlagSet {
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	flags.SetOutput(io.Discard)
	enumflag.Register(flags, target, "severity", "MEDIUM", allowed, "Annotation severity.")
	return flags
}

var severities = []string{"LOW", "MEDIUM", "HIGH"}

func TestADocumentedValueIsAccepted(t *testing.T) {
	t.Parallel()

	var severity string
	flags := newFlagSet(&severity, severities)
	if err := flags.Parse([]string{"--severity", "HIGH"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if severity != "HIGH" {
		t.Errorf("severity = %q", severity)
	}
}

// TestAnUndocumentedValueIsRefusedBeforeTheRequest is the defect: eight flags
// enumerated a set in their help and forwarded anything to the server.
func TestAnUndocumentedValueIsRefusedBeforeTheRequest(t *testing.T) {
	t.Parallel()

	var severity string
	flags := newFlagSet(&severity, severities)
	err := flags.Parse([]string{"--severity", "BOGUS"})
	if err == nil {
		t.Fatal("an undocumented value was accepted")
	}
	// ADR-054: the message has to name the values, or the caller has to go
	// and find them.
	for _, want := range []string{"LOW", "MEDIUM", "HIGH", "severity"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not name %q: %v", want, err)
		}
	}
	// pflag reports this as an InvalidValueError, which is what
	// cli.ClassifyUsageError matches on to give the envelope kind=validation
	// and exit 2. Asserting the type here rather than the kind keeps this
	// package free of the CLI it is used by; the end-to-end assertion lives
	// in TestClassifyUsageErrorMatchesCobrasRealMessages.
	var invalidValue *pflag.InvalidValueError
	if !errors.As(err, &invalidValue) {
		t.Errorf("error is %T, want a *pflag.InvalidValueError so it classifies as validation", err)
	}
}

// TestCaseIsNormalisedToTheCanonicalSpelling pins the decision this package
// makes once.
//
// The sets are inconsistent with each other -- LOW, MEDIUM, HIGH beside no-ff
// and squash-ff-only -- so requiring a caller to match each one's case is a
// rule they cannot follow from the help text. What reaches the server is the
// canonical spelling either way.
func TestCaseIsNormalisedToTheCanonicalSpelling(t *testing.T) {
	t.Parallel()

	for _, given := range []string{"high", "HIGH", "HiGh", "  high  "} {
		var severity string
		flags := newFlagSet(&severity, severities)
		if err := flags.Parse([]string{"--severity", given}); err != nil {
			t.Fatalf("parse(%q): %v", given, err)
		}
		if severity != "HIGH" {
			t.Errorf("parse(%q) gave %q, want the canonical HIGH", given, severity)
		}
	}

	// A lower-case set canonicalises the other way, so this is normalisation
	// rather than upper-casing.
	var strategy string
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	enumflag.Register(flags, &strategy, "strategy", "no-ff", []string{"no-ff", "squash-ff-only"}, "Merge strategy.")
	if err := flags.Parse([]string{"--strategy", "SQUASH-FF-ONLY"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if strategy != "squash-ff-only" {
		t.Errorf("strategy = %q, want the canonical squash-ff-only", strategy)
	}
}

// TestTheUsageTextComesFromTheSameSlice is the coupling that stops the two
// drifting: eight flags documented values they did not enforce because the
// help string and the validator each listed them separately.
func TestTheUsageTextComesFromTheSameSlice(t *testing.T) {
	t.Parallel()

	var severity string
	flags := newFlagSet(&severity, severities)
	usage := flags.Lookup("severity").Usage
	for _, want := range severities {
		if !strings.Contains(usage, want) {
			t.Errorf("usage %q does not name %q", usage, want)
		}
	}
	if !strings.Contains(usage, "Annotation severity") {
		t.Errorf("usage lost its description: %q", usage)
	}
}

func TestTheDefaultIsSetWithoutParsing(t *testing.T) {
	t.Parallel()

	var severity string
	newFlagSet(&severity, severities)
	if severity != "MEDIUM" {
		t.Errorf("severity = %q before parsing, want the default", severity)
	}
}
