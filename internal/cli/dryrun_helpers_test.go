package cli

import (
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-server-cli/internal/services/pullrequest"
)

func TestBranchRestrictionDryRunHelpers(t *testing.T) {
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

	if !matchesRestrictionSignature(restriction, "read-only", "BRANCH", "refs/heads/main") {
		t.Fatal("expected restriction signature to match")
	}
	if matchesRestrictionSignature(restriction, "no-deletes", "BRANCH", "refs/heads/main") {
		t.Fatal("expected restriction signature mismatch on type")
	}

	if !matchesRestrictionUpdate(restriction, "read-only", "BRANCH", "refs/heads/main", []string{"alice"}, []string{"devs"}, []int32{7}) {
		t.Fatal("expected restriction update equivalence")
	}
	if matchesRestrictionUpdate(restriction, "read-only", "BRANCH", "refs/heads/main", []string{"bob"}, []string{"devs"}, []int32{7}) {
		t.Fatal("expected restriction update mismatch when users differ")
	}

	if got := normalizeBranchName("feature/test"); got != "refs/heads/feature/test" {
		t.Fatalf("expected refs/heads/ prefix, got %q", got)
	}
	if got := normalizeBranchName("refs/heads/main"); got != "refs/heads/main" {
		t.Fatalf("expected existing refs path unchanged, got %q", got)
	}
}

func TestWebhookHelperFunctions(t *testing.T) {
	payload := map[string]any{"values": []any{map[string]any{"id": float64(42), "name": "ci", "url": "http://example.invalid/hook"}}}

	entries := webhookEntries(payload)
	if len(entries) != 1 {
		t.Fatalf("expected one webhook entry, got %d", len(entries))
	}
	if !webhookExistsByNameAndURL(payload, "CI", "http://example.invalid/hook") {
		t.Fatal("expected webhook to match by name+url case-insensitively")
	}
	if !webhookExistsByID(payload, "42") {
		t.Fatal("expected webhook to match by numeric id")
	}
	if webhookExistsByID(payload, "999") {
		t.Fatal("did not expect webhook id 999 to exist")
	}
}

// normalizeJSONShape is what keeps an echoed-back payload printing like every
// other command's, so it is worth a check of its own now that the hook commands
// that first needed it are gone.
func TestNormalizeJSONShape(t *testing.T) {
	normalized := normalizeJSONShape(struct {
		Enabled bool `json:"enabled"`
	}{Enabled: true})
	object, ok := normalized.(map[string]any)
	if !ok || object["enabled"] != true {
		t.Fatalf("expected normalized JSON object with enabled=true, got %#v", normalized)
	}
}

func TestReviewerAndPRDryRunHelpers(t *testing.T) {
	conditionID := int32(11)
	required := int32(1)
	reviewer := "alice"
	condition := openapigenerated.RestPullRequestCondition{Id: &conditionID, RequiredApprovals: &required, Reviewers: &[]openapigenerated.RestReviewerGroup{{Name: &reviewer}}}

	if !reviewerConditionExists([]openapigenerated.RestPullRequestCondition{condition}, "11") {
		t.Fatal("expected reviewer condition to exist")
	}
	if _, ok := findReviewerCondition([]openapigenerated.RestPullRequestCondition{condition}, "99"); ok {
		t.Fatal("did not expect reviewer condition id 99")
	}

	desired := openapigenerated.RestDefaultReviewersRequest{RequiredApprovals: &required, Reviewers: &[]openapigenerated.RestApplicationUser{{Name: &reviewer}}}
	if !reviewerConditionEquivalentExists([]openapigenerated.RestPullRequestCondition{condition}, desired) {
		t.Fatal("expected equivalent reviewer condition")
	}
	if !reviewerConditionUpdateEquivalent(condition, normalizeJSONShape(condition)) {
		t.Fatal("expected update equivalence for normalized identical payload")
	}

	reviewers := []pullrequestservice.Reviewer{{Name: "alice", Status: "APPROVED"}, {Name: "bob", Approved: false}}
	if !hasApprovedReviewer(reviewers) {
		t.Fatal("expected approved reviewer to be detected")
	}
	if !hasReviewer(reviewers, " ALICE ") {
		t.Fatal("expected reviewer lookup to be case-insensitive")
	}
}
