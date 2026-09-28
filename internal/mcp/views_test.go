package mcp

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// viewClient is a client that renders views, as Claude Desktop and VS Code
// declare it.
func viewClient() *mcp.ClientOptions {
	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension(appsExtension, map[string]any{"mimeTypes": []string{viewMIMEType}})
	return &mcp.ClientOptions{Capabilities: capabilities}
}

func listedTool(t *testing.T, session *mcp.ClientSession, name string) *mcp.Tool {
	t.Helper()
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == name {
			return tool
		}
	}
	return nil
}

// show names the page it renders in, in both the forms hosts read, and the
// page is served as MCP Apps defines one: text/html;profile=mcp-app, with no
// domains declared, so the view has no network.
func TestShowNamesItsPageAndThePageIsServed(t *testing.T) {
	t.Parallel()

	session := connect(t, testClients(t), nil, nil)
	tool := listedTool(t, session, "show")
	if tool == nil {
		t.Fatal("show is not listed")
	}
	ui, _ := tool.Meta["ui"].(map[string]any)
	if ui["resourceUri"] != viewURI || tool.Meta["ui/resourceUri"] != viewURI {
		t.Errorf("show's _meta = %v, want ui.resourceUri and ui/resourceUri both %s", tool.Meta, viewURI)
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Error("show is not annotated read-only")
	}

	read, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: viewURI})
	if err != nil {
		t.Fatalf("read %s: %v", viewURI, err)
	}
	if len(read.Contents) != 1 {
		t.Fatalf("the page came back as %d contents, want 1", len(read.Contents))
	}
	page := read.Contents[0]
	if page.MIMEType != viewMIMEType || page.Text != viewPage() {
		t.Errorf("the page is %q with %d characters, want %q and the assembled page", page.MIMEType, len(page.Text), viewMIMEType)
	}
	pageUI, _ := page.Meta["ui"].(map[string]any)
	if csp, ok := pageUI["csp"].(map[string]any); !ok || len(csp) != 0 {
		t.Errorf("the page declares csp %v, want none", pageUI["csp"])
	}

	// The page is for a client to render, not for the person to attach. The
	// list would also list the person's pull requests, which this server
	// cannot reach, so it is asked of one without list_pull_requests.
	listed, err := connect(t, testClients(t), nil, []string{"list_pull_requests"}).ListResources(context.Background(), nil)
	if err != nil {
		t.Fatalf("resources/list: %v", err)
	}
	for _, resource := range listed.Resources {
		if strings.HasPrefix(resource.URI, "ui://") {
			t.Errorf("resources/list offers %s", resource.URI)
		}
	}
}

// A client that renders no views is told so, and nothing is fetched for it:
// the server here points at a port that refuses every connection, so a call
// that reached for Bitbucket would fail.
func TestShowInAClientWithoutViewsShowsNothing(t *testing.T) {
	t.Parallel()

	session := connect(t, testClients(t), nil, nil)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "show",
		Arguments: map[string]any{"kind": "pull_request", "project": "PROJ", "repo": "app", "id": "7"},
	})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if result.IsError {
		t.Fatalf("show failed: %s", resultText(result))
	}
	if !strings.Contains(resultText(result), "displays no views") {
		t.Errorf("show said %q, want it to say nothing was shown", resultText(result))
	}
	var out ShowOutput
	if err := remarshal(result.StructuredContent, &out); err != nil {
		t.Fatalf("decode the output: %v", err)
	}
	if out.Shown || out.Kind != "pull_request" || out.Target != "PROJ/app#7" {
		t.Errorf("output = %+v, want not shown, for PROJ/app#7", out)
	}
	if result.Meta[viewPayloadKey] != nil {
		t.Error("a client without views was sent a view payload")
	}
}

// In a client that renders views, show reaches for Bitbucket; here it cannot,
// and says so as a tool error rather than an empty view.
func TestShowInAClientWithViewsFetchesWhatItShows(t *testing.T) {
	t.Parallel()

	session := connectClient(t, ServerOptions{Name: "bb", Version: "test", Clients: testClients(t)}, viewClient(), "")
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "show",
		Arguments: map[string]any{"kind": "pull_request", "project": "PROJ", "repo": "app", "id": "7"},
	})
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	if !result.IsError || !strings.Contains(resultText(result), "show failed") {
		t.Errorf("show against a refused port answered %q, want a tool error", resultText(result))
	}
}

func TestShowNeedsWhatItsKindNeeds(t *testing.T) {
	t.Parallel()

	session := connect(t, testClients(t), nil, nil)
	for _, arguments := range []map[string]any{
		{"kind": "pull_request", "project": "PROJ", "repo": "app"},
		{"kind": "diff", "project": "PROJ", "id": "7"},
		{"kind": "pull_requests", "id": "7"},
	} {
		result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "show", Arguments: arguments})
		if err != nil {
			t.Fatalf("show %v: %v", arguments, err)
		}
		if !result.IsError {
			t.Errorf("show %v was accepted", arguments)
		}
	}
}

