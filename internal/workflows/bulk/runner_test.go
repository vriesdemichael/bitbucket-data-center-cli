package bulk

import (
	"context"
	"math"
	"net/http/httptest"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	qualityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/quality"
	reposettings "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

func TestServiceRunnerUnconfigured(t *testing.T) {
	t.Parallel()

	t.Run("nil runner", func(t *testing.T) {
		var runner *ServiceRunner
		_, err := runner.Run(context.Background(), RepositoryTarget{}, OperationSpec{Type: OperationRepoPermissionUserGrant})
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("missing services", func(t *testing.T) {
		runner := &ServiceRunner{}
		repo := RepositoryTarget{ProjectKey: "P", Slug: "s"}
		ops := []OperationSpec{
			{Type: OperationRepoPermissionUserGrant},
			{Type: OperationRepoPermissionGroupGrant},
			{Type: OperationRepoWebhookCreate},
			{Type: OperationRepoPullRequestRequiredAllTasksComplete, RequiredAllTasksComplete: boolPtr(true)},
			{Type: OperationRepoPullRequestRequiredApproversCount, Count: intPtr(1)},
			{Type: OperationBuildRequiredCreate},
			{Type: OperationRepoSettingsAutoMerge, Enabled: boolPtr(true)},
			{Type: OperationRepoSettingsAutoDecline, Enabled: boolPtr(true), InactivityWeeks: intPtr(4)},
			{Type: OperationRepoDefaultTaskCreate, Description: stringPtr("my task")},
		}
		for _, op := range ops {
			_, err := runner.Run(context.Background(), repo, op)
			if err == nil {
				t.Fatalf("expected error for %s", op.Type)
			}
		}
	})
}

// Every case here is refused before a request is built, so the listener fails
// the test if one arrives.
//
// The one that was not -- a webhook create defaulting active to true -- went
// with TestServiceRunnerOperations: it needed the write to succeed, and a
// handler answering ok to everything cannot tell a hook Bitbucket accepted
// from one it did not. TestLiveBulkEveryOperationType creates that hook and
// then finds it in `webhook list`.
func TestServiceRunnerValidationBranches(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	defer server.Close()

	client, _ := openapi.NewClientWithResponsesFromConfig(config.AppConfig{BitbucketURL: server.URL})
	runner := NewServiceRunner(reposettings.NewService(client), qualityservice.NewService(client))
	repo := RepositoryTarget{ProjectKey: "PRJ", Slug: "repo"}

	t.Run("requiredAllTasksComplete nil is validation error", func(t *testing.T) {
		_, err := runner.Run(context.Background(), repo, OperationSpec{Type: OperationRepoPullRequestRequiredAllTasksComplete})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("requiredApprovers count nil is validation error", func(t *testing.T) {
		_, err := runner.Run(context.Background(), repo, OperationSpec{Type: OperationRepoPullRequestRequiredApproversCount})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("auto-merge enabled nil is validation error", func(t *testing.T) {
		_, err := runner.Run(context.Background(), repo, OperationSpec{Type: OperationRepoSettingsAutoMerge})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("auto-decline enabled nil is validation error", func(t *testing.T) {
		_, err := runner.Run(context.Background(), repo, OperationSpec{Type: OperationRepoSettingsAutoDecline})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("auto-decline inactivityWeeks nil when enabled is validation error", func(t *testing.T) {
		_, err := runner.Run(context.Background(), repo, OperationSpec{Type: OperationRepoSettingsAutoDecline, Enabled: boolPtr(true)})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})

	t.Run("default-task description nil is validation error", func(t *testing.T) {
		_, err := runner.Run(context.Background(), repo, OperationSpec{Type: OperationRepoDefaultTaskCreate})
		if err == nil {
			t.Fatal("expected validation error")
		}
	})
}

// TestAutoDeclineRejectsOutOfRangeInactivityWeeks covers the bound on a value
// that arrives from a policy file.
//
// The API field is 32 bits; before this check a week count beyond its range
// wrapped rather than being rejected, so a policy could silently configure the
// opposite of what it said.
func TestAutoDeclineRejectsOutOfRangeInactivityWeeks(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	defer server.Close()

	client, _ := openapi.NewClientWithResponsesFromConfig(config.AppConfig{BitbucketURL: server.URL})
	runner := NewServiceRunner(reposettings.NewService(client), qualityservice.NewService(client))
	repo := RepositoryTarget{ProjectKey: "PRJ", Slug: "repo"}

	for _, weeks := range []int{-1, math.MaxInt32 + 1} {
		operation := OperationSpec{
			Type:            OperationRepoSettingsAutoDecline,
			Enabled:         boolPtr(true),
			InactivityWeeks: intPtr(weeks),
		}

		_, err := runner.Run(context.Background(), repo, operation)
		if err == nil {
			t.Fatalf("expected inactivityWeeks %d to be rejected", weeks)
		}
		if !apperrors.IsKind(err, apperrors.KindValidation) {
			t.Fatalf("expected KindValidation for %d, got %v", weeks, err)
		}
	}
}

// TestServiceRunnerOperations is live now, in TestLiveBulkEveryOperationType.
//
// It ran all nine operation types past a handler that answered
// `{"status":"ok"}` to every request and checked only that Run returned no
// error, which is true of an operation that sends nonsense to the wrong
// route. The live version reads the apply status one operation result at a
// time -- Bitbucket's verdict on each of the nine -- and then reads the
// settings and the webhook back.
