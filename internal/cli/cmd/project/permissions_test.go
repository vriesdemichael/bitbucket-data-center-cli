package projectcmd

import (
	"bytes"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

func TestProjectPermissionAliasRejectsBadInvocations(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	// A listener that fails the test if reached: every invocation here is
	// refused for its arguments before a request exists.
	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)
	t.Setenv("BITBUCKET_URL", server.URL)

	cases := [][]string{
		{"--json", "project", "permissions", "list"},
		{"--json", "project", "permissions", "grant", "PRJ", "alice"},
		{"--json", "project", "permissions", "revoke", "PRJ"},
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

// TestCanonicalPermissionCommandsPointAtTheirAlias covers the direction the
// cross-reference was missing. The alias already names its canonical path; a
// reader who lands on the canonical one — which is where the docs send them —
// needs to be told the shorter spelling exists.
func TestCanonicalPermissionCommandsPointAtTheirAlias(t *testing.T) {
	t.Parallel()

	cases := []struct {
		args     []string
		expected string
	}{
		{
			args:     []string{"project", "permissions", "users", "list", "--help"},
			expected: "Also available as bb project permissions list, one level shallower.",
		},
		{
			args:     []string{"project", "permissions", "groups", "grant", "--help"},
			expected: "Also available as bb project permissions grant --group, one level shallower.",
		},
	}

	for _, testCase := range cases {
		command := NewRootCommand()
		buffer := &bytes.Buffer{}
		command.SetOut(buffer)
		command.SetErr(buffer)
		command.SetArgs(testCase.args)
		if err := command.Execute(); err != nil {
			t.Fatalf("%v failed: %v", testCase.args, err)
		}
		if !strings.Contains(buffer.String(), testCase.expected) {
			t.Fatalf("expected %q in the help for %v, got:\n%s", testCase.expected, testCase.args, buffer.String())
		}
	}
}

// TestProjectPermissionsShowRequiresAWiredChecker is the project-scoped twin of
// the repository test: an unwired dependency is genuinely internal, and now
// says so rather than arriving there by falling through (#475).
func TestProjectPermissionsShowRequiresAWiredChecker(t *testing.T) {
	// A listener that fails the test if it is reached: an unwired checker is
	// caught before a request exists, and a request arriving would mean the
	// guard was skipped rather than that it held.
	guard := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(guard.Close)

	t.Setenv("BITBUCKET_URL", guard.URL)
	t.Setenv("BITBUCKET_TOKEN", "token")

	cfg := config.AppConfig{BitbucketURL: guard.URL}
	root := &cobra.Command{Use: "bb"}
	root.PersistentFlags().Bool("json", false, "")
	root.PersistentFlags().Bool("dry-run", false, "")
	root.PersistentFlags().Bool("no-input", false, "")
	root.AddCommand(New(Dependencies{
		JSONEnabled:   func() bool { return false },
		DryRunEnabled: func() bool { return false },
		LoadConfig:    func() (config.AppConfig, error) { return cfg, nil },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)

			return cfg, client, err
		},
		// Wired, but declines to build one -- the second of the two guards.
		PermissionChecker: func(*openapigenerated.ClientWithResponses) PermissionChecker { return nil },
	}))

	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"project", "permissions", "show", "PRJ"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected an unwired checker to fail")
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindInternal {
		t.Errorf("kind = %v, want internal (error: %v)", kind, err)
	}
}

// Listing the permissions of a project that is not there was in the table
// above. It is not a bad invocation -- the arguments are fine and the command
// asks -- so it needs a server to say no, and the one it had said no because
// this file wrote a 404 into it. It is live in
// TestLiveErrorTaxonomyMissingResources, beside every other service's
// missing-resource case, where the answer is Bitbucket's and the exit code is
// checked against it.

// The project-scoped twins of those suites are live in
// TestLivePermissionAliasSubjectsForProjects, including the one assertion the
// repository side does not carry: that a user grant and a group grant publish
// the same field names, which is what lets --describe state one shape for one
// command.
