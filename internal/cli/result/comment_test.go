package result

import (
	"encoding/json"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestFlattenCommentsMakesRepliesReachable is the regression this exists for.
//
// Bitbucket nests a thread, and the model is flat, so a converter that read
// only the top level published a reply count and discarded every reply body.
// For a commit comment that was the whole loss: `bb repo comment` has no thread
// view, so no bb command could show the reply at all.
func TestFlattenCommentsMakesRepliesReachable(t *testing.T) {
	t.Parallel()

	var thread []openapigenerated.RestComment
	if err := json.Unmarshal([]byte(`[{
		"id": 1, "version": 0, "text": "the root",
		"comments": [
			{"id": 2, "version": 1, "text": "a reply",
			 "comments": [{"id": 3, "version": 0, "text": "a reply to the reply"}]},
			{"id": 4, "version": 0, "text": "a second reply"}
		]
	}]`), &thread); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	flattened := FlattenComments(thread)
	if len(flattened) != 4 {
		t.Fatalf("expected the root and its three descendants, got %d: %+v", len(flattened), flattened)
	}

	// Root first, then its replies depth first, so a thread reads top to
	// bottom.
	wantIDs := []int64{1, 2, 3, 4}
	for index, want := range wantIDs {
		if flattened[index].ID != want {
			t.Fatalf("order = %v, want %v", idsOf(flattened), wantIDs)
		}
	}

	if flattened[0].Reply || flattened[0].ParentID != 0 {
		t.Errorf("the root reported itself as a reply: %+v", flattened[0])
	}
	if flattened[0].ReplyCount != 2 {
		t.Errorf("replyCount = %d, want the two direct replies", flattened[0].ReplyCount)
	}

	// Each reply names what it answers, which is how a caller rebuilds the
	// tree the flattening took apart.
	for _, expected := range []struct{ id, parent int64 }{{2, 1}, {3, 2}, {4, 1}} {
		found := false
		for _, comment := range flattened {
			if comment.ID != expected.id {
				continue
			}
			found = true
			if !comment.Reply {
				t.Errorf("comment %d did not report itself as a reply", expected.id)
			}
			if comment.ParentID != expected.parent {
				t.Errorf("comment %d names parent %d, want %d", expected.id, comment.ParentID, expected.parent)
			}
		}
		if !found {
			t.Errorf("comment %d is missing: %v", expected.id, idsOf(flattened))
		}
	}

	// The bodies are the point: a count told a caller a reply existed and
	// nothing more.
	if flattened[1].Text != "a reply" || flattened[2].Text != "a reply to the reply" {
		t.Errorf("reply text was lost: %+v", flattened)
	}

	if empty := FlattenComments(nil); empty == nil || len(empty) != 0 {
		t.Errorf("FlattenComments(nil) = %v, want an empty slice rather than nil", empty)
	}
}

// TestFormatCommentIndentsARepliedComment covers the human side of the same
// listing: both renderings show the reply, and they agree on its version.
func TestFormatCommentIndentsARepliedComment(t *testing.T) {
	t.Parallel()

	root := FormatComment(Comment{ID: 1, Version: 0, Text: "the root"})
	if root != "[1 v0] the root" {
		t.Errorf("root = %q", root)
	}

	reply := FormatComment(Comment{ID: 2, Version: 3, Text: "  a reply  ", Reply: true, ParentID: 1})
	if reply != "  [2 v3] a reply" {
		t.Errorf("reply = %q, want it indented and trimmed", reply)
	}

	if empty := FormatComment(Comment{ID: 9}); empty != "[9 v0] <empty>" {
		t.Errorf("empty text = %q", empty)
	}
}

func idsOf(comments []Comment) []int64 {
	ids := make([]int64, 0, len(comments))
	for _, comment := range comments {
		ids = append(ids, comment.ID)
	}

	return ids
}

// TestFlattenCommentsPublishesAReplyOnce is the defect the activity timeline
// would have caused.
//
// That endpoint emits an activity per comment action, commentAction REPLIED
// among them, so a reply arrives twice: nested under its thread root, and as
// the subject of its own activity. Publishing both would have listed the same
// comment twice, and the second copy would have claimed to be a thread root
// because nothing nested it -- so a caller counting open threads would count
// one that does not exist.
func TestFlattenCommentsPublishesAReplyOnce(t *testing.T) {
	t.Parallel()

	var feed []openapigenerated.RestComment
	if err := json.Unmarshal([]byte(`[
		{"id": 10, "text": "the root", "comments": [{"id": 11, "text": "the reply"}]},
		{"id": 11, "text": "the reply"}
	]`), &feed); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	flattened := FlattenComments(feed)
	if len(flattened) != 2 {
		t.Fatalf("expected the root and one reply, got %d: %v", len(flattened), idsOf(flattened))
	}

	// The nested copy wins, because it is the one that knows what it answers.
	reply := flattened[1]
	if reply.ID != 11 || !reply.Reply || reply.ParentID != 10 {
		t.Fatalf("the surviving copy did not say what it answers: %+v", reply)
	}
}

// TestFlattenCommentsSurvivesACycle keeps a malformed tree from hanging bb.
//
// Nothing should send one, but the flattening is recursive and reads ids off
// the wire, so a self-referential tree would otherwise recurse until the stack
// gave out -- against a caller's terminal rather than a test.
func TestFlattenCommentsSurvivesACycle(t *testing.T) {
	t.Parallel()

	root := openapigenerated.RestComment{}
	id := int64(1)
	root.Id = &id
	root.Comments = &[]openapigenerated.RestComment{root}

	flattened := FlattenComments([]openapigenerated.RestComment{root})
	if len(flattened) != 1 {
		t.Fatalf("a self-referential tree produced %d comments: %v", len(flattened), idsOf(flattened))
	}
}

// TestCommentFromReadsAnAnchor covers the fields that locate an inline comment.
//
// joinCommentPath had no coverage at all, which is how the anchor handling
// stayed broken end to end: nothing ran an anchored comment through the
// converter, and the listings that would have hit it could not decode one.
func TestCommentFromReadsAnAnchor(t *testing.T) {
	t.Parallel()

	// The object form, which is what reaches the converter once the response
	// has been repaired.
	var upstream openapigenerated.RestComment
	if err := json.Unmarshal([]byte(`{
		"id": 5, "version": 2, "text": "handle nil here", "state": "OPEN",
		"anchor": {
			"line": 42, "lineType": "ADDED", "fileType": "TO", "diffType": "EFFECTIVE",
			"fromHash": "aaa", "toHash": "bbb",
			"path": {"components": ["internal", "cli", "root.go"], "name": "root.go", "parent": "internal/cli"},
			"srcPath": {"components": ["internal", "cli", "old.go"], "name": "old.go", "parent": "internal/cli"}
		}
	}`), &upstream); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	converted := CommentFrom(upstream)
	if converted.Anchor == nil {
		t.Fatal("an anchored comment reported no anchor")
	}
	if converted.Anchor.Path != "internal/cli/root.go" {
		t.Errorf("path = %q, want the components joined", converted.Anchor.Path)
	}
	if converted.Anchor.SrcPath != "internal/cli/old.go" {
		t.Errorf("srcPath = %q", converted.Anchor.SrcPath)
	}
	if converted.Anchor.Line != 42 || converted.Anchor.LineType != "ADDED" {
		t.Errorf("line = %d %q", converted.Anchor.Line, converted.Anchor.LineType)
	}
	if converted.Anchor.FileType != "TO" || converted.Anchor.DiffType != "EFFECTIVE" {
		t.Errorf("fileType/diffType = %q %q", converted.Anchor.FileType, converted.Anchor.DiffType)
	}
	if converted.Anchor.FromHash != "aaa" || converted.Anchor.ToHash != "bbb" {
		t.Errorf("hashes = %q %q", converted.Anchor.FromHash, converted.Anchor.ToHash)
	}
}

// TestCommentFromLeavesAPathEmptyRatherThanGuessing covers the anchor shapes
// that carry no components.
//
// The object also has name and parent, and a bare name is a file name rather
// than a repository-relative path. Publishing one under a key a caller reads as
// a path would be wrong where an absent path is merely unhelpful.
func TestCommentFromLeavesAPathEmptyRatherThanGuessing(t *testing.T) {
	t.Parallel()

	var nameOnly openapigenerated.RestComment
	if err := json.Unmarshal([]byte(`{"id":6,"anchor":{"line":1,"path":{"name":"root.go"}}}`), &nameOnly); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	converted := CommentFrom(nameOnly)
	if converted.Anchor == nil {
		t.Fatal("the anchor was dropped entirely")
	}
	if converted.Anchor.Path != "" {
		t.Errorf("path = %q, want empty rather than a bare file name", converted.Anchor.Path)
	}

	// No anchor at all is a comment on the pull request or commit itself.
	var unanchored openapigenerated.RestComment
	if err := json.Unmarshal([]byte(`{"id":7,"text":"top level"}`), &unanchored); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if CommentFrom(unanchored).Anchor != nil {
		t.Error("a top-level comment reported an anchor")
	}
}

// TestCommentFromCarriesEveryPublishedField is the guard for the defect this
// whole model exists to stop.
//
// Reply and ParentID were declared on the struct and never assigned by the
// converter, so every comment published reply=false and no parent, and nothing
// failed: the schema was right, the payload was wrong, and the two were written
// in different files. This walks the encoded document and requires every key
// the model declares to carry the value the fixture put there, so a field added
// to the struct and forgotten in the converter fails here.
func TestCommentFromCarriesEveryPublishedField(t *testing.T) {
	t.Parallel()

	var upstream openapigenerated.RestComment
	if err := json.Unmarshal([]byte(`{
		"id": 91, "version": 3, "text": "the body", "state": "RESOLVED", "severity": "BLOCKER",
		"pending": true, "threadResolved": true, "anchored": true, "reply": true,
		"createdDate": 1700000000000, "updatedDate": 1700000001000, "resolvedDate": 1700000002000,
		"parent": {"id": 90},
		"author": {"id": 7, "name": "alice", "displayName": "Alice A", "emailAddress": "a@example.com",
		           "slug": "alice", "type": "NORMAL", "active": true},
		"properties": {"commentReactions": [{"emoticon": {"value": "thumbsup"}}]},
		"comments": [{"id": 92, "text": "a reply"}],
		"anchor": {"line": 42, "lineType": "ADDED", "fileType": "TO", "diffType": "EFFECTIVE",
		           "fromHash": "aaa", "toHash": "bbb",
		           "path": {"components": ["a", "b.go"]}, "srcPath": {"components": ["a", "old.go"]}}
	}`), &upstream); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	encoded, err := json.Marshal(CommentFrom(upstream))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode: %v", err)
	}

	want := map[string]any{
		"id": float64(91), "version": float64(3), "text": "the body",
		"state": "RESOLVED", "severity": "BLOCKER",
		"pending": true, "resolved": true, "anchored": true,
		"reply": true, "parentId": float64(90),
		"replyCount":  float64(1),
		"createdDate": float64(1700000000000), "updatedDate": float64(1700000001000),
		"resolvedDate": float64(1700000002000),
	}
	for key, expected := range want {
		if document[key] != expected {
			t.Errorf("%s = %v (%T), want %v", key, document[key], document[key], expected)
		}
	}

	author, ok := document["author"].(map[string]any)
	if !ok {
		t.Fatalf("author is missing: %s", encoded)
	}
	if author["name"] != "alice" || author["displayName"] != "Alice A" || author["id"] != float64(7) {
		t.Errorf("author = %+v", author)
	}
	if author["emailAddress"] != "a@example.com" || author["slug"] != "alice" || author["active"] != true {
		t.Errorf("author = %+v", author)
	}

	// Properties is open on purpose: reactions live here and bb pr comment
	// react is what writes them.
	if properties, ok := document["properties"].(map[string]any); !ok || properties["commentReactions"] == nil {
		t.Errorf("properties = %v, want the reaction kept", document["properties"])
	}

	anchor, ok := document["anchor"].(map[string]any)
	if !ok {
		t.Fatalf("anchor is missing: %s", encoded)
	}
	for key, expected := range map[string]any{
		"path": "a/b.go", "srcPath": "a/old.go", "line": float64(42),
		"lineType": "ADDED", "fileType": "TO", "diffType": "EFFECTIVE",
		"fromHash": "aaa", "toHash": "bbb",
	} {
		if anchor[key] != expected {
			t.Errorf("anchor.%s = %v, want %v", key, anchor[key], expected)
		}
	}

	// Every key the model declares is accounted for above. A new field lands
	// here as an unexpected key rather than as a silent zero in the payload.
	known := map[string]bool{"author": true, "properties": true, "anchor": true}
	for key := range want {
		known[key] = true
	}
	for key := range document {
		if !known[key] {
			t.Errorf("%q is published but not asserted here; add it to this test with the value it should carry", key)
		}
	}
}
