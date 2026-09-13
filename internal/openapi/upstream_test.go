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
