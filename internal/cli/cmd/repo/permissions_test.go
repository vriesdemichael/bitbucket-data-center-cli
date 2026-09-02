package repocmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newRepoPermissionTestServer serves the four repository permission endpoints
// the commands touch, and records the mutating calls so a test can assert which
// subject a shallow invocation resolved to.
func newRepoPermissionTestServer(t *testing.T, recorded *[]string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		// The REPO_ADMIN check a dry run runs before predicting anything.
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/repos":
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"slug":"demo","name":"demo","project":{"key":"PRJ"}}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/users":
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"user":{"name":"alice","displayName":"Alice A"},"permission":"REPO_READ"}]}`))
		case request.Method == http.MethodGet && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/groups":
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"group":{"name":"admins"},"permission":"REPO_ADMIN"}]}`))
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/users":
			*recorded = append(*recorded, "PUT users")
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodPut && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/groups":
			*recorded = append(*recorded, "PUT groups")
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/users":
			*recorded = append(*recorded, "DELETE users")
			writer.WriteHeader(http.StatusNoContent)
		case request.Method == http.MethodDelete && request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/permissions/groups":
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

	command := NewRootCommand()
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)
	command.SetArgs(args)
	if err := command.Execute(); err != nil {
		t.Fatalf("%v failed: %v\noutput: %s", args, err, buffer.String())
	}

	return buffer.String()
}

