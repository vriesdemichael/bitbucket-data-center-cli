package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// refresh_view is for views: MCP Apps keeps it from the model, and nothing in
// its metadata lets a host from before visibility take it for a tool that
// shows a view of its own.
func TestRefreshViewIsOfferedToViewsOnly(t *testing.T) {
	t.Parallel()

	tool := listedTool(t, connect(t, testClients(t), nil, nil), "refresh_view")
	if tool == nil {
		t.Fatal("refresh_view is not listed")
	}
	ui, _ := tool.Meta["ui"].(map[string]any)
	visibility, _ := ui["visibility"].([]any)
	if len(visibility) != 1 || visibility[0] != "app" {
		t.Errorf("refresh_view's visibility = %v, want [app]", ui["visibility"])
	}
	if _, flat := tool.Meta["ui/resourceUri"]; flat {
		t.Error("refresh_view carries the flat ui/resourceUri key, which a host without visibility reads as a view tool")
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Error("refresh_view is not annotated read-only")
	}

	for _, spec := range AllSpecs() {
		want := spec.Tool.Name == "refresh_view" || spec.Tool.Name == "suggest_form_values"
		if spec.AppOnly() != want {
			t.Errorf("%s: AppOnly() = %v, want %v", spec.Tool.Name, spec.AppOnly(), want)
		}
	}
}

// refresh_view goes with show: --tools selects what the model is offered and
// need not name it, and it goes when show does.
func TestRefreshViewGoesWithShow(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name           string
		allow, exclude []string
		want           bool
	}{
		{name: "every tool", want: true},
		{name: "--tools naming show and not refresh_view", allow: []string{"get_pull_request", "show"}, want: true},
		{name: "--exclude refresh_view", exclude: []string{"refresh_view"}, want: false},
		{name: "--exclude show", exclude: []string{"show"}, want: false},
		{name: "--tools without show", allow: []string{"get_pull_request"}, want: false},
		{name: "show gone with the tools it shows", allow: []string{"get_commit", "show"}, want: false},
	} {
		session := connect(t, testClients(t), tc.allow, tc.exclude)
		if listed := listedTool(t, session, "refresh_view") != nil; listed != tc.want {
			t.Errorf("%s: refresh_view listed = %v, want %v", tc.name, listed, tc.want)
		}
		if tc.want && listedTool(t, session, "show") == nil {
			t.Errorf("%s: refresh_view is listed without show", tc.name)
		}
	}

	if listedTool(t, connectWith(t, ServerOptions{Name: "bb", Version: "test", Clients: testClients(t), ReadOnly: true}), "refresh_view") == nil {
		t.Error("a read-only server does not keep its views current")
	}
}

// refresh_view refreshes the kinds show shows, and no other.
func TestRefreshViewOffersTheKindsShowOffers(t *testing.T) {
	t.Parallel()

	session := connect(t, testClients(t), nil, []string{"get_pr_diff"})
	show, refresh := listedTool(t, session, "show"), listedTool(t, session, "refresh_view")
	if show == nil || refresh == nil {
		t.Fatal("show or refresh_view is missing")
	}
	kinds := func(tool *mcp.Tool) []any {
		encoded, _ := json.Marshal(tool.InputSchema)
		var schema struct {
			Properties map[string]struct {
				Enum []any `json:"enum"`
			} `json:"properties"`
		}
		_ = json.Unmarshal(encoded, &schema)
		return schema.Properties["kind"].Enum
	}
	if !slices.Equal(kinds(show), kinds(refresh)) || slices.Contains(kinds(refresh), any(showKindDiff)) {
		t.Errorf("refresh_view offers %v and show %v; want the same, without diff", kinds(refresh), kinds(show))
	}

	handler := refreshViewHandler(testClients(t), viewOffers{Kinds: []string{showKindPullRequest}})
	if _, _, err := handler(context.Background(), nil, RefreshViewInput{ShowInput: ShowInput{Kind: showKindDiff, Project: "PROJ", Repo: "app", ID: "7"}}); err == nil ||
		!strings.Contains(err.Error(), "cannot refresh") {
		t.Errorf("refresh_view accepted a kind it does not offer: %v", err)
	}
	if _, _, err := handler(context.Background(), nil, RefreshViewInput{ShowInput: ShowInput{Kind: showKindPullRequest, Project: "PROJ"}}); err == nil ||
		!strings.Contains(err.Error(), "needs project, repo and id") {
		t.Errorf("refresh_view accepted a pull request without its id: %v", err)
	}
}

