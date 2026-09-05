package prcmd

import (
	"bytes"
	"context"
	"net/http/httptest"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

type testChecker struct{}

func (testChecker) CheckRepoPermission(ctx context.Context, projectKey, repoSlug string, permission openapigenerated.GetRepositories1ParamsPermission) error {
	return nil
}
func executePr(t *testing.T, serverURL string, args ...string) (string, error) {
	t.Helper()

	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", serverURL)
	t.Setenv("BITBUCKET_PROJECT_KEY", "PRJ")
	t.Setenv("BITBUCKET_REPO_SLUG", "demo")
	t.Setenv("BITBUCKET_TOKEN", "test-token")

	var jsonFlag bool
	var dryRunFlag bool
	filteredArgs := make([]string, 0, len(args))
	for _, a := range args {
		if a == "--json" {
			jsonFlag = true
		} else if a == "--dry-run" {
			dryRunFlag = true
		} else {
			filteredArgs = append(filteredArgs, a)
		}
	}

	deps := Dependencies{
		JSONEnabled:   func() bool { return jsonFlag },
		DryRunEnabled: func() bool { return dryRunFlag },
		LoadConfig: func() (config.AppConfig, error) {
			return config.LoadFromEnv()
		},
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			cfg, err := config.LoadFromEnv()
			if err != nil {
				return config.AppConfig{}, nil, err
			}
			client, err := openapi.NewClientWithResponsesFromConfig(cfg)
			return cfg, client, err
		},
		WriteJSON:     jsonoutput.Write,
		WriteJSONList: jsonoutput.WriteList,
		PermissionChecker: func(client *openapigenerated.ClientWithResponses) PermissionChecker {
			return testChecker{}
		},
	}

	root := New(deps)
	buffer := &bytes.Buffer{}
	root.SetOut(buffer)
	root.SetErr(buffer)

	root.SetArgs(filteredArgs)
	err := root.Execute()
	return buffer.String(), err
}

func TestPRDefaultDependencies(t *testing.T) {
	cmd := New(Dependencies{})
	if cmd == nil {
		t.Fatal("expected New to return command with default dependencies")
	}
	checker := nopPermissionChecker{}
	if err := checker.CheckRepoPermission(context.Background(), "PRJ", "demo", openapi.RepoRead); err != nil {
		t.Fatalf("expected nop checker to return nil error: %v", err)
	}
}

// Both spellings are refused before a request exists, so a listener here
// could only hide the refusal not happening.
func TestPRValidationErrors(t *testing.T) {
	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)

	// --unresolved with conflicting --state
	_, err := executePr(t, server.URL, "comment", "list", "42", "--unresolved", "--state", "resolved")
	if err == nil {
		t.Fatalf("expected error on --unresolved with --state resolved")
	}

	// invalid state
	_, err = executePr(t, server.URL, "comment", "list", "42", "--state", "invalid-state")
	if err == nil {
		t.Fatalf("expected error on invalid comment state")
	}
}

// #506 is live now, in TestLivePRCreateFromAFork.
//
// pr create pre-flighted REPO_WRITE on the repository the pull request targets,
// which a fork contributor does not hold upstream, and had no way to say the
// source branch lives somewhere else -- so the standard contribution flow was
// refused twice over.
//
// Three of the four cases here read the POST body off a mock and checked what
// fromRef carried. Two of them turned on a field being *absent*, which is the
// weakest thing a payload assertion can watch: it passes if the field is
// missing for any reason, including the request never being built the way the
// server needs. The live test creates a real fork, opens the pull request from
// it, and reads back which repository each side points at -- and does the same
// for a pull request that is not from a fork, and for one that names its own
// target as the source.
//
// What is left is the case that never reaches a server.
func TestPRCreateFromAForkRefusesAMalformedRepository(t *testing.T) {
	// A listener that fails the test if it is reached. The kind asserted below
	// is the whole point -- a command that got as far as a request would fail
	// for a different reason and report a different kind.
	guard := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(guard.Close)

	_, err := executePr(t, guard.URL, "create", "--from-ref", "feature/x", "--to-ref", "main",
		"--title", "probe", "--from-repo", "no-slash", "--repo", "GHBS/uploader",
		"--no-default-reviewers", "--no-codeowners")
	if err == nil {
		t.Fatal("a --from-repo without a project was accepted")
	}
	if kind := apperrors.KindOf(err); kind != apperrors.KindValidation {
		t.Errorf("kind = %v, want validation", kind)
	}
}

// Thirty-seven suites are live now rather than here.
//
// Each one drove a pull-request command against newMockPRServer and asserted
// the rendering: TestPRList looked for "#42" in a listing whose only pull
// request this file numbered 42, TestPRMerge for "Merged pull request #42"
// from a handler that answered every merge with MERGED. Command reach is
// still 234/234, so every one of those commands is asserted against a real
// Bitbucket -- there against pull requests the harness created, in both
// output modes, and with the state read back afterwards.
//
// TestPRMergeDryRunReadsMergeability went with them, which is #479. Its
// subject was that the merge preview reads mergeability rather than guessing
// from the state, and it built the vetoes it then required the preview to
// name. TestLivePullRequestMergeability makes a real instance refuse -- a
// required-approver check on a pull request that merges cleanly -- and
// requires "blocked" with a reason; TestLiveDryRunPredictionsReadRealState
// holds the other end, that one nothing stands against is still "update".
