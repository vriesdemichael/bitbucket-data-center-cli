package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

var testContext = context.Background()

func newPermissionCheckerTestClient(t *testing.T, handler http.HandlerFunc) (*PermissionChecker, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := openapigenerated.NewClientWithResponses(server.URL + "/rest")
	if err != nil {
		t.Fatalf("create client: %v", err)
	}

	return NewPermissionChecker(client), server
}

func TestReviewerApprovedByUser(t *testing.T) {
	reviewers := []pullrequestservice.Reviewer{
		{Name: "alice", Status: "UNAPPROVED", Approved: false},
		{Name: "bob", Status: "APPROVED", Approved: false},
		{Name: "carol", Status: "UNAPPROVED", Approved: true},
	}

	if !reviewerApprovedByUser(reviewers, " bob ") {
		t.Fatal("expected approved reviewer status match")
	}
	if !reviewerApprovedByUser(reviewers, "carol") {
		t.Fatal("expected approved reviewer flag match")
	}
	if reviewerApprovedByUser(reviewers, "alice") {
		t.Fatal("expected unapproved reviewer to fail")
	}
	if reviewerApprovedByUser(reviewers, "") {
		t.Fatal("expected blank username to fail")
	}
}

func TestRootOptionsPermissionCheckerFor(t *testing.T) {
	clientA := &openapigenerated.ClientWithResponses{}
	clientB := &openapigenerated.ClientWithResponses{}

	var nilOptions *rootOptions
	if checker := nilOptions.permissionCheckerFor(clientA); checker != nil {
		t.Fatalf("expected nil options to return nil checker, got %#v", checker)
	}

	options := &rootOptions{}
	if checker := options.permissionCheckerFor(nil); checker != nil {
		t.Fatalf("expected nil client to return nil checker, got %#v", checker)
	}

	checkerA := options.permissionCheckerFor(clientA)
	if checkerA == nil {
		t.Fatal("expected checker to be created")
	}
	checkerB := options.permissionCheckerFor(clientB)
	if checkerA != checkerB {
		t.Fatal("expected checker to be reused once created")
	}
	if checkerA.Client() != clientA {
		t.Fatalf("expected first client to be retained, got %p want %p", checkerA.Client(), clientA)
	}
}

func TestLoadQualityRepoServiceAndClientReturnsSelectorValidationError(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://example.local")
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "repo")

	_, _, err := (&rootOptions{}).loadQualityRepoAndService("bad-selector")
	if !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("expected validation error, got: %v", err)
	}
}

func TestLoadConfigAndClientPropagatesConfigValidationError(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://example.local")
	t.Setenv("BB_CA_FILE", "/definitely/missing-ca.pem")

	_, _, err := (&rootOptions{}).loadConfigAndClient()
	if !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("expected validation error, got: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "BB_CA_FILE is invalid") {
		t.Fatalf("expected config validation message, got: %v", err)
	}
}
