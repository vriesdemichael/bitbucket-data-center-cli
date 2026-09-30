package mcp

import (
	"encoding/json"
	"strings"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

// A file's diff with comments is read for where it draws each comment: a
// comment on a removed line under that line as the file was, on an added or
// unchanged one under it as the file is, and a file's own at its top. The
// live suite proves Bitbucket answers in this shape.
func TestAFilesCommentsArePlacedWhereItsDiffDrawsThem(t *testing.T) {
	t.Parallel()

	var answer fileDiff
	if err := json.Unmarshal([]byte(`{"diffs":[{"hunks":[{"segments":[
		{"type":"CONTEXT","lines":[{"source":1,"destination":1,"commentIds":[7]}]},
		{"type":"REMOVED","lines":[{"source":2,"destination":2,"commentIds":[8]}]},
		{"type":"ADDED","lines":[{"source":2,"destination":2,"commentIds":[9,10]}]},
		{"type":"CONTEXT","lines":[{"source":25,"destination":28,"commentIds":[12]}]}]}],
		"fileComments":[{"id":11}]}]}`), &answer); err != nil {
		t.Fatal(err)
	}
	places := placesOf(answer)
	want := map[int64]string{7: "new:1", 8: "old:2", 9: "new:2", 10: "new:2", 11: "file", 12: "new:28"}
	for id, place := range want {
		if places[id] != place {
			t.Errorf("comment %d is placed at %q, want %q", id, places[id], place)
		}
	}
	if len(places) != len(want) {
		t.Errorf("places = %v, want %v", places, want)
	}
}

// A thread on a file Bitbucket did not place goes by where it was written.
func TestAThreadIsPlacedByItsAnchorWhereBitbucketDidNotSay(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		anchor pullrequestactivityservice.Anchor
		want   string
	}{
		{pullrequestactivityservice.Anchor{Path: "a.go", Line: 3, LineType: "ADDED"}, "new:3"},
		{pullrequestactivityservice.Anchor{Path: "a.go", Line: 4, LineType: "REMOVED"}, "old:4"},
		{pullrequestactivityservice.Anchor{Path: "a.go", Line: 5, LineType: "CONTEXT"}, "new:5"},
		{pullrequestactivityservice.Anchor{Path: "a.go"}, "file"},
		{pullrequestactivityservice.Anchor{Path: "a.go", Line: 6, LineType: "ADDED", Orphaned: true}, ""},
	} {
		if got := placeByAnchor(tc.anchor); got != tc.want {
			t.Errorf("%+v is placed at %q, want %q", tc.anchor, got, tc.want)
		}
	}
}

// The lines an activity shows a comment among mark the line it is on: a
// removed line by its old number, an added one by its new number, and a line
// on both sides by the side the comment was made on.
func TestAnActivityMarksTheLineItsCommentIsOn(t *testing.T) {
	t.Parallel()

	diff := map[string]any{"hunks": []any{map[string]any{"segments": []any{
		map[string]any{"type": "CONTEXT", "lines": []any{map[string]any{"source": 6.0, "destination": 9.0, "line": "keep"}}},
		map[string]any{"type": "REMOVED", "lines": []any{map[string]any{"source": 7.0, "destination": 10.0, "line": "old"}}},
		map[string]any{"type": "ADDED", "lines": []any{map[string]any{"source": 8.0, "destination": 10.0, "line": "new"}}},
	}}}}
	for _, tc := range []struct {
		anchor map[string]any
		want   string
	}{
		{map[string]any{"line": 7.0, "lineType": "REMOVED", "fileType": "FROM"}, "old"},
		{map[string]any{"line": 10.0, "lineType": "ADDED", "fileType": "TO"}, "new"},
		{map[string]any{"line": 9.0, "lineType": "CONTEXT", "fileType": "TO"}, "keep"},
		{map[string]any{"line": 6.0, "lineType": "CONTEXT", "fileType": "FROM"}, "keep"},
		{map[string]any{"line": 6.0, "lineType": "CONTEXT", "fileType": "TO"}, ""},
	} {
		lines := contextFromActivity(map[string]any{"diff": diff, "commentAnchor": tc.anchor})
		marked := ""
		for _, line := range lines {
			if line.Anchor {
				marked += line.Text
			}
		}
		if marked != tc.want {
			t.Errorf("anchor %v marks %q, want %q", tc.anchor, marked, tc.want)
		}
		if len(lines) != 3 || lines[1].Type != "del" || lines[1].Old != 7 || lines[2].Type != "add" || lines[2].New != 10 {
			t.Errorf("lines = %+v, want the context, removed and added line numbered on their sides", lines)
		}
	}
	if lines := contextFromActivity(map[string]any{"commentAnchor": map[string]any{"line": 1.0}}); lines != nil {
		t.Errorf("an activity without a diff gave lines %v", lines)
	}
}

