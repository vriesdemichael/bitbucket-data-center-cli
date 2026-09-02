package result

import (
	"encoding/json"
	"reflect"
	"testing"

	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	repositoryservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
)

// The converters in this package are where a payload's shape is actually
// decided, and every one of them is a chain of "if the server said so" branches.
// A branch nobody exercises is a branch nobody has read: bb repo browse blame
// shipped for months reading Bitbucket's blame as an object when it is a list,
// and the fixture that should have caught it encoded the same misreading.
//
// So these tests do two things per converter: an empty upstream value, which is
// what a sparse instance sends, and a fully populated one, which is where the
// field mapping can be wrong without anything failing.

func stringPointer(value string) *string { return &value }
func boolPointer(value bool) *bool       { return &value }
func int32Pointer(value int32) *int32    { return &value }

func TestUserFromCarriesEveryField(t *testing.T) {
	t.Parallel()

	userType := openapigenerated.ApplicationUserType("NORMAL")
	converted := UserFrom(openapigenerated.ApplicationUser{
		Id:           int32Pointer(7),
		Name:         stringPointer("alice"),
		DisplayName:  stringPointer("Alice A"),
		EmailAddress: stringPointer("alice@example.com"),
		Slug:         stringPointer("alice"),
		Type:         &userType,
		Active:       boolPointer(true),
	})

	want := User{ID: 7, Name: "alice", DisplayName: "Alice A", EmailAddress: "alice@example.com", Slug: "alice", Type: "NORMAL", Active: true}
	if converted != want {
		t.Fatalf("UserFrom() = %+v, want %+v", converted, want)
	}

	if empty := UserFrom(openapigenerated.ApplicationUser{}); empty != (User{}) {
		t.Fatalf("an empty upstream user produced %+v, want the zero value", empty)
	}
}

func TestRestUserFromMatchesTheNestedSpelling(t *testing.T) {
	t.Parallel()

	// The two upstream user types are the reason this package has one User:
	// which one a caller saw used to depend on which endpoint answered.
	userType := openapigenerated.RestApplicationUserType("NORMAL")
	rest := RestUserFrom(openapigenerated.RestApplicationUser{
		Id:           int32Pointer(7),
		Name:         stringPointer("alice"),
		DisplayName:  stringPointer("Alice A"),
		EmailAddress: stringPointer("alice@example.com"),
		Slug:         stringPointer("alice"),
		Type:         &userType,
		Active:       boolPointer(true),
	})

	nestedType := openapigenerated.ApplicationUserType("NORMAL")
	nested := UserFrom(openapigenerated.ApplicationUser{
		Id:           int32Pointer(7),
		Name:         stringPointer("alice"),
		DisplayName:  stringPointer("Alice A"),
		EmailAddress: stringPointer("alice@example.com"),
		Slug:         stringPointer("alice"),
		Type:         &nestedType,
		Active:       boolPointer(true),
	})

	if rest != nested {
		t.Fatalf("the two upstream user types converge on different values:\nrest:   %+v\nnested: %+v", rest, nested)
	}

	if users := RestUsersFrom(nil); users == nil || len(users) != 0 {
		t.Fatalf("RestUsersFrom(nil) = %v, want an empty slice rather than nil", users)
	}
	if users := UsersFrom(nil); users == nil || len(users) != 0 {
		t.Fatalf("UsersFrom(nil) = %v, want an empty slice rather than nil", users)
	}
}

func TestRefFromReadsTheMinimalRef(t *testing.T) {
	t.Parallel()

	refType := openapigenerated.RestMinimalRefType("BRANCH")
	converted := RefFrom(openapigenerated.RestMinimalRef{
		Id:        stringPointer("refs/heads/main"),
		DisplayId: stringPointer("main"),
		Type:      &refType,
	})

	want := Ref{ID: "refs/heads/main", DisplayID: "main", Type: "BRANCH"}
	if converted != want {
		t.Fatalf("RefFrom() = %+v, want %+v", converted, want)
	}

	if refs := RefsFrom(nil); refs == nil || len(refs) != 0 {
		t.Fatalf("RefsFrom(nil) = %v, want an empty slice rather than nil", refs)
	}
}

