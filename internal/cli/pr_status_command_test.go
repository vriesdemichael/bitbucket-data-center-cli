package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
)

// newPullRequestStatusServer serves the two endpoints bb pr status reads: the
// cross-repository dashboard, filtered by role, and the per-repository listing
// the current-branch section uses.
func newPullRequestStatusServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.URL.Path == "/rest/api/1.0/dashboard/pull-requests":
			switch request.URL.Query().Get("role") {
			case "AUTHOR":
				_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"id":11,"title":"Mine","state":"OPEN","open":true,"toRef":{"repository":{"slug":"demo","project":{"key":"PRJ"}}}}]}`))
			case "REVIEWER":
				// The fixture narrows the way Bitbucket does, so a command that
				// forgets to ask for participantStatus gets the superset back and
				// the assertions below catch it.
				switch request.URL.Query().Get("participantStatus") {
				case "UNAPPROVED":
					_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"id":21,"title":"Waiting on me","state":"OPEN","open":true}]}`))
				default:
					_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[
						{"id":21,"title":"Waiting on me","state":"OPEN","open":true},
						{"id":22,"title":"Approved by me","state":"OPEN","open":true},
						{"id":23,"title":"Marked needs work by me","state":"OPEN","open":true}
					]}`))
				}
			default:
				_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[]}`))
			}
		case request.URL.Path == "/rest/api/latest/projects/PRJ/repos/demo/pull-requests",
			request.URL.Path == "/rest/api/1.0/projects/PRJ/repos/demo/pull-requests":
			_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[{"id":31,"title":"On this branch","state":"OPEN","open":true,"fromRef":{"displayId":"feature/x"},"toRef":{"displayId":"main"}}]}`))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

func configurePullRequestStatusEnv(t *testing.T, serverURL string, username string) {
	t.Helper()
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")
	t.Setenv("BITBUCKET_USERNAME", username)
	t.Setenv("BITBUCKET_PASSWORD", "")
}

func withGitBackend(t *testing.T, backend git.Backend) {
	t.Helper()
	original := gitBackendFactory
	gitBackendFactory = func() git.Backend { return backend }
	t.Cleanup(func() { gitBackendFactory = original })
}

func executePullRequestStatus(t *testing.T, args ...string) string {
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

// decodePullRequestStatus returns the data half of the bb.machine envelope,
// which is where the three sections live.
func decodePullRequestStatus(t *testing.T, output string) map[string]any {
	t.Helper()

	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("pr status output is not JSON: %v\noutput: %s", err, output)
	}

	payload, ok := envelope["data"].(map[string]any)
	if !ok {
		t.Fatalf("expected an enveloped data object, got: %s", output)
	}

	return payload
}

func statusSection(t *testing.T, payload map[string]any, key string) map[string]any {
	t.Helper()

	section, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("expected a %q section, got: %v", key, payload)
	}

	return section
}

func statusPullRequestIDs(t *testing.T, section map[string]any) []float64 {
	t.Helper()

	raw, ok := section["pullRequests"].([]any)
	if !ok {
		t.Fatalf("expected pull_requests in section, got: %v", section)
	}

	ids := make([]float64, 0, len(raw))
	for _, item := range raw {
		pullRequest, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("expected a pull request object, got: %v", item)
		}
		id, ok := pullRequest["id"].(float64)
		if !ok {
			t.Fatalf("expected a numeric id, got: %v", pullRequest)
		}
		ids = append(ids, id)
	}

	return ids
}

func TestPullRequestStatusReportsAllThreeSections(t *testing.T) {
	server := newPullRequestStatusServer(t)
	configurePullRequestStatusEnv(t, server.URL, "me")
	withGitBackend(t, inferenceGitBackendStub{repoRoot: "/repo", branch: "feature/x"})

	payload := decodePullRequestStatus(t, executePullRequestStatus(t, "--json", "pr", "status"))

	currentBranch := statusSection(t, payload, "currentBranch")
	if currentBranch["branch"] != "feature/x" {
		t.Fatalf("expected the checked-out branch to be reported, got: %v", currentBranch)
	}
	if currentBranch["repository"] != "PRJ/demo" {
		t.Fatalf("expected the resolved repository to be reported, got: %v", currentBranch)
	}
	if ids := statusPullRequestIDs(t, currentBranch); len(ids) != 1 || ids[0] != 31 {
		t.Fatalf("expected the branch pull request, got: %v", ids)
	}

	if ids := statusPullRequestIDs(t, statusSection(t, payload, "createdByYou")); len(ids) != 1 || ids[0] != 11 {
		t.Fatalf("expected the authored pull request, got: %v", ids)
	}
}

// TestPullRequestStatusAsksOnlyForReviewsNotYetGiven is the assertion behind
// the section's name.
//
// role=REVIEWER alone means "you are a reviewer", which keeps listing pull
// requests you already approved or already sent back as needs-work. The
// narrowing is participantStatus=UNAPPROVED, and it has to be asked for: the
// dashboard's default is every status.
func TestPullRequestStatusAsksOnlyForReviewsNotYetGiven(t *testing.T) {
	server := newPullRequestStatusServer(t)
	configurePullRequestStatusEnv(t, server.URL, "me")
	withGitBackend(t, inferenceGitBackendStub{repoRoot: "/repo", branch: "feature/x"})

	payload := decodePullRequestStatus(t, executePullRequestStatus(t, "--json", "pr", "status"))

	reviewing := statusSection(t, payload, "requestingYourReview")
	ids := statusPullRequestIDs(t, reviewing)
	if len(ids) != 1 || ids[0] != 21 {
		t.Fatalf("expected only the pull request still waiting on me, got: %v", ids)
	}
	if reviewing["note"] != nil {
		t.Fatalf("expected no note on a section the server narrowed, got: %v", reviewing["note"])
	}
}

// TestPullRequestStatusDegradesOutsideARepository is the reason the current
// branch is a section rather than a precondition: the other two answers are
// still available, and failing the whole command would make bb pr status
// unusable anywhere but a checkout.
func TestPullRequestStatusDegradesOutsideARepository(t *testing.T) {
	cases := []struct {
		name         string
		backend      git.Backend
		wantedInNote string
	}{
		{
			name:         "detached head",
			backend:      inferenceGitBackendStub{repoRoot: "/repo", branch: ""},
			wantedInNote: "not on a branch",
		},
		{
			name:         "not a git repository",
			backend:      inferenceGitBackendStub{rootErr: errors.New("fatal: not a git repository (or any of the parent directories): .git")},
			wantedInNote: "not on a branch",
		},
		{
			name:         "branch lookup failed",
			backend:      inferenceGitBackendStub{repoRoot: "/repo", branchErr: errors.New("git exploded")},
			wantedInNote: "could not read the current branch",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := newPullRequestStatusServer(t)
			configurePullRequestStatusEnv(t, server.URL, "me")
			withGitBackend(t, testCase.backend)

			payload := decodePullRequestStatus(t, executePullRequestStatus(t, "--json", "pr", "status"))

			currentBranch := statusSection(t, payload, "currentBranch")
			note, _ := currentBranch["note"].(string)
			if !strings.Contains(note, testCase.wantedInNote) {
				t.Fatalf("expected a note containing %q, got: %q", testCase.wantedInNote, note)
			}
			if ids := statusPullRequestIDs(t, currentBranch); len(ids) != 0 {
				t.Fatalf("expected no branch pull requests, got: %v", ids)
			}

			// The sections that do not need a checkout must still answer.
			if ids := statusPullRequestIDs(t, statusSection(t, payload, "createdByYou")); len(ids) != 1 {
				t.Fatalf("expected the authored section to survive, got: %v", ids)
			}
		})
	}
}

func TestPullRequestStatusReportsMissingRepositoryContext(t *testing.T) {
	server := newPullRequestStatusServer(t)
	configurePullRequestStatusEnv(t, server.URL, "me")
	t.Setenv("BITBUCKET_PROJECT_KEY", "")
	t.Setenv("BITBUCKET_REPO_SLUG", "")
	withGitBackend(t, inferenceGitBackendStub{repoRoot: "/repo", branch: "feature/x"})

	payload := decodePullRequestStatus(t, executePullRequestStatus(t, "--json", "pr", "status"))

	note, _ := statusSection(t, payload, "currentBranch")["note"].(string)
	if !strings.Contains(note, "no repository context") {
		t.Fatalf("expected a note about the missing repository context, got: %q", note)
	}
}

func TestPullRequestStatusHumanOutput(t *testing.T) {
	server := newPullRequestStatusServer(t)
	configurePullRequestStatusEnv(t, server.URL, "me")
	withGitBackend(t, inferenceGitBackendStub{repoRoot: "/repo", branch: "feature/x"})

	output := executePullRequestStatus(t, "pr", "status")

	for _, expected := range []string{
		"Current branch (feature/x)",
		"Created by you",
		"Requesting a code review from you",
		"#31",
		"#11",
		"#21",
	} {
		if !strings.Contains(output, expected) {
			t.Fatalf("expected %q in pr status output, got:\n%s", expected, output)
		}
	}

	// The ones already responded to are outside participantStatus=UNAPPROVED.
	for _, unexpected := range []string{"#22", "#23"} {
		if strings.Contains(output, unexpected) {
			t.Fatalf("expected %s to be outside the review section, got:\n%s", unexpected, output)
		}
	}
}

// TestPullRequestStatusFailsWhenTheDashboardFails draws the line the other way
// from the current-branch section: the two dashboard queries are the command's
// reason to exist, so a failure there is a failure, not a note.
// mock-inventory: transport-fault — the dashboard is made to fail; the subject is that pr status reports it rather than printing an empty board.
func TestPullRequestStatusFailsWhenTheDashboardFails(t *testing.T) {
	cases := []struct {
		name        string
		failingRole string
	}{
		{name: "author query", failingRole: "AUTHOR"},
		{name: "reviewer query", failingRole: "REVIEWER"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				if request.URL.Path == "/rest/api/1.0/dashboard/pull-requests" && request.URL.Query().Get("role") == testCase.failingRole {
					writer.WriteHeader(http.StatusInternalServerError)
					_, _ = writer.Write([]byte(`{"errors":[{"message":"boom"}]}`))
					return
				}
				_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[]}`))
			}))
			t.Cleanup(server.Close)

			configurePullRequestStatusEnv(t, server.URL, "me")
			withGitBackend(t, inferenceGitBackendStub{repoRoot: "/repo", branch: "feature/x"})

			command := NewRootCommand()
			buffer := &bytes.Buffer{}
			command.SetOut(buffer)
			command.SetErr(buffer)
			command.SetArgs([]string{"--json", "pr", "status"})
			if err := command.Execute(); err == nil {
				t.Fatalf("expected a failing %s query to fail the command, got: %s", testCase.failingRole, buffer.String())
			}
		})
	}
}

