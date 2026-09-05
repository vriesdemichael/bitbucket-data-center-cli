package repocmd

import (
	"bytes"
	"net/http/httptest"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// TestRepoPermissionAliasRejectsBadInvocations covers the error branches the
// shared constructors added.
func TestRepoPermissionAliasRejectsBadInvocations(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	t.Setenv("BITBUCKET_REPO_SLUG", "")
	// A listener that fails the test if reached: every invocation here is
	// refused for its arguments before a request exists.
	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)
	t.Setenv("BITBUCKET_URL", server.URL)

	cases := [][]string{
		{"--json", "repo", "permissions", "list", "--repo", "not-a-selector"},
		{"--json", "repo", "permissions", "grant", "alice", "repo_read", "--repo", "not-a-selector"},
		{"--json", "repo", "permissions", "revoke", "alice", "--repo", "not-a-selector"},
		{"--json", "repo", "permissions", "grant", "alice"},
		{"--json", "repo", "permissions", "revoke"},
	}

	for _, args := range cases {
		command := NewRootCommand()
		buffer := &bytes.Buffer{}
		command.SetOut(buffer)
		command.SetErr(buffer)
		command.SetArgs(args)
		if err := command.Execute(); err == nil {
			t.Fatalf("expected %v to fail, got output: %s", args, buffer.String())
		}
	}
}

// TestPermissionsShowRequiresAWiredChecker covers the guard that is genuinely
// internal: a binary that failed to wire its permission checker.
//
// It says so explicitly now rather than returning a bare error and landing on
// internal by falling through, which is the difference between a
// classification and an accident (#475).
func TestPermissionsShowRequiresAWiredChecker(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://127.0.0.1:1")
	t.Setenv("BITBUCKET_TOKEN", "token")

	root := &cobra.Command{Use: "bb"}
	root.PersistentFlags().Bool("json", false, "")
	root.PersistentFlags().Bool("dry-run", false, "")
	root.PersistentFlags().Bool("no-input", false, "")
	// Deliberately no PermissionChecker.
	root.AddCommand(New(Dependencies{
		JSONEnabled:   func() bool { return false },
		DryRunEnabled: func() bool { return false },
		LoadConfig:    func() (config.AppConfig, error) { return config.AppConfig{BitbucketURL: "http://127.0.0.1:1"}, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg := config.AppConfig{BitbucketURL: "http://127.0.0.1:1"}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)

			return cfg, client, err
		},
		// Wired, but declines to build one -- the second of the two guards.
		PermissionChecker: func(*openapigenerated.ClientWithResponses) PermissionChecker { return nil },
	}))

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"repo", "permissions", "show", "--repo", "PRJ/demo"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected an unwired checker to fail")
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindInternal {
		t.Errorf("kind = %v, want internal -- an unwired dependency is a defect in bb (error: %v)", kind, err)
	}
}

// Six suites are live now, in TestLivePermissionAliasSubjects.
//
// They drove `repo permissions` against a handwritten permissions endpoint
// that already held "alice with REPO_READ" and "admins with REPO_ADMIN", so
// the no-op, update, create and delete predictions they checked were lookups
// in the same file that answered them, and the route a --group grant reached
// was recorded by the mock rather than read back. The live version grants to a
// real user and a real group, reads both listings back, and asks for the four
// predictions against entries the commands themselves left behind.
