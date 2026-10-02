package reviewer

import (
	"context"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// A body that cannot be read as an object is a failure before the release is
// asked: the service has no client here, so reaching for one would panic.
func TestConditionChecksOfABodyItCannotReadFailBeforeAskingTheRelease(t *testing.T) {
	t.Parallel()

	service := &Service{}
	for _, condition := range []any{make(chan int), "not an object"} {
		if err := service.RefuseCondition(context.Background(), condition); !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Errorf("RefuseCondition(%T) = %v, want an internal failure", condition, err)
		}
		if older, err := service.OlderConditionChecks(context.Background(), condition); older || !apperrors.IsKind(err, apperrors.KindInternal) {
			t.Errorf("OlderConditionChecks(%T) = %t, %v; want an internal failure", condition, older, err)
		}
	}
}

// Neither asks the release about a condition no release answers differently.
func TestConditionChecksAskTheReleaseOnlyWhenItDecides(t *testing.T) {
	t.Parallel()

	service := &Service{}
	condition := map[string]any{
		"sourceMatcher":     map[string]any{"id": "ANY_REF", "type": map[string]any{"id": "ANY_REF"}},
		"targetMatcher":     map[string]any{"id": "refs/heads/main", "type": map[string]any{"id": "BRANCH"}},
		"reviewers":         []any{map[string]any{"id": 2}},
		"reviewerGroups":    []any{},
		"requiredApprovals": 1,
	}
	if err := service.RefuseCondition(context.Background(), condition); err != nil {
		t.Fatalf("RefuseCondition = %v, want nil without asking", err)
	}
	if older, err := service.OlderConditionChecks(context.Background(), condition); older || err != nil {
		t.Fatalf("OlderConditionChecks = %t, %v; want false without asking", older, err)
	}
}
