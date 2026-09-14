package prcmd

import (
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/prsel"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

func TestNoDraftReviewRemedyNamesACommandForEachPartAsked(t *testing.T) {
	t.Parallel()

	target := prsel.Target{ProjectKey: "PROJ", RepoSlug: "repo", PullRequestID: "42"}
	setStatus := "bb pr review set 42 NEEDS_WORK --repo PROJ/repo"
	addComment := "bb pr comment add 42 --text 'tests fail' --repo PROJ/repo"

	cases := []struct {
		name    string
		status  string
		comment string
		want    []string
		notWant []string
	}{
		{name: "status", status: "NEEDS_WORK", want: []string{setStatus}, notWant: []string{"bb pr comment add"}},
		{name: "comment", comment: "tests fail", want: []string{addComment}, notWant: []string{"bb pr review set"}},
		{name: "both", status: "NEEDS_WORK", comment: "tests fail", want: []string{setStatus, addComment}},
		{name: "neither", want: []string{"--pending"}, notWant: []string{"bb pr review set", "--text"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			remedy := noDraftReviewRemedy(target, tc.status, tc.comment)
			for _, want := range tc.want {
				if !strings.Contains(remedy, want) {
					t.Errorf("remedy %q does not contain %q", remedy, want)
				}
			}
			for _, notWant := range tc.notWant {
				if strings.Contains(remedy, notWant) {
					t.Errorf("remedy %q contains %q", remedy, notWant)
				}
			}
		})
	}
}

func TestNoDraftReviewErrorStaysNotFoundAndKeepsTheUpstreamDetails(t *testing.T) {
	t.Parallel()

	target := prsel.Target{ProjectKey: "PROJ", RepoSlug: "repo", PullRequestID: "42"}
	err := noDraftReviewError(target, "APPROVED", "")

	if kind := apperrors.KindOf(err); kind != apperrors.KindNotFound {
		t.Errorf("kind = %v, want not_found", kind)
	}
	if code := apperrors.ExitCode(err); code != 4 {
		t.Errorf("exit code = %d, want 4", code)
	}
	details := apperrors.DetailsOf(err)
	if details["upstreamException"] != noSuchPullRequestReviewException {
		t.Errorf("upstreamException = %q", details["upstreamException"])
	}
	if details["upstreamStatus"] != "404" {
		t.Errorf("upstreamStatus = %q", details["upstreamStatus"])
	}
	message := apperrors.MessageOf(err)
	for _, want := range []string{"no draft review on pull request #42", "nothing was changed", "bb pr review set 42 APPROVED --repo PROJ/repo"} {
		if !strings.Contains(message, want) {
			t.Errorf("message %q does not contain %q", message, want)
		}
	}

	reason := noDraftReviewReason(target, "APPROVED", "")
	if strings.Contains(reason, "was changed") || !strings.Contains(reason, "bb pr review set 42 APPROVED --repo PROJ/repo") {
		t.Errorf("preview reason %q should name the command without reporting an outcome", reason)
	}
}

func TestShellQuoteKeepsACommentOneUnexpandedWord(t *testing.T) {
	t.Parallel()

	if got, want := shellQuote(`it's $HOME`), `'it'\''s $HOME'`; got != want {
		t.Errorf("shellQuote = %s, want %s", got, want)
	}
}
