package cli

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
)

// TestPullRequestCheckoutSurfacesBackendFailures walks every point at which git
// can refuse partway through.
//
// Each of these leaves the repository in some intermediate state — a remote
// added but not fetched, a branch created but not tracked — so the requirement
// is that the caller is told rather than left to discover it. Silently
// continuing past any of them would produce a checkout that looks complete and
// is not.
func TestPullRequestCheckoutSurfacesBackendFailures(t *testing.T) {
	cases := []struct {
		name    string
		failing string
		fork    bool
	}{
		{name: "listing remotes", failing: "list-remotes"},
		{name: "reading the working tree", failing: "working-tree-state"},
		{name: "looking for the local branch", failing: "branch-exists"},
		{name: "adding the fork remote", failing: "add-remote", fork: true},
		{name: "fetching the source branch", failing: "fetch"},
		{name: "checking out", failing: "checkout"},
		{name: "setting the upstream", failing: "set-config"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sourceProject := "PRJ"
			if testCase.fork {
				sourceProject = "~jdoe"
			}
			server := newCheckoutServer(t, sourceProject, "demo", "feature/login")
			configureCheckoutEnv(t, server.URL)

			stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
			stub.failOn = map[string]error{testCase.failing: errors.New("git said no")}
			withGitBackend(t, stub)

			err := runCheckoutExpectingError(t, "pr", "checkout", "42")
			if !strings.Contains(err.Error(), "git said no") {
				t.Fatalf("expected the git failure to surface, got: %v", err)
			}
		})
	}
}

// newIncompletePullRequestServer returns a pull request payload missing one of
// the two refs a checkout depends on.
func newIncompletePullRequestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if !strings.Contains(request.URL.Path, "/repos/demo/pull-requests/42") {
			http.NotFound(writer, request)
			return
		}
		_, _ = fmt.Fprint(writer, body)
	}))
	t.Cleanup(server.Close)

	return server
}

// TestPullRequestCheckoutRefusesIncompletePayloads is about not guessing.
//
// Bitbucket always reports both refs, so a payload without them is one this
// code does not understand. Defaulting to the target repository would fetch
// from the wrong place and check out a branch that is not the pull request —
// a wrong answer dressed as a right one.
func TestPullRequestCheckoutRefusesIncompletePayloads(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name: "no source branch",
			body: `{"id":42,"title":"t","state":"OPEN","open":true,
				"fromRef":{"repository":{"slug":"demo","project":{"key":"PRJ"}}},
				"toRef":{"displayId":"main","repository":{"slug":"demo","project":{"key":"PRJ"}}}}`,
			expected: "does not report a source branch",
		},
		{
			name: "no source repository",
			body: `{"id":42,"title":"t","state":"OPEN","open":true,
				"fromRef":{"id":"refs/heads/feature/x","displayId":"feature/x"},
				"toRef":{"displayId":"main","repository":{"slug":"demo","project":{"key":"PRJ"}}}}`,
			expected: "does not report a source repository",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newIncompletePullRequestServer(t, testCase.body)
			configureCheckoutEnv(t, server.URL)

			stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
			withGitBackend(t, stub)

			err := runCheckoutExpectingError(t, "pr", "checkout", "42")
			if !strings.Contains(err.Error(), testCase.expected) {
				t.Fatalf("expected %q in the error, got: %v", testCase.expected, err)
			}
			if len(stub.calls) != 0 {
				t.Fatalf("expected nothing to have been attempted, got calls: %v", stub.calls)
			}
		})
	}
}

