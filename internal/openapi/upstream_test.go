package openapi

import (
	"strings"
	"testing"
	"unicode/utf8"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestWhatBitbucketAnsweredIsInTheDetails is #574's "put the structured
// upstream payload in error.details". The message is written for people and
// reworded between releases; the status and the exception name are what a
// script branches on.
func TestWhatBitbucketAnsweredIsInTheDetails(t *testing.T) {
	t.Parallel()

	approval := []byte(`{"errors":[{"message":"Authors may not update their status.","exceptionName":"com.atlassian.bitbucket.pull.InvalidPullRequestRoleException"}]}`)
	details := apperrors.DetailsOf(MapStatusError(400, approval))
	if details["upstreamStatus"] != "400" {
		t.Errorf("upstreamStatus = %q, want 400", details["upstreamStatus"])
	}
	if details["upstreamException"] != "com.atlassian.bitbucket.pull.InvalidPullRequestRoleException" {
		t.Errorf("upstreamException = %q", details["upstreamException"])
	}

	// A body that is not an envelope names no exception, and says nothing
	// rather than something empty.
	details = apperrors.DetailsOf(MapStatusError(502, []byte("<html>Bad Gateway</html>")))
	if details["upstreamStatus"] != "502" {
		t.Errorf("upstreamStatus = %q, want 502", details["upstreamStatus"])
	}
	if _, named := details["upstreamException"]; named {
		t.Errorf("an HTML page reported an exception: %v", details)
	}

	// A service wrapping the failure with its own context must not lose them.
	wrapped := apperrors.New(apperrors.KindConflict, "could not merge", MapStatusError(409, approval))
	if apperrors.DetailsOf(wrapped)["upstreamStatus"] != "409" {
		t.Errorf("the details did not survive a wrap: %v", apperrors.DetailsOf(wrapped))
	}
}

// A byte slice ends wherever it ends, which put invalid UTF-8 on stderr and
// counted bytes as characters.
func TestATruncatedBodyIsCutInCharacters(t *testing.T) {
	t.Parallel()

	body := []byte(strings.Repeat("é", 400))
	message := MapStatusError(400, body).Error()

	if !utf8.ValidString(message) {
		t.Fatalf("the truncated message is not valid UTF-8: %q", message)
	}
	if !strings.Contains(message, "(100 more characters;") {
		t.Fatalf("the message miscounts what was left out: %s", message)
	}
}

// TestTheFullBodyFlagShowsAnEnvelopeToo is the flag meaning one thing.
//
// The summary prefers Bitbucket's own sentences, which replaces the body rather
// than shortening it, so --full-error-body changed nothing for a JSON error --
// the shape a Bitbucket server answers with, and the shape somebody passing the
// flag is looking at. The help says "the whole upstream response body".
//
// Not parallel: the flag is process-wide, and Go runs the parallel tests in
// this package after the ones that are not.
func TestTheFullBodyFlagShowsAnEnvelopeToo(t *testing.T) {
	envelope := []byte(`{"errors":[{"message":"Authors may not update their status.","exceptionName":"com.atlassian.bitbucket.pull.InvalidPullRequestRoleException"}]}`)

	summarised := MapStatusError(400, envelope).Error()
	if strings.Contains(summarised, "exceptionName") {
		t.Fatalf("the default answer is the summary, not the body: %s", summarised)
	}

	SetFullUpstreamBodies(true)
	t.Cleanup(func() { SetFullUpstreamBodies(false) })

	whole := MapStatusError(400, envelope).Error()
	if !strings.Contains(whole, "exceptionName") {
		t.Fatalf("--full-error-body still summarised a JSON envelope: %s", whole)
	}
	if !strings.Contains(whole, "Authors may not update their status.") {
		t.Fatalf("--full-error-body dropped the sentence the summary would have kept: %s", whole)
	}
}

// MissingPayload pasted the raw body, so an SSO login page answering 200 put
// all of itself, and whatever it echoed, into the message.
func TestAPayloadTheClientCouldNotReadIsSummarised(t *testing.T) {
	t.Parallel()

	const secret = "S3cr3tT0k3nValue"
	page := []byte("<html><body>Sign in. Authorization: Bearer " + secret + strings.Repeat(" padding", 2_000) + "</body></html>")

	message := MissingPayload(200, page, "reading a commit").Error()
	if strings.Contains(message, secret) {
		t.Fatalf("the credential reached the message:\n%s", message)
	}
	if len(message) > 1_000 {
		t.Fatalf("an %d byte page produced a %d character message", len(page), len(message))
	}
	if !strings.Contains(message, "--full-error-body") {
		t.Fatalf("the truncated message does not say how to see the rest: %s", message)
	}
}
