package cli

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

func TestReviewerApprovedByUser(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	options := &rootOptions{runtime: config.Overrides{Host: "http://example.local", ProjectKey: "PRJ", RepoSlug: "repo"}}

	_, _, err := options.loadQualityRepoAndService("bad-selector")
	if !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("expected validation error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--repo must be in PROJECT/slug format") {
		t.Fatalf("expected the selector to be what was refused, got: %v", err)
	}
}

// The variable is the subject: the message has to name BB_CA_FILE, which is
// what the user set, rather than a flag they did not pass.
func TestLoadConfigAndClientPropagatesConfigValidationError(t *testing.T) {
	t.Setenv("BB_CA_FILE", "/definitely/missing-ca.pem")

	options := &rootOptions{runtime: config.Overrides{Host: "http://example.local"}}
	_, _, err := options.loadConfigAndClient()
	if !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("expected validation error, got: %v", err)
	}
	if err == nil || !strings.Contains(err.Error(), "BB_CA_FILE is invalid") {
		t.Fatalf("expected config validation message, got: %v", err)
	}
}
