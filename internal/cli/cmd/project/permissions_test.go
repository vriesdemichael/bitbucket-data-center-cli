package projectcmd

import (
	"bytes"
	"encoding/json"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

// newProjectPermissionTestServer serves the project permission endpoints and
// records the mutating calls, so a test can assert which subject a shallow
// invocation resolved to.
func newProjectPermissionTestServer(t *testing.T, recorded *[]string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		// The PROJECT_ADMIN check a dry run runs before predicting anything.
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects":
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"key":"PRJ","name":"Project"}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"user":{"name":"alice","displayName":"Alice A"},"permission":"PROJECT_READ"}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups":
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"group":{"name":"admins"},"permission":"PROJECT_ADMIN"}]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			*recorded = append(*recorded, "PUT users")
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups":
			*recorded = append(*recorded, "PUT groups")
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/users":
			*recorded = append(*recorded, "DELETE users")
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/permissions/groups":
			*recorded = append(*recorded, "DELETE groups")
			writer.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func runRepoPermissionCommand(t *testing.T, args ...string) string {
	t.Helper()
	root := &cobra.Command{Use: "bb"}
	root.PersistentFlags().Bool("json", false, "")
	root.PersistentFlags().Bool("dry-run", false, "")
	jsonFlag := false
	dryRunFlag := false
	for _, arg := range args {
		if arg == "--json" {
			jsonFlag = true
		}
		if arg == "--dry-run" {
			dryRunFlag = true
		}
	}
	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonFlag },
		DryRunEnabled: func() bool { return dryRunFlag },
	}
	root.AddCommand(New(deps))
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(args)
	_ = root.Execute()
	return buf.String()
}

// TestProjectPermissionShallowAliasesMatchDeepPaths is the same assertion the
// repository tree carries: byte equality is what makes these aliases rather
// than a second implementation.
func TestProjectPermissionShallowAliasesMatchDeepPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newProjectPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	cases := []struct {
		name    string
		deep    []string
		shallow []string
	}{
		{
			name:    "users list",
			deep:    []string{"--json", "project", "permissions", "users", "list", "PRJ"},
			shallow: []string{"--json", "project", "permissions", "list", "PRJ"},
		},
		{
			name:    "groups list",
			deep:    []string{"--json", "project", "permissions", "groups", "list", "PRJ"},
			shallow: []string{"--json", "project", "permissions", "list", "--group", "PRJ"},
		},
		{
			name:    "users grant",
			deep:    []string{"--json", "project", "permissions", "users", "grant", "PRJ", "alice", "project_write"},
			shallow: []string{"--json", "project", "permissions", "grant", "PRJ", "alice", "project_write"},
		},
		{
			name:    "groups grant",
			deep:    []string{"--json", "project", "permissions", "groups", "grant", "PRJ", "admins", "project_admin"},
			shallow: []string{"--json", "project", "permissions", "grant", "--group", "PRJ", "admins", "project_admin"},
		},
		{
			name:    "users revoke",
			deep:    []string{"--json", "project", "permissions", "users", "revoke", "PRJ", "alice"},
			shallow: []string{"--json", "project", "permissions", "revoke", "PRJ", "alice"},
		},
		{
			name:    "groups revoke",
			deep:    []string{"--json", "project", "permissions", "groups", "revoke", "PRJ", "admins"},
			shallow: []string{"--json", "project", "permissions", "revoke", "--group", "PRJ", "admins"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			deepOutput := runRepoPermissionCommand(t, testCase.deep...)
			shallowOutput := runRepoPermissionCommand(t, testCase.shallow...)
			if deepOutput != shallowOutput {
				t.Fatalf("shallow alias diverged\ndeep:    %s\nshallow: %s", deepOutput, shallowOutput)
			}
		})
	}
}

