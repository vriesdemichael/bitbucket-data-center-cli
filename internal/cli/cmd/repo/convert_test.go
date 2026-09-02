package repocmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
	reposettings "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
)

// upstreamFromJSON decodes a Bitbucket response into the generated type, which
// is what the converter sees in production. Building the generated types by
// hand is impractical: they render nested objects as anonymous structs.
func upstreamFromJSON[T any](t *testing.T, body string) T {
	t.Helper()

	var decoded T
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	return decoded
}

// TestFileLinesFromReadsBlameAsAList is the regression this file exists for.
//
// Bitbucket returns the file's lines and, with blame=true, a blame array
// alongside them -- one entry per run of lines. bb read that as a single object
// with one author, so against a real server the decode failed and both
// renderings fell back to printing raw JSON. The unit fixture encoded the same
// misreading, which is why nothing caught it until the live suite ran.
func TestFileLinesFromReadsBlameAsAList(t *testing.T) {
	t.Parallel()

	lines, _, _ := fileLinesFrom([]byte(`{
		"lines": [{"text": "package main"}, {"text": ""}, {"text": "func main() {}"}],
		"blame": [
			{"author": {"name": "alice"}, "lineNumber": 1, "spannedLines": 2},
			{"author": {"name": "bob"}, "lineNumber": 3, "spannedLines": 1}
		]
	}`))

	if len(lines) != 3 {
		t.Fatalf("expected three lines, got %d: %+v", len(lines), lines)
	}
	if lines[0].Author != "alice" || lines[1].Author != "alice" {
		t.Fatalf("the first span covers two lines: %+v", lines)
	}
	if lines[2].Author != "bob" {
		t.Fatalf("the second span was not applied: %+v", lines)
	}
	if lines[2].Text != "func main() {}" {
		t.Fatalf("the text was lost: %+v", lines)
	}
}

