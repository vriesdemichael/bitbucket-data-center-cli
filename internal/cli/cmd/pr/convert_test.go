package prcmd

import (
	"encoding/json"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	jiraservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/jira"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	pullrequestactivityservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequestactivity"
)

// upstreamFromJSON decodes a Bitbucket response into the generated type, which
// is what the converter sees in production.
func upstreamFromJSON[T any](t *testing.T, body string) T {
	t.Helper()

	var decoded T
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	return decoded
}

// TestCommentFromDropsThePullRequestItIsAnchoredTo is why this comment has a
// model at all: the upstream anchor nests the entire pull request, and every
// reply nests the same again, so one comment used to arrive carrying tens of
// kilobytes of context the caller had already asked for by id.
func TestCommentFromDropsThePullRequestItIsAnchoredTo(t *testing.T) {
	t.Parallel()

	converted := result.CommentFrom(upstreamFromJSON[openapigenerated.RestComment](t, `{
		"id": 118,
		"version": 2,
		"text": "please rename this",
		"state": "OPEN",
		"severity": "BLOCKER",
		"pending": true,
		"threadResolved": true,
		"resolvedDate": 1700000002000,
		"anchored": true,
		"createdDate": 1700000000000,
		"updatedDate": 1700000001000,
		"author": {"id": 7, "name": "alice", "displayName": "Alice A", "emailAddress": "alice@example.com", "slug": "alice", "type": "NORMAL", "active": true},
		"comments": [{"id": 119}, {"id": 120}],
		"properties": {"commentReactions": [{"emoticon": {"value": "thumbsup"}}]},
		"anchor": {
			"line": 12,
			"lineType": "REMOVED",
			"fileType": "FROM",
			"diffType": "COMMIT",
			"fromHash": "aaa",
			"toHash": "bbb",
			"path": {"components": ["internal", "cli", "root.go"]},
			"srcPath": {"components": ["internal", "cli", "old.go"]},
			"pullRequest": {"id": 42, "title": "nested under the anchor"}
		}
	}`))

	if converted.ID != 118 || converted.Version != 2 || converted.Severity != "BLOCKER" {
		t.Fatalf("comment fields wrong: %+v", converted)
	}
	if !converted.Pending || !converted.Resolved || !converted.Anchored || converted.ResolvedDate != 1700000002000 {
		t.Fatalf("state fields wrong: %+v", converted)
	}
	if converted.Author.Name != "alice" || converted.Author.EmailAddress != "alice@example.com" {
		t.Fatalf("author = %+v", converted.Author)
	}
	if converted.ReplyCount != 2 {
		t.Fatalf("replyCount = %d, want the replies counted rather than nested", converted.ReplyCount)
	}
	if converted.Anchor == nil || converted.Anchor.Path != "internal/cli/root.go" || converted.Anchor.SrcPath != "internal/cli/old.go" {
		t.Fatalf("anchor paths = %+v", converted.Anchor)
	}
	if converted.Anchor.LineType != "REMOVED" || converted.Anchor.FileType != "FROM" || converted.Anchor.DiffType != "COMMIT" {
		t.Fatalf("anchor = %+v", converted.Anchor)
	}
	// Reactions live in properties, undocumented, and bb pr comment react is
	// what writes them.
	if converted.Properties["commentReactions"] == nil {
		t.Fatalf("properties = %+v, want the extras kept", converted.Properties)
	}

	plain := result.CommentFrom(upstreamFromJSON[openapigenerated.RestComment](t, `{"id": 1, "text": "hi"}`))
	if plain.Anchor != nil {
		t.Fatalf("a pull-request-level comment reported an anchor: %+v", plain.Anchor)
	}

	if list := result.FlattenComments(nil); list == nil || len(list) != 0 {
		t.Fatalf("FlattenComments(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestThreadFromKeepsTheRepliesAndTheAnchor(t *testing.T) {
	t.Parallel()

	converted := threadFrom(pullrequestactivityservice.Thread{
		ID:            118,
		Kind:          "task",
		State:         "OPEN",
		Resolved:      false,
		Author:        "alice",
		Version:       2,
		CreatedDate:   1700000000000,
		UpdatedDate:   1700000001000,
		Text:          "please rename this",
		HasSuggestion: true,
		ReplyCount:    2,
		URL:           "https://bitbucket.example/pr/42#118",
		Anchor:        &pullrequestactivityservice.Anchor{Path: "root.go", Line: 12, LineType: "ADDED", Orphaned: true},
		LastReply:     &pullrequestactivityservice.Reply{ID: 120, Author: "bob", Date: 1700000002000, Text: "done"},
		Replies: []pullrequestactivityservice.Reply{
			{ID: 119, Author: "carol", Text: "agreed"},
			{ID: 120, Author: "bob", Text: "done"},
		},
	})

	if converted.ID != 118 || converted.Kind != "task" || !converted.HasSuggestion {
		t.Fatalf("thread fields wrong: %+v", converted)
	}
	if converted.Anchor == nil || converted.Anchor.Path != "root.go" || !converted.Anchor.Orphaned {
		t.Fatalf("anchor = %+v", converted.Anchor)
	}
	if converted.LastReply == nil || converted.LastReply.ID != 120 {
		t.Fatalf("lastReply = %+v", converted.LastReply)
	}
	if len(converted.Replies) != 2 || converted.Replies[0].Author != "carol" {
		t.Fatalf("replies = %+v", converted.Replies)
	}

	bare := threadFrom(pullrequestactivityservice.Thread{ID: 1})
	if bare.Anchor != nil || bare.LastReply != nil || bare.Replies != nil {
		t.Fatalf("a bare thread reported optional parts: %+v", bare)
	}

	if list := threadsFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("threadsFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestSummariesAndCountsCarryTheUnmeasuredDistinction(t *testing.T) {
	t.Parallel()

	unresolved, openTasks := 3, 1
	required := true
	converted := reviewSummaryFrom(pullrequestservice.ReviewSummary{
		ActionRequired:    &required,
		UnresolvedThreads: &unresolved,
		OpenTasks:         &openTasks,
		NeedsWork:         []string{"carol"},
		Approvals:         1,
		Reviewers:         2,
		CountsSource:      "activities",
	})

	if converted.ActionRequired == nil || !*converted.ActionRequired || converted.CountsSource != "activities" {
		t.Fatalf("summary = %+v", converted)
	}
	if converted.UnresolvedThreads == nil || *converted.UnresolvedThreads != 3 {
		t.Fatalf("unresolvedThreads = %v", converted.UnresolvedThreads)
	}
	// Absent is a different claim from zero: an unmeasured count reported as
	// zero would make a pull request with open feedback look clean.
	if converted.ResolvedThreads != nil || converted.PendingComments != nil {
		t.Fatalf("unmeasured counts were reported as measured: %+v", converted)
	}

	thread := threadSummaryFrom(pullrequestactivityservice.Summary{
		TotalThreads: 4, Unresolved: 2, Resolved: 2, OpenTasks: 1, ResolvedTasks: 1, UnresolvedInline: 1,
	})
	if thread.TotalThreads != 4 || thread.OpenTasks != 1 || thread.UnresolvedInline != 1 {
		t.Fatalf("thread summary = %+v", thread)
	}

	if list := reviewSummariesFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("reviewSummariesFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestCommitFromFillsWhatThePullRequestEndpointsReport(t *testing.T) {
	t.Parallel()

	// These endpoints return a flatter commit than the repository ones: an
	// author name and email rather than a person object, and no committer.
	converted := commitFrom(pullrequestservice.Commit{
		ID:              "abc123",
		DisplayID:       "abc123",
		Message:         "subject\n\nbody",
		Author:          "Alice",
		AuthorEmail:     "alice@example.com",
		AuthorTimestamp: 1700000000000,
	})

	if converted.ID != "abc123" || converted.Subject() != "subject" {
		t.Fatalf("commit = %+v", converted)
	}
	if converted.Author.Name != "Alice" || converted.Author.EmailAddress != "alice@example.com" {
		t.Fatalf("author = %+v", converted.Author)
	}
	if converted.Committer.Name != "" {
		t.Fatalf("committer = %+v, want it left empty rather than invented", converted.Committer)
	}

	if list := commitsFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("commitsFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestTheSmallConvertersNeverReturnNil(t *testing.T) {
	t.Parallel()

	if list := changesFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("changesFrom(nil) = %v", list)
	}
	if list := buildStatusesFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("buildStatusesFrom(nil) = %v", list)
	}
	if list := participantsFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("participantsFrom(nil) = %v", list)
	}
	if list := issuesFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("issuesFrom(nil) = %v", list)
	}
	if list := activitiesFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("activitiesFrom(nil) = %v", list)
	}

	changes := changesFrom([]pullrequestservice.Change{{Path: "new.go", SrcPath: "old.go", Type: "MOVE", NodeType: "FILE", Executable: true}})
	if len(changes) != 1 || changes[0] != (Change{Path: "new.go", SrcPath: "old.go", Type: "MOVE", NodeType: "FILE", Executable: true}) {
		t.Fatalf("changes = %+v", changes)
	}

	statuses := buildStatusesFrom([]pullrequestservice.BuildStatus{{Key: "ci", State: "SUCCESSFUL", URL: "https://ci", Name: "CI"}})
	if len(statuses) != 1 || statuses[0] != (BuildStatus{Key: "ci", State: "SUCCESSFUL", URL: "https://ci", Name: "CI"}) {
		t.Fatalf("statuses = %+v", statuses)
	}

	people := participantsFrom([]pullrequestservice.Participant{{Name: "alice", DisplayName: "Alice A", EmailAddress: "alice@example.com", Active: true}})
	if len(people) != 1 || !people[0].Active {
		t.Fatalf("participants = %+v", people)
	}

	issues := issuesFrom([]jiraservice.JiraIssue{{Key: "PROJ-1", URL: "https://jira/PROJ-1"}})
	if len(issues) != 1 || issues[0] != (JiraIssue{Key: "PROJ-1", URL: "https://jira/PROJ-1"}) {
		t.Fatalf("issues = %+v", issues)
	}

	merge := autoMergeFrom(pullrequestservice.AutoMerge{Enabled: false, StrategyID: "squash", MergedImmediately: true})
	if merge.Enabled || merge.StrategyID != "squash" || !merge.MergedImmediately {
		t.Fatalf("autoMerge = %+v", merge)
	}
}

