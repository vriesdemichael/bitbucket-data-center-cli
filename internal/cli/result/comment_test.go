package result

import (
	"encoding/json"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-server-cli/internal/openapi/generated"
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
