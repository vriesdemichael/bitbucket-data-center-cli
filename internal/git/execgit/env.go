package execgit

import (
	"fmt"
	"os"
	"slices"
	"strings"
)

// repositoryLocatorVars are the environment variables that name the repository
// git acts on. Git honours them over the process working directory, so a
// command with Dir set to one repository still reads and writes whatever these
// point at, without searching upward at all.
//
// Git exports them to every hook it runs. Inheriting them means a git command
// issued from inside a hook — or from any tool invoked by one — silently
// operates on the hook's repository rather than the directory it was pointed
// at.
var repositoryLocatorVars = []string{
	"GIT_DIR",
	"GIT_WORK_TREE",
	"GIT_INDEX_FILE",
	"GIT_OBJECT_DIRECTORY",
	"GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR",
	"GIT_PREFIX",
	"GIT_NAMESPACE",
}

// repositoryScopeVars are the locators plus the ceiling that bounds git's
// upward search. Together they are everything in the environment that decides
// which repository a git command reaches.
var repositoryScopeVars = append(slices.Clone(repositoryLocatorVars), "GIT_CEILING_DIRECTORIES")

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

// ClearRepositoryLocators removes the locating variables from this process's
// own environment.
//
// ScopeFreeEnv covers a git subprocess this package starts. It cannot cover one
// started anywhere else, and inside a git hook there is something to cover: git
// exports GIT_DIR to every hook, and it is absolute whenever the hook runs in a
// linked worktree, because no relative path reaches .git/worktrees/<name> from
// the checkout. A git command that inherits it goes straight there rather than
// searching upward, so GIT_CEILING_DIRECTORIES never gets a say.
//
// GIT_CEILING_DIRECTORIES is deliberately left in place. It bounds a search
// rather than naming a repository, and a caller that has set one is relying on
// it to still be there.
func ClearRepositoryLocators() error {
	for _, name := range repositoryLocatorVars {
		if err := os.Unsetenv(name); err != nil {
			return fmt.Errorf("unset %s: %w", name, err)
		}
	}
	return nil
}
