// Package commentanchor holds the one description of how an inline comment
// anchor is put on the wire and read back off it.
//
// Three callers create comments — the CLI's `bb pr comment add`, the MCP
// add_pr_comment tool, and the comment service they both sit on — and an anchor
// that is built slightly differently by any of them is not a compile error, it
// is a comment that silently lands unanchored. The rules, the payload and the
// response fixup therefore live here rather than in each caller.
package commentanchor

import (
	"encoding/json"
	"fmt"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

// Options is the anchor a caller asked for, before validation.
type Options struct {
	Path     string
	Line     int
	LineType string
	ParentID int64
	Blocker  bool
}

// Inline reports whether the caller asked for an anchored comment at all.
func (options Options) Inline() bool {
	return strings.TrimSpace(options.Path) != "" || options.Line > 0
}

// Names labels the fields a validation error refers to so the shared rules can
// speak the caller's vocabulary: flag names for the CLI, input keys for the MCP
// tools and the service API.
type Names struct {
	Path     string
	Line     string
	LineType string
	ParentID string
	Blocker  string
}

// CLINames renders errors in terms of `bb pr comment add` flags.
var CLINames = Names{Path: "path", Line: "line", LineType: "line-type", ParentID: "parent-id", Blocker: "blocker"}

// APINames renders errors in terms of the MCP tool and service input keys.
var APINames = Names{Path: "path", Line: "line", LineType: "line_type", ParentID: "parent_id", Blocker: "blocker"}

// Validate rejects partial or conflicting anchors rather than silently
// downgrading to a general comment, which would leave the comment attached to
// the wrong place with no indication anything was off.
func Validate(options Options, names Names) error {
	inline := options.Inline()

	if inline && strings.TrimSpace(options.Path) == "" {
		return apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("%s requires %s for an inline comment", names.Line, names.Path), nil)
	}
	if inline && options.Line <= 0 {
		return apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("%s requires a positive %s for an inline comment", names.Path, names.Line), nil)
	}
	if inline && options.ParentID > 0 {
		return apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("%s cannot be combined with %s/%s; reply to a comment or anchor a new one, not both",
				names.ParentID, names.Path, names.Line), nil)
	}
	if !inline && strings.TrimSpace(options.LineType) != "" {
		return apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("%s only applies to inline comments; provide %s and %s too",
				names.LineType, names.Path, names.Line), nil)
	}
	if options.ParentID > 0 && options.Blocker {
		return apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("%s cannot be combined with %s", names.ParentID, names.Blocker), nil)
	}
	if inline {
		if _, err := NormalizeLineType(options.LineType, names); err != nil {
			return err
		}
	}

	return nil
}

// NormalizeLineType validates the diff side an inline comment anchors to,
// defaulting to ADDED when unset.
func NormalizeLineType(lineType string, names Names) (string, error) {
	normalized := strings.ToUpper(strings.TrimSpace(lineType))
	switch normalized {
	case "":
		return "ADDED", nil
	case "ADDED", "REMOVED", "CONTEXT":
		return normalized, nil
	default:
		return "", apperrors.New(apperrors.KindValidation,
			fmt.Sprintf("%s must be ADDED, REMOVED, or CONTEXT", names.LineType), nil)
	}
}

// Payload builds the request fields an anchored comment or a reply adds to a
// create-comment body. It returns nil when the caller asked for a plain
// pull-request-level comment.
//
// anchor.path is a plain string here, not the name/parent/components object the
// published spec models. The object is what Bitbucket returns; the create
// endpoint wants the string, which is why NormalizeResponsePaths exists to undo
// the mismatch on the way back.
func Payload(options Options, names Names) (map[string]any, error) {
	if err := Validate(options, names); err != nil {
		return nil, err
	}

	fields := map[string]any{}

	if options.ParentID > 0 {
		fields["parent"] = map[string]any{"id": options.ParentID}
	}

	if options.Inline() {
		lineType, err := NormalizeLineType(options.LineType, names)
		if err != nil {
			return nil, err
		}

		// fileType selects which side of the diff the line number refers to:
		// removed lines only exist in the original file, everything else is
		// anchored against the updated one. diffType EFFECTIVE anchors the
		// comment against the pull request diff rather than a single commit.
		fileType := "TO"
		if lineType == "REMOVED" {
			fileType = "FROM"
		}

		fields["anchor"] = map[string]any{
			"line":     options.Line,
			"path":     strings.TrimSpace(options.Path),
			"lineType": lineType,
			"fileType": fileType,
			"diffType": "EFFECTIVE",
		}
	}

	if len(fields) == 0 {
		return nil, nil
	}

	return fields, nil
}