// TestPullRequestStatusNotesABranchListingFailure keeps a per-repository
// failure inside its section: the branch may be one Bitbucket has never heard
// of, which says nothing about the other two answers.
// mock-inventory: transport-fault — the branch listing is made to fail; the subject is that the section says so rather than looking empty.
func TestPullRequestStatusNotesABranchListingFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.Contains(request.URL.Path, "/repos/demo/pull-requests") {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(`{"errors":[{"message":"boom"}]}`))
			return
		}
		_, _ = writer.Write([]byte(`{"isLastPage":true,"values":[]}`))
	}))
	t.Cleanup(server.Close)

	configurePullRequestStatusEnv(t, server.URL, "me")
	withGitBackend(t, inferenceGitBackendStub{repoRoot: "/repo", branch: "feature/x"})

	payload := decodePullRequestStatus(t, executePullRequestStatus(t, "--json", "pr", "status"))
	note, _ := statusSection(t, payload, "currentBranch")["note"].(string)
	if !strings.Contains(note, "could not list pull requests for feature/x") {
		t.Fatalf("expected the listing failure to be reported as a note, got: %q", note)
	}
}

func TestPullRequestStatusWithoutAGitBackend(t *testing.T) {
	server := newPullRequestStatusServer(t)
	configurePullRequestStatusEnv(t, server.URL, "me")
	withGitBackend(t, nil)

	payload := decodePullRequestStatus(t, executePullRequestStatus(t, "--json", "pr", "status"))
	note, _ := statusSection(t, payload, "currentBranch")["note"].(string)
	if !strings.Contains(note, "not on a branch") {
		t.Fatalf("expected a note when there is no git backend at all, got: %q", note)
	}
}

// TestPullRequestStatusReportsEmptySections is live now, in
// TestLivePullRequestStatus, as a user created a moment earlier who
// has authored nothing and been asked to review nothing. The unit version
// answered every dashboard query with an empty page, which is also what a
// broken query looks like.
