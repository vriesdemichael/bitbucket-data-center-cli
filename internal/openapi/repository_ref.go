package openapi

import (
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// ValidateRepository refuses a repository that is not fully named.
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

	return nil
}
