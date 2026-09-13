package diff

import (
	"context"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// The refs ComparePatch swaps are checked against Bitbucket by the live suite;
// what it refuses before sending anything is checked here.
func TestComparePatchRequiresARepository(t *testing.T) {
	t.Parallel()

	_, err := NewService(nil).ComparePatch(context.Background(), RepositoryRef{}, "feature", "main")
	if !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("got %v, want a validation error", err)
	}
}
