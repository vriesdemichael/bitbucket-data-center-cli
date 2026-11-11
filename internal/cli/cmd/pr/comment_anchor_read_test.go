package prcmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// singleCommentWithoutItsAnchor and the timeline page below are reduced from
// what a real Bitbucket 8.14.3 answers: the single-comment view omits the
// anchor entirely, while the activity timeline carries it beside the comment as
// commentAnchor. Inside the tested release window the single-comment view nests
// the anchor in the comment, so these two are what keep that answer honest.

const singleCommentWithoutItsAnchor = `{
	"properties": {"repositoryId": 383},
	"id": 1084625,
	"version": 1,
	"text": "改成 classmethod，方便调用。",
	"author": {"name": "xiedw", "displayName": "谢德伟"},
	"createdDate": 1794329089854,
	"updatedDate": 1794329089854,
	"comments": [],
	"threadResolved": false,
	"severity": "NORMAL",
	"state": "OPEN",
	"permittedOperations": {"editable": true}
}`

const timelineWithAnchorBesideTheComment = `{
	"isLastPage": true,
	"values": [
		{
			"id": 20450132,
			"createdDate": 1794329089867,
			"action": "COMMENTED",
			"commentAction": "ADDED",
			"comment": {
				"properties": {"repositoryId": 383},
				"id": 1084625,
				"version": 1,
				"text": "改成 classmethod，方便调用。",
				"author": {"name": "xiedw", "displayName": "谢德伟"},
				"createdDate": 1794329089854,
				"updatedDate": 1794329089854,
				"threadResolved": false,
				"severity": "NORMAL",
				"state": "OPEN"
			},
			"commentAnchor": {
				"fromHash": "80271030ed8a",
				"toHash": "31e4e6922acf",
				"line": 194,
				"lineType": "CONTEXT",
				"fileType": "TO",
				"path": "nwd2md/nwd2md.py",
				"diffType": "RANGE",
				"orphaned": false
			}
		},
		{
			"id": 20450199,
			"createdDate": 1794360000000,
			"action": "COMMENTED",
			"commentAction": "ADDED",
			"comment": {
				"properties": {"repositoryId": 383},
				"id": 1085641,
				"version": 1,
				"text": "错了，我是说 configSchema",
				"author": {"name": "xiedw"},
				"createdDate": 1794360000000,
				"updatedDate": 1794360000000,
				"threadResolved": false,
				"severity": "NORMAL",
				"state": "OPEN"
			}
		}
	]
}`

// newAnchorBesideCommentServer answers the two reads an anchor recovery
// needs: the single comment, and the timeline that carries the anchor beside
// it. Both api versions of each path, because which one the client speaks is
// not this test's subject.
//
// mock-inventory: unreachable-state — the answers are reduced from what a real Bitbucket 8.14.3 sent: the anchor beside the comment on the activity, and a single-comment view without one. The release window the live stack tests starts above 8.14.3, so no instance CI brings up can be put into this shape; the live confirmation is against the server that sent them.
func newAnchorBesideCommentServer(t *testing.T) *httptest.Server {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		path := request.URL.Path
		switch {
		case strings.HasSuffix(path, "/pull-requests/42/comments/1084625"):
			_, _ = writer.Write([]byte(singleCommentWithoutItsAnchor))
		case strings.HasSuffix(path, "/pull-requests/42/activities"):
			_, _ = writer.Write([]byte(timelineWithAnchorBesideTheComment))
		default:
			http.NotFound(writer, request)
		}
	}))
	t.Cleanup(server.Close)

	return server
}