func TestProjectPermissionAliasGroupFlagPicksTheSubject(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newProjectPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	runRepoPermissionCommand(t, "--json", "project", "permissions", "grant", "PRJ", "alice", "project_write")
	runRepoPermissionCommand(t, "--json", "project", "permissions", "grant", "--group", "PRJ", "admins", "project_admin")
	runRepoPermissionCommand(t, "--json", "project", "permissions", "revoke", "PRJ", "alice")
	runRepoPermissionCommand(t, "--json", "project", "permissions", "revoke", "--group", "PRJ", "admins")

	expected := []string{"PUT users", "PUT groups", "DELETE users", "DELETE groups"}
	if len(recorded) != len(expected) {
		t.Fatalf("expected %v, got %v", expected, recorded)
	}
	for index, want := range expected {
		if recorded[index] != want {
			t.Fatalf("call %d: expected %q, got %q (all: %v)", index, want, recorded[index], recorded)
		}
	}
}

func TestProjectPermissionAliasJSONNamesTheSubject(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newProjectPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	userOutput := runRepoPermissionCommand(t, "--json", "project", "permissions", "grant", "PRJ", "alice", "project_write")
	if !strings.Contains(userOutput, `"subject": "user"`) ||
		!strings.Contains(userOutput, `"name": "alice"`) ||
		!strings.Contains(userOutput, `"project": "PRJ"`) {
		t.Fatalf("expected subject, name and project for a user grant, got: %s", userOutput)
	}

	groupOutput := runRepoPermissionCommand(t, "--json", "project", "permissions", "grant", "--group", "PRJ", "admins", "project_admin")
	if !strings.Contains(groupOutput, `"subject": "group"`) || !strings.Contains(groupOutput, `"name": "admins"`) {
		t.Fatalf("expected subject and name for a group grant, got: %s", groupOutput)
	}

	// The point of naming the subject in a field rather than in the key: one
	// command that can report either kind has to have one shape, or --describe
	// cannot state it and a consumer needs two code paths for one command.
	if userKeys, groupKeys := jsonFieldNames(userOutput), jsonFieldNames(groupOutput); userKeys != groupKeys {
		t.Fatalf("user and group grants published different shapes\nuser:  %s\ngroup: %s", userKeys, groupKeys)
	}

	listOutput := runRepoPermissionCommand(t, "--json", "project", "permissions", "list", "PRJ")
	if !strings.Contains(listOutput, `"subject": "user"`) || !strings.Contains(listOutput, `"displayName": "Alice A"`) {
		t.Fatalf("expected a user listing with display name, got: %s", listOutput)
	}

	groupListOutput := runRepoPermissionCommand(t, "--json", "project", "permissions", "list", "--group", "PRJ")
	if listKeys, groupListKeys := jsonFieldNames(listOutput), jsonFieldNames(groupListOutput); listKeys != groupListKeys {
		t.Fatalf("user and group listings published different shapes\nusers:  %s\ngroups: %s", listKeys, groupListKeys)
	}
}

// jsonFieldNames returns every field name in a document, sorted, so two
// payloads can be compared on shape rather than on values.
func jsonFieldNames(document string) string {
	var decoded any
	if err := json.Unmarshal([]byte(document), &decoded); err != nil {
		return "unparseable: " + err.Error()
	}

	names := map[string]struct{}{}
	var walk func(node any, prefix string)
	walk = func(node any, prefix string) {
		switch typed := node.(type) {
		case map[string]any:
			for key, value := range typed {
				names[prefix+key] = struct{}{}
				walk(value, prefix+key+".")
			}
		case []any:
			for _, item := range typed {
				walk(item, prefix)
			}
		}
	}
	walk(decoded, "")

	collected := make([]string, 0, len(names))
	for name := range names {
		collected = append(collected, name)
	}
	sort.Strings(collected)

	return strings.Join(collected, ",")
}

func TestProjectPermissionAliasHumanOutputLabelsGroups(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newProjectPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	userOutput := runRepoPermissionCommand(t, "project", "permissions", "grant", "PRJ", "alice", "project_write")
	if !strings.Contains(userOutput, "to alice for project PRJ") {
		t.Fatalf("expected a bare user name, got: %s", userOutput)
	}

	groupOutput := runRepoPermissionCommand(t, "project", "permissions", "revoke", "--group", "PRJ", "admins")
	if !strings.Contains(groupOutput, "for group admins on project PRJ") {
		t.Fatalf("expected the group to be named as one, got: %s", groupOutput)
	}
}