// A kind is offered while the tool whose answer it shows is exposed, and show
// goes when none of them is: --tools and --exclude decide the views.
func TestShowKindsFollowTheirTools(t *testing.T) {
	t.Parallel()

	withoutDiff := connect(t, testClients(t), nil, []string{"get_pr_diff"})
	tool := listedTool(t, withoutDiff, "show")
	if tool == nil {
		t.Fatal("show went with get_pr_diff alone")
	}
	schema, _ := json.Marshal(tool.InputSchema)
	if strings.Contains(string(schema), `"diff"`) {
		t.Errorf("show offers the diff while get_pr_diff is excluded: %s", schema)
	}
	result, err := withoutDiff.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "show",
		Arguments: map[string]any{"kind": "diff", "project": "PROJ", "repo": "app", "id": "7"},
	})
	if err == nil && !result.IsError {
		t.Error("show showed a diff while get_pr_diff is excluded")
	}

	if listedTool(t, connect(t, testClients(t), []string{"get_commit", "show"}, nil), "show") != nil {
		t.Error("show is listed with none of the tools it shows")
	}
}

// Without show there is no view page and no extension to announce.
func TestNoViewsWithoutShow(t *testing.T) {
	t.Parallel()

	session := connect(t, testClients(t), nil, []string{"show"})
	result := session.InitializeResult()
	if result.Capabilities.Extensions != nil {
		t.Errorf("extensions = %v with show excluded, want none", result.Capabilities.Extensions)
	}
	if strings.Contains(result.Instructions, "show") {
		t.Error("the instructions mention show while it is excluded")
	}
	if _, err := session.ReadResource(context.Background(), &mcp.ReadResourceParams{URI: viewURI}); err == nil {
		t.Error("the view page is served with show excluded")
	}
}

func TestClientDisplaysViewsReadsTheDeclaredTypes(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		settings any
		declared bool
		want     bool
	}{
		"no extension":             {declared: false, want: false},
		"the type, as Go declares": {settings: map[string]any{"mimeTypes": []string{viewMIMEType}}, declared: true, want: true},
		"the type, off the wire":   {settings: map[string]any{"mimeTypes": []any{viewMIMEType}}, declared: true, want: true},
		"no types named":           {settings: map[string]any{}, declared: true, want: true},
		"another type only":        {settings: map[string]any{"mimeTypes": []any{"text/html"}}, declared: true, want: false},
	} {
		capabilities := &mcp.ClientCapabilities{}
		if testCase.declared {
			settings, _ := testCase.settings.(map[string]any)
			capabilities.AddExtension(appsExtension, settings)
		}
		request := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Meta: mcp.Meta{
			mcp.MetaKeyClientCapabilities: capabilities,
		}}}
		if got := clientDisplaysViews(request); got != testCase.want {
			t.Errorf("%s: clientDisplaysViews = %v, want %v", name, got, testCase.want)
		}
	}
}

// The page is one document that needs nothing from outside: no network, no
// placeholder left unfilled.
func TestViewPageIsOneSelfContainedDocument(t *testing.T) {
	t.Parallel()

	page := viewPage()
	if !strings.HasPrefix(page, "<!doctype html>") {
		t.Error("the page does not start as an HTML5 document")
	}
	for _, placeholder := range []string{viewStylePlaceholder, viewScriptPlaceholder} {
		if strings.Contains(page, placeholder) {
			t.Errorf("the page still holds %s", placeholder)
		}
	}
	if count := strings.Count(page, "<script"); count != 1 {
		t.Errorf("the page has %d script elements, want the one", count)
	}

	// The SVG namespace is a name, not an address.
	withoutNamespace := strings.ReplaceAll(page, `"http://www.w3.org/2000/svg"`, "")
	if found := regexp.MustCompile(`(?i)(https?:)?//[a-z0-9.-]+\.[a-z]{2,}`).FindAllString(withoutNamespace, -1); len(found) > 0 {
		t.Errorf("the page names addresses outside itself: %v", found)
	}
	for _, reach := range []string{"@import", "url(", " src=", " href=", "fetch(", "XMLHttpRequest", "WebSocket", "EventSource"} {
		if strings.Contains(page, reach) {
			t.Errorf("the page contains %q, which reaches outside it", reach)
		}
	}
}

