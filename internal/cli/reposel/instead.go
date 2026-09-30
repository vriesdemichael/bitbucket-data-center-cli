package reposel

import (
	"fmt"
	"strings"

	"github.com/spf13/pflag"
)

// annotationInsteadOfRepo marks a flag that names what a command acts on in
// place of --repo.
const annotationInsteadOfRepo = "bb/instead-of-repo"

// MarkInsteadOfRepo declares that each named flag of flags names the command's
// scope in place of --repo: --project where a command acts on a project or on
// one of its repositories, --role where only the dashboard can answer.
//
// A caller who sets one has said what the command is about, as much as with
// --repo, so the repository of the checkout it runs in is not inferred beside
// it. Declared on the flag rather than recognised by its name, because what a
// flag means is the command's: --project is the scope of bb reviewer-group and
// the destination of bb repo admin fork.
func MarkInsteadOfRepo(flags *pflag.FlagSet, names ...string) {
	for _, name := range names {
		if err := flags.SetAnnotation(name, annotationInsteadOfRepo, []string{"true"}); err != nil {
			panic(fmt.Sprintf("reposel: cannot mark --%s as naming the scope instead of --repo: %v", name, err))
		}
	}
}

// NamedInsteadOfRepo returns the name of a flag given a value on this
// invocation that names the scope in place of --repo, or "" when none was. An
// empty value names nothing, as with --repo.
func NamedInsteadOfRepo(flags *pflag.FlagSet) string {
	named := ""
	flags.VisitAll(func(flag *pflag.Flag) {
		if named != "" || !flag.Changed || len(flag.Annotations[annotationInsteadOfRepo]) == 0 {
			return
		}
		if strings.TrimSpace(flag.Value.String()) != "" {
			named = flag.Name
		}
	})

	return named
}
