package cli

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// The marker enumflag.Register puts in every usage string it builds. It is the
// only way a flag can claim to be validated, which is what makes the second
// test below able to tell "documents a set and enforces it" from "documents a
// set and does not".
const enumFlagMarker = "(one of: "

// advertisedValues pulls the set out of an enumflag usage string.
func advertisedValues(usage string) []string {
	start := strings.Index(usage, enumFlagMarker)
	if start < 0 {
		return nil
	}

	tail := usage[start+len(enumFlagMarker):]
	end := strings.LastIndex(tail, ")")
	if end < 0 {
		return nil
	}

	values := strings.Split(tail[:end], ", ")
	for index := range values {
		values[index] = strings.TrimSpace(values[index])
	}

	return values
}

// enumerationInProse matches a value set written out by hand: a colon or an
// opening bracket, then either two alternatives joined by "or" or three or more
// separated by commas, every one of them a single bare token.
//
// Deliberately conservative. A governance test that cries wolf gets deleted, so
// this would rather miss an unusually worded flag than fail on "Commit ID or
// ref" or "Query filter (checks username, name, or email)" -- both of which
// have a space inside an alternative and so do not match.
var enumerationInProse = regexp.MustCompile(
	`[:(]\s*(?:` +
		`[A-Za-z0-9][A-Za-z0-9._-]*\s+or\s+[A-Za-z0-9][A-Za-z0-9._-]*` +
		`|` +
		`[A-Za-z0-9][A-Za-z0-9._-]*(?:,\s*(?:or\s+)?[A-Za-z0-9][A-Za-z0-9._-]*){2,}` +
		`)\s*[).]?\s*$`)

// visitFlags walks every non-hidden command's own and persistent flags once.
func visitFlags(t *testing.T, visit func(cmd *cobra.Command, flag *pflag.Flag)) {
	t.Helper()

	root := NewRootCommand()

	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		if cmd.Hidden || cmd.Name() == "help" || cmd.Name() == "completion" {
			return
		}

		seen := map[string]bool{}
		for _, flags := range []*pflag.FlagSet{cmd.Flags(), cmd.PersistentFlags()} {
			flags.VisitAll(func(flag *pflag.Flag) {
				if seen[flag.Name] {
					return
				}
				seen[flag.Name] = true
				visit(cmd, flag)
			})
		}

		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(root)
}

// TestEveryAdvertisedEnumIsEnforced is the first half of #482's test for this
// pattern: a flag that names its values must refuse everything else, before the
// request rather than after it (ADR-054).
func TestEveryAdvertisedEnumIsEnforced(t *testing.T) {
	checked := 0

	visitFlags(t, func(cmd *cobra.Command, flag *pflag.Flag) {
		values := advertisedValues(flag.Usage)
		if len(values) == 0 {
			return
		}
		checked++

		for _, value := range values {
			if err := flag.Value.Set(value); err != nil {
				t.Errorf("%s --%s advertises %q and rejects it: %v", cmd.CommandPath(), flag.Name, value, err)
			}
		}

		// A value that is not in any set anywhere, so this cannot pass by
		// accident on a flag whose set happens to contain the probe.
		const outside = "bb-governance-not-a-real-value"
		if err := flag.Value.Set(outside); err == nil {
			t.Errorf("%s --%s advertises %v but accepted %q", cmd.CommandPath(), flag.Name, values, outside)
		} else {
			for _, value := range values {
				if !strings.Contains(err.Error(), value) {
					t.Errorf("%s --%s rejected %q without naming %q, which ADR-054 requires: %v",
						cmd.CommandPath(), flag.Name, outside, value, err)
				}
			}
		}
	})

	// Guards the marker: if enumflag stops writing "(one of: " the loop above
	// would silently check nothing and pass.
	if checked == 0 {
		t.Fatal("no enum flags were found -- has enumflag.Register stopped writing its marker?")
	}
	t.Logf("%d enum flags checked", checked)
}

// TestNoFlagEnumeratesValuesWithoutEnforcingThem is the second half, and the
// one that closes #480 rather than fixing it: a new flag that writes its values
// into its own help string instead of registering them fails here.
//
// The exemptions are flags whose usage matches the shape but is not a value
// set. Each needs a reason; a new entry without one should not get through
// review.
func TestNoFlagEnumeratesValuesWithoutEnforcingThem(t *testing.T) {
	exempt := map[string]string{
		// These two carry an empty value that means "unset BB_LOG_LEVEL"
		// rather than "not given" -- the same rule every override flag on
		// the root follows -- and enumflag has no way to allow an empty
		// string without allowing it everywhere. The set is still enforced,
		// by config.Load, which names the values when it rejects one.
		"bb --log-level":  "empty means unset; diagnostics.ParseLevel enforces the set",
		"bb --log-format": "empty means unset; diagnostics.ParseFormat enforces the set",
	}

	visitFlags(t, func(cmd *cobra.Command, flag *pflag.Flag) {
		if strings.Contains(flag.Usage, enumFlagMarker) {
			return
		}
		if !enumerationInProse.MatchString(flag.Usage) {
			return
		}
		if reason, ok := exempt[cmd.CommandPath()+" --"+flag.Name]; ok {
			t.Logf("exempt: %s --%s (%s)", cmd.CommandPath(), flag.Name, reason)
			return
		}

		t.Errorf("%s --%s enumerates its values in its help text (%q) but is not registered with enumflag, "+
			"so it will forward anything to the server. Register it with enumflag.Register, which builds the "+
			"usage text from the same slice it validates against.",
			cmd.CommandPath(), flag.Name, flag.Usage)
	})
}
