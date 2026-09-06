package prcmd

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// errPermissionRefused stands in for the error a real checker returns when the
// caller lacks the permission a command requires.
var errPermissionRefused = errors.New("permission refused by test checker")

// recordingChecker captures the permission a command demands before it plans a
// mutation, and refuses it.
//
// Refusing is what makes this worth testing. Allowing only proves the call was
// reached; refusing proves the command actually stops, which is the whole point
// of checking. Nothing covered that branch before -- every command could have
// ignored a refusal and still passed its tests.
type recordingChecker struct {
	permissions *[]openapi.RepositoryPermission
}

func (c recordingChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	*c.permissions = append(*c.permissions, permission)
	return errPermissionRefused
}

// executePrRecordingPermissions runs a command in dry-run mode and returns every
// repository permission it checked.
func executePrRecordingPermissions(t *testing.T, serverURL string, args ...string) ([]openapi.RepositoryPermission, error) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var recorded []openapi.RepositoryPermission

	deps := Dependencies{
		JSONEnabled:   func() bool { return false },
		DryRunEnabled: func() bool { return true },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, clientErr := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, clientErr
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) PermissionChecker {
			return recordingChecker{permissions: &recorded}
		},
	}

	root := New(deps)
	root.SilenceUsage = true
	root.SilenceErrors = true
	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)
	root.SetArgs(args)

	err := root.Execute()
	return recorded, err
}

// Every mutating pull request command has to establish the caller may act before
// it plans anything, and has to stop when told no. Nothing asserted either: not
// which permission each command demands, nor that a refusal is honoured.
func TestPRCommandsCheckRepositoryPermission(t *testing.T) {
	// A listener that fails the test if reached: every case is refused by the
	// permission check before a request is built.
	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)

	tests := []struct {
		name string
		args []string
		want openapi.RepositoryPermission
	}{
		// Read, not write: Bitbucket requires REPO_READ on the repository a pull
		// request targets, and a fork contributor holds only that upstream (#506).
		{name: "create", args: []string{"create", "--from-ref", "feature/y", "--to-ref", "main", "--title", "T", "--no-default-reviewers", "--no-codeowners"}, want: openapi.RepoRead},
		{name: "update", args: []string{"update", "42", "--title", "T", "--version", "1"}, want: openapi.RepoWrite},
		{name: "merge", args: []string{"merge", "42"}, want: openapi.RepoWrite},
		{name: "decline", args: []string{"decline", "42"}, want: openapi.RepoWrite},
		{name: "reopen", args: []string{"reopen", "42"}, want: openapi.RepoWrite},
		{name: "review approve", args: []string{"review", "approve", "42"}, want: openapi.RepoRead},
		{name: "review unapprove", args: []string{"review", "unapprove", "42"}, want: openapi.RepoRead},
		{name: "reviewer add", args: []string{"review", "reviewer", "add", "42", "--user", "bob"}, want: openapi.RepoWrite},
		{name: "reviewer remove", args: []string{"review", "reviewer", "remove", "42", "--user", "bob"}, want: openapi.RepoWrite},
		{name: "review complete", args: []string{"review", "complete", "42"}, want: openapi.RepoWrite},
		{name: "review discard", args: []string{"review", "discard", "42"}, want: openapi.RepoWrite},
		// Commenting needs only read: Bitbucket lets any reader comment.
		{name: "comment add", args: []string{"comment", "add", "42", "--text", "hello"}, want: openapi.RepoRead},
		{name: "comment react", args: []string{"comment", "react", "42", "101", "thumbsup"}, want: openapi.RepoRead},
		// Applying a suggestion writes a commit to the source branch, so this
		// is RepoWrite. It asserted RepoRead until #481 -- the test pinned the
		// defect rather than catching it.
		{name: "comment apply-suggestion", args: []string{"comment", "apply-suggestion", "42", "101"}, want: openapi.RepoWrite},
		{name: "comment resolve", args: []string{"comment", "resolve", "42", "101"}, want: openapi.RepoRead},
		{name: "comment reopen", args: []string{"comment", "reopen", "42", "101"}, want: openapi.RepoRead},
		{name: "auto-merge enable", args: []string{"auto-merge", "enable", "42"}, want: openapi.RepoWrite},
		{name: "auto-merge disable", args: []string{"auto-merge", "disable", "42"}, want: openapi.RepoWrite},
		{name: "watch", args: []string{"watch", "42"}, want: openapi.RepoRead},
		{name: "unwatch", args: []string{"unwatch", "42"}, want: openapi.RepoRead},
		{name: "rebase", args: []string{"rebase", "42"}, want: openapi.RepoWrite},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			recorded, err := executePrRecordingPermissions(t, server.URL, testCase.args...)
			if len(recorded) == 0 {
				t.Fatal("command planned a mutation without checking any repository permission")
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

// The permission constants are aliases for generated values whose names change
// between spec versions. Their meaning is the wire string, which must not.
func TestRepositoryPermissionValues(t *testing.T) {
	t.Parallel()

	tests := map[openapi.RepositoryPermission]string{
		openapi.RepoRead:  "REPO_READ",
		openapi.RepoWrite: "REPO_WRITE",
		openapi.RepoAdmin: "REPO_ADMIN",
	}

	for constant, want := range tests {
		if string(constant) != want {
			t.Fatalf("permission constant is %q, want %q", constant, want)
		}
	}

	// A typo that made two of them equal would silently widen or narrow every
	// command's requirement.
	if openapi.RepoRead == openapi.RepoWrite || openapi.RepoWrite == openapi.RepoAdmin {
		t.Fatal("permission constants must be distinct")
	}
}

// The check has to come before the plan, so a caller who cannot perform the
// operation never receives a preview implying they can.
func TestPRCommandsCheckPermissionBeforePlanning(t *testing.T) {
	// A listener that fails the test if reached: every case is refused by the
	// permission check before a request is built.
	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)

	recorded, err := executePrRecordingPermissions(t, server.URL, "merge", "42")
	if !errors.Is(err, errPermissionRefused) {
		t.Fatalf("expected the refusal to surface, got %v", err)
	}
	if len(recorded) != 1 {
		t.Fatalf("expected exactly one permission check, got %d", len(recorded))
	}
	if !strings.EqualFold(string(recorded[0]), "REPO_WRITE") {
		t.Fatalf("merge checked %q", recorded[0])
	}
}
