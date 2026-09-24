package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// shallowAliasCanonical names the command each shallow alias stands for
// (ADR-050), which is what meta.command reports for it (ADR-096).
//
// An alias writes its canonical command's document byte for byte -- that is
// what makes it an alias rather than a second implementation -- and meta.command
// is part of the document: it names the command whose --describe describes it.
// Cobra's own aliases (pr view for pr get) resolve to their canonical command
// by themselves; these are second registrations under a shorter path, so they
// have to be told.
var shallowAliasCanonical = map[string]func(*cobra.Command) string{
	"pr checks":         canonicalIs("pr build status"),
	"pr diff":           canonicalIs("diff pr"),
	"repo admin create": canonicalIs("repo create"),
	"repo admin fork":   canonicalIs("repo fork"),
	"repo admin delete": canonicalIs("repo delete"),

	// The permission aliases drop a path segment and move it into --group, so
	// the command they stand for depends on the flag.
	"repo permissions list":      canonicalBySubject("repo settings security permissions %s list"),
	"repo permissions grant":     canonicalBySubject("repo settings security permissions %s grant"),
	"repo permissions revoke":    canonicalBySubject("repo settings security permissions %s revoke"),
	"project permissions list":   canonicalBySubject("project permissions %s list"),
	"project permissions grant":  canonicalBySubject("project permissions %s grant"),
	"project permissions revoke": canonicalBySubject("project permissions %s revoke"),
}

// CanonicalPath is the path meta.command reports for cmd: its own, or, for a
// shallow alias, the path of the command it stands for.
func CanonicalPath(cmd *cobra.Command) string {
	path := commandPathWithoutRoot(cmd)
	if canonical, ok := shallowAliasCanonical[path]; ok {
		return canonical(cmd)
	}

	return path
}

func canonicalIs(path string) func(*cobra.Command) string {
	return func(*cobra.Command) string { return path }
}

// canonicalBySubject is the users path, or the groups path under --group.
func canonicalBySubject(format string) func(*cobra.Command) string {
	return func(cmd *cobra.Command) string {
		subject := "users"
		if group, err := cmd.Flags().GetBool("group"); err == nil && group {
			subject = "groups"
		}

		return fmt.Sprintf(format, subject)
	}
}