// A scoped server binds refresh_view as it binds show: a view cannot reach
// past the scope by asking for another project, and a call inside it goes
// through to Bitbucket, which this test's server cannot reach.
func TestRefreshViewIsBoundByTheScope(t *testing.T) {
	t.Parallel()

	session := connectWith(t, ServerOptions{
		Name: "bb", Version: "test", Clients: testClients(t),
		Scope: Scope{ProjectKey: "PROJ", RepoSlug: "payments"},
	})
	call := func(project string) string {
		t.Helper()
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name:      "refresh_view",
			Arguments: map[string]any{"kind": "pull_request", "project": project, "repo": "payments", "id": "7", "since": "0"},
		})
		if err != nil {
			t.Fatalf("tools/call returned a protocol error: %v", err)
		}
		if !result.IsError {
			t.Fatalf("refresh_view of %s succeeded against a server that refuses every connection", project)
		}
		return resultText(result)
	}
	if outside := call("OTHER"); !strings.Contains(outside, "PROJ") || strings.Contains(outside, "refresh failed") {
		t.Errorf("a refresh outside the scope answered %q, want the scope's refusal", outside)
	}
	if inside := call("PROJ"); !strings.Contains(inside, "refresh failed") {
		t.Errorf("a refresh inside the scope answered %q, want it to have reached for Bitbucket", inside)
	}
}

// The fingerprint changes with what a view shows, and with nothing else: not
// with when it was read, nor with the avatars, which differ between two reads
// when one avatar comes in late.
func TestTheFingerprintIsWhatTheViewShows(t *testing.T) {
	t.Parallel()

	pr := viewPullRequest{PullRequest: pullrequestservice.PullRequest{ID: 7, Title: "Retry payments", State: "OPEN"}}
	payload := viewPayload{
		Version: viewPayloadVersion, Kind: showKindPullRequest, GeneratedAt: "2026-09-29T10:00:00Z",
		PullRequest: &pr, Show: &ShowInput{Kind: showKindPullRequest, Project: "PAY", Repo: "ledger", ID: "7"},
	}
	first := fingerprintOf(payload)
	if first == "" {
		t.Fatal("no fingerprint")
	}

	later := payload
	later.GeneratedAt = "2026-09-29T11:00:00Z"
	later.Avatars = map[string]string{"alice": onePixelDataURI}
	later.Fingerprint = first
	if again := fingerprintOf(later); again != first {
		t.Errorf("a later read of the same state has fingerprint %s, want %s", again, first)
	}

	merged := pr
	merged.State = "MERGED"
	changed := payload
	changed.PullRequest = &merged
	if fingerprintOf(changed) == first {
		t.Error("a merged pull request has the fingerprint of an open one")
	}
}

// onePixelDataURI stands in for an avatar.
const onePixelDataURI = "data:image/png;base64,iVBORw0KGgo="

// The model reads what a view shows once when it is shown, and again when it
// changes, about the same subject.
func TestTheModelIsToldWhenAViewChanges(t *testing.T) {
	t.Parallel()

	summary := summarizePullRequests([]viewPullRequest{
		{PullRequest: pullrequestservice.PullRequest{ID: 7, Title: "Retry payments", State: "OPEN", Author: "Alice",
			Repository: &pullrequestservice.RepositoryRef{ProjectKey: "PAY", Slug: "ledger"}}},
	}, true)
	shown, changed := summary.shown(), summary.changed()
	if !strings.HasPrefix(shown, "Showed the person 1 pull requests as an interactive list (there may be more):\n- PAY/ledger#7") {
		t.Errorf("show's answer reads %q", shown)
	}
	if !strings.HasPrefix(changed, "The view of 1 pull requests you showed the person has changed:\n- PAY/ledger#7") {
		t.Errorf("the change reads %q", changed)
	}

	card := summarizePullRequest(viewPullRequest{PullRequest: pullrequestservice.PullRequest{ID: 7, Title: "Retry payments", State: "MERGED",
		Repository: &pullrequestservice.RepositoryRef{ProjectKey: "PAY", Slug: "ledger"}}})
	if got := card.changed(); !strings.HasPrefix(got, `The view of pull request PAY/ledger#7 you showed the person has changed: "Retry payments", merged`) {
		t.Errorf("the card's change reads %q", got)
	}
}

