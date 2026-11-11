package pullrequestactivity

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"
)

// timelinePageWithAnchorBesideTheComment is a page reduced from one a real
// Bitbucket 8.14.3 sent: the anchor of an inline comment sits beside the
// comment on the activity (commentAnchor), and the comment itself carries
// none. The releases inside the tested window nest the anchor inside the
// comment, which is the shape every other fixture here encodes, so this page
// is what keeps the other one honest.
//
// The PR-level comment beside it is the control: nothing to graft, and a
// thread that must stay unanchored rather than pick up its neighbour's.
const timelinePageWithAnchorBesideTheComment = `{
	"isLastPage": true,
	"values": [
		{
			"id": 20450132,
			"createdDate": 1794329089867,
			"action": "COMMENTED",
			"commentAction": "ADDED",
			"user": {"name": "xiedw", "displayName": "谢德伟"},
			"comment": {
				"properties": {"repositoryId": 383},
				"id": 1084625,
				"version": 1,
				"text": "改成 classmethod，方便调用。",
				"author": {"name": "xiedw", "displayName": "谢德伟"},
				"createdDate": 1794329089854,
				"updatedDate": 1794329089854,
				"comments": [{"id": 1085384, "version": 0, "text": "已落实", "author": {"name": "luowj"}}],
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
			"user": {"name": "xiedw"},
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

func listFixturePage(t *testing.T, page string) []Activity {
	t.Helper()

	service := newActivityTestService(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(page))
	})

	activities, err := service.List(context.Background(), RepositoryRef{ProjectKey: "TEST", Slug: "demo"}, "12", ListOptions{})
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	return activities
}

// TestAnchorBesideTheComment grafts the activity-level commentAnchor onto the
// comment it belongs to, so the thread view reports an inline comment as
// inline. Before the graft every one of these read as a pull-request-level
// comment on 8.x, which sent an agent with no file or line back to guessing
// which code a review comment meant.
func TestAnchorBesideTheComment(t *testing.T) {
	t.Parallel()

	activities := listFixturePage(t, timelinePageWithAnchorBesideTheComment)
	threads, summary := ExtractThreads(activities, ThreadOptions{})

	var inline, topLevel *Thread
	for i := range threads {
		switch threads[i].ID {
		case 1084625:
			inline = &threads[i]
		case 1085641:
			topLevel = &threads[i]
		}
	}
	if inline == nil || topLevel == nil {
		t.Fatalf("expected both threads on the timeline, got %#v", threads)
	}

	if inline.Anchor == nil {
		t.Fatal("expected the inline thread to carry the anchor from beside the comment")
	}
	if inline.Anchor.Path != "nwd2md/nwd2md.py" || inline.Anchor.Line != 194 || inline.Anchor.LineType != "CONTEXT" {
		t.Errorf("inline anchor = path %q line %d lineType %q, want nwd2md/nwd2md.py 194 CONTEXT", inline.Anchor.Path, inline.Anchor.Line, inline.Anchor.LineType)
	}
	if topLevel.Anchor != nil {
		t.Errorf("a comment with nothing beside it read as anchored to %#v", topLevel.Anchor)
	}

	if summary.UnresolvedInline != 1 {
		t.Errorf("summary.UnresolvedInline = %d, want 1: the count is what tells an agent inline feedback is waiting", summary.UnresolvedInline)
	}

	// The grafted anchor keeps what the comment's own anchor would have
	// carried, because it is decoded into the same model.
	for _, comment := range ExtractComments(activities) {
		if comment.Id != nil && *comment.Id == 1084625 {
			if comment.Anchor == nil || comment.Anchor.FileType == nil || *comment.Anchor.FileType != "TO" {
				t.Fatalf("expected the grafted anchor to keep fileType TO, got %#v", comment.Anchor)
			}
			if comment.Anchor.FromHash == nil || *comment.Anchor.FromHash != "80271030ed8a" {
				t.Fatalf("expected the grafted anchor to keep fromHash, got %#v", comment.Anchor)
			}
		}
	}
}

// TestOrphanedBesideTheComment reads the orphaned flag from the same place:
// an anchor pointing at a line the diff no longer carries, which an agent
// needs to know before it acts on the location.
func TestOrphanedBesideTheComment(t *testing.T) {
	t.Parallel()

	page := `{"isLastPage":true,"values":[
		{"id": 1, "action": "COMMENTED",
		 "comment": {"id": 7, "version": 0, "text": "where did this line go", "state": "OPEN", "severity": "NORMAL"},
		 "commentAnchor": {"line": 12, "lineType": "ADDED", "path": "gone.txt", "orphaned": true}}
	]}`

	threads, _ := ExtractThreads(listFixturePage(t, page), ThreadOptions{})
	if len(threads) != 1 {
		t.Fatalf("expected one thread, got %#v", threads)
	}
	if threads[0].Anchor == nil || !threads[0].Anchor.Orphaned {
		t.Fatalf("expected the orphaned flag from beside the comment on the anchor, got %#v", threads[0].Anchor)
	}
}

// TestGraftKeepsTheCommentOwnAnchor pins the precedence: a comment that
// already carries an anchor keeps it, and the one beside the activity is
// dropped. Both present means a server that moved the field and still sends
// the old one, and the comment's own is the one its other endpoints agree
// with.
func TestGraftKeepsTheCommentOwnAnchor(t *testing.T) {
	t.Parallel()

	comment := json.RawMessage(`{"id": 5, "text": "inline", "anchor": {"path": "inside.go", "line": 3}}`)
	anchor := json.RawMessage(`{"line": 40, "lineType": "CONTEXT", "path": "beside.go"}`)

	grafted, err := graftTimelineAnchor(comment, anchor)
	if err != nil {
		t.Fatalf("graft failed: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(grafted, &decoded); err != nil {
		t.Fatalf("grafted comment did not decode: %v", err)
	}
	kept := decoded["anchor"].(map[string]any)
	if kept["path"] != "inside.go" || kept["line"] != float64(3) {
		t.Fatalf("expected the comment's own anchor to win, got %#v", kept)
	}

	// A null beside the comment, and a null comment, are shapes a server sends
	// for an unanchored one; both pass through unchanged rather than failing
	// the page.
	if through, err := graftTimelineAnchor(comment, json.RawMessage(`null`)); err != nil || string(through) != string(comment) {
		t.Fatalf("a null anchor changed the comment: %s %v", through, err)
	}
	if through, err := graftTimelineAnchor(json.RawMessage(`null`), anchor); err != nil || string(through) != "null" {
		t.Fatalf("a null comment was changed: %s %v", through, err)
	}
}

// TestCommentByID finds one comment with the grafted anchor, which is what
// answers "is this comment inline, and where" on a release whose
// single-comment view omits the anchor.
func TestCommentByID(t *testing.T) {
	t.Parallel()

	activities := listFixturePage(t, timelinePageWithAnchorBesideTheComment)

	found, ok := CommentByID(activities, 1084625)
	if !ok {
		t.Fatal("expected comment 1084625 on the timeline")
	}
	if found.Anchor == nil || safederef.String(found.Anchor.FromHash) != "80271030ed8a" {
		t.Fatalf("expected the found comment to carry the grafted anchor, got %#v", found.Anchor)
	}

	if _, ok := CommentByID(activities, 999); ok {
		t.Fatal("expected comment 999 to be absent")
	}
}
