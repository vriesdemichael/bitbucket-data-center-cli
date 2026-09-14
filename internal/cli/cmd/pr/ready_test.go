package prcmd

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// The preview has to predict what SetDraft does, by the same rule: an open pull
// request already in the requested state is left alone, and one that is not
// open is refused whatever its draft flag says.
func TestReadyPreviewPredictsWhatTheCommandDoes(t *testing.T) {
	t.Parallel()

	repo := pullrequestservice.RepositoryRef{ProjectKey: "PRJ", Slug: "demo"}
	open := func(draft bool) pullrequestservice.PullRequest {
		return pullrequestservice.PullRequest{State: "OPEN", Open: true, Draft: draft}
	}
	declined := pullrequestservice.PullRequest{State: "DECLINED", Closed: true}

	testCases := []struct {
		name    string
		current pullrequestservice.PullRequest
		draft   bool
		want    string
	}{
		{name: "a draft marked ready", current: open(true), draft: false, want: "update"},
		{name: "a ready pull request marked ready", current: open(false), draft: false, want: "no-op"},
		{name: "a ready pull request turned into a draft", current: open(false), draft: true, want: "update"},
		{name: "a draft turned into a draft", current: open(true), draft: true, want: "no-op"},
		{name: "a declined pull request marked ready", current: declined, draft: false, want: "blocked"},
		{name: "a declined pull request turned into a draft", current: declined, draft: true, want: "blocked"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			item := readyPreviewItem(repo, "42", testCase.current, testCase.draft)
			if item.PredictedAction != testCase.want {
				t.Fatalf("predicted %q, want %q (reason %q)", item.PredictedAction, testCase.want, item.Reason)
			}
			if item.Intent != "pr.ready" || item.Action != "update" {
				t.Errorf("intent=%q action=%q, want pr.ready and update", item.Intent, item.Action)
			}
			if blocked := testCase.want == "blocked"; blocked != (len(item.BlockingReasons) > 0) {
				t.Errorf("blocking reasons %v do not match a %q prediction", item.BlockingReasons, testCase.want)
			}
		})
	}
}

func TestReadyMessageSaysWhetherAnythingChanged(t *testing.T) {
	t.Parallel()

	for _, draft := range []bool{false, true} {
		for _, changed := range []bool{false, true} {
			message := readyMessage("42", draft, changed)

			if !strings.Contains(message, "#42") {
				t.Errorf("draft=%v changed=%v: %q does not name the pull request", draft, changed, message)
			}
			if saysNothing := strings.Contains(message, "nothing changed"); saysNothing == changed {
				t.Errorf("draft=%v changed=%v: %q", draft, changed, message)
			}
			if saysDraft := strings.Contains(message, "draft"); saysDraft != draft {
				t.Errorf("draft=%v changed=%v: %q names the wrong state", draft, changed, message)
			}
		}
	}
}

// refusingChecker answers every permission check with the same refusal.
type refusingChecker struct{ err error }

func (checker refusingChecker) CheckRepoPermission(context.Context, string, string, openapigenerated.GetRepositories1ParamsPermission) error {
	return checker.err
}

// The failures bb pr ready decides before it asks Bitbucket anything. Each must
// come back as the error it is: not swallowed into a preview, and not replaced
// by a later failure. The client points at a closed port, so a command that
// went on to the network would fail with a transport error instead of the one
// each case expects.
func TestReadyCommandReportsFailuresBeforeAnyRequest(t *testing.T) {
	t.Parallel()

	closedPort := func(t *testing.T) (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
		t.Helper()

		client, err := openapigenerated.NewClientWithResponses(testsupport.RefusedURL)
		if err != nil {
			t.Fatalf("build client: %v", err)
		}

		return config.AppConfig{BitbucketURL: testsupport.RefusedURL}, client, nil
	}

	run := func(deps Dependencies, args ...string) error {
		command := New(deps)
		command.SetOut(io.Discard)
		command.SetErr(io.Discard)
		command.SetArgs(append([]string{"ready"}, args...))

		return command.Execute()
	}

	t.Run("configuration that cannot be loaded", func(t *testing.T) {
		t.Parallel()

		broken := errors.New("no Bitbucket host is configured")
		err := run(Dependencies{
			LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
				return config.AppConfig{}, nil, broken
			},
		}, "42", "--repo", "PRJ/demo")
		if !errors.Is(err, broken) {
			t.Fatalf("got %v, want the configuration failure", err)
		}
	})

	t.Run("no repository to act on", func(t *testing.T) {
		t.Parallel()

		err := run(Dependencies{
			LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
				return closedPort(t)
			},
		}, "42")
		if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "repository is required") {
			t.Fatalf("got %v, want a validation error asking for the repository", err)
		}
	})

	t.Run("a dry run by a caller who may not write", func(t *testing.T) {
		t.Parallel()

		refused := errors.New("you may not write to PRJ/demo")
		err := run(Dependencies{
			DryRunEnabled: func() bool { return true },
			LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
				return closedPort(t)
			},
			PermissionChecker: func(*openapigenerated.ClientWithResponses) PermissionChecker {
				return refusingChecker{err: refused}
			},
		}, "42", "--repo", "PRJ/demo")
		if !errors.Is(err, refused) {
			t.Fatalf("got %v, want the permission refusal rather than a preview", err)
		}
	})
}
