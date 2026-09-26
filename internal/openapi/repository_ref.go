package openapi

import (
	"fmt"
	"strings"
	"unicode"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// ValidateRepository refuses a repository that is not fully named, or whose
// project key or slug could reach a different endpoint than the one meant.
//
// Nine services carried their own copy of this check, byte for byte the same.
// Nothing had drifted yet, which is the only reason it read as harmless: the
// message a caller sees when they forget a project key was nine strings that
// happened to agree, and correcting one of them would have left eight behind.
//
// It takes the two fields rather than a struct because every package declares
// its own RepositoryRef type. Unifying those is a larger change with no
// behaviour in it; agreeing on the check costs nothing.
func ValidateRepository(projectKey, slug string) error {
	if strings.TrimSpace(projectKey) == "" || strings.TrimSpace(slug) == "" {
		return apperrors.New(apperrors.KindValidation, "repository must be specified as project/repo", nil)
	}
	for _, value := range []string{projectKey, slug} {
		if err := validatePathSegment(value); err != nil {
			return err
		}
	}

	return nil
}

// validatePathSegment refuses a value that would not stay one segment of a
// request path.
//
// A project key or slug is written into the path of the request that acts on
// the repository. A slash, a question mark or a hash ends the segment, starts a
// query or starts a fragment, so a slug of "repo/pull-requests/7/merge?version=3#"
// turned a comment into a merge: the request went to the merge endpoint, and
// Bitbucket merged, ignoring the comment it was sent (confirmed against a
// running Data Center). Through bb's MCP server that let a tool that never
// asks the person do what the tools that ask exist to hold back. A percent
// sign smuggles an encoded one, and a dot segment climbs a level.
//
// No Bitbucket project key or slug contains any of these, so refusing them
// refuses nothing that names a real repository.
func validatePathSegment(value string) error {
	trimmed := strings.TrimSpace(value)
	unsafe := trimmed == "." || trimmed == ".." ||
		strings.ContainsAny(trimmed, `/\?#%`) ||
		strings.IndexFunc(trimmed, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0
	if !unsafe {
		return nil
	}

	return apperrors.New(apperrors.KindValidation,
		fmt.Sprintf("%q is not a project key or repository slug: neither may contain a slash, a backslash, ?, #, %%, spaces, or be . or ..", value), nil)
}
