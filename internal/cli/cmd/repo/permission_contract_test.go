package repocmd

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// errPermissionRefused stands in for the error a real checker returns when the
// caller lacks the permission a command requires.
var errPermissionRefused = errors.New("permission refused by test checker")

// refusingChecker records the permission a command demands and refuses it.
//
// The repo tests leave PermissionChecker unset, so every one of these checks was
// skipped entirely: nothing established which permission a command requires, nor
// that a refusal stops it.
type refusingChecker struct {
	permissions *[]openapi.RepositoryPermission
}

func (c refusingChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	*c.permissions = append(*c.permissions, permission)
	return errPermissionRefused
}

func (c refusingChecker) CheckProjectAdmin(ctx context.Context, projectKey string) error {
	return errPermissionRefused
}

func (c refusingChecker) CheckProjectWrite(ctx context.Context, projectKey string) error {
	return errPermissionRefused
}

func (c refusingChecker) InspectRepoPermissions(ctx context.Context, projectKey, repoSlug string) (map[string]bool, error) {
	return map[string]bool{}, nil
}

// newPermissiveServer answers anything with a plausible empty payload. These
// tests care only about reaching the permission check, which sits after whatever
// state a command loads first, so the payloads need to parse rather than mean
// anything.
func newPermissiveServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/webhooks") && r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"size":0,"isLastPage":true,"values":[]}`))
		case strings.HasSuffix(r.URL.Path, "/settings/pull-requests"):
			_, _ = w.Write([]byte(`{"mergeConfig":{"defaultStrategy":{"id":"no-ff"},"strategies":[{"id":"no-ff"}]},"requiredApprovers":0}`))
		case strings.Contains(r.URL.Path, "/tasks"):
			_, _ = w.Write([]byte(`{"size":0,"isLastPage":true,"values":[]}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"size":0,"isLastPage":true,"values":[],"id":1,"slug":"repo1","name":"repo1","enabled":true}`))
		default:
			_, _ = w.Write([]byte(`{"id":1}`))
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func executeRefusing(t *testing.T, serverURL string, args ...string) ([]openapi.RepositoryPermission, error) {
	t.Helper()

	cfg := config.AppConfig{BitbucketURL: serverURL, ProjectKey: "PRJ"}

	var recorded []openapi.RepositoryPermission

	deps := Dependencies{
		JSONEnabled:   func() bool { return false },
		DryRunEnabled: func() bool { return true },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) PermissionChecker {
			return refusingChecker{permissions: &recorded}
		},
	}

	command := New(deps)
	command.SilenceUsage = true
	command.SilenceErrors = true
	buffer := new(bytes.Buffer)
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs(append(args, "--repo", "PRJ/repo1"))

	return recorded, command.Execute()
}

// Each repository command that mutates or reads protected state has to establish
// the caller may act before it plans anything, and stop when told no.
func TestRepoCommandsHonourRefusedPermission(t *testing.T) {
	server := newPermissiveServer(t)

	tests := []struct {
		name string
		args []string
		want openapi.RepositoryPermission
	}{
		{name: "fork", args: []string{"fork"}, want: openapi.RepoRead},
		{name: "delete", args: []string{"delete"}, want: openapi.RepoAdmin},
		{name: "admin update", args: []string{"admin", "update"}, want: openapi.RepoAdmin},

		{name: "comment create", args: []string{"comment", "create", "--text", "t", "--commit", "c123"}, want: openapi.RepoRead},
		{name: "comment update", args: []string{"comment", "update", "--id", "1", "--text", "t", "--commit", "c123"}, want: openapi.RepoRead},
		{name: "comment delete", args: []string{"comment", "delete", "--id", "1", "--commit", "c123"}, want: openapi.RepoRead},

		{name: "label add", args: []string{"label", "add", "l1"}, want: openapi.RepoWrite},
		{name: "label remove", args: []string{"label", "remove", "l1"}, want: openapi.RepoWrite},
		{name: "watch", args: []string{"watch"}, want: openapi.RepoRead},
		{name: "unwatch", args: []string{"unwatch"}, want: openapi.RepoRead},

		{name: "default-task add", args: []string{"default-task", "add", "d"}, want: openapi.RepoAdmin},
		{name: "default-task update", args: []string{"default-task", "update", "1", "--description", "d"}, want: openapi.RepoAdmin},
		{name: "default-task delete", args: []string{"default-task", "delete", "1"}, want: openapi.RepoAdmin},

		{name: "sync enable", args: []string{"sync", "enable"}, want: openapi.RepoAdmin},
		{name: "sync disable", args: []string{"sync", "disable"}, want: openapi.RepoAdmin},

		{name: "webhooks create", args: []string{"settings", "workflow", "webhooks", "create", "n", "http://h"}, want: openapi.RepoAdmin},
		{name: "webhooks delete", args: []string{"settings", "workflow", "webhooks", "delete", "1"}, want: openapi.RepoAdmin},

		{name: "pull-requests update", args: []string{"settings", "pull-requests", "update"}, want: openapi.RepoAdmin},
		{name: "pull-requests update-approvers", args: []string{"settings", "pull-requests", "update-approvers", "--count", "2"}, want: openapi.RepoAdmin},
		{name: "pull-requests set-strategy", args: []string{"settings", "pull-requests", "set-strategy", "no-ff"}, want: openapi.RepoAdmin},

		{name: "auto-decline set", args: []string{"settings", "auto-decline", "set", "--enabled", "--inactivity-weeks", "4"}, want: openapi.RepoAdmin},
		{name: "auto-decline delete", args: []string{"settings", "auto-decline", "delete"}, want: openapi.RepoAdmin},
		{name: "auto-merge set", args: []string{"settings", "auto-merge", "set", "--enabled"}, want: openapi.RepoAdmin},
		{name: "auto-merge delete", args: []string{"settings", "auto-merge", "delete"}, want: openapi.RepoAdmin},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			recorded, err := executeRefusing(t, server.URL, testCase.args...)
			if len(recorded) == 0 {
				t.Fatalf("command planned without checking any repository permission (err = %v)", err)
			}
			if recorded[0] != testCase.want {
				t.Fatalf("checked %q, want %q", recorded[0], testCase.want)
			}
			if !errors.Is(err, errPermissionRefused) {
				t.Fatalf("command continued past a refused permission check: err = %v", err)
			}
		})
	}
}