// ExplainRejection replaces Bitbucket's generic rejection of an anchored
// comment with the rule the caller actually tripped over.
//
// An anchor is only accepted on a line that appears in the pull request diff.
// When it does not, the server answers with a plain validation error that names
// neither the line nor the rule, so the caller is left staring at a 400 with no
// way to tell a bad line from a bad path or a bad diff side. The server's own
// message is kept as the cause.
//
// Anything that is not a validation failure on an anchored comment is returned
// untouched: a transport error or a 403 has nothing to do with the anchor, and
// dressing it up in anchor advice would send the reader the wrong way.
func ExplainRejection(err error, options Options) error {
	if err == nil || !options.Inline() || !apperrors.IsKind(err, apperrors.KindValidation) {
		return err
	}

	lineType := strings.ToUpper(strings.TrimSpace(options.LineType))
	if lineType == "" {
		lineType = "ADDED"
	}

	return apperrors.New(
		apperrors.KindValidation,
		fmt.Sprintf(
			"Bitbucket rejected the inline comment anchored at %s:%d (line type %s). "+
				"An anchor is only accepted on a line that appears in the pull request diff: check that the line is inside a changed hunk, "+
				"that the path matches the diff entry exactly, and that the line type matches the side it is on "+
				"(REMOVED for a line that only exists in the original file, CONTEXT for an unchanged line shown around a hunk)",
			strings.TrimSpace(options.Path), options.Line, lineType),
		err,
	)
}

// NormalizeResponsePaths rewrites string anchor paths in a comment payload into
// the object form the generated model expects (ADR-077).
//
// Bitbucket serialises an inline comment.s anchor path as a plain string
// ("src/main.go") on the activity timeline, the create-comment response and
// the path-scoped listings, while the published spec uses an object with
// name/parent/extension/components. Without this a single inline comment makes
// the whole response fail to decode — which is how a comment that was created
// successfully gets reported as a failure, and how a listing containing one
// comment written in the web interface returns nothing at all.
//
// Takes either a single comment or a page of them, because both shapes carry
// the same mismatch and the caller should not have to know which it holds.
func NormalizeResponsePaths(raw json.RawMessage) (json.RawMessage, error) {
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}

	comment, ok := decoded.(map[string]any)
	if !ok {
		return raw, nil
	}

	changed := normalizeCommentTree(comment)
	if values, isPage := comment["values"].([]any); isPage {
		for _, value := range values {
			if entry, isMap := value.(map[string]any); isMap {
				changed = normalizeCommentTree(entry) || changed
			}
		}
	}
	if !changed {
		return raw, nil
	}

	return json.Marshal(comment)
}

// normalizeCommentTree rewrites a comment and its replies in place, reporting
// whether anything changed.
func normalizeCommentTree(comment map[string]any) bool {
	changed := false

	for _, key := range []string{"anchor", "parent"} {
		nested, ok := comment[key].(map[string]any)
		if !ok {
			continue
		}
		if key == "anchor" {
			changed = NormalizeAnchorPaths(nested) || changed
			continue
		}
		changed = normalizeCommentTree(nested) || changed
	}

	replies, ok := comment["comments"].([]any)
	if !ok {
		return changed
	}
	for _, reply := range replies {
		if nested, isMap := reply.(map[string]any); isMap {
			changed = normalizeCommentTree(nested) || changed
		}
	}

	return changed
}

// NormalizeAnchorPaths rewrites the string path fields of a single anchor in
// place, reporting whether anything changed.
func NormalizeAnchorPaths(anchor map[string]any) bool {
	changed := false

	for _, key := range []string{"path", "srcPath"} {
		value, ok := anchor[key].(string)
		if !ok {
			continue
		}
		anchor[key] = PathObjectFromString(value)
		changed = true
	}

	return changed
}

// PathObjectFromString splits "a/b/c.go" into the name/parent/extension/
// components shape Bitbucket uses elsewhere.
func PathObjectFromString(path string) map[string]any {
	trimmed := strings.Trim(strings.TrimSpace(path), "/")
	if trimmed == "" {
		return map[string]any{}
	}

	components := strings.Split(trimmed, "/")
	name := components[len(components)-1]
	parent := strings.Join(components[:len(components)-1], "/")

	object := map[string]any{
		"name":       name,
		"parent":     parent,
		"components": components,
	}
	if index := strings.LastIndex(name, "."); index > 0 && index < len(name)-1 {
		object["extension"] = name[index+1:]
	}

	return object
}