// TestPullRequestCheckoutForkWithoutAnOwnerName covers a project key that is
// nothing but the personal-fork marker. Prefixing with an empty owner would
// produce a branch name starting with a slash, which git rejects.
func TestPullRequestCheckoutForkWithoutAnOwnerName(t *testing.T) {
	server := newCheckoutServer(t, "~", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	withGitBackend(t, stub)

	result := decodeCheckoutResult(t, runCheckout(t, "--json", "pr", "checkout", "42"))

	if result["branch"] != "feature/login" {
		t.Fatalf("expected an unprefixed branch name, got: %v", result)
	}
	if result["remote"] != "fork" {
		t.Fatalf("expected a fallback remote name, got: %v", result)
	}
}

// TestPullRequestCheckoutTruncatesALongDirtyList keeps the refusal readable
// when the whole tree is modified.
func TestPullRequestCheckoutTruncatesALongDirtyList(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	entries := make([]string, 0, 9)
	for index := range 9 {
		entries = append(entries, fmt.Sprintf(" M file-%d.go", index))
	}

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	stub.status = git.WorkingTreeStatus{Dirty: true, Entries: entries}
	withGitBackend(t, stub)

	err := runCheckoutExpectingError(t, "pr", "checkout", "42")
	if !strings.Contains(err.Error(), "and 4 more") {
		t.Fatalf("expected the list to be truncated with a count, got: %v", err)
	}
	if strings.Contains(err.Error(), "file-8.go") {
		t.Fatalf("expected the tail to be omitted, got: %v", err)
	}
}

// TestPullRequestCheckoutHumanOutputVariants covers the three shapes the
// success line takes. They are not cosmetic: "Updated" versus "Checked out"
// is how a caller knows whether the branch already existed, and the detached
// line has to name the ref rather than a branch that was never created.
func TestPullRequestCheckoutHumanOutputVariants(t *testing.T) {
	t.Run("detached", func(t *testing.T) {
		server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
		configureCheckoutEnv(t, server.URL)
		withGitBackend(t, newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"}))

		output := runCheckout(t, "pr", "checkout", "42", "--detach")
		if !strings.Contains(output, "Checked out #42 at origin/feature/login (detached HEAD)") {
			t.Fatalf("expected a detached-HEAD line, got:\n%s", output)
		}
	})

	t.Run("existing branch", func(t *testing.T) {
		server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
		configureCheckoutEnv(t, server.URL)
		stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
		stub.branches["feature/login"] = true
		withGitBackend(t, stub)

		output := runCheckout(t, "pr", "checkout", "42")
		if !strings.Contains(output, "Updated #42 on branch feature/login") {
			t.Fatalf("expected the update wording for an existing branch, got:\n%s", output)
		}
	})
}

// TestPullRequestCheckoutWithoutAGitBackend covers the guard that would
// otherwise be a nil dereference.
func TestPullRequestCheckoutWithoutAGitBackend(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)
	withGitBackend(t, nil)

	err := runCheckoutExpectingError(t, "pr", "checkout", "42")
	if !strings.Contains(err.Error(), "no git backend") {
		t.Fatalf("expected a missing-backend error, got: %v", err)
	}
}

// TestPullRequestCheckoutSuppliesCredentialsToTheFetch is a regression guard
// for a real CI failure.
//
// The first version left authentication to git, on the reasoning that ADR-044
// makes the credential helper the mechanism for an existing repository. But a
// repository cloned by bb deliberately holds no credential, so `bb repo clone`
// followed by `bb pr checkout` stopped to prompt for a username — breaking the
// exact two-command sequence this command exists to serve, and failing the live
// suite with "could not read Username".
func TestPullRequestCheckoutSuppliesCredentialsToTheFetch(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	withGitBackend(t, stub)

	runCheckout(t, "--json", "pr", "checkout", "42")

	if len(stub.fetches) != 1 {
		t.Fatalf("expected one fetch, got %d", len(stub.fetches))
	}
	credentials := stub.fetches[0].Credentials
	if credentials == nil {
		t.Fatal("expected the fetch to carry credentials")
	}
	if credentials.Token != "test-token" {
		t.Fatalf("expected the configured token, got %q", credentials.Token)
	}
	// The URL is what scopes the header to one host. Without it the token would
	// be attached to every request git makes from this repository.
	if !strings.Contains(credentials.URL, "/scm/PRJ/demo.git") {
		t.Fatalf("expected credentials scoped to the remote URL, got %q", credentials.URL)
	}
}

// TestPullRequestCheckoutWithoutCredentialsLeavesItToGit covers the other side:
// with nothing configured, the fetch carries nothing and git falls back to
// whatever helper the user has.
func TestPullRequestCheckoutWithoutCredentialsLeavesItToGit(t *testing.T) {
	server := newCheckoutServer(t, "PRJ", "demo", "feature/login")
	configureCheckoutEnv(t, server.URL)
	t.Setenv("BITBUCKET_TOKEN", "")

	stub := newCheckoutBackendStub(git.Remote{Name: "origin", URL: server.URL + "/scm/PRJ/demo.git"})
	withGitBackend(t, stub)

	runCheckout(t, "--json", "pr", "checkout", "42")

	if len(stub.fetches) != 1 {
		t.Fatalf("expected one fetch, got %d", len(stub.fetches))
	}
	if stub.fetches[0].Credentials != nil {
		t.Fatalf("expected no credentials, got %#v", stub.fetches[0].Credentials)
	}
}