// The activity lists what happened newest first: the actions an overview
// draws, a thread once, by its first comment, and nothing it has no words for.
func TestTheActivityListsWhatHappenedNewestFirst(t *testing.T) {
	t.Parallel()

	user := func(name string) map[string]any {
		return map[string]any{"name": name, "slug": name, "displayName": strings.ToUpper(name[:1]) + name[1:]}
	}
	id := func(value int64) *int64 { return &value }
	activities := []pullrequestactivityservice.Activity{
		{ID: 1, Action: "OPENED", CreatedDate: 100, Raw: map[string]any{"user": user("alice")}},
		{ID: 2, Action: "COMMENTED", CreatedDate: 200, Comment: &openapigenerated.RestComment{Id: id(5)}, Raw: map[string]any{"user": user("bob"), "commentAction": "ADDED"}},
		{ID: 3, Action: "COMMENTED", CreatedDate: 250, Comment: &openapigenerated.RestComment{Id: id(6)}, Raw: map[string]any{"user": user("alice"), "commentAction": "REPLIED"}},
		{ID: 4, Action: "APPROVED", CreatedDate: 300, Raw: map[string]any{"user": user("carol")}},
		{ID: 5, Action: "RESCOPED", CreatedDate: 400, Raw: map[string]any{"user": user("alice"), "added": map[string]any{"total": 2.0}}},
		{ID: 6, Action: "UPDATED", CreatedDate: 450, Raw: map[string]any{"user": user("alice")}},
		{ID: 7, Action: "MERGED", CreatedDate: 500, Raw: map[string]any{"user": user("dave"), "commit": map[string]any{"displayId": "7723c86c3c1"}}},
	}
	threads := map[int64]pullrequestactivityservice.Thread{
		5: {ID: 5, Author: "Bob", Text: "Should this retry?", AuthorAccount: pullrequestactivityservice.Account{Username: "bob", Slug: "bob"},
			Replies: []pullrequestactivityservice.Reply{{ID: 6, Author: "Alice", Text: "Yes.", AuthorAccount: pullrequestactivityservice.Account{Username: "alice", Slug: "alice"}}}},
	}
	activity, people := activityForView(activities, threads)

	var actions []string
	for _, item := range activity.Items {
		actions = append(actions, item.Action)
	}
	if got, want := strings.Join(actions, " "), "MERGED RESCOPED APPROVED COMMENTED OPENED"; got != want {
		t.Errorf("actions = %s, want %s", got, want)
	}
	if activity.Total != 5 {
		t.Errorf("total = %d, want the 5 items it lists", activity.Total)
	}
	merged, rescoped, commented := activity.Items[0], activity.Items[1], activity.Items[3]
	if merged.Commit != "7723c86c3c1" || merged.User != "Dave" || merged.Username != "dave" {
		t.Errorf("the merge is %+v, want Dave's, in commit 7723c86c3c1", merged)
	}
	if rescoped.Added != 2 {
		t.Errorf("the push added %d commits, want 2", rescoped.Added)
	}
	if commented.Thread == nil || commented.Thread.ID != 5 || len(commented.Thread.Replies) != 1 || commented.Thread.Replies[0].ID != 6 {
		t.Errorf("the comment's thread is %+v, want thread 5 with its reply 6", commented.Thread)
	}
	for _, username := range []string{"alice", "bob", "carol", "dave"} {
		if people[username] == "" {
			t.Errorf("the activity draws %s without an avatar: %v", username, people)
		}
	}
}

func TestAViewSaysWhatItCutOfALongComment(t *testing.T) {
	t.Parallel()

	short := "fine as it is"
	if text, cut := cutComment(short); text != short || cut {
		t.Errorf("a short comment came back %q, cut %v", text, cut)
	}
	// A multi-byte character straddles the cut, which falls before it.
	long := strings.Repeat("a", maxViewCommentBytes-1) + "é" + strings.Repeat("b", 10)
	text, cut := cutComment(long)
	if !cut || len(text) != maxViewCommentBytes-1 || !strings.HasSuffix(text, "a") {
		t.Errorf("a long comment came back %d bytes ending %q, cut %v; want it cut before the é", len(text), text[len(text)-3:], cut)
	}

	line := strings.Repeat("x", maxContextLineRunes) + strings.Repeat("é", 12)
	if kept, more := cutLine(line); kept != strings.Repeat("x", maxContextLineRunes) || more != 12 {
		t.Errorf("a long diff line kept %d bytes and counted %d more, want %d and 12", len(kept), more, maxContextLineRunes)
	}
}