// Text that came from Bitbucket only ever becomes a text node. The page has no
// way to parse a string as HTML, and nothing can close its script or style
// element from inside.
func TestViewScriptsBuildNoHTMLFromStrings(t *testing.T) {
	t.Parallel()

	forbidden := []string{"innerHTML", "outerHTML", "insertAdjacentHTML", "document.write", "eval(", "new Function", "createContextualFragment", "DOMParser", "srcdoc ="}
	for _, name := range append(slices.Clone(viewScripts), "view.css") {
		source := mustReadViewAsset(name)
		if strings.Contains(strings.ToLower(source), "</script") || strings.Contains(strings.ToLower(source), "</style") {
			t.Errorf("%s contains a closing script or style tag", name)
		}
		if !strings.HasSuffix(name, ".js") {
			continue
		}
		for _, api := range forbidden {
			if strings.Contains(source, api) {
				t.Errorf("%s uses %s", name, api)
			}
		}
	}
}

func TestViewPageReadsThePayloadKeyTheToolWrites(t *testing.T) {
	t.Parallel()

	main := mustReadViewAsset("main.js")
	if !strings.Contains(main, `const VIEW_PAYLOAD_KEY = "`+viewPayloadKey+`";`) {
		t.Errorf("main.js does not read %s", viewPayloadKey)
	}
	if !strings.Contains(main, "const VIEW_PAYLOAD_VERSION = 1;") || viewPayloadVersion != 1 {
		t.Errorf("main.js and views.go disagree on the payload version")
	}
}

func TestAvatarsAreRasterImagesOnly(t *testing.T) {
	t.Parallel()

	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	uri, err := avatarDataURI(png)
	if err != nil || !strings.HasPrefix(uri, "data:image/png;base64,") {
		t.Errorf("a PNG became %q, %v", uri, err)
	}
	for name, data := range map[string][]byte{
		"svg":   []byte(`<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"/>`),
		"html":  []byte("<html><body>not an avatar</body></html>"),
		"empty": nil,
	} {
		if uri, err := avatarDataURI(data); err == nil {
			t.Errorf("%s became an avatar: %q", name, uri)
		}
	}
}

// Every file of a diff is listed with its counts, however large the diff; the
// patch keeps the files that fit, and a file too large to carry is marked
// rather than dropped, even when it comes first.
func TestSplitPatchListsEveryFileAndKeepsWhatFits(t *testing.T) {
	t.Parallel()

	huge := "diff --git src://gen/big.go dst://gen/big.go\n@@ -1 +1,40 @@\n-old\n" + strings.Repeat("+generated line\n", 40)
	small := "diff --git src://a.go dst://a.go\n--- src://a.go\n+++ dst://a.go\n@@ -1,2 +1,2 @@\n--- a comment\n+new\n context\n"
	renamed := "diff --git src://old.yaml dst://new.yaml\nsimilarity index 90%\nrename from old.yaml\nrename to new.yaml\n@@ -1 +1 @@\n-a\n+b\n"

	files, patch, truncated := splitPatch(huge+small+renamed, 200, 10_000)
	if len(files) != 3 || !truncated {
		t.Fatalf("got %d files, truncated %v; want all three, truncated", len(files), truncated)
	}
	if !files[0].Omitted || files[0].Additions != 40 || files[0].Deletions != 1 || files[0].Path != "gen/big.go" {
		t.Errorf("the large file is %+v, want it listed with its counts and omitted", files[0])
	}
	if files[1].Omitted || files[1].Additions != 1 || files[1].Deletions != 1 {
		t.Errorf("the small file is %+v; a removed line reading -- counts as a removal, and the file fits", files[1])
	}
	if files[2].Status != "renamed" || files[2].OldPath != "old.yaml" || files[2].Path != "new.yaml" {
		t.Errorf("the rename is %+v", files[2])
	}
	if patch != small+renamed {
		t.Errorf("the patch kept %q, want the two files that fit", patch)
	}

	// The whole budget: a file after it is full is omitted too.
	files, patch, _ = splitPatch(small+renamed, 10_000, len(small)+1)
	if files[0].Omitted || !files[1].Omitted || patch != small {
		t.Errorf("within %d bytes got %+v and %q", len(small)+1, files, patch)
	}

	added, _, _ := splitPatch("diff --git src://n dst://n\nnew file mode 100644\n--- /dev/null\n+++ dst://n\n@@ -0,0 +1 @@\n+x\n", 1000, 1000)
	gone, _, _ := splitPatch("diff --git src://g dst://g\ndeleted file mode 100644\n--- src://g\n+++ /dev/null\n@@ -1 +0,0 @@\n-x\n", 1000, 1000)
	binary, _, _ := splitPatch("diff --git a/i.png b/i.png\nBinary files a/i.png and b/i.png differ\n", 1000, 1000)
	if added[0].Status != "added" || gone[0].Status != "deleted" || !binary[0].Binary || binary[0].Path != "i.png" {
		t.Errorf("added %+v, deleted %+v, binary %+v", added[0], gone[0], binary[0])
	}
}