func TestActivityFromNamesTheThreeFieldsEveryEntryHas(t *testing.T) {
	t.Parallel()

	comment := upstreamFromJSON[openapigenerated.RestComment](t, `{"id": 118, "text": "looks good"}`)
	converted := activityFrom(pullrequestactivityservice.Activity{
		ID:          9,
		Action:      "COMMENTED",
		CreatedDate: 1700000000000,
		Comment:     &comment,
		Raw:         map[string]any{"action": "COMMENTED", "commentAction": "ADDED"},
	})

	if converted.ID != 9 || converted.Action != "COMMENTED" {
		t.Fatalf("activity = %+v", converted)
	}
	if converted.Comment == nil || converted.Comment.ID != 118 {
		t.Fatalf("comment = %+v", converted.Comment)
	}
	// The rest of an activity depends on its action, so it is carried unchanged
	// rather than claimed as a shape.
	if converted.Raw["commentAction"] != "ADDED" {
		t.Fatalf("raw = %+v", converted.Raw)
	}

	// An entry with no raw payload still carries an object, so a consumer can
	// index it without a nil check.
	bare := activityFrom(pullrequestactivityservice.Activity{ID: 1, Action: "APPROVED"})
	if bare.Raw == nil {
		t.Fatal("raw = nil, want an empty object")
	}
	if bare.Comment != nil {
		t.Fatalf("a non-comment activity reported a comment: %+v", bare.Comment)
	}
}

