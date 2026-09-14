package prcmd

import (
	"strings"
	"testing"

	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// The preview has to predict what SetDraft does, by the same rule: an open pull
// request already in the requested state is left alone, and one that is not
// open is refused by Bitbucket whatever its draft flag says.
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
