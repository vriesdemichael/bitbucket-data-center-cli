package cli

import (
	"testing"

	branchcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/branch"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

func TestBranchRestrictionDryRunHelpers(t *testing.T) {
	t.Parallel()

	readOnly := "read-only"
	matcherID := "refs/heads/main"
	matcherType := openapigenerated.RestRefRestrictionMatcherTypeId("BRANCH")
	userA := "alice"
	groupA := "devs"
	keyID := int32(7)
	accessKey := openapigenerated.RestSshAccessKey{}
	accessKey.Key = &struct {
		AlgorithmType     *string `json:"algorithmType,omitempty"`
		BitLength         *int32  `json:"bitLength,omitempty"`
		CreatedDate       *int64  `json:"createdDate,omitempty"`
		ExpiryDays        *int32  `json:"expiryDays,omitempty"`
		Fingerprint       *string `json:"fingerprint,omitempty"`
		Id                *int32  `json:"id,omitempty"`
		Label             *string `json:"label,omitempty"`
		LastAuthenticated *string `json:"lastAuthenticated,omitempty"`
		Text              *string `json:"text,omitempty"`
		Warning           *string `json:"warning,omitempty"`
	}{Id: &keyID}

	restriction := openapigenerated.RestRefRestriction{
		Type: &readOnly,
		Matcher: &struct {
			DisplayId *string `json:"displayId,omitempty"`
			Id        *string `json:"id,omitempty"`
			Type      *struct {
				Id   openapigenerated.RestRefRestrictionMatcherTypeId `json:"id"`
				Name string                                           `json:"name"`
			} `json:"type,omitempty"`
		}{
			Id: &matcherID,
			Type: &struct {
				Id   openapigenerated.RestRefRestrictionMatcherTypeId `json:"id"`
				Name string                                           `json:"name"`
			}{Id: matcherType},
		},
		Users:      &[]openapigenerated.RestApplicationUser{{Name: &userA}},
		Groups:     &[]string{groupA},
		AccessKeys: &[]openapigenerated.RestSshAccessKey{accessKey},
	}

	if !branchcmd.MatchesRestrictionSignature(restriction, "read-only", "BRANCH", "refs/heads/main") {
		t.Fatal("expected restriction signature to match")
	}
	if branchcmd.MatchesRestrictionSignature(restriction, "no-deletes", "BRANCH", "refs/heads/main") {
		t.Fatal("expected restriction signature mismatch on type")
	}

	if !branchcmd.MatchesRestrictionUpdate(restriction, "read-only", "BRANCH", "refs/heads/main", []string{"alice"}, []string{"devs"}, []int32{7}) {
		t.Fatal("expected restriction update equivalence")
	}
	if branchcmd.MatchesRestrictionUpdate(restriction, "read-only", "BRANCH", "refs/heads/main", []string{"bob"}, []string{"devs"}, []int32{7}) {
		t.Fatal("expected restriction update mismatch when users differ")
	}

	if got := branchcmd.NormalizeBranchName("feature/test"); got != "refs/heads/feature/test" {
		t.Fatalf("expected refs/heads/ prefix, got %q", got)
	}
	if got := branchcmd.NormalizeBranchName("refs/heads/main"); got != "refs/heads/main" {
		t.Fatalf("expected existing refs path unchanged, got %q", got)
	}
}

// normalizeJSONShape is what keeps an echoed-back payload printing like every
// other command's, so it is worth a check of its own now that the hook commands
// that first needed it are gone.
func TestNormalizeJSONShape(t *testing.T) {
	t.Parallel()

	normalized := normalizeJSONShape(struct {
		Enabled bool `json:"enabled"`
	}{Enabled: true})
	object, ok := normalized.(map[string]any)
	if !ok || object["enabled"] != true {
		t.Fatalf("expected normalized JSON object with enabled=true, got %#v", normalized)
	}
}

func TestPRDryRunHelpers(t *testing.T) {
	t.Parallel()

	reviewers := []pullrequestservice.Reviewer{{Name: "alice", Status: "APPROVED"}, {Name: "bob", Approved: false}}
	if !hasApprovedReviewer(reviewers) {
		t.Fatal("expected approved reviewer to be detected")
	}
	if !hasReviewer(reviewers, " ALICE ") {
		t.Fatal("expected reviewer lookup to be case-insensitive")
	}
}
