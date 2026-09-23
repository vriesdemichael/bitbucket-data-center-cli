// Package inherited keeps a repository's commands off what the repository
// inherits from its project.
//
// Bitbucket's repository listings of branch restrictions, default reviewer
// conditions and default tasks include the project's as well, marked with the
// scope PROJECT, and its repository routes take their ids without complaint. A
// restriction or a reviewer condition deleted through them is deleted from the
// project, and so from every repository in it; a default task answers 204 and
// stays where it is (#657). A repository's command therefore reads the scope
// first, and refuses the project's with the command that changes it where it
// is defined.
package inherited

import (
	"fmt"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// FromProject reports whether scope, as Bitbucket spells it, is a project's.
func FromProject(scope string) bool {
	return strings.EqualFold(strings.TrimSpace(scope), "PROJECT")
}

// Refusal is the error for changing, through a repository's command, an object
// the repository inherits from projectKey. change is what the command would
// have done, and command is the one that does it for the project.
func Refusal(object, id, projectKey, change, command string) error {
	return apperrors.New(apperrors.KindValidation, fmt.Sprintf(
		"%s %s is inherited from project %s; %s it for every repository in %s with %s",
		object, id, projectKey, change, projectKey, command), nil)
}

// Label is what a text listing shows beside an entry inherited from projectKey,
// and nothing beside the repository's own.
func Label(scope, projectKey string) string {
	if !FromProject(scope) {
		return ""
	}

	return "inherited from " + projectKey
}
