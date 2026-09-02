// Package enumflag registers a flag whose value must come from a fixed set.
//
// The set, the usage text and the validator are one thing here, because they
// were three things before: the help string enumerated the values, a validator
// enumerated them again where one existed at all, and eight of twelve flags
// documented a set they did not enforce (#480). A flag that advertises
// LOW, MEDIUM, HIGH and forwards "Medium" to the server is worse than one that
// documents nothing -- it invites the caller to trust a contract it does not
// keep.
//
// ADR-054 requires that invalid input fail immediately, naming the allowed
// values. That is only possible if something owns them; Register is that
// something.
//
// # Case
//
// Values are matched case-insensitively and normalised to their canonical
// spelling before use. That is a decision this package makes once, because the
// answer differed per flag by accident: commentanchor.NormalizeLineType
// upper-cased before comparing, pr list --state took lower-case values as
// written, and the eight unenforced flags took whatever they were given. The
// sets themselves are inconsistent -- LOW, MEDIUM, HIGH beside no-ff and
// squash-ff-only -- so requiring the caller to match the case of each one is a
// rule nobody could follow from the help text alone.
//
// # Empty
//
// An empty value means "not given" and resets the flag to its default. Every
// one of these flags behaved that way before this package existed, because the
// service layer normalised "" to the default -- "" to open, "" to ADDED, "" to
// MERGE. Refusing it broke `bb pr list --state "$STATE"` with $STATE unset,
// which is an ordinary thing for a script to write. On the flags whose
// default is itself "" -- most of them -- it meant the flag rejected the very
// value it defaults to.
//
// Use RegisterStrict where an empty value has to be an error rather than an
// omission.
package enumflag

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// value is a pflag.Value that accepts only what it was told to.
type value struct {
	target       *string
	allowed      []string
	defaultValue string
	allowEmpty   bool
}

func (enum *value) String() string {
	if enum.target == nil {
		return ""
	}

	return *enum.target
}

// Set validates and canonicalises, and is called by pflag during parsing --
// which is what makes the rejection happen before the command runs rather than
// before the request is sent, and long before the server sees it.
func (enum *value) Set(raw string) error {
	trimmed := strings.TrimSpace(raw)

	if trimmed == "" && enum.allowEmpty {
		// Not given. Resetting to the default rather than storing "" means the
		// value leaving here is always one the set names, so nothing
		// downstream has to handle the empty case a second time.
		*enum.target = enum.defaultValue

		return nil
	}

	for _, candidate := range enum.allowed {
		if strings.EqualFold(trimmed, candidate) {
			*enum.target = candidate

			return nil
		}
	}

	// A plain error, not an apperrors one. pflag wraps whatever Set returns in
	// `invalid argument "X" for "--flag" flag: ...`, so the flag name is already
	// said and an apperrors prefix would put "validation:" in the middle of the
	// sentence. The envelope still reports kind=validation, because the root
	// command's FlagErrorFunc classifies a flag-parse failure.
	return fmt.Errorf("must be one of: %s", strings.Join(enum.allowed, ", "))
}

func (enum *value) Type() string { return "string" }

// Register declares a flag whose value must come from allowed. An empty value
// means "not given" and resets the flag to defaultValue.
//
// The usage text is built from the same slice the validator checks, so the two
// cannot disagree -- which is the failure this package exists to prevent.
func Register(flags *pflag.FlagSet, target *string, name string, defaultValue string, allowed []string, description string) {
	register(flags, target, name, "", defaultValue, allowed, description, true)
}

// RegisterP is Register with a one-letter shorthand, matching pflag's own
// StringVarP naming.
func RegisterP(flags *pflag.FlagSet, target *string, name string, shorthand string, defaultValue string, allowed []string, description string) {
	register(flags, target, name, shorthand, defaultValue, allowed, description, true)
}

// RegisterStrict is Register for a flag where an empty value must be an error
// rather than an omission -- one that changes state in a way no default can
// stand in for, so that taking "" as "leave it alone" would silently do the
// wrong thing.
func RegisterStrict(flags *pflag.FlagSet, target *string, name string, defaultValue string, allowed []string, description string) {
	register(flags, target, name, "", defaultValue, allowed, description, false)
}

func register(flags *pflag.FlagSet, target *string, name string, shorthand string, defaultValue string, allowed []string, description string, allowEmpty bool) {
	// A default outside the set ships a flag that refuses to be reset to its
	// own default, and nothing downstream would report it. Registration runs
	// while the command tree is built, so this fails the first test that
	// builds one rather than reaching anybody.
	if defaultValue != "" && !contains(allowed, defaultValue) {
		panic(fmt.Sprintf("enumflag: --%s defaults to %q, which is not in %v", name, defaultValue, allowed))
	}

	*target = defaultValue

	flags.VarP(
		&value{target: target, allowed: allowed, defaultValue: defaultValue, allowEmpty: allowEmpty},
		name,
		shorthand,
		usageFor(description, allowed),
	)
}

func contains(allowed []string, want string) bool {
	for _, candidate := range allowed {
		if candidate == want {
			return true
		}
	}

	return false
}

// usageFor writes the description and the set it will be checked against,
// so the help text cannot name a value the validator does not accept.
func usageFor(description string, allowed []string) string {
	return fmt.Sprintf("%s (one of: %s)", strings.TrimSuffix(strings.TrimSpace(description), "."), strings.Join(allowed, ", "))
}

// Value normalises a positional argument against a closed set.
//
// The flags in this package are validated by pflag before a command runs.
// A positional has no such hook, so a command with an enum-shaped argument --
// `permissions grant <name> <permission>`, `set-strategy <strategy-id>` --
// has to ask. Sharing the check keeps one message format and one place to
// change the rule, and lets the argument reuse whichever slice already
// declares the set.
//
// cobra.OnlyValidArgs is the built-in answer and does not fit: it checks every
// positional against a single list, and in two of the three cases here the
// enum is the last of several arguments.
//
// Unlike Set, this returns an apperrors error, because nothing downstream will
// classify it -- the value goes straight from RunE to the caller.
func Value(name string, raw string, allowed []string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	for _, candidate := range allowed {
		if strings.EqualFold(trimmed, candidate) {
			return candidate, nil
		}
	}

	return "", apperrors.New(apperrors.KindValidation,
		fmt.Sprintf("%s must be one of: %s", name, strings.Join(allowed, ", ")), nil)
}
