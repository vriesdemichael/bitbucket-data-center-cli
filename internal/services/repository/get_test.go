package repository

import (
	"context"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// A repository that is not fully named is refused before any request. The
// service is built without a client, so a request would not get far enough to
// be mistaken for a refusal.
func TestGetAndReadmeRefuseAnIncompleteRepositoryBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	service := NewAdminService(nil)

	for _, ref := range []RepositoryRef{{}, {ProjectKey: "PRJ"}, {Slug: "demo"}} {
		if _, err := service.Get(context.Background(), ref); !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Errorf("Get(%+v): got %v, want a validation error", ref, err)
		}
		if _, _, err := service.Readme(context.Background(), ref); !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Errorf("Readme(%+v): got %v, want a validation error", ref, err)
		}
	}
}