func TestFileLinesFromWithoutBlameLeavesTheAuthorEmpty(t *testing.T) {
	t.Parallel()

	lines, _, _ := fileLinesFrom([]byte(`{"lines": [{"text": "package main"}]}`))
	if len(lines) != 1 || lines[0].Text != "package main" {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Author != "" {
		t.Fatalf("bb repo browse file reported an author: %+v", lines[0])
	}

	// A span reaching past the end is clamped rather than panicking: the file
	// and its blame are two reads of the same commit, not the same request.
	clamped, _, _ := fileLinesFrom([]byte(`{
		"lines": [{"text": "only line"}],
		"blame": [{"author": {"name": "alice"}, "lineNumber": 1, "spannedLines": 50}]
	}`))
	if len(clamped) != 1 || clamped[0].Author != "alice" {
		t.Fatalf("clamped = %+v", clamped)
	}

	if empty, _, _ := fileLinesFrom(nil); empty == nil || len(empty) != 0 {
		t.Fatalf("fileLinesFrom(nil) = %v, want an empty slice rather than nil", empty)
	}
	if garbage, _, _ := fileLinesFrom([]byte("not json")); len(garbage) != 0 {
		t.Fatalf("fileLinesFrom(garbage) = %v, want an empty slice", garbage)
	}
}

// TestRawFileFromSaysHowTheBytesAreEncoded guards the other silent corruption:
// a JSON string cannot carry arbitrary bytes, and Go's encoder substitutes
// U+FFFD for invalid UTF-8 without saying so.
func TestRawFileFromSaysHowTheBytesAreEncoded(t *testing.T) {
	t.Parallel()

	repository := result.Repository{ProjectKey: "PRJ", Slug: "payments"}

	text := rawFileFrom(repository, "README.md", "main", []byte("# hello\n"))
	if text.Encoding != "utf-8" || text.Content != "# hello\n" {
		t.Fatalf("text file = %+v", text)
	}
	if text.Path != "README.md" || text.At != "main" || text.Repository != repository {
		t.Fatalf("context fields wrong: %+v", text)
	}

	binary := rawFileFrom(repository, "logo.png", "", []byte{0x89, 0x50, 0x4e, 0x47, 0xff})
	if binary.Encoding != "base64" {
		t.Fatalf("binary encoding = %q, want base64", binary.Encoding)
	}
}

func TestCommentFromTrimsTheAnchorAndKeepsTheExtras(t *testing.T) {
	t.Parallel()

	converted := result.CommentFrom(upstreamFromJSON[openapigenerated.RestComment](t, `{
		"id": 118,
		"version": 2,
		"text": "please rename this",
		"state": "OPEN",
		"severity": "BLOCKER",
		"pending": false,
		"threadResolved": false,
		"anchored": true,
		"createdDate": 1700000000000,
		"updatedDate": 1700000001000,
		"author": {"id": 7, "name": "alice", "displayName": "Alice A", "slug": "alice", "type": "NORMAL", "active": true},
		"comments": [{"id": 119, "text": "done"}],
		"properties": {"repositoryId": 11, "commentReactions": [{"emoticon": {"value": "thumbsup"}}]},
		"anchor": {
			"line": 12,
			"lineType": "ADDED",
			"fileType": "TO",
			"diffType": "EFFECTIVE",
			"fromHash": "aaa",
			"toHash": "bbb",
			"path": {"components": ["internal", "cli", "root.go"], "name": "root.go", "parent": "internal/cli"},
			"srcPath": {"components": ["internal", "cli", "old.go"], "name": "old.go", "parent": "internal/cli"},
			"pullRequest": {"id": 42, "title": "the entire pull request, nested under the anchor"}
		}
	}`))

	if converted.ID != 118 || converted.Version != 2 || converted.Severity != "BLOCKER" {
		t.Fatalf("comment fields wrong: %+v", converted)
	}
	if converted.Author.Name != "alice" || converted.Author.ID != 7 || !converted.Author.Active {
		t.Fatalf("author = %+v", converted.Author)
	}
	if converted.ReplyCount != 1 {
		t.Fatalf("replyCount = %d, want the nested replies counted rather than published", converted.ReplyCount)
	}
	if converted.Anchor == nil {
		t.Fatal("the anchor was dropped")
	}
	if converted.Anchor.Path != "internal/cli/root.go" || converted.Anchor.SrcPath != "internal/cli/old.go" {
		t.Fatalf("paths = %q / %q, want them joined", converted.Anchor.Path, converted.Anchor.SrcPath)
	}
	if converted.Anchor.Line != 12 || converted.Anchor.LineType != "ADDED" || converted.Anchor.FileType != "TO" || converted.Anchor.DiffType != "EFFECTIVE" {
		t.Fatalf("anchor = %+v", converted.Anchor)
	}
	// Reactions live in properties, undocumented. Dropping the map would lose
	// what bb pr comment react writes.
	if converted.Properties["commentReactions"] == nil {
		t.Fatalf("properties = %+v, want the extras kept", converted.Properties)
	}

	if list := result.FlattenComments(nil); list == nil || len(list) != 0 {
		t.Fatalf("FlattenComments(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestCommentContextFromNamesWhatTheCommentIsOn(t *testing.T) {
	t.Parallel()

	commit := commentContextFrom(commentservice.Context{
		Type: "commit", ProjectKey: "PRJ", RepositorySlug: "payments", CommitID: "abc123",
	})
	if commit != (CommentContext{Type: "commit", ProjectKey: "PRJ", Slug: "payments", CommitID: "abc123"}) {
		t.Fatalf("commit context = %+v", commit)
	}

	pullRequest := commentContextFrom(commentservice.Context{
		Type: "pull_request", ProjectKey: "PRJ", RepositorySlug: "payments", PullRequestID: "42",
	})
	if pullRequest.PullRequestID != "42" || pullRequest.CommitID != "" {
		t.Fatalf("pull request context = %+v", pullRequest)
	}
}

func TestEffectivePermissionsFromIsOrderedRatherThanAMap(t *testing.T) {
	t.Parallel()

	// A map keyed by permission name would iterate in Go's randomised order, so
	// two runs of the same command would not compare.
	converted := effectivePermissionsFrom(map[string]bool{"REPO_READ": true, "REPO_ADMIN": true})

	if len(converted) != 3 {
		t.Fatalf("expected one entry per level, got %+v", converted)
	}
	for index, want := range []string{"REPO_READ", "REPO_WRITE", "REPO_ADMIN"} {
		if converted[index].Permission != want {
			t.Fatalf("entry %d = %q, want %q -- increasing privilege", index, converted[index].Permission, want)
		}
	}
	if !converted[0].Granted || converted[1].Granted || !converted[2].Granted {
		t.Fatalf("granted flags wrong: %+v", converted)
	}
}

func TestPermissionEntriesFromCarriesTheDisplayName(t *testing.T) {
	t.Parallel()

	converted := permissionEntriesFrom([]permissionEntry{
		{name: "alice", display: "Alice A", permission: "REPO_WRITE"},
	})

	want := PermissionEntry{Name: "alice", DisplayName: "Alice A", Permission: "REPO_WRITE"}
	if len(converted) != 1 || converted[0] != want {
		t.Fatalf("entries = %+v, want one %+v", converted, want)
	}

	if list := permissionEntriesFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("permissionEntriesFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestDefaultTaskFromFlattensBothMatchers(t *testing.T) {
	t.Parallel()

	id := int64(9)
	description := "check the changelog"
	sourceID, sourceDisplay := "refs/heads/feature/*", "feature/*"
	targetID, targetDisplay := "refs/heads/main", "main"

	converted := defaultTaskFrom(reposettings.DefaultTask{
		Id:            &id,
		Description:   &description,
		SourceMatcher: &reposettings.DefaultTaskMatcher{Id: &sourceID, DisplayId: &sourceDisplay},
		TargetMatcher: &reposettings.DefaultTaskMatcher{Id: &targetID, DisplayId: &targetDisplay},
	})

	if converted.ID != 9 || converted.Description != description {
		t.Fatalf("task fields wrong: %+v", converted)
	}
	if converted.SourceMatcher.ID != sourceID || converted.TargetMatcher.DisplayID != targetDisplay {
		t.Fatalf("matchers = %+v / %+v", converted.SourceMatcher, converted.TargetMatcher)
	}

	if value := defaultTaskValue(nil); value.ID != 0 {
		t.Fatalf("defaultTaskValue(nil) = %+v, want the zero value", value)
	}
	if list := defaultTasksFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("defaultTasksFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestSyncStatusFromSplitsTheThreeRefLists(t *testing.T) {
	t.Parallel()

	repository := result.Repository{ProjectKey: "PRJ", Slug: "fork"}
	converted := syncStatusFrom(repository, ptr(upstreamFromJSON[openapigenerated.RestRefSyncStatus](t, `{
		"available": true,
		"enabled": true,
		"lastSync": 1700000000000,
		"aheadRefs": [{"id": "refs/heads/main", "displayId": "main", "type": "BRANCH", "state": "AHEAD"}],
		"divergedRefs": [{"id": "refs/heads/old", "displayId": "old", "type": "BRANCH", "state": "DIVERGED", "tag": false}],
		"orphanedRefs": [{"id": "refs/tags/v1", "displayId": "v1", "type": "TAG", "state": "ORPHANED", "tag": true}]
	}`)))

	if converted.Repository != repository || !converted.Available || !converted.Enabled {
		t.Fatalf("status fields wrong: %+v", converted)
	}
	if len(converted.AheadRefs) != 1 || converted.AheadRefs[0].DisplayID != "main" || converted.AheadRefs[0].State != "AHEAD" {
		t.Fatalf("aheadRefs = %+v", converted.AheadRefs)
	}
	if len(converted.DivergedRefs) != 1 || converted.DivergedRefs[0].State != "DIVERGED" {
		t.Fatalf("divergedRefs = %+v", converted.DivergedRefs)
	}
	if len(converted.OrphanedRefs) != 1 || !converted.OrphanedRefs[0].Tag {
		t.Fatalf("orphanedRefs = %+v", converted.OrphanedRefs)
	}

	// A repository that is not a fork reports nothing, and the three lists must
	// still be lists rather than absent.
	empty := syncStatusFrom(repository, nil)
	if empty.AheadRefs == nil || empty.DivergedRefs == nil || empty.OrphanedRefs == nil {
		t.Fatalf("an unavailable status left a nil list: %+v", empty)
	}
}

func TestAutoMergeAndAutoDeclineSettingsFrom(t *testing.T) {
	t.Parallel()

	repository := result.Repository{ProjectKey: "PRJ", Slug: "payments"}

	merge := autoMergeSettingsFrom(repository, ptr(upstreamFromJSON[openapigenerated.RestAutoMergeRestrictedSettings](t,
		`{"enabled": true, "restrictionState": "RESTRICTED_MODIFIABLE", "scope": {"resourceId": 1, "type": "REPOSITORY"}}`)))
	if !merge.Enabled || merge.RestrictionState != "RESTRICTED_MODIFIABLE" || merge.Repository != repository {
		t.Fatalf("auto-merge = %+v", merge)
	}
	if absent := autoMergeSettingsFrom(repository, nil); absent.Enabled {
		t.Fatalf("an absent setting reported enabled: %+v", absent)
	}

	decline := autoDeclineSettingsFrom(repository, ptr(upstreamFromJSON[openapigenerated.RestAutoDeclineSettings](t,
		`{"enabled": true, "inactivityWeeks": 4, "scope": {"resourceId": 1, "type": "REPOSITORY"}}`)))
	if !decline.Enabled || decline.InactivityWeeks != 4 {
		t.Fatalf("auto-decline = %+v", decline)
	}
	if absent := autoDeclineSettingsFrom(repository, nil); absent.Enabled {
		t.Fatalf("an absent setting reported enabled: %+v", absent)
	}
}

// TestPullRequestSettingsFromReadsBothSpellingsOfTheCount is the reason the
// count and the enabled flag are separate fields: Bitbucket sends the count as
// a string on some versions and a number on others, and reports a count while
// the check is off.
func TestPullRequestSettingsFromReadsBothSpellingsOfTheCount(t *testing.T) {
	t.Parallel()

	repository := result.Repository{ProjectKey: "PRJ", Slug: "payments"}

	asNumber := pullRequestSettingsFrom(repository, map[string]any{
		"requiredAllTasksComplete": true,
		"requiredApprovers":        map[string]any{"enabled": true, "count": float64(2)},
		"mergeConfig": map[string]any{
			"defaultStrategy": map[string]any{"id": "squash"},
			"strategies": []any{
				map[string]any{"id": "squash", "name": "Squash", "enabled": true},
				map[string]any{"id": "no-ff", "name": "Merge commit", "enabled": false},
			},
		},
	})
	if valueOf(asNumber.RequiredApprovers) != 2 || !valueOf(asNumber.RequiredApproversEnabled) || !valueOf(asNumber.RequiredAllTasksComplete) {
		t.Fatalf("numeric count = %+v", asNumber)
	}
	if valueOf(asNumber.DefaultMergeStrategy) != "squash" || asNumber.MergeStrategies == nil || len(*asNumber.MergeStrategies) != 2 {
		t.Fatalf("merge config = %+v", asNumber)
	}
	if strategies := *asNumber.MergeStrategies; !strategies[0].Enabled || strategies[1].Enabled {
		t.Fatalf("strategy flags = %+v", strategies)
	}

	asString := pullRequestSettingsFrom(repository, map[string]any{
		"requiredApprovers": map[string]any{"enabled": true, "count": "2"},
	})
	if valueOf(asString.RequiredApprovers) != 2 {
		t.Fatalf("string count = %+v", asString)
	}

	// Bitbucket answers set-strategy with the default alone, so the strategy
	// list is absent and the default must still come through.
	defaultOnly := pullRequestSettingsFrom(repository, map[string]any{
		"mergeConfig": map[string]any{"defaultStrategy": map[string]any{"id": "squash"}},
	})
	if valueOf(defaultOnly.DefaultMergeStrategy) != "squash" {
		t.Fatalf("default-only response = %+v", defaultOnly)
	}
	// The configuration was reported and the strategies key was not, so the
	// list is absent rather than empty. An empty list would say the repository
	// offers no strategies, which this response does not claim.
	if defaultOnly.MergeStrategies != nil {
		t.Fatalf("strategies = %v, want absent when the response carried no list", *defaultOnly.MergeStrategies)
	}

	nonsense := pullRequestSettingsFrom(repository, map[string]any{"requiredApprovers": map[string]any{"enabled": true, "count": "two"}})
	if valueOf(nonsense.RequiredApprovers) != 0 {
		t.Fatalf("an unparseable count produced %v, want zero", nonsense.RequiredApprovers)
	}

	// The response said nothing, so neither does the payload. Publishing a Go
	// zero here asserted "this repository does not require all tasks complete"
	// about a repository nothing had read -- the update endpoints answer with
	// an empty body on some versions, and the service then hands back the
	// request map, where every unasked key is simply missing.
	silent := pullRequestSettingsFrom(repository, map[string]any{})
	if silent.RequiredApprovers != nil || silent.RequiredApproversEnabled != nil ||
		silent.RequiredAllTasksComplete != nil || silent.DefaultMergeStrategy != nil || silent.MergeStrategies != nil {
		t.Fatalf("an empty response reported settings: %+v", silent)
	}
	encoded, err := json.Marshal(silent)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	for _, key := range []string{"requiredApprovers", "requiredApproversEnabled", "requiredAllTasksComplete", "mergeStrategies"} {
		if strings.Contains(string(encoded), key) {
			t.Errorf("%q reached the document for a response that did not report it: %s", key, encoded)
		}
	}
}

// valueOf reads a pointer field, treating absent as the zero value. Only for
// assertions that are about the value; absence has its own checks above.
func valueOf[T any](value *T) T {
	if value == nil {
		var zero T
		return zero
	}

	return *value
}

func TestChangesFromJoinsThePaths(t *testing.T) {
	t.Parallel()

	converted := changesFrom([]openapigenerated.RestChange{
		upstreamFromJSON[openapigenerated.RestChange](t, `{
			"path": {"components": ["internal", "cli", "new.go"]},
			"srcPath": {"components": ["internal", "cli", "old.go"]},
			"type": "MOVE",
			"nodeType": "FILE",
			"executable": true
		}`),
	})

	want := Change{Path: "internal/cli/new.go", SrcPath: "internal/cli/old.go", Type: "MOVE", NodeType: "FILE", Executable: true}
	if len(converted) != 1 || converted[0] != want {
		t.Fatalf("changes = %+v, want one %+v", converted, want)
	}

	if list := changesFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("changesFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestSSHKeyFromKeepsOnlyTheKeyItself(t *testing.T) {
	t.Parallel()

	converted := sshKeyFrom(upstreamFromJSON[openapigenerated.RestSshAccessKey](t, `{
		"permission": "REPO_WRITE",
		"key": {"id": 9, "label": "deploy", "fingerprint": "SHA256:abc", "text": "ssh-rsa AAAA"},
		"repository": {"slug": "payments", "project": {"key": "PRJ"}}
	}`))

	want := SSHKey{ID: 9, Label: "deploy", Fingerprint: "SHA256:abc", Text: "ssh-rsa AAAA", Permission: "REPO_WRITE"}
	if converted != want {
		t.Fatalf("key = %+v, want %+v", converted, want)
	}

	if list := sshKeysFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("sshKeysFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestFileEditFromCarriesTheCommitItProduced(t *testing.T) {
	t.Parallel()

	repository := result.Repository{ProjectKey: "PRJ", Slug: "payments"}
	commit := upstreamFromJSON[openapigenerated.RestCommit](t, `{"id": "abc123", "displayId": "abc123", "message": "edit"}`)

	converted := fileEditFrom(repository, "README.md", "main", &commit)
	if converted.Path != "README.md" || converted.Branch != "main" || converted.Commit.ID != "abc123" {
		t.Fatalf("edit = %+v", converted)
	}

	if absent := fileEditFrom(repository, "README.md", "main", nil); absent.Commit.ID != "" {
		t.Fatalf("a missing commit produced %+v", absent.Commit)
	}
}

func ptr[T any](value T) *T { return &value }

// TestFileLinesFromSaysWhenTheFileIsBinaryOrTruncated covers the two facts a
// caller cannot recover from the lines themselves.
//
// A binary file comes back with no lines and a caller reading only lines sees
// an empty file; a file longer than one page comes back with a prefix and looks
// like the whole thing. Both used to be dropped, so the payload was wrong in a
// way nothing in it admitted.
func TestFileLinesFromSaysWhenTheFileIsBinaryOrTruncated(t *testing.T) {
	t.Parallel()

	_, binary, complete := fileLinesFrom([]byte(`{"binary": true, "isLastPage": true}`))
	if !binary {
		t.Error("a binary response did not report itself as binary")
	}
	if !complete {
		t.Error("a binary response is the whole answer Bitbucket has; it is complete")
	}

	lines, binary, complete := fileLinesFrom([]byte(`{
		"lines": [{"text": "one"}, {"text": "two"}],
		"isLastPage": false
	}`))
	if binary {
		t.Error("a text response reported itself as binary")
	}
	if complete {
		t.Error("isLastPage false means more lines follow, so this is not the whole file")
	}
	if len(lines) != 2 {
		t.Fatalf("lines = %+v", lines)
	}
}

// TestPullRequestSettingsFromReadsRequiredApproversEitherWay is the second
// shape Bitbucket answers with.
//
// Older instances return requiredApprovers as a bare count; newer ones return
// an object with enabled and count. bb read only the object, so on an older
// instance the count came back zero and enabled came back false -- a repository
// that requires two approvers reported that it requires none.
func TestPullRequestSettingsFromReadsRequiredApproversEitherWay(t *testing.T) {
	t.Parallel()

	object := pullRequestSettingsFrom(result.Repository{ProjectKey: "PRJ", Slug: "payments"}, map[string]any{
		"requiredApprovers": map[string]any{"enabled": true, "count": float64(2)},
	})
	if valueOf(object.RequiredApprovers) != 2 || !valueOf(object.RequiredApproversEnabled) {
		t.Fatalf("object form = %+v", object)
	}

	// The bare count carries no enabled flag, so a non-zero count is what says
	// the rule is on.
	bare := pullRequestSettingsFrom(result.Repository{ProjectKey: "PRJ", Slug: "payments"}, map[string]any{"requiredApprovers": float64(3)})
	if valueOf(bare.RequiredApprovers) != 3 || !valueOf(bare.RequiredApproversEnabled) {
		t.Fatalf("bare form = %+v", bare)
	}

	off := pullRequestSettingsFrom(result.Repository{ProjectKey: "PRJ", Slug: "payments"}, map[string]any{"requiredApprovers": float64(0)})
	if valueOf(off.RequiredApprovers) != 0 || valueOf(off.RequiredApproversEnabled) {
		t.Fatalf("a count of zero is the rule being off: %+v", off)
	}

	// An instance that quotes its numbers is still answering the question.
	quoted := pullRequestSettingsFrom(result.Repository{ProjectKey: "PRJ", Slug: "payments"}, map[string]any{"requiredApprovers": "4"})
	if valueOf(quoted.RequiredApprovers) != 4 {
		t.Fatalf("quoted form = %+v", quoted)
	}

	// Absent, not zero: an instance that did not answer is not an instance
	// that answered no.
	if absent := pullRequestSettingsFrom(result.Repository{ProjectKey: "PRJ", Slug: "payments"}, map[string]any{}); absent.RequiredApprovers != nil || absent.RequiredApproversEnabled != nil {
		t.Fatalf("absent = %+v", absent)
	}
}