func TestWebhooksFromHandlesBothShapesBitbucketSends(t *testing.T) {
	t.Parallel()

	// The defect this converter exists for: an array from one Bitbucket
	// version, a pagination envelope from another, for the same command.
	bare := []any{map[string]any{"id": 42.0, "name": "ci", "url": "https://ci.example", "active": true, "events": []any{"repo:refs_changed"}}}
	paginated := map[string]any{"values": bare, "size": 1.0}

	fromBare := WebhooksFrom(bare)
	fromPaginated := WebhooksFrom(paginated)

	if len(fromBare) != 1 || len(fromPaginated) != 1 {
		t.Fatalf("expected one webhook from each shape, got %d and %d", len(fromBare), len(fromPaginated))
	}
	if !reflect.DeepEqual(fromBare, fromPaginated) {
		t.Fatalf("the two shapes produced different webhooks:\narray:     %+v\nenvelope:  %+v", fromBare, fromPaginated)
	}
	want := Webhook{ID: 42, Name: "ci", URL: "https://ci.example", Active: true, Events: []string{"repo:refs_changed"}}
	if !reflect.DeepEqual(fromBare[0], want) {
		t.Fatalf("WebhooksFrom() = %+v, want %+v", fromBare[0], want)
	}

	if hooks := WebhooksFrom(nil); hooks == nil || len(hooks) != 0 {
		t.Fatalf("WebhooksFrom(nil) = %v, want an empty slice rather than nil", hooks)
	}
	if hook := WebhookFrom(nil); !reflect.DeepEqual(hook, Webhook{}) {
		t.Fatalf("WebhookFrom(nil) = %+v, want the zero value", hook)
	}
	if hooks := WebhooksFrom("not a webhook payload"); len(hooks) != 0 {
		t.Fatalf("WebhooksFrom(garbage) = %v, want an empty slice", hooks)
	}
}

// upstreamFromJSON decodes a Bitbucket response into the generated type.
//
// Building these by hand is impractical: the generated client renders nested
// objects as anonymous structs, so a literal has to repeat the whole shape. A
// response body is also what the converter sees in production, which makes the
// fixture the thing being tested rather than a translation of it.
func upstreamFromJSON[T any](t *testing.T, body string) T {
	t.Helper()

	var decoded T
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}

	return decoded
}

func TestRepositoryDetailFromDropsNavigationAndKeepsTheOrigin(t *testing.T) {
	t.Parallel()

	converted := RepositoryDetailFrom(upstreamFromJSON[openapigenerated.RestRepository](t, `{
		"id": 11,
		"slug": "payments",
		"name": "Payments",
		"description": "money",
		"defaultBranch": "refs/heads/main",
		"public": true,
		"forkable": true,
		"archived": false,
		"scmId": "git",
		"state": "AVAILABLE",
		"statusMessage": "ready",
		"links": {"self": [{"href": "https://bitbucket.example/payments"}]},
		"project": {"key": "PRJ", "name": "Project"},
		"origin": {"slug": "payments", "project": {"key": "UPSTREAM"}}
	}`))

	if converted.ID != 11 || converted.ProjectKey != "PRJ" || converted.Slug != "payments" {
		t.Fatalf("identity fields wrong: %+v", converted)
	}
	if converted.Name != "Payments" || converted.Description != "money" || converted.DefaultBranch != "refs/heads/main" {
		t.Fatalf("descriptive fields wrong: %+v", converted)
	}
	if !converted.Public || !converted.Forkable || converted.Archived {
		t.Fatalf("flags wrong: %+v", converted)
	}
	if converted.ScmID != "git" || converted.State != "AVAILABLE" || converted.StatusMessage != "ready" {
		t.Fatalf("state fields wrong: %+v", converted)
	}
	if converted.Origin == nil || *converted.Origin != (Repository{ProjectKey: "UPSTREAM", Slug: "payments"}) {
		t.Fatalf("origin = %+v, want the parent named", converted.Origin)
	}

	plain := RepositoryDetailFrom(upstreamFromJSON[openapigenerated.RestRepository](t, `{"slug": "demo"}`))
	if plain.Origin != nil {
		t.Fatalf("a repository that is not a fork reported an origin: %+v", plain.Origin)
	}
}