// A view is offered what the server exposes, and nothing more: the kinds it
// can open, and the model's tools it can call for the person.
func TestAViewIsOfferedWhatTheServerExposes(t *testing.T) {
	t.Parallel()

	offers := offersFor(map[string]bool{
		"get_pull_request": true, "list_pr_comments": true, "add_pr_comment": true, "show": true, "get_commit": true,
	})
	if !slices.Equal(offers.Kinds, []string{showKindPullRequest, showKindThreads}) {
		t.Errorf("kinds = %v, want pull_request and threads", offers.Kinds)
	}
	if !slices.Equal(offers.Tools, []string{"add_pr_comment"}) {
		t.Errorf("tools = %v, want add_pr_comment alone", offers.Tools)
	}
	if offers.kind(showKindDiff) {
		t.Error("a server without get_pr_diff offers the diff")
	}

	all := allOffers()
	if !slices.Equal(all.Kinds, showKinds) || !slices.Equal(all.Tools, append(slices.Clone(viewActionTools), viewHelperTools...)) {
		t.Errorf("a server with every tool offers %v and %v, want every kind and every action", all.Kinds, all.Tools)
	}
}

// What the person opens in a view is told to the model as what they opened,
// not as a change to what they had.
func TestTheModelIsToldWhatThePersonOpened(t *testing.T) {
	t.Parallel()

	summary := summarizeDiff(ShowInput{Project: "PAY", Repo: "ledger", ID: "7"}, pullrequestservice.PullRequest{Title: "Retry payments"}, "diff --git a/x b/x\n+new\n")
	if got := summary.opened(); !strings.HasPrefix(got, `The person opened the diff of PAY/ledger#7 "Retry payments" in a view: 1 files`) {
		t.Errorf("opening reads %q", got)
	}
}

// The person bb acts for is seen as the pull request sees them: its author,
// or a reviewer with the review they gave, matched however the username is
// cased.
func TestMeIsHowThePullRequestSeesThePerson(t *testing.T) {
	t.Parallel()

	pr := pullrequestservice.PullRequest{
		AuthorUsername: "alice",
		Reviewers: []pullrequestservice.Reviewer{
			{Name: "bob", Status: "UNAPPROVED", Approved: true},
			{Name: "Carol", Status: "NEEDS_WORK"},
		},
	}
	for _, tc := range []struct {
		username string
		want     *viewMe
	}{
		{"alice", &viewMe{Username: "alice", Author: true}},
		{"bob", &viewMe{Username: "bob", Status: "APPROVED"}},
		{"carol", &viewMe{Username: "carol", Status: "NEEDS_WORK"}},
		{"dave", &viewMe{Username: "dave"}},
		{"", nil},
	} {
		got := meFor(tc.username, pr)
		if (got == nil) != (tc.want == nil) || (got != nil && *got != *tc.want) {
			t.Errorf("meFor(%q) = %+v, want %+v", tc.username, got, tc.want)
		}
	}
}

// A diff is highlighted when it is sent, in the file order of its patch, and
// the highlighting stays out of the fingerprint: it stops at a deadline, so
// two reads of the same code can colour it differently.
func TestADiffIsHighlightedOutsideItsFingerprint(t *testing.T) {
	t.Parallel()

	patch := "diff --git a/notes.txt b/notes.txt\n--- a/notes.txt\n+++ b/notes.txt\n@@ -1 +1 @@\n-old\n+new\n" +
		"diff --git a/ledger.go b/ledger.go\n--- a/ledger.go\n+++ b/ledger.go\n@@ -1,2 +1,2 @@\n package ledger\n-var x = 1\n+var x = 2\n"
	payload := viewPayload{Version: viewPayloadVersion, Kind: showKindDiff, Diff: &viewDiff{Patch: patch}}
	before := fingerprintOf(payload)
	withHighlights(&payload)

	if _, plain := payload.Diff.Highlight[0]; plain {
		t.Errorf("the text file is highlighted: %v", payload.Diff.Highlight[0])
	}
	spans := payload.Diff.Highlight[1]
	if len(spans) != 3 || !strings.HasPrefix(spans[0], "k7") || !strings.HasPrefix(spans[1], "k3") {
		t.Errorf("the Go file's code lines are highlighted as %q, want its three lines with package and var as keywords", spans)
	}
	if after := fingerprintOf(payload); after != before {
		t.Errorf("highlighting changed the fingerprint from %s to %s", before, after)
	}
}