// TestRepoPermissionShallowAliasesMatchDeepPaths is the assertion that makes
// `bb repo permissions grant` an alias rather than a second implementation.
//
// Byte equality, not "looks similar": if the two spellings ever stop producing
// the same output this fails, which is the whole reason the shared constructors
// exist.
func TestRepoPermissionShallowAliasesMatchDeepPaths(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newRepoPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	cases := []struct {
		name    string
		deep    []string
		shallow []string
	}{
		{
			name:    "users list",
			deep:    []string{"--json", "repo", "settings", "security", "permissions", "users", "list", "--repo", "PRJ/demo"},
			shallow: []string{"--json", "repo", "permissions", "list", "--repo", "PRJ/demo"},
		},
		{
			name:    "groups list",
			deep:    []string{"--json", "repo", "settings", "security", "permissions", "groups", "list", "--repo", "PRJ/demo"},
			shallow: []string{"--json", "repo", "permissions", "list", "--group", "--repo", "PRJ/demo"},
		},
		{
			name:    "users grant",
			deep:    []string{"--json", "repo", "settings", "security", "permissions", "users", "grant", "alice", "repo_write", "--repo", "PRJ/demo"},
			shallow: []string{"--json", "repo", "permissions", "grant", "alice", "repo_write", "--repo", "PRJ/demo"},
		},
		{
			name:    "groups grant",
			deep:    []string{"--json", "repo", "settings", "security", "permissions", "groups", "grant", "admins", "repo_admin", "--repo", "PRJ/demo"},
			shallow: []string{"--json", "repo", "permissions", "grant", "--group", "admins", "repo_admin", "--repo", "PRJ/demo"},
		},
		{
			name:    "users revoke",
			deep:    []string{"--json", "repo", "settings", "security", "permissions", "users", "revoke", "alice", "--repo", "PRJ/demo"},
			shallow: []string{"--json", "repo", "permissions", "revoke", "alice", "--repo", "PRJ/demo"},
		},
		{
			name:    "groups revoke",
			deep:    []string{"--json", "repo", "settings", "security", "permissions", "groups", "revoke", "admins", "--repo", "PRJ/demo"},
			shallow: []string{"--json", "repo", "permissions", "revoke", "--group", "admins", "--repo", "PRJ/demo"},
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

// TestRepoPermissionAliasGroupFlagPicksTheSubject pins the trade the shallow
// spelling makes: without --group it is the user endpoint, with it the group
// one. A flag that silently did nothing would grant the wrong principal.
func TestRepoPermissionAliasGroupFlagPicksTheSubject(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newRepoPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	runRepoPermissionCommand(t, "--json", "repo", "permissions", "grant", "alice", "repo_write", "--repo", "PRJ/demo")
	runRepoPermissionCommand(t, "--json", "repo", "permissions", "grant", "--group", "admins", "repo_admin", "--repo", "PRJ/demo")
	runRepoPermissionCommand(t, "--json", "repo", "permissions", "revoke", "alice", "--repo", "PRJ/demo")
	runRepoPermissionCommand(t, "--json", "repo", "permissions", "revoke", "--group", "admins", "--repo", "PRJ/demo")

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

// TestRepoPermissionAliasJSONNamesTheSubject pins the machine contract: the
// subject is a field and the holder is always "name", so one command reports
// one shape whichever subject --group resolved to.
func TestRepoPermissionAliasJSONNamesTheSubject(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newRepoPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	userOutput := runRepoPermissionCommand(t, "--json", "repo", "permissions", "grant", "alice", "repo_write", "--repo", "PRJ/demo")
	if !strings.Contains(userOutput, `"subject": "user"`) || !strings.Contains(userOutput, `"name": "alice"`) {
		t.Fatalf("expected subject and name for a user grant, got: %s", userOutput)
	}

	groupOutput := runRepoPermissionCommand(t, "--json", "repo", "permissions", "grant", "--group", "admins", "repo_admin", "--repo", "PRJ/demo")
	if !strings.Contains(groupOutput, `"subject": "group"`) || !strings.Contains(groupOutput, `"name": "admins"`) {
		t.Fatalf("expected subject and name for a group grant, got: %s", groupOutput)
	}

	listOutput := runRepoPermissionCommand(t, "--json", "repo", "permissions", "list", "--repo", "PRJ/demo")
	if !strings.Contains(listOutput, `"subject": "user"`) || !strings.Contains(listOutput, `"displayName": "Alice A"`) {
		t.Fatalf("expected a user listing with display name, got: %s", listOutput)
	}

	groupListOutput := runRepoPermissionCommand(t, "--json", "repo", "permissions", "list", "--group", "--repo", "PRJ/demo")
	if !strings.Contains(groupListOutput, `"subject": "group"`) || !strings.Contains(groupListOutput, `"admins"`) {
		t.Fatalf("expected a group listing, got: %s", groupListOutput)
	}
}

// TestRepoPermissionAliasHumanOutputLabelsGroups covers the one cosmetic
// difference between the subjects: a group is named as one, a user is not.
func TestRepoPermissionAliasHumanOutputLabelsGroups(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newRepoPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	userOutput := runRepoPermissionCommand(t, "repo", "permissions", "grant", "alice", "repo_write", "--repo", "PRJ/demo")
	if !strings.Contains(userOutput, "to alice") || strings.Contains(userOutput, "to group") {
		t.Fatalf("expected a bare user name, got: %s", userOutput)
	}

	groupOutput := runRepoPermissionCommand(t, "repo", "permissions", "revoke", "--group", "admins", "--repo", "PRJ/demo")
	if !strings.Contains(groupOutput, "for group admins") {
		t.Fatalf("expected the group to be named as one, got: %s", groupOutput)
	}
}

// TestRepoPermissionAliasDryRunIntentFollowsTheSubject checks that the dry-run
// plan reports the principal it would actually touch. The dryRunProfiles entry
// for the alias records the user intent as its nominal one, so a --group dry
// run reporting repo.permission.user.grant would be a plausible bug.
func TestRepoPermissionAliasDryRunIntentFollowsTheSubject(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newRepoPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	userPreview := runRepoPermissionCommand(t, "--json", "--dry-run", "repo", "permissions", "grant", "alice", "repo_write", "--repo", "PRJ/demo")
	if !strings.Contains(userPreview, `"repo.permission.user.grant"`) {
		t.Fatalf("expected user grant intent, got: %s", userPreview)
	}

	groupPreview := runRepoPermissionCommand(t, "--json", "--dry-run", "repo", "permissions", "grant", "--group", "admins", "repo_admin", "--repo", "PRJ/demo")
	if !strings.Contains(groupPreview, `"repo.permission.group.grant"`) {
		t.Fatalf("expected group grant intent, got: %s", groupPreview)
	}
	if !strings.Contains(groupPreview, `"subject": "group"`) || !strings.Contains(groupPreview, `"name": "admins"`) {
		t.Fatalf("expected the group named in the dry-run target, got: %s", groupPreview)
	}

	if len(recorded) != 0 {
		t.Fatalf("a dry run must not mutate, but recorded: %v", recorded)
	}
}

// TestRepoPermissionAliasPredictsExistingEntries exercises the create/update/
// no-op branches of the dry-run prediction through the shared code path.
func TestRepoPermissionAliasPredictsExistingEntries(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	recorded := []string{}
	server := newRepoPermissionTestServer(t, &recorded)
	t.Setenv("BITBUCKET_URL", server.URL)

	// alice already holds REPO_READ in the fixture.
	noop := runRepoPermissionCommand(t, "--json", "--dry-run", "repo", "permissions", "grant", "alice", "repo_read", "--repo", "PRJ/demo")
	if !strings.Contains(noop, `"predictedAction": "no-op"`) {
		t.Fatalf("expected no-op for an unchanged permission, got: %s", noop)
	}

	update := runRepoPermissionCommand(t, "--json", "--dry-run", "repo", "permissions", "grant", "alice", "repo_admin", "--repo", "PRJ/demo")
	if !strings.Contains(update, `"predictedAction": "update"`) {
		t.Fatalf("expected update for a changed permission, got: %s", update)
	}

	create := runRepoPermissionCommand(t, "--json", "--dry-run", "repo", "permissions", "grant", "bob", "repo_read", "--repo", "PRJ/demo")
	if !strings.Contains(create, `"predictedAction": "create"`) {
		t.Fatalf("expected create for an unknown user, got: %s", create)
	}

	deletePreview := runRepoPermissionCommand(t, "--json", "--dry-run", "repo", "permissions", "revoke", "alice", "--repo", "PRJ/demo")
	if !strings.Contains(deletePreview, `"predictedAction": "delete"`) {
		t.Fatalf("expected delete for an existing entry, got: %s", deletePreview)
	}

	missingPreview := runRepoPermissionCommand(t, "--json", "--dry-run", "repo", "permissions", "revoke", "bob", "--repo", "PRJ/demo")
	if !strings.Contains(missingPreview, `"predictedAction": "no-op"`) {
		t.Fatalf("expected no-op for a missing entry, got: %s", missingPreview)
	}
}

// TestRepoPermissionAliasRejectsBadInvocations covers the error branches the
// shared constructors added.
func TestRepoPermissionAliasRejectsBadInvocations(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	t.Setenv("BITBUCKET_REPO_SLUG", "")
	recorded := []string{}
	server := newRepoPermissionTestServer(t, &recorded)
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