func TestRestrictionFromFlattensTheMatcherAndTrimsTheAccessKeys(t *testing.T) {
	t.Parallel()

	converted := RestrictionFrom(upstreamFromJSON[openapigenerated.RestRefRestriction](t, `{
		"id": 3,
		"type": "read-only",
		"matcher": {"id": "refs/heads/main", "displayId": "main", "type": {"id": "BRANCH", "name": "Branch"}},
		"scope": {"resourceId": 1, "type": "REPOSITORY"},
		"groups": ["release-managers"],
		"users": [{"name": "alice", "displayName": "Alice A", "slug": "alice", "active": true}],
		"accessKeys": [{
			"permission": "REPO_WRITE",
			"key": {"id": 9, "label": "deploy", "fingerprint": "SHA256:abc", "text": "ssh-rsa AAAA"},
			"repository": {"slug": "payments", "project": {"key": "PRJ"}}
		}]
	}`))

	if converted.ID != 3 || converted.Type != "read-only" || converted.Scope != "REPOSITORY" {
		t.Fatalf("restriction fields wrong: %+v", converted)
	}
	if converted.Matcher != (RefMatcher{ID: "refs/heads/main", DisplayID: "main", Type: "BRANCH"}) {
		t.Fatalf("matcher = %+v, want the kind flattened to its id", converted.Matcher)
	}
	if len(converted.Groups) != 1 || converted.Groups[0] != "release-managers" {
		t.Fatalf("groups = %v", converted.Groups)
	}
	if len(converted.Users) != 1 || converted.Users[0].Name != "alice" {
		t.Fatalf("users = %+v", converted.Users)
	}
	// The point of the trimmed key: the upstream nests the whole repository and
	// its project underneath, none of which describes the restriction.
	want := AccessKey{ID: 9, Label: "deploy", Fingerprint: "SHA256:abc", Permission: "REPO_WRITE"}
	if len(converted.AccessKeys) != 1 || converted.AccessKeys[0] != want {
		t.Fatalf("accessKeys = %+v, want one %+v", converted.AccessKeys, want)
	}

	if list := RestrictionsFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("RestrictionsFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestConditionFromFlattensBothMatchers(t *testing.T) {
	t.Parallel()

	converted := ConditionFrom(upstreamFromJSON[openapigenerated.RestPullRequestCondition](t, `{
		"id": 5,
		"requiredApprovals": 2,
		"scope": {"resourceId": 1, "type": "PROJECT"},
		"sourceRefMatcher": {"id": "ANY_REF_MATCHER_ID", "displayId": "any ref", "type": {"id": "ANY_REF", "name": "Any ref"}},
		"targetRefMatcher": {"id": "refs/heads/main", "displayId": "main", "type": {"id": "BRANCH", "name": "Branch"}},
		"reviewers": [{"id": 12, "name": "alice"}],
		"reviewerGroups": [{"id": 30, "name": "reviewers"}]
	}`))

	if converted.ID != 5 || converted.RequiredApprovals != 2 || converted.Scope != "PROJECT" {
		t.Fatalf("condition fields wrong: %+v", converted)
	}
	if converted.SourceRefMatcher.Type != "ANY_REF" || converted.TargetRefMatcher.Type != "BRANCH" {
		t.Fatalf("matchers wrong: %+v / %+v", converted.SourceRefMatcher, converted.TargetRefMatcher)
	}
	if len(converted.Reviewers) != 1 || converted.Reviewers[0] != (Participant{ID: 12, Name: "alice"}) {
		t.Fatalf("reviewers = %+v", converted.Reviewers)
	}
	if len(converted.ReviewerGroups) != 1 || converted.ReviewerGroups[0] != (Participant{ID: 30, Name: "reviewers"}) {
		t.Fatalf("reviewerGroups = %+v", converted.ReviewerGroups)
	}

	if list := ConditionsFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("ConditionsFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestRequiredBuildCheckFromReadsBothMatchersAndBothEnforcementFlags(t *testing.T) {
	t.Parallel()

	body := `{
		"id": 4,
		"buildParentKeys": ["ci"],
		"refMatcher": {"id": "refs/heads/main", "displayId": "main", "type": {"id": "BRANCH", "name": "Branch"}},
		"exemptRefMatcher": {"id": "release/*", "displayId": "release/*", "type": {"id": "PATTERN", "name": "Pattern"}},
		"requiredForPullRequest": true,
		"requiredForMergeQueue": false
	}`

	converted := RequiredBuildCheckFrom(upstreamFromJSON[openapigenerated.RestRequiredBuildCondition](t, body))
	if converted.ID != 4 || len(converted.BuildParentKeys) != 1 {
		t.Fatalf("check fields wrong: %+v", converted)
	}
	if converted.RefMatcher.Type != "BRANCH" || converted.ExemptRefMatcher.Type != "PATTERN" {
		t.Fatalf("matchers wrong: %+v / %+v", converted.RefMatcher, converted.ExemptRefMatcher)
	}
	if !converted.RequiredForPullRequest || converted.RequiredForMergeQueue {
		t.Fatalf("enforcement flags wrong: %+v", converted)
	}

	// The same object arrives untyped from the create and update calls, and as
	// either an array or a pagination envelope from the merge-checks listing.
	var asMap map[string]any
	if err := json.Unmarshal([]byte(body), &asMap); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if fromMap := RequiredBuildCheckFromMap(asMap); !reflect.DeepEqual(fromMap, converted) {
		t.Fatalf("the untyped path produced a different check:\nmap:   %+v\ntyped: %+v", fromMap, converted)
	}
	if empty := RequiredBuildCheckFromMap(nil); empty.ID != 0 {
		t.Fatalf("RequiredBuildCheckFromMap(nil) = %+v, want the zero value", empty)
	}

	bare := []any{asMap}
	paginated := map[string]any{"values": bare}
	if fromArray, fromEnvelope := RequiredBuildChecksFromAny(bare), RequiredBuildChecksFromAny(paginated); len(fromArray) != 1 || len(fromEnvelope) != 1 {
		t.Fatalf("expected one check from each shape, got %d and %d", len(fromArray), len(fromEnvelope))
	}
	if checks := RequiredBuildChecksFromAny(nil); checks == nil || len(checks) != 0 {
		t.Fatalf("RequiredBuildChecksFromAny(nil) = %v, want an empty slice rather than nil", checks)
	}
}

func TestCommitFromKeepsBothPeopleAndOnlyTheParentIds(t *testing.T) {
	t.Parallel()

	converted := CommitFrom(upstreamFromJSON[openapigenerated.RestCommit](t, `{
		"id": "1111111111111111111111111111111111111111",
		"displayId": "1111111",
		"message": "subject line\n\nbody",
		"author": {"name": "Alice", "emailAddress": "alice@example.com"},
		"authorTimestamp": 1700000000000,
		"committer": {"name": "Bob", "emailAddress": "bob@example.com"},
		"committerTimestamp": 1700000001000,
		"parents": [{"id": "2222222222222222222222222222222222222222"}, {"id": "3333333333333333333333333333333333333333"}]
	}`))

	if converted.Subject() != "subject line" {
		t.Fatalf("Subject() = %q, want the first line only", converted.Subject())
	}
	if converted.Author.Name != "Alice" || converted.Committer.Name != "Bob" {
		t.Fatalf("expected the author and committer to differ: %+v", converted)
	}
	if len(converted.Parents) != 2 || converted.Parents[0] != "2222222222222222222222222222222222222222" {
		t.Fatalf("parents = %v, want the ids only", converted.Parents)
	}

	if list := CommitsFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("CommitsFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestPullRequestFromCarriesTheForkAndTheCounts(t *testing.T) {
	t.Parallel()

	commentCount := 4
	openTasks := 1
	resolvedTasks := 2
	upstream := pullrequestservice.PullRequest{
		ID:                42,
		Title:             "Add payments",
		Description:       "the body",
		State:             "OPEN",
		Open:              true,
		Draft:             true,
		Version:           3,
		Author:            "Alice A",
		AuthorUsername:    "alice",
		SourceBranch:      "feature/pay",
		TargetBranch:      "main",
		SourceCommit:      "abc123",
		CreatedDate:       1700000000000,
		UpdatedDate:       1700000001000,
		Repository:        &pullrequestservice.RepositoryRef{ProjectKey: "PRJ", Slug: "payments"},
		SourceRepository:  &pullrequestservice.RepositoryRef{ProjectKey: "~alice", Slug: "payments"},
		CommentCount:      &commentCount,
		OpenTaskCount:     &openTasks,
		ResolvedTaskCount: &resolvedTasks,
		Reviewers: []pullrequestservice.Reviewer{
			{Name: "bob", DisplayName: "Bob B", Email: "bob@example.com", Role: "REVIEWER", Status: "APPROVED", Approved: true},
		},
		Mergeability: &pullrequestservice.Mergeability{
			Mergeable:  false,
			Outcome:    "CONFLICTED",
			Conflicted: true,
			Blockers:   []pullrequestservice.MergeBlocker{{Summary: "conflicts", Detail: "in payments.go"}},
		},
	}

	converted := PullRequestFrom(upstream)

	// A fork pull request is told apart from a same-repository one by exactly
	// this: the two repositories differing.
	if converted.Repository != (Repository{ProjectKey: "PRJ", Slug: "payments"}) {
		t.Fatalf("repository = %+v", converted.Repository)
	}
	if converted.SourceRepository != (Repository{ProjectKey: "~alice", Slug: "payments"}) {
		t.Fatalf("sourceRepository = %+v", converted.SourceRepository)
	}
	if converted.ID != 42 || converted.Title != "Add payments" || !converted.Draft || converted.Version != 3 {
		t.Fatalf("scalar fields wrong: %+v", converted)
	}
	if len(converted.Reviewers) != 1 || !converted.Reviewers[0].Approved || converted.Reviewers[0].Status != "APPROVED" {
		t.Fatalf("reviewers = %+v", converted.Reviewers)
	}
	if converted.Mergeability == nil || !converted.Mergeability.Conflicted || len(converted.Mergeability.Blockers) != 1 {
		t.Fatalf("mergeability = %+v", converted.Mergeability)
	}
	// The counts stay pointers because absent and zero are different claims.
	if converted.CommentCount == nil || *converted.CommentCount != 4 {
		t.Fatalf("commentCount = %v, want the reported value carried through", converted.CommentCount)
	}

	bare := PullRequestFrom(pullrequestservice.PullRequest{ID: 1})
	if bare.Repository != (Repository{}) || bare.SourceRepository != (Repository{}) {
		t.Fatalf("an unreported repository produced %+v / %+v", bare.Repository, bare.SourceRepository)
	}
	if bare.Mergeability != nil || bare.CommentCount != nil {
		t.Fatalf("unmeasured values were reported as measured: %+v", bare)
	}

	if list := PullRequestsFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("PullRequestsFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestRepositorySummariesFromNamesTheRepositoryPlainly(t *testing.T) {
	t.Parallel()

	converted := RepositorySummariesFrom([]repositoryservice.Repository{
		{ProjectKey: "PRJ", Slug: "payments", Name: "Payments", Public: true},
	})

	if len(converted) != 1 {
		t.Fatalf("expected one summary, got %d", len(converted))
	}
	want := RepositorySummary{ProjectKey: "PRJ", Slug: "payments", Name: "Payments", Public: true}
	if converted[0] != want {
		t.Fatalf("summary = %+v, want %+v", converted[0], want)
	}

	if list := RepositorySummariesFrom(nil); list == nil || len(list) != 0 {
		t.Fatalf("RepositorySummariesFrom(nil) = %v, want an empty slice rather than nil", list)
	}
}

func TestOKIsTheOutcomeEveryCommandReports(t *testing.T) {
	t.Parallel()

	if OK() != (Status{Status: "ok"}) {
		t.Fatalf("OK() = %+v", OK())
	}
}

func TestDeclareAndSchemaForRoundTrip(t *testing.T) {
	t.Parallel()

	// Not t.Parallel-safe against Declare from another test, but the registry
	// is keyed by path and this path belongs to no command.
	const path = "zz test only"

	if _, ok := SchemaFor(path); ok {
		t.Fatalf("%q was already declared", path)
	}

	Declare(path, For[Status](nil))

	schema, ok := SchemaFor(path)
	if !ok || schema == nil {
		t.Fatalf("SchemaFor(%q) did not return what was declared", path)
	}

	found := false
	for _, declared := range DeclaredPaths() {
		if declared == path {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("DeclaredPaths() does not list %q", path)
	}
}
