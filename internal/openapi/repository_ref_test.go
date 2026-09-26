package openapi

import (
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// A project key or slug is written into a request path, so one that leaves its
// segment reaches another endpoint. The first case is the one that merged a
// pull request through a comment.
func TestValidateRepositoryRefusesAValueThatLeavesItsPathSegment(t *testing.T) {
	t.Parallel()

	for _, slug := range []string{
		"payments/pull-requests/7/merge?version=3#",
		"payments/../other",
		"payments?at=main",
		"payments#fragment",
		"payments%2Fpull-requests",
		`payments\pull-requests`,
		"..",
		".",
		"pay ments",
		"pay\tments",
		"pay\u0000ments",
	} {
		err := ValidateRepository("PROJ", slug)
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Errorf("slug %q: got %v, want a validation error", slug, err)
		}
		if err := ValidateRepository(slug, "payments"); !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Errorf("project key %q: got %v, want a validation error", slug, err)
		}
	}
}

// Real keys and slugs, a personal project's among them, still pass.
func TestValidateRepositoryAcceptsRealKeysAndSlugs(t *testing.T) {
	t.Parallel()

	for _, ref := range [][2]string{
		{"PROJ", "payments"},
		{"PROJ_2", "payments-api"},
		{"~alice", "dotfiles"},
		{"~alice.smith", "my.repo_name"},
		{"proj", "Payments.Git"},
	} {
		if err := ValidateRepository(ref[0], ref[1]); err != nil {
			t.Errorf("%s/%s: %v", ref[0], ref[1], err)
		}
	}
}
