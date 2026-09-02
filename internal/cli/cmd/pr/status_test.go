package prcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
)

type statusBackendStub struct {
	branch  string
	rootErr error
}

func (stub *statusBackendStub) Version(context.Context) (string, error)               { return "", nil }
func (stub *statusBackendStub) Clone(context.Context, string, git.CloneOptions) error { return nil }
func (stub *statusBackendStub) AddRemote(context.Context, string, git.Remote) error   { return nil }
func (stub *statusBackendStub) Fetch(context.Context, string, git.FetchOptions) error { return nil }
func (stub *statusBackendStub) Checkout(context.Context, string, git.CheckoutOptions) error {
	return nil
}
func (stub *statusBackendStub) RepositoryRoot(context.Context, string) (string, error) {
	if stub.rootErr != nil {
		return "", stub.rootErr
	}
	return "/repo", nil
}
func (stub *statusBackendStub) CurrentBranch(context.Context, string) (string, error) {
	return stub.branch, nil
}
func (stub *statusBackendStub) WorkingTreeState(context.Context, string) (git.WorkingTreeStatus, error) {
	return git.WorkingTreeStatus{}, nil
}
func (stub *statusBackendStub) BranchExists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (stub *statusBackendStub) FastForward(context.Context, string, string) error { return nil }
func (stub *statusBackendStub) ListRemotes(context.Context, string) ([]git.Remote, error) {
	return nil, nil
}
func (stub *statusBackendStub) GetConfig(context.Context, git.ConfigOptions) (string, error) {
	return "", nil
}
func (stub *statusBackendStub) SetConfig(context.Context, git.ConfigOptions) error   { return nil }
func (stub *statusBackendStub) UnsetConfig(context.Context, git.ConfigOptions) error { return nil }

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

func executeStatus(t *testing.T, backend git.Backend, serverURL string, args ...string) string {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var jsonFlag bool
	deps := Dependencies{
		JSONEnabled: func() bool { return jsonFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		GitBackend:    func() git.Backend { return backend },
	}

	command := testPrCommand(deps)
	buffer := &bytes.Buffer{}
	command.SetOut(buffer)
	command.SetErr(buffer)

	fullArgs := append([]string{"pr", "status"}, args...)
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		}
	}
	command.SetArgs(fullArgs)
	if err := command.Execute(); err != nil {
		t.Fatalf("%v failed: %v\noutput: %s", fullArgs, err, buffer.String())
	}

	return buffer.String()
}

func decodeStatusOutput(t *testing.T, output string) map[string]any {
	t.Helper()

	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("pr status output is not JSON: %v\noutput: %s", err, output)
	}

	payload, ok := envelope["data"].(map[string]any)
	if !ok {
		// Bare JSON fallback
		return envelope
	}
	return payload
}

func TestPullRequestStatusThreeSections(t *testing.T) {
	server := newPullRequestStatusServer(t)
	backend := &statusBackendStub{branch: "feature/x"}

	output := executeStatus(t, backend, server.URL)

	wantHeadings := []string{
		"Current branch (feature/x)",
		"Created by you",
		"Requesting a code review from you",
	}
	for _, heading := range wantHeadings {
		if !strings.Contains(output, heading) {
			t.Fatalf("expected heading %q in output:\n%s", heading, output)
		}
	}
	if !strings.Contains(output, "#31\tOPEN\tOn this branch") {
		t.Fatalf("expected current-branch PR in output:\n%s", output)
	}
	if !strings.Contains(output, "#11\tOPEN\t[PRJ/demo] Mine") {
		t.Fatalf("expected created PR in output:\n%s", output)
	}
	if !strings.Contains(output, "#21\tOPEN\tWaiting on me") {
		t.Fatalf("expected review-requested PR in output:\n%s", output)
	}
}

func TestPullRequestStatusJSONShape(t *testing.T) {
	server := newPullRequestStatusServer(t)
	backend := &statusBackendStub{branch: "feature/x"}

	output := executeStatus(t, backend, server.URL, "--json")
	payload := decodeStatusOutput(t, output)

	currentBranch, ok := payload["currentBranch"].(map[string]any)
	if !ok {
		t.Fatalf("expected currentBranch section, got %#v", payload)
	}
	if currentBranch["branch"] != "feature/x" {
		t.Fatalf("expected branch feature/x, got %v", currentBranch["branch"])
	}

	created, ok := payload["createdByYou"].(map[string]any)
	if !ok {
		t.Fatalf("expected createdByYou section, got %#v", payload)
	}
	createdPRs, _ := created["pullRequests"].([]any)
	if len(createdPRs) != 1 {
		t.Fatalf("expected 1 created PR, got %v", createdPRs)
	}

	reviewing, ok := payload["requestingYourReview"].(map[string]any)
	if !ok {
		t.Fatalf("expected requestingYourReview section, got %#v", payload)
	}
	reviewingPRs, _ := reviewing["pullRequests"].([]any)
	if len(reviewingPRs) != 1 {
		t.Fatalf("expected 1 reviewing PR, got %v", reviewingPRs)
	}
}

func TestPullRequestStatusNotOnBranchNotesSection(t *testing.T) {
	server := newPullRequestStatusServer(t)
	backend := &statusBackendStub{branch: ""}

	output := executeStatus(t, backend, server.URL, "--json")
	payload := decodeStatusOutput(t, output)

	currentBranch, ok := payload["currentBranch"].(map[string]any)
	if !ok {
		t.Fatalf("expected currentBranch section, got %#v", payload)
	}
	note, _ := currentBranch["note"].(string)
	if !strings.Contains(note, "not on a branch") {
		t.Fatalf("expected note about not being on a branch, got %q", note)
	}
}
