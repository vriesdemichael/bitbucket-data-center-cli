package repository

import (
	"context"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
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

// A request that never reached Bitbucket is a failure. For Readme that matters
// twice over: "no README" is an answer Bitbucket gives, and a connection that
// failed must not be reported as one.
//
// The client points at a closed port, so nothing answers; no server is standing
// in for Bitbucket.
func TestGetAndReadmeReportAConnectionThatFailed(t *testing.T) {
	t.Parallel()

	client, err := openapigenerated.NewClientWithResponses(testsupport.RefusedURL)
	if err != nil {
		t.Fatalf("build client: %v", err)
	}
	service := NewAdminService(client)
	repo := RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}

	if _, err := service.Get(context.Background(), repo); err == nil || apperrors.IsKind(err, apperrors.KindValidation) {
		t.Errorf("Get over a failed connection: got %v, want a transport failure", err)
	}

	content, found, err := service.Readme(context.Background(), repo)
	if err == nil || apperrors.IsKind(err, apperrors.KindValidation) {
		t.Errorf("Readme over a failed connection: got %v, want a transport failure", err)
	}
	if found || content != nil {
		t.Errorf("Readme over a failed connection reported a README: found=%v content=%q", found, content)
	}
}