// TestPRCommentGetReadsTheAnchorFromTheTimeline is the read a caller asks
// which code a review comment means. On a release whose single-comment view
// omits the anchor, the answer used to be anchored false with no file or line,
// and the caller went back to guessing. The timeline beside the comment is
// where the anchor is, so the get reads it from there.
func TestPRCommentGetReadsTheAnchorFromTheTimeline(t *testing.T) {
	t.Parallel()

	server := newAnchorBesideCommentServer(t)

	output, err := executePr(t, server.URL, "--json", "comment", "get", "42", "1084625")
	if err != nil {
		t.Fatalf("pr comment get failed: %v\noutput: %s", err, output)
	}

	comment := decodeCommentEnvelope(t, output)
	if comment["anchored"] != true {
		t.Fatalf("expected the timeline anchor to make the comment anchored, got %#v", comment["anchored"])
	}

	anchor, _ := comment["anchor"].(map[string]any)
	if anchor == nil || anchor["path"] != "nwd2md/nwd2md.py" || anchor["line"] != float64(194) {
		t.Fatalf("expected the anchor read from the timeline, got %#v", anchor)
	}
}

// TestPRCommentGetKeepsTheServerAnswerWhenTheTimelineCannotBeRead pins the
// failure side of the recovery: the comment is the payload and was fetched, so
// a timeline that cannot be read leaves that answer as it is rather than
// failing a get that worked.
//
// mock-inventory: unreachable-state — the missing activity timeline is the subject, and every live pull request carries one; no instance CI brings up can be asked to drop it, and the subject is bb's own answer rather than the server's.
func TestPRCommentGetKeepsTheServerAnswerWhenTheTimelineCannotBeRead(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "/comments/1084625") {
			_, _ = writer.Write([]byte(singleCommentWithoutItsAnchor))
			return
		}
		http.NotFound(writer, request)
	}))
	t.Cleanup(server.Close)

	output, err := executePr(t, server.URL, "--json", "comment", "get", "42", "1084625")
	if err != nil {
		t.Fatalf("a missing timeline must not fail the get: %v\noutput: %s", err, output)
	}

	comment := decodeCommentEnvelope(t, output)
	if comment["anchored"] != false {
		t.Fatalf("expected the server's own answer to stand, got %#v", comment["anchored"])
	}
	if _, has := comment["anchor"]; has {
		t.Fatalf("expected no anchor without a timeline, got %#v", comment["anchor"])
	}
}

// TestPRCommentListCarriesTheAnchorFromBesideTheComment reads the aggregate
// listing -- the one a caller runs first -- and expects the inline thread to
// say where it is. Every inline thread reading as a pull-request-level one is
// what sent a caller with no file or line back to guessing which code a
// review comment meant.
func TestPRCommentListCarriesTheAnchorFromBesideTheComment(t *testing.T) {
	t.Parallel()

	server := newAnchorBesideCommentServer(t)

	output, err := executePr(t, server.URL, "--json", "comment", "list", "42")
	if err != nil {
		t.Fatalf("pr comment list failed: %v\noutput: %s", err, output)
	}

	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("list output is not JSON: %v\noutput: %s", err, output)
	}
	data, _ := envelope["data"].(map[string]any)

	var inline map[string]any
	for _, entry := range data["threads"].([]any) {
		thread, _ := entry.(map[string]any)
		if thread["id"] == float64(1084625) {
			inline = thread
		}
	}
	if inline == nil {
		t.Fatalf("expected the inline thread in the listing, got %s", output)
	}

	anchor, _ := inline["anchor"].(map[string]any)
	if anchor == nil || anchor["path"] != "nwd2md/nwd2md.py" || anchor["line"] != float64(194) {
		t.Fatalf("expected the anchor from beside the comment on the thread, got %#v", anchor)
	}

	summary, _ := data["summary"].(map[string]any)
	if summary["unresolvedInline"] != float64(1) {
		t.Fatalf("expected the inline count to see the anchor, got %#v", summary["unresolvedInline"])
	}
}

// decodeCommentEnvelope pulls the comment object out of a `pr comment get`
// envelope.
func decodeCommentEnvelope(t *testing.T, output string) map[string]any {
	t.Helper()

	envelope := map[string]any{}
	if err := json.Unmarshal([]byte(output), &envelope); err != nil {
		t.Fatalf("output is not JSON: %v\noutput: %s", err, output)
	}
	data, _ := envelope["data"].(map[string]any)
	comment, _ := data["comment"].(map[string]any)
	if comment == nil {
		t.Fatalf("no comment in the envelope:\n%s", output)
	}

	return comment
}
