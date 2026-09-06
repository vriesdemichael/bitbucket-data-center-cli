package reviewergroupcmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

type mockReviewerGroupPermChecker struct {
	repoErr    error
	projectErr error
}

func (m *mockReviewerGroupPermChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return m.repoErr
}

func (m *mockReviewerGroupPermChecker) CheckProjectAdmin(ctx context.Context, projectKey string) error {
	return m.projectErr
}

func TestReviewerGroupDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	var deps Dependencies
	d := deps.withDefaults()

	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected JSONEnabled to default to false")
	}
	if d.DryRunEnabled == nil || d.DryRunEnabled() {
		t.Fatal("expected DryRunEnabled to default to false")
	}
	if d.WriteJSON == nil {
		t.Fatal("expected WriteJSON to default to non-nil")
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

func TestReviewerGroupPermissionRejections(t *testing.T) {
	t.Parallel()

	// A listener that fails the test if it is reached, which is the assertion:
	// the permission checker refuses before a request exists.
	guard := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(guard.Close)

	cfg := config.AppConfig{BitbucketURL: guard.URL, ProjectKey: "PRJ"}
	deps := Dependencies{
		DryRunEnabled: func() bool { return true },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		PermissionChecker: func(c *openapigenerated.ClientWithResponses) PermissionChecker {
			return &mockReviewerGroupPermChecker{repoErr: http.ErrAbortHandler, projectErr: http.ErrAbortHandler}
		},
	}

	// Repo create dry-run permission rejection
	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "group-new", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on repo create dry-run")
	}

	// Repo update dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "201", "--name", "group-renamed", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on repo update dry-run")
	}

	// Repo delete dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "201", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on repo delete dry-run")
	}

	// Project create dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"create", "group-new", "--project", "PRJ"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on project create dry-run")
	}

	// Project update dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"update", "201", "--name", "group-renamed", "--project", "PRJ"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on project update dry-run")
	}

	// Project delete dry-run permission rejection
	cmd = New(deps)
	buf.Reset()
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"delete", "201", "--project", "PRJ"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected permission error on project delete dry-run")
	}
}

// A reviewer group may be named with digits, and nothing stops a team calling
// one "42". Reading every run of digits as an id sent them to whichever group
// happened to hold that id, or to a not-found for a group sitting right there
// in the listing.
//
// The name is evidence; a string of digits is a guess, so the name wins and the
// numeric reading is the fallback.
func TestResolveReviewerGroupIDPrefersANameOverANumericGuess(t *testing.T) {
	t.Parallel()

	name42 := "42"
	other := "qa-leads"
	var idNine int64 = 9
	var idFortyTwo int64 = 42

	groups := []openapigenerated.RestReviewerGroup{
		{Name: &name42, Id: &idNine},
		{Name: &other, Id: &idFortyTwo},
	}

	t.Run("a numeric name resolves to its own group", func(t *testing.T) {
		got, err := resolveReviewerGroupID(groups, "42")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "9" {
			t.Errorf("id = %q, want 9 -- the group named 42, not the group with id 42", got)
		}
	})

	t.Run("a number matching no name is still read as an id", func(t *testing.T) {
		got, err := resolveReviewerGroupID(groups, "1234")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "1234" {
			t.Errorf("id = %q, want it passed through so the server answers", got)
		}
	})

	t.Run("a name that is not there is not found", func(t *testing.T) {
		if _, err := resolveReviewerGroupID(groups, "nope"); err == nil {
			t.Fatal("expected a not-found for a name no group answers to")
		}
	})
}

// The reviewer group command suite is live now.
//
// It asserted create, list, update, delete and the user subcommands
// against a fixture. Reviewer groups are the surface where a payload that
// looks right and is silently ignored has bitten twice already -- a member
// named rather than numbered yields an empty group (#533) -- which is exactly
// what a fixture cannot show. TestLiveReviewerGroupUpdate and its neighbours
// drive them against a server.
