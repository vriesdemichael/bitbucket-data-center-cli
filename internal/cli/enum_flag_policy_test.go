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

// enumerationInProse matches a value set written out by hand. Three shapes,
// each of which earns its place by catching something the others do not:
//
//	A: ":" or "(", then three or more comma-separated tokens, then the end of
//	   the text or a ";". Catches "Diagnostics verbosity: error, warn, info,
//	   debug".
//	B: the same markers, then two tokens joined by "or". Catches "Active status
//	   (true or false); unchanged when omitted".
//	C: two or more SHOUTING_TOKENS in a list, no marker needed, because
//	   upper-case tokens in a series are values rather than prose. Catches
//	   "Order by NEWEST, OLDEST, or STATUS", which has no marker at all, and
//	   "ADDED (default), REMOVED, or CONTEXT", where a parenthetical breaks the
//	   run that A would need.
//
// The first version had only A and B, both anchored to the end of the string.
// That let `bb webhook update --active` through on its trailing "; unchanged
// when omitted", and would have missed two of the flags converted by hand.
//
// A and B keep their markers. Without one, "Commit ID or ref" and "should be
// modified or created" both match, and a governance test that cries wolf gets
// deleted. B still has one false positive, exempted below with its reason.
//
// The gap is a lower-case value set with no marker -- "either alphabetical or
// modification" is invisible to all three. A flag would have to enumerate that
// way and never be registered, but it is a gap, not a guarantee.
const (
	// A bare value token, and the ways an enumeration can end: at the close of
	// a bracket, at the end of the text, or at a semicolon introducing a
	// trailing clause. That last one is what `--active` needed.
	enumToken = `[A-Za-z0-9][A-Za-z0-9._-]*`
	enumEnd   = `\s*[).]?\s*(?:$|;)`
)

var enumerationInProse = regexp.MustCompile(
	// A: marker, then three or more comma-separated tokens.
	`[:(]\s*` + enumToken + `(?:,\s*(?:or\s+)?` + enumToken + `){2,}` + enumEnd +
		`|` +
		// B: marker, then two tokens joined by "or".
		`[:(]\s*` + enumToken + `\s+or\s+` + enumToken + enumEnd +
		`|` +
		// C: no marker needed, because SHOUTING_TOKENS in a list are values
		// and not prose. This is what catches "Order by NEWEST, OLDEST, or
		// STATUS", and a list interrupted by a parenthetical.
		`\b[A-Z][A-Z0-9_]+(?:,\s*(?:or\s+)?[A-Z][A-Z0-9_]+)+`)

// visitFlags walks every command's own and persistent flags once.
func visitFlags(t *testing.T, visit func(cmd *cobra.Command, flag *pflag.Flag)) {
	t.Helper()

	root := NewRootCommand()

	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		// Hidden commands are not skipped -- a hidden flag still takes input.
		if cmd.Name() == "help" || cmd.Name() == "completion" {
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

		// "(repeatable or comma-separated; ...)" describes how to write the
		// flag, not what it accepts -- the values are group names, an open
		// set. Shape B cannot tell that from "(true or false)", and the cost
		// of teaching it is worse than two entries here.
		"bb pr create --reviewer-group":              "describes repetition, not a value set; group names are open",
		"bb pr review reviewer add --reviewer-group": "describes repetition, not a value set; group names are open",
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
