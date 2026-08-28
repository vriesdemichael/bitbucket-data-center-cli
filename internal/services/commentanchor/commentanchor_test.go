package commentanchor

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func TestValidateRules(t *testing.T) {
	cases := []struct {
		name    string
		options Options
		names   Names
		want    string
	}{
		{
			name:    "line without path",
			options: Options{Line: 10},
			names:   APINames,
			want:    "line requires path for an inline comment",
		},
		{
			name:    "path without line",
			options: Options{Path: "a.go"},
			names:   APINames,
			want:    "path requires a positive line for an inline comment",
		},
		{
			name:    "negative line",
			options: Options{Path: "a.go", Line: -3},
			names:   APINames,
			want:    "path requires a positive line for an inline comment",
		},
		{
			name:    "parent with anchor",
			options: Options{Path: "a.go", Line: 1, ParentID: 7},
			names:   APINames,
			want:    "parent_id cannot be combined with path/line",
		},
		{
			name:    "line type without anchor",
			options: Options{LineType: "ADDED"},
			names:   APINames,
			want:    "line_type only applies to inline comments",
		},
		{
			name:    "parent with blocker",
			options: Options{ParentID: 7, Blocker: true},
			names:   APINames,
			want:    "parent_id cannot be combined with blocker",
		},
		{
			name:    "unknown line type",
			options: Options{Path: "a.go", Line: 1, LineType: "SIDEWAYS"},
			names:   APINames,
			want:    "line_type must be ADDED, REMOVED, or CONTEXT",
		},
		// The CLI says --line-type and --parent-id, the API says line_type and
		// parent_id. Same rules, the caller's vocabulary.
		{
			name:    "cli names on line type",
			options: Options{LineType: "ADDED"},
			names:   CLINames,
			want:    "line-type only applies to inline comments",
		},
		{
			name:    "cli names on parent",
			options: Options{ParentID: 7, Blocker: true},
			names:   CLINames,
			want:    "parent-id cannot be combined with blocker",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := Validate(testCase.options, testCase.names)
			if err == nil {
				t.Fatalf("expected a validation error for %#v", testCase.options)
			}
			if !apperrors.IsKind(err, apperrors.KindValidation) {
				t.Fatalf("expected a validation kind, got %v", err)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected %q, got %q", testCase.want, err.Error())
			}
		})
	}
}

func TestValidateAccepts(t *testing.T) {
	for _, options := range []Options{
		{},                      // plain pull-request comment
		{ParentID: 7},           // reply
		{Path: "a.go", Line: 1}, // anchor, default line type
		{Path: "pkg/a.go", Line: 12, LineType: "removed"}, // case-insensitive
		{Path: "a.go", Line: 1, Blocker: true},            // anchored task
		{Path: "a.go", Line: 1, LineType: "  CONTEXT  "},  // padded
	} {
		if err := Validate(options, APINames); err != nil {
			t.Errorf("expected %#v to be accepted, got %v", options, err)
		}
	}
}

func TestPayloadShape(t *testing.T) {
	// anchor.path must be a plain string. The generated model describes the
	// object Bitbucket returns, and building the request from that shape leaves
	// the comment unanchored.
	fields, err := Payload(Options{Path: " pkg/foo/bar.go ", Line: 42}, APINames)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	anchor, ok := fields["anchor"].(map[string]any)
	if !ok {
		t.Fatalf("expected an anchor, got %#v", fields)
	}
	if anchor["path"] != "pkg/foo/bar.go" {
		t.Fatalf("expected a trimmed string path, got %#v", anchor["path"])
	}
	if anchor["line"] != 42 || anchor["lineType"] != "ADDED" || anchor["fileType"] != "TO" || anchor["diffType"] != "EFFECTIVE" {
		t.Fatalf("unexpected anchor: %#v", anchor)
	}
	if _, exists := fields["parent"]; exists {
		t.Fatalf("an anchor must not carry a parent: %#v", fields)
	}

	// A removed line only exists in the original file, so it anchors against the
	// FROM side.
	fields, err = Payload(Options{Path: "a.go", Line: 5, LineType: "REMOVED"}, APINames)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if anchor := fields["anchor"].(map[string]any); anchor["fileType"] != "FROM" {
		t.Fatalf("expected fileType FROM for a removed line, got %#v", anchor)
	}

	fields, err = Payload(Options{ParentID: 55}, APINames)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	parent, ok := fields["parent"].(map[string]any)
	if !ok || parent["id"] != int64(55) {
		t.Fatalf("expected a parent id, got %#v", fields)
	}
	if _, exists := fields["anchor"]; exists {
		t.Fatalf("a reply must not carry an anchor: %#v", fields)
	}

	// A plain comment adds nothing to the body.
	fields, err = Payload(Options{}, APINames)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fields != nil {
		t.Fatalf("expected no extra fields for a plain comment, got %#v", fields)
	}

	if _, err := Payload(Options{Path: "a.go"}, APINames); err == nil {
		t.Fatal("Payload must reject what Validate rejects")
	}
}

