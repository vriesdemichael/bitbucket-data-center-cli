package result

import (
	"fmt"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/safederef"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// Comment is one comment on a commit or a pull request.
//
// The upstream object nests the entire pull request under its anchor, and every
// reply nests the same again. Only what identifies and locates the comment is
// published.
//
// Shared by `bb pr comment` and `bb repo comment`, which read the same
// Bitbucket object through two endpoints. They held a copy each, and the copies
// were already drifting: both dropped the parent and reply fields, so fixing it
// meant fixing it twice, and the descriptions of identical fields had started
// to diverge.
type Comment struct {
	ID           int64          `json:"id,omitempty" jsonschema:"Comment identifier."`
	Version      int32          `json:"version" jsonschema:"Optimistic-locking version. Pass it back when updating or deleting, or the call is refused. Always present: a never-edited comment is at version 0."`
	Text         string         `json:"text,omitempty" jsonschema:"The comment text."`
	State        string         `json:"state,omitempty" jsonschema:"OPEN, RESOLVED or PENDING."`
	Severity     string         `json:"severity,omitempty" jsonschema:"NORMAL for an ordinary comment, BLOCKER for a task."`
	Pending      bool           `json:"pending" jsonschema:"Whether this is an unpublished draft comment."`
	Resolved     bool           `json:"resolved" jsonschema:"Whether the thread this comment belongs to is resolved."`
	Anchored     bool           `json:"anchored" jsonschema:"Whether the comment is attached to a line rather than to the commit or pull request."`
	Reply        bool           `json:"reply" jsonschema:"Whether this comment is a reply to another rather than the root of a thread."`
	ParentID     int64          `json:"parentId,omitempty" jsonschema:"Comment this one replies to. Absent on a thread root, which is what resolve, reopen and react address -- so a caller holding a reply id follows this to reach the comment those commands take."`
	Anchor       *CommentAnchor `json:"anchor,omitempty" jsonschema:"Where in the diff it sits. Absent for a top-level comment."`
	Author       User           `json:"author,omitzero" jsonschema:"Who wrote it."`
	ReplyCount   int            `json:"replyCount" jsonschema:"Direct replies to this comment."`
	CreatedDate  int64          `json:"createdDate,omitempty" jsonschema:"When it was written, in milliseconds since the epoch."`
	UpdatedDate  int64          `json:"updatedDate,omitempty" jsonschema:"When it last changed, in milliseconds since the epoch."`
	ResolvedDate int64          `json:"resolvedDate,omitempty" jsonschema:"When it was resolved, in milliseconds since the epoch."`

	// Properties is left open. Bitbucket stores per-comment extras here without
	// documenting them, and reactions -- what bb pr comment react writes and a
	// caller reads back -- are among them. Dropping it would lose the reaction;
	// claiming a shape for it would describe whichever instance was looked at.
	Properties map[string]any `json:"properties,omitempty" jsonschema:"Per-comment extras Bitbucket attaches, reactions among them. Left open because Bitbucket does not document what goes here."`
}

// CommentAnchor locates a comment in the diff.
//
// Only the fields that say where the comment is. The upstream anchor nests the
// entire pull request -- its author, both refs, and both refs' repositories and
// projects -- underneath, which is how a single comment used to arrive carrying
// tens of kilobytes of context the caller already had.
type CommentAnchor struct {
	Path     string `json:"path,omitempty" jsonschema:"File the comment is anchored to."`
	SrcPath  string `json:"srcPath,omitempty" jsonschema:"Path before a rename, when the file was renamed."`
	Line     int32  `json:"line,omitempty" jsonschema:"Line within that file."`
	LineType string `json:"lineType,omitempty" jsonschema:"ADDED, REMOVED or CONTEXT."`
	FileType string `json:"fileType,omitempty" jsonschema:"FROM or TO, which side of the diff the line is on."`
	DiffType string `json:"diffType,omitempty" jsonschema:"COMMIT, EFFECTIVE or RANGE."`
	FromHash string `json:"fromHash,omitempty" jsonschema:"Commit the diff was taken from."`
	ToHash   string `json:"toHash,omitempty" jsonschema:"Commit the diff was taken to."`
}

// CommentFrom converts one upstream comment.
func CommentFrom(upstream openapigenerated.RestComment) Comment {
	converted := Comment{
		Text:     safederef.String(upstream.Text),
		State:    safederef.String(upstream.State),
		Severity: safederef.String(upstream.Severity),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Version != nil {
		converted.Version = *upstream.Version
	}
	if upstream.Pending != nil {
		converted.Pending = *upstream.Pending
	}
	// Reported as the server sends it, and it is not the same question as
	// State. Bitbucket answers threadResolved false beside state RESOLVED on a
	// comment that has just been resolved: the comment is resolved, the thread
	// it belongs to is not. A caller asking whether this comment is resolved
	// wants State.
	if upstream.ThreadResolved != nil {
		converted.Resolved = *upstream.ThreadResolved
	}
	if upstream.Anchored != nil {
		converted.Anchored = *upstream.Anchored
	}
	if upstream.CreatedDate != nil {
		converted.CreatedDate = *upstream.CreatedDate
	}
	if upstream.UpdatedDate != nil {
		converted.UpdatedDate = *upstream.UpdatedDate
	}
	if upstream.ResolvedDate != nil {
		converted.ResolvedDate = *upstream.ResolvedDate
	}
	if upstream.Comments != nil {
		converted.ReplyCount = len(*upstream.Comments)
	}
	if upstream.Properties != nil {
		converted.Properties = *upstream.Properties
	}
	if upstream.Reply != nil {
		converted.Reply = *upstream.Reply
	}
	// Only the id. The upstream parent is a whole comment, and its anchor nests
	// the pull request again -- the id is what a caller follows to reach the
	// thread root that resolve, reopen and react take.
	if upstream.Parent != nil && upstream.Parent.Id != nil {
		converted.ParentID = *upstream.Parent.Id
	}
	if upstream.Author != nil {
		converted.Author = User{
			Name:         upstream.Author.Name,
			DisplayName:  upstream.Author.DisplayName,
			EmailAddress: safederef.String(upstream.Author.EmailAddress),
			Slug:         upstream.Author.Slug,
			Type:         string(upstream.Author.Type),
		}
		if upstream.Author.Id != nil {
			converted.Author.ID = *upstream.Author.Id
		}
		if upstream.Author.Active != nil {
			converted.Author.Active = *upstream.Author.Active
		}
	}
	if upstream.Anchor != nil {
		anchor := CommentAnchor{
			Path:     joinCommentPath(upstream.Anchor.Path),
			SrcPath:  joinCommentPath(upstream.Anchor.SrcPath),
			FromHash: safederef.String(upstream.Anchor.FromHash),
			ToHash:   safederef.String(upstream.Anchor.ToHash),
		}
		if upstream.Anchor.Line != nil {
			anchor.Line = *upstream.Anchor.Line
		}
		if upstream.Anchor.LineType != nil {
			anchor.LineType = string(*upstream.Anchor.LineType)
		}
		if upstream.Anchor.FileType != nil {
			anchor.FileType = string(*upstream.Anchor.FileType)
		}
		if upstream.Anchor.DiffType != nil {
			anchor.DiffType = string(*upstream.Anchor.DiffType)
		}
		converted.Anchor = &anchor
	}

	return converted
}

// joinCommentPath renders the path an anchor carries.
//
// Bitbucket sends it as an object with the components split. The generated
// client types that object inline rather than naming it, which is why the
// parameter is an anonymous struct.
func joinCommentPath(path *struct {
	Components *[]string `json:"components,omitempty"`
	Extension  *string   `json:"extension,omitempty"`
	Name       *string   `json:"name,omitempty"`
	Parent     *string   `json:"parent,omitempty"`
}) string {
	// Components only. The object also carries name and parent, but name alone
	// is a bare file name -- publishing that under a key a caller reads as a
	// repository-relative path would be wrong where an absent path is merely
	// unhelpful.
	if path == nil || path.Components == nil {
		return ""
	}

	return strings.Join(*path.Components, "/")
}

// FlattenComments returns every comment in the listing, replies included.
//
// Bitbucket nests a thread: the endpoint returns root comments, each carrying
// its replies under comments, and those carrying theirs. The model is flat on
// purpose -- a recursive struct would publish a schema that describes itself,
// and the nesting is what made a single comment arrive with tens of kilobytes
// of repeated context. So the tree is flattened instead, and every entry says
// where it sits: reply marks it as one, parentId names what it answers.
//
// This is what makes a reply reachable at all for a commit comment. `bb pr
// comment list` reaches them through its thread view; `bb repo comment` has no
// thread view, so before this the reply bodies were counted and discarded, and
// no bb command could show them.
//
// Order is the one a reader expects: each root, then its replies depth first,
// so a thread reads top to bottom.
func FlattenComments(upstream []openapigenerated.RestComment) []Comment {
	flattened := make([]Comment, 0, len(upstream))
	seen := map[int64]bool{}
	for _, one := range upstream {
		flattened = appendCommentTree(flattened, one, 0, seen)
	}

	return flattened
}

// appendCommentTree appends one comment and everything below it, once.
//
// parentID is threaded down rather than read off each reply: nesting is how
// Bitbucket expresses the relationship on this endpoint, and a nested reply
// does not always repeat its parent as a field.
//
// seen is what makes this safe on the activity timeline. That endpoint is a feed
// of actions rather than a list of comments (ADR-077): it carries a
// commentAction per entry, so the same comment can reach us twice -- nested
// under the root of its thread, and as the subject of its own activity. ExtractComments dedupes what it hands
// over, but it cannot see inside the trees it is handing over, so without this
// a reply would be published twice -- and the second copy would say it was a
// thread root, because nothing nested it.
func appendCommentTree(into []Comment, upstream openapigenerated.RestComment, parentID int64, seen map[int64]bool) []Comment {
	converted := CommentFrom(upstream)
	if converted.ID != 0 {
		if seen[converted.ID] {
			return into
		}
		seen[converted.ID] = true
	}
	if parentID != 0 {
		converted.Reply = true
		converted.ParentID = parentID
	}
	into = append(into, converted)

	if upstream.Comments == nil {
		return into
	}
	for _, reply := range *upstream.Comments {
		into = appendCommentTree(into, reply, converted.ID, seen)
	}

	return into
}

// FormatComment renders one comment as a line for a person.
//
// Here rather than in a command package because both bb pr comment and bb repo
// comment print it, from the same model. Rendering from the model is also what
// lets a reply be shown at all: the upstream object nests replies, so a
// renderer walking the flat list never saw one.
//
// A reply is indented under what it answers. FlattenComments orders each root
// before its replies, so the indent reads as the thread without the parent id
// having to be spelled out on every line.
func FormatComment(comment Comment) string {
	text := strings.TrimSpace(comment.Text)
	if text == "" {
		text = "<empty>"
	}

	indent := ""
	if comment.Reply {
		indent = "  "
	}

	return fmt.Sprintf("%s[%d v%d] %s", indent, comment.ID, comment.Version, text)
}
