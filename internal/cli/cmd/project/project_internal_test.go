package projectcmd

import (
	"math"
	"reflect"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

func TestProjectSafeHelpers(t *testing.T) {
	t.Parallel()

	// The pointer helpers moved to internal/safederef and are tested
	// there. safeUsers is this package's own and stays.
	s := "test"
	if safeUsers(nil) != nil {
		t.Fatal("expected nil for safeUsers(nil)")
	}
	u := []openapigenerated.RestApplicationUser{{Name: &s}}
	if len(safeUsers(&u)) != 1 {
		t.Fatal("expected 1 user for safeUsers(&u)")
	}

}

func TestProjectDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.DryRunEnabled == nil || d.DryRunEnabled() {
		t.Fatal("expected DryRunEnabled to default to false")
	}
	if d.WriteJSON == nil || d.WriteJSONList == nil {
		t.Fatal("expected WriteJSON and WriteJSONList to default to non-nil")
	}
	if d.LoadConfig != nil {
		cfg, err := d.LoadConfig()
		if err != nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfig: %v", err)
		}
	}
	if d.LoadConfigAndClient != nil {
		cfg, client, err := d.LoadConfigAndClient()
		if err != nil || client == nil || cfg.BitbucketURL != "http://localhost:7990" {
			t.Fatalf("unexpected LoadConfigAndClient: %v", err)
		}
	}
}

func TestNormalizeAccessKeyIDs(t *testing.T) {
	t.Parallel()

	got, err := normalizeAccessKeyIDs(nil)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil) for normalizeAccessKeyIDs(nil), got (%v, %v)", got, err)
	}

	input := []int{5, 2, 8}
	got, err = normalizeAccessKeyIDs(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []int32{5, 2, 8}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeAccessKeyIDs = %v, want %v", got, want)
	}

	// Negative ID -> validation error
	if _, err := normalizeAccessKeyIDs([]int{-1}); err == nil {
		t.Fatal("expected error for negative access key id")
	}
	// Overflow ID
	if _, err := normalizeAccessKeyIDs([]int{math.MaxInt32 + 10}); err == nil {
		t.Fatal("expected error for overflow access key id")
	}
}

func TestMatchesProjectRestrictionSignature(t *testing.T) {
	t.Parallel()

	matcherType := openapigenerated.RestRefRestrictionMatcherTypeIdBRANCH
	matcherID := "refs/heads/main"
	resType := "read-only"
	existing := openapigenerated.RestRefRestriction{
		Type: &resType,
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
			}{
				Id: matcherType,
			},
		},
	}

	if !matchesProjectRestrictionSignature(existing, "read-only", "BRANCH", "refs/heads/main") {
		t.Fatal("expected matchesProjectRestrictionSignature to return true")
	}

	if matchesProjectRestrictionSignature(existing, "no-deletes", "BRANCH", "refs/heads/main") {
		t.Fatal("expected matchesProjectRestrictionSignature to return false for different type")
	}

	if matchesProjectRestrictionSignature(existing, "read-only", "TAG", "refs/heads/main") {
		t.Fatal("expected matchesProjectRestrictionSignature to return false for different matcher type")
	}
}