func TestProjectPermissionAliasDryRunIntentFollowsTheSubject(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newProjectPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	userPreview := runRepoPermissionCommand(t, "--json", "--dry-run", "project", "permissions", "grant", "PRJ", "alice", "project_write")
	if !strings.Contains(userPreview, `"project.permission.user.grant"`) {
		t.Fatalf("expected user grant intent, got: %s", userPreview)
	}
	if !strings.Contains(userPreview, `"predictedAction": "update"`) {
		t.Fatalf("expected update for a changed permission, got: %s", userPreview)
	}

	groupPreview := runRepoPermissionCommand(t, "--json", "--dry-run", "project", "permissions", "grant", "--group", "PRJ", "admins", "project_admin")
	if !strings.Contains(groupPreview, `"project.permission.group.grant"`) {
		t.Fatalf("expected group grant intent, got: %s", groupPreview)
	}
	if !strings.Contains(groupPreview, `"predictedAction": "no-op"`) {
		t.Fatalf("expected no-op for an unchanged permission, got: %s", groupPreview)
	}

	createPreview := runRepoPermissionCommand(t, "--json", "--dry-run", "project", "permissions", "grant", "PRJ", "bob", "project_read")
	if !strings.Contains(createPreview, `"predictedAction": "create"`) {
		t.Fatalf("expected create for an unknown user, got: %s", createPreview)
	}

	deletePreview := runRepoPermissionCommand(t, "--json", "--dry-run", "project", "permissions", "revoke", "PRJ", "alice")
	if !strings.Contains(deletePreview, `"predictedAction": "delete"`) {
		t.Fatalf("expected delete for an existing entry, got: %s", deletePreview)
	}

	missingPreview := runRepoPermissionCommand(t, "--json", "--dry-run", "project", "permissions", "revoke", "--group", "PRJ", "nobody")
	if !strings.Contains(missingPreview, `"predictedAction": "no-op"`) {
		t.Fatalf("expected no-op for a missing entry, got: %s", missingPreview)
	}

	if len(recorded) != 0 {
		t.Fatalf("a dry run must not mutate, but recorded: %v", recorded)
	}
}

func TestProjectPermissionAliasRejectsBadInvocations(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newProjectPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	cases := [][]string{
		{"--json", "project", "permissions", "list"},
		{"--json", "project", "permissions", "grant", "PRJ", "alice"},
		{"--json", "project", "permissions", "revoke", "PRJ"},
		{"--json", "project", "permissions", "list", "MISSING"},
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

func TestProjectPermissionHumanOutput(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newProjectPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	// List users human mode
	out := runRepoPermissionCommand(t, "project", "permissions", "list", "PRJ")
	if !strings.Contains(out, "Alice A") {
		t.Fatalf("expected Alice A in list output, got:\n%s", out)
	}

	// Grant user human mode
	out = runRepoPermissionCommand(t, "project", "permissions", "grant", "PRJ", "alice", "project_write")
	if !strings.Contains(out, "Granted PROJECT_WRITE") {
		t.Fatalf("expected Granted PROJECT_WRITE in output, got:\n%s", out)
	}

	// Revoke user human mode
	out = runRepoPermissionCommand(t, "project", "permissions", "revoke", "PRJ", "alice")
	if !strings.Contains(out, "Revoked permission") {
		t.Fatalf("expected Revoked permission in output, got:\n%s", out)
	}

	// Dry run human mode
	out = runRepoPermissionCommand(t, "--dry-run", "project", "permissions", "grant", "PRJ", "bob", "project_read")
	if !strings.Contains(out, "Dry-run") {
		t.Fatalf("expected Dry-run in output, got:\n%s", out)
	}
}

// TestProjectPermissionsShowRequiresAWiredChecker is the project-scoped twin of
// the repository test: an unwired dependency is genuinely internal, and now
// says so rather than arriving there by falling through (#475).
func TestProjectPermissionsShowRequiresAWiredChecker(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://127.0.0.1:1")
	t.Setenv("BITBUCKET_TOKEN", "token")

	cfg := config.AppConfig{BitbucketURL: "http://127.0.0.1:1"}
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
