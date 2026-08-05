package execgit

import (
	"os"
	"slices"
	"strings"
)

// repositoryScopeVars are the environment variables git uses to locate the
// repository it acts on. Git honours them over the process working directory,
// so a command with Dir set to one repository still reads and writes whatever
// these point at.
//
// Git exports them to every hook it runs. Inheriting them means a git command
// issued from inside a hook — or from any tool invoked by one — silently
// operates on the hook's repository rather than the directory it was pointed
// at.
var repositoryScopeVars = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_PREFIX",
	"GIT_NAMESPACE",
	"GIT_CEILING_DIRECTORIES",
}

// ScopeFreeEnv returns the process environment with git's repository-scoping
// variables removed, so a git command is scoped by its working directory alone.
//
// Use it for any git subprocess that targets a specific directory. Everything
// else in the environment is preserved, including credential and proxy
// configuration.
func ScopeFreeEnv() []string {
	environment := os.Environ()
	filtered := make([]string, 0, len(environment))

	for _, entry := range environment {
		name, _, found := strings.Cut(entry, "=")
		if found && slices.Contains(repositoryScopeVars, name) {
			continue
		}
		filtered = append(filtered, entry)
	}

	return filtered
}
