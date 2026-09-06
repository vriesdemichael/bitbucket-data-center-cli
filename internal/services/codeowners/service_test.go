package codeowners_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/codeowners"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// TestOwnersRefusesBeforeItAsks covers the three arguments the service checks
// itself.
//
// Everything else about code owners is Bitbucket's, and ADR-080 says so: which
// rules match, what an entry means, who a group expands to. What is decided
// here is only whether there is enough to form a request at all, and the
// handler proves it: a request arriving means one of these checks stopped
// running, which is otherwise invisible -- the call would succeed against a
// real server and answer about the wrong thing, or about nothing.
func TestOwnersRefusesBeforeItAsks(t *testing.T) {
	t.Parallel()

	repository := codeowners.RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}

	cases := []struct {
		name       string
		repository codeowners.RepositoryRef
		sourceRef  string
		targetRef  string
	}{
		{name: "no project", repository: codeowners.RepositoryRef{Slug: "demo"}, sourceRef: "feature/x", targetRef: "master"},
		{name: "no slug", repository: codeowners.RepositoryRef{ProjectKey: "PRJ"}, sourceRef: "feature/x", targetRef: "master"},
		{name: "blank project", repository: codeowners.RepositoryRef{ProjectKey: "  ", Slug: "demo"}, sourceRef: "feature/x", targetRef: "master"},
		{name: "no source ref", repository: repository, targetRef: "master"},
		{name: "blank source ref", repository: repository, sourceRef: "   ", targetRef: "master"},
		{name: "no target ref", repository: repository, sourceRef: "feature/x"},
		{name: "blank target ref", repository: repository, sourceRef: "feature/x", targetRef: "\t"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(testsupport.UnreachedHandler(t))
			t.Cleanup(server.Close)

			service := codeowners.NewService(httpclient.NewFromConfig(config.AppConfig{
				BitbucketURL:   server.URL,
				RequestTimeout: 5 * time.Second,
				RetryCount:     0,
			}))

			owners, err := service.Owners(context.Background(), testCase.repository, testCase.sourceRef, testCase.targetRef, nil)
			if err == nil {
				t.Fatalf("expected a refusal, got owners %v", owners)
			}
			if kind := apperrors.KindOf(err); kind != apperrors.KindValidation {
				t.Errorf("kind = %v, want validation: %v", kind, err)
			}
		})
	}
}
