package prompt

import (
	"bytes"
	"strings"
	"testing"
)

// TestTheRefusalNamesSomethingThatCanBePassed covers the message a pipeline
// gets when --yes lands on an inferred target.
//
// It used to end "pass PROJ/demo branch feature to confirm", which names the
// resource and not a flag: there is no argument of that shape on any command,
// so the one reader who cannot ask anybody was told to type something that does
// not exist. The remedy has to be the way to name the repository.
func TestTheRefusalNamesSomethingThatCanBePassed(t *testing.T) {
	t.Parallel()

	request := Request{
		Yes:            true,
		TargetExplicit: false,
		Resource:       "PROJ/demo branch feature",
		Flag:           "--yes",
	}

	err := ConfirmDestructive(request)
	if err == nil {
		t.Fatal("--yes was honoured on an inferred target")
	}

	message := err.Error()
	for _, remedy := range []string{"--repo PROJECT/slug", "BITBUCKET_PROJECT_KEY", "BITBUCKET_REPO_SLUG"} {
		if !strings.Contains(message, remedy) {
			t.Errorf("the refusal does not mention %s: %q", remedy, message)
		}
	}
	if strings.Contains(message, request.Resource) {
		t.Errorf("the refusal still asks for the resource to be passed: %q", message)
	}
}

// TestTheQuestionUsesTheCommandsOwnVerb covers what the three non-delete verbs
// read like.
//
// ADR-073 guards delete, remove, clear and revoke, and every one of them asked
// about a deletion: `bb auth token revoke` said "Type X to confirm deletion",
// and refusing said "--yes is required to delete X". Nothing on that command
// deletes anything, and a person reading a refusal about deletion while
// revoking a token has to go and check what bb is actually about to do.
func TestTheQuestionUsesTheCommandsOwnVerb(t *testing.T) {
	cases := []struct {
		verb     string
		action   string
		question string
		outcome  string
	}{
		{verb: "revoke", action: "revoke ci-token", question: "confirm revocation", outcome: "nothing was revoked"},
		{verb: "remove", action: "remove ci-token", question: "confirm removal", outcome: "nothing was removed"},
		{verb: "clear", action: "clear ci-token", question: "confirm clearing", outcome: "nothing was cleared"},
		{verb: "delete", action: "delete ci-token", question: "confirm deletion", outcome: "nothing was deleted"},
		// A verb nobody has taught this package still has to read as English.
		{verb: "purge", action: "purge ci-token", question: "to confirm:", outcome: "nothing was changed"},
	}

	for _, testCase := range cases {
		t.Run(testCase.verb, func(t *testing.T) {
			refusal := ConfirmDestructive(Request{
				In:             strings.NewReader(""),
				Out:            &bytes.Buffer{},
				Disabled:       true,
				TargetExplicit: true,
				Resource:       "ci-token",
				Flag:           "--yes",
				Verb:           testCase.verb,
				Lookup:         noEnvironment,
			})
			if refusal == nil {
				t.Fatal("a destructive command ran with nobody to confirm it")
			}
			if !strings.Contains(refusal.Error(), testCase.action) {
				t.Errorf("the refusal does not say what would happen: %q, want %q", refusal.Error(), testCase.action)
			}

			allowPrompting(t)

			out := &bytes.Buffer{}
			mismatch := ConfirmDestructive(Request{
				In:             strings.NewReader("no\n"),
				Out:            out,
				TargetExplicit: true,
				Resource:       "ci-token",
				Flag:           "--yes",
				Verb:           testCase.verb,
				Lookup:         noEnvironment,
			})
			if mismatch == nil {
				t.Fatal("a wrong answer confirmed the command")
			}
			if !strings.Contains(out.String(), testCase.question) {
				t.Errorf("the question reads %q, want it to contain %q", out.String(), testCase.question)
			}
			if !strings.Contains(mismatch.Error(), testCase.outcome) {
				t.Errorf("the outcome reads %q, want it to contain %q", mismatch.Error(), testCase.outcome)
			}
		})
	}
}