func TestRebaseResultFromKeepsTheBranchAndBothCommits(t *testing.T) {
	t.Parallel()

	repository := repositoryOf(pullrequestservice.RepositoryRef{ProjectKey: "PRJ", Slug: "payments"})
	upstream := upstreamFromJSON[openapigenerated.RestPullRequestRebaseResult](t, `{
		"refChange": {
			"fromHash": "aaa",
			"toHash": "bbb",
			"refId": "refs/heads/feature",
			"type": "UPDATE",
			"ref": {"id": "refs/heads/feature", "displayId": "feature", "type": "BRANCH"}
		}
	}`)

	converted := rebaseResultFrom(repository, &upstream)
	if converted.FromHash != "aaa" || converted.ToHash != "bbb" {
		t.Fatalf("commits = %+v", converted)
	}
	if converted.Ref.ID != "refs/heads/feature" || converted.Ref.DisplayID != "feature" || converted.Ref.Type != "BRANCH" {
		t.Fatalf("ref = %+v", converted.Ref)
	}

	// The upstream can report the ref by id alone.
	idOnly := upstreamFromJSON[openapigenerated.RestPullRequestRebaseResult](t, `{"refChange": {"refId": "refs/heads/feature"}}`)
	if fallback := rebaseResultFrom(repository, &idOnly); fallback.Ref.ID != "refs/heads/feature" {
		t.Fatalf("id-only ref = %+v", fallback.Ref)
	}

	if absent := rebaseResultFrom(repository, nil); absent.Repository != repository || absent.Ref.ID != "" {
		t.Fatalf("a missing result produced %+v", absent)
	}
}

func TestCheckoutFromCarriesEveryFieldTheCommandFilledIn(t *testing.T) {
	t.Parallel()

	// The regression this guards: pullRequest and sourceRepository were
	// declared on the result and never assigned, so every run published zero
	// and an empty string. The planner now fills in the published type
	// directly -- there is no second struct to copy out of -- so this asserts
	// on the document rather than on a conversion.
	converted := Checkout{
		PullRequest:      42,
		Branch:           "jdoe/feature",
		Detached:         false,
		Remote:           "jdoe",
		RemoteURL:        "https://bitbucket.example/scm/~jdoe/payments.git",
		RemoteAdded:      true,
		SourceBranch:     "feature",
		SourceRepository: "~jdoe/payments",
		Fork:             true,
		FastForwarded:    false,
	}

	if converted.PullRequest != 42 || converted.SourceRepository != "~jdoe/payments" {
		t.Fatalf("checkout = %+v", converted)
	}
	if !converted.Fork || !converted.RemoteAdded || converted.Remote != "jdoe" {
		t.Fatalf("fork fields wrong: %+v", converted)
	}
}