func TestNormalizeLineType(t *testing.T) {
	for input, want := range map[string]string{
		"":          "ADDED",
		"  ":        "ADDED",
		"added":     "ADDED",
		"REMOVED":   "REMOVED",
		" context ": "CONTEXT",
	} {
		got, err := NormalizeLineType(input, APINames)
		if err != nil {
			t.Errorf("NormalizeLineType(%q): unexpected error %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("NormalizeLineType(%q) = %q, want %q", input, got, want)
		}
	}

	if _, err := NormalizeLineType("SIDEWAYS", APINames); err == nil {
		t.Fatal("expected an error for an unknown line type")
	}
}

func TestExplainRejection(t *testing.T) {
	anchored := Options{Path: "pkg/foo/bar.go", Line: 157}
	serverErr := apperrors.New(apperrors.KindValidation, "bitbucket API returned 400: anchor invalid", nil)

	explained := ExplainRejection(serverErr, anchored)
	if explained == nil {
		t.Fatal("expected an error back")
	}
	for _, want := range []string{
		"pkg/foo/bar.go:157",
		"line type ADDED",
		"only accepted on a line that appears in the pull request diff",
		"anchor invalid", // the server's own words survive as the cause
	} {
		if !strings.Contains(explained.Error(), want) {
			t.Errorf("expected %q in %q", want, explained.Error())
		}
	}
	if !errors.Is(explained, serverErr) {
		t.Error("the original error must remain in the chain")
	}

	if ExplainRejection(nil, anchored) != nil {
		t.Error("a nil error must stay nil")
	}

	// Nothing to do with the anchor: a plain comment, or a failure that is not
	// the server rejecting the anchor. Dressing either up in anchor advice
	// sends the reader the wrong way.
	plain := ExplainRejection(serverErr, Options{})
	if plain != serverErr {
		t.Error("a non-inline comment must be left alone")
	}
	transient := apperrors.New(apperrors.KindTransient, "connection reset", nil)
	if ExplainRejection(transient, anchored) != transient {
		t.Error("a non-validation failure must be left alone")
	}
}

func TestNormalizeResponsePaths(t *testing.T) {
	raw := json.RawMessage(`{
		"id": 1,
		"anchor": {"line": 4, "path": "pkg/foo/bar.go", "srcPath": "pkg/foo/old.go"},
		"comments": [
			{"id": 2, "anchor": {"path": "nested/reply.go"}}
		]
	}`)

	normalized, err := NormalizeResponsePaths(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(normalized, &decoded); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	anchor := decoded["anchor"].(map[string]any)
	path := anchor["path"].(map[string]any)
	if path["name"] != "bar.go" || path["parent"] != "pkg/foo" || path["extension"] != "go" {
		t.Fatalf("unexpected path object: %#v", path)
	}
	if srcPath := anchor["srcPath"].(map[string]any); srcPath["name"] != "old.go" {
		t.Fatalf("srcPath must be rewritten too: %#v", srcPath)
	}

	// Replies carry anchors of their own, and one bad one fails the whole decode.
	reply := decoded["comments"].([]any)[0].(map[string]any)
	replyPath := reply["anchor"].(map[string]any)["path"].(map[string]any)
	if replyPath["name"] != "reply.go" || replyPath["parent"] != "nested" {
		t.Fatalf("expected nested replies to be rewritten, got %#v", reply)
	}

	// Already-object paths are left exactly as they are.
	objectForm := json.RawMessage(`{"anchor":{"path":{"name":"a.go","parent":""}}}`)
	unchanged, err := NormalizeResponsePaths(objectForm)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(unchanged) != string(objectForm) {
		t.Fatalf("expected the object form to be untouched, got %s", unchanged)
	}
}

func TestPathObjectFromString(t *testing.T) {
	object := PathObjectFromString("a/b/c.go")
	if object["name"] != "c.go" || object["parent"] != "a/b" || object["extension"] != "go" {
		t.Fatalf("unexpected object: %#v", object)
	}
	if components := object["components"].([]string); len(components) != 3 || components[0] != "a" {
		t.Fatalf("unexpected components: %#v", object["components"])
	}

	root := PathObjectFromString("README")
	if root["name"] != "README" || root["parent"] != "" {
		t.Fatalf("unexpected root object: %#v", root)
	}
	if _, hasExtension := root["extension"]; hasExtension {
		t.Fatalf("a file with no extension must not get one: %#v", root)
	}

	if empty := PathObjectFromString("  /  "); len(empty) != 0 {
		t.Fatalf("expected an empty object, got %#v", empty)
	}
}
