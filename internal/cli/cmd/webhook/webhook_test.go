package webhookcmd

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type mockPermChecker struct {
	err error
}

func (m *mockPermChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return m.err
}

func TestWebhookHelperFunctions(t *testing.T) {
	t.Parallel()

	b := boolPtr(true)
	if b == nil || !*b {
		t.Fatal("expected boolPtr(true) to be non-nil and true")
	}

	if safederef.String(nil) != "" {
		t.Fatal("expected safederef.String(nil) to be empty string")
	}
	s := "hello"
	if safederef.String(&s) != "hello" {
		t.Fatal("expected safederef.String(&s) to be hello")
	}
}

func TestWebhookWithDefaults(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://localhost:7990")
	d := Dependencies{}.withDefaults()
	if d.JSONEnabled == nil || d.JSONEnabled() {
		t.Fatal("expected default JSONEnabled to return false")
	}
	if d.DryRunEnabled == nil || d.DryRunEnabled() {
		t.Fatal("expected default DryRunEnabled to return false")
	}
	if d.WriteJSON == nil {
		t.Fatal("expected default WriteJSON to be non-nil")
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

// A webhook listing that is a bare array rather than a page.
//
// mock-inventory: unreachable-state — Bitbucket answers this endpoint with a page; the array is a shape it does not send, and the subject is that the listing reads either.
func TestWebhookListAcceptsABareArray(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet && r.URL.Path == "/rest/api/latest/projects/PRJ/repos/repo1/webhooks" {
			_, _ = w.Write([]byte(`[{"id":123,"name":"wh","url":"http://url","active":true,"events":["repo:refs_changed"]}]`))

			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{BitbucketURL: server.URL, ProjectKey: "PRJ"}
	deps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)

			return cfg, client, err
		},
	}

	cmd := New(deps)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"list", "--repo", "PRJ/repo1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("a bare array listing failed: %v", err)
	}
	if !strings.Contains(buf.String(), "wh") {
		t.Fatalf("the webhook in the array is not in the output: %s", buf.String())
	}
}

// Every mutating webhook command checks the caller may write before planning,
// and stops when told no. The listener fails the test if anything reaches it,
// because nothing should: the refusal comes first.
func TestWebhookCommandsHonourRefusedPermission(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)

	cfg := config.AppConfig{BitbucketURL: server.URL, ProjectKey: "PRJ"}
	deps := Dependencies{
		DryRunEnabled: func() bool { return true },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)

			return cfg, client, err
		},
		PermissionChecker: func(*openapigenerated.ClientWithResponses) PermissionChecker {
			return &mockPermChecker{err: http.ErrAbortHandler}
		},
	}

	for _, testCase := range []struct {
		name string
		args []string
	}{
		{name: "create", args: []string{"create", "wh", "http://url", "--repo", "PRJ/repo1"}},
		{name: "update", args: []string{"update", "123", "--name", "wh-new", "--repo", "PRJ/repo1"}},
		{name: "delete", args: []string{"delete", "123", "--repo", "PRJ/repo1"}},
		{name: "test", args: []string{"test", "123", "--repo", "PRJ/repo1"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			cmd := New(deps)
			buf := new(bytes.Buffer)
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(testCase.args)
			if err := cmd.Execute(); err == nil {
				t.Fatalf("planned a mutation the caller may not make: %s", buf.String())
			}
		})
	}
}

// The webhook command suite is live now.
//
// It asserted each command's output against a webhook the file invented.
// The live suite creates a real webhook, lists it, tests it, reads its
// statistics and deletes it.