// The avatars a view carries are the people it draws, in the order it draws
// them, and no more than it may carry.
func TestViewsCarryTheAvatarsOfWhomTheyDraw(t *testing.T) {
	t.Parallel()

	pr := pullrequestservice.PullRequest{AuthorUsername: "alice", AuthorSlug: "alice", Reviewers: []pullrequestservice.Reviewer{
		{Name: "dave", Slug: "dave", DisplayName: "Dave"},
		{Name: "carol", Slug: "carol", DisplayName: "Carol", Status: "NEEDS_WORK"},
		{Name: "bob", Slug: "bob", DisplayName: "Bob", Approved: true},
		{Name: "anna", Slug: "anna", DisplayName: "Anna"},
	}}
	var order []string
	for _, reviewer := range sortedReviewers(pr.Reviewers) {
		order = append(order, reviewer.Name)
	}
	if strings.Join(order, ",") != "bob,carol,anna,dave" {
		t.Errorf("reviewers sort as %v, want approvals, then changes requested, then the rest by name", order)
	}
	people := peopleOf(pr, 3)
	if len(people) != 3 || people["alice"] == "" || people["bob"] == "" || people["carol"] == "" {
		t.Errorf("three people are %v, want the author and the first two reviewers drawn", people)
	}
}

// The summary a model reads counts the builds as the card does: Bitbucket's
// totals, however many builds the card lists, and when it has only the list
// to count, it says the count is of part of them.
func TestSummariesCountEveryBuild(t *testing.T) {
	t.Parallel()

	pr := viewPullRequest{
		PullRequest: pullrequestservice.PullRequest{ID: 6, Title: "Generate the client", State: "OPEN",
			Repository: &pullrequestservice.RepositoryRef{ProjectKey: "PAY", Slug: "ledger"}},
		Checks:             []viewCheck{{State: "FAILED"}, {State: "SUCCESSFUL"}},
		CheckCounts:        &viewCheckCounts{Failed: 4, InProgress: 2, Cancelled: 1, Unknown: 1, Successful: 142},
		ChecksLimitReached: true,
	}
	if summary := summarizePullRequest(pr); !strings.Contains(summary, "Builds: 4 failed, 2 in progress, 1 canceled, 1 unknown, 142 passed.") {
		t.Errorf("the summary counts the builds as %q, want Bitbucket's totals", summary)
	}
	pr.CheckCounts = nil
	if summary := summarizePullRequest(pr); !strings.Contains(summary, "Builds, of the first 2: 1 failed, 1 passed.") {
		t.Errorf("without totals the summary reads %q, want the listed builds counted as a part", summary)
	}
}

func TestDiffCountsCountFilesAndLines(t *testing.T) {
	t.Parallel()

	patch := "diff --git src://a dst://a\n--- src://a\n+++ dst://a\n@@ -1,2 +1,2 @@\n-old\n+new\n+more\n context\n"
	if files, additions, deletions := diffCounts(patch); files != 1 || additions != 2 || deletions != 1 {
		t.Errorf("counted %d files, +%d -%d; want 1, +2 -1", files, additions, deletions)
	}
}

func TestTheViewPageMayBeCachedByAnyone(t *testing.T) {
	t.Parallel()

	page := &mcp.Cacheable{}
	cacheHints(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: viewURI}}, page)
	if page.CacheScope != "public" || page.TTLMs <= 0 {
		t.Errorf("the view page caches as %+v, want public with a lifetime", page)
	}
	content := &mcp.Cacheable{}
	cacheHints(context.Background(), &mcp.ReadResourceRequest{Params: &mcp.ReadResourceParams{URI: "bitbucket://projects/P/repos/r/pull-requests/1"}}, content)
	if content.CacheScope != "private" {
		t.Errorf("Bitbucket content caches as %+v, want private", content)
	}
}

func remarshal(from any, to any) error {
	encoded, err := json.Marshal(from)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, to)
}

// The handler refuses a kind it was not given, whatever the schema said: the
// schema a server advertises is narrowed to the kinds it offers, and this is
// the check behind it.
func TestShowRefusesAKindItDoesNotOffer(t *testing.T) {
	t.Parallel()

	handler := showHandler(testClients(t), []string{showKindPullRequest})
	_, _, err := handler(context.Background(), nil, ShowInput{Kind: showKindDiff, Project: "PROJ", Repo: "app", ID: "7"})
	if err == nil || !strings.Contains(err.Error(), "cannot show") {
		t.Errorf("show accepted a kind it does not offer: %v", err)
	}
}
