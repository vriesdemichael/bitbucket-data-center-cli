//go:build views

// The views, drawn by a real browser: headless Chrome, driven by chromedp,
// renders the view page inside the stand-in host of internal/mcp/viewhost.
// Run with -tags views on a machine with Chrome; CI runs them in a job of
// their own, which fails when Chrome is missing rather than skipping.

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

// Text other people wrote, each piece of which would run a script if the page
// ever parsed it as HTML.
const (
	hostileTitle       = `Fix <img src=x onerror="window.parent.bbHost.pwned='title'"> rounding`
	hostileDescription = "Refunds truncated.\n<script>window.parent.bbHost.pwned='description'</script>"
	hostileName        = `Alice <b onmouseover="window.parent.bbHost.pwned='name'">Smith</b>`
	hostileCode        = `<script>window.parent.bbHost.pwned='diff'</script>`
)

// onePixelPNG is a real PNG, as bb embeds an avatar.
const onePixelPNG = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=="

func fixturePullRequest() pullrequestservice.PullRequest {
	return pullrequestservice.PullRequest{
		ID:             42,
		Title:          hostileTitle,
		Description:    hostileDescription,
		State:          "OPEN",
		Open:           true,
		Repository:     &pullrequestservice.RepositoryRef{ProjectKey: "PAY", Slug: "ledger"},
		Author:         hostileName,
		AuthorUsername: "alice",
		SourceBranch:   "fix/rounding",
		TargetBranch:   "master",
		SourceCommit:   "0123456789abcdef0123456789abcdef01234567",
		UpdatedDate:    time.Now().Add(-2 * time.Hour).UnixMilli(),
		Reviewers: []pullrequestservice.Reviewer{
			{Name: "bob", DisplayName: "Bob Chen", Role: "REVIEWER", Status: "APPROVED", Approved: true},
			{Name: "carol", DisplayName: "Carol Diaz", Role: "REVIEWER", Status: "NEEDS_WORK"},
			{Name: "dave", DisplayName: "Dave Okafor", Role: "REVIEWER", Status: "UNAPPROVED"},
		},
	}
}

func fixtureResult(t *testing.T, payload viewPayload) json.RawMessage {
	t.Helper()
	payload.Version = viewPayloadVersion
	result := &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: "Showed the person something."}},
		Meta:    mcp.Meta{viewPayloadKey: payload},
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("encode the result: %v", err)
	}
	return encoded
}

func fixtureFrames(t *testing.T) []viewhost.Frame {
	t.Helper()
	pr := fixturePullRequest()
	summary := pullrequestservice.BuildReviewSummary(pr, pullrequestservice.ReviewCounts{})
	card := viewPullRequest{
		PullRequest:   pr,
		URL:           "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview",
		ReviewSummary: &summary,
		Checks: []viewCheck{
			{Name: "Unit tests", State: "SUCCESSFUL", URL: "https://ci.example.com/1"},
			{Name: "Integration tests", State: "FAILED", URL: "javascript:window.parent.bbHost.pwned='link'"},
			{Name: "Coverage", State: "INPROGRESS"},
		},
	}
	avatars := map[string]string{
		"alice": onePixelPNG,
		// Not an embedded image, so never a source: carol is drawn with initials.
		"carol": "javascript:window.parent.bbHost.pwned='avatar'",
	}

	second, third := fixturePullRequest(), fixturePullRequest()
	second.ID, second.Title, second.Draft = 43, "Send an idempotency key", true
	third.ID, third.Title, third.State, third.Open = 44, "Bump golang.org/x/net", "MERGED", false
	list := []viewPullRequest{
		{PullRequest: pr, URL: card.URL, CheckCounts: &viewCheckCounts{Successful: 1, Failed: 1, InProgress: 1}},
		{PullRequest: second, URL: "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/43/overview"},
		{PullRequest: third, URL: "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/44/overview"},
	}

	patch := strings.Join([]string{
		"diff --git src://internal/ledger/ledger.go dst://internal/ledger/ledger.go",
		"index 1111111..2222222 100644",
		"--- src://internal/ledger/ledger.go",
		"+++ dst://internal/ledger/ledger.go",
		"@@ -8,3 +8,4 @@ func Refund(e Entry, fraction float64) Entry {",
		" // Refund returns the entry that reverses e.",
		"-\treturn Entry{Amount: -int64(float64(e.Amount) * fraction)}",
		"+\treturn Entry{Amount: -int64(math.Round(float64(e.Amount) * fraction))}",
		"+\t// " + hostileCode,
		" }",
		"diff --git src://docs/old-notes.md dst://docs/old-notes.md",
		"deleted file mode 100644",
		"--- src://docs/old-notes.md",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-Superseded by the README.",
		"",
	}, "\n")
	diff := viewPullRequest{PullRequest: pr, URL: card.URL}
	plainCard := card
	plainCard.URL = plainCardURL

	arguments := map[string]any{"kind": "pull_request", "project": "PAY", "repo": "ledger", "id": "42"}
	return []viewhost.Frame{
		{Title: "card", Mode: "inline", Fullscreen: true, Arguments: arguments,
			Result: fixtureResult(t, viewPayload{Kind: showKindPullRequest, PullRequest: &card, Avatars: avatars})},
		{Title: "list", Mode: "inline", Fullscreen: true, Arguments: map[string]any{"kind": "pull_requests"},
			Result: fixtureResult(t, viewPayload{Kind: showKindPullRequests, PullRequests: list, Avatars: avatars})},
		{Title: "diff inline", Mode: "inline", Fullscreen: false, Arguments: arguments,
			Result: fixtureResult(t, viewPayload{Kind: showKindDiff, PullRequest: &diff, Diff: &viewDiff{Patch: patch}})},
		{Title: "diff fullscreen", Theme: "dark", Mode: "fullscreen", Fullscreen: true, Arguments: arguments,
			Result: fixtureResult(t, viewPayload{Kind: showKindDiff, PullRequest: &diff, Diff: &viewDiff{Patch: patch}})},
		{Title: "card in a host without fullscreen", Mode: "inline", Fullscreen: false, Arguments: arguments,
			Result: fixtureResult(t, viewPayload{Kind: showKindPullRequest, PullRequest: &card, Avatars: avatars})},
		{Title: "card in a host that does not say, and grants fullscreen", Mode: "inline", Fullscreen: true, HideDisplayModes: true, Arguments: arguments,
			Result: fixtureResult(t, viewPayload{Kind: showKindPullRequest, PullRequest: &card, Avatars: avatars})},
		{Title: "card in a host that does not say, and stays inline", Mode: "inline", Fullscreen: false, HideDisplayModes: true, Arguments: arguments,
			Result: fixtureResult(t, viewPayload{Kind: showKindPullRequest, PullRequest: &card, Avatars: avatars})},
		{Title: "card on http, in a host that refuses its links", Mode: "inline", Fullscreen: true, RefuseLinks: true, Arguments: arguments,
			Result: fixtureResult(t, viewPayload{Kind: showKindPullRequest, PullRequest: &plainCard, Avatars: avatars})},
		{Title: "card in a host that opens no links", Mode: "inline", Fullscreen: true, OpensNoLinks: true, Arguments: arguments,
			Result: fixtureResult(t, viewPayload{Kind: showKindPullRequest, PullRequest: &card, Avatars: avatars})},
	}
}

// The fixture frames past the first five, by what they show.
const (
	fixtureUnsaidGrants = 5 + iota
	fixtureUnsaidInline
	fixtureRefusesLinks
	fixtureOpensNoLinks
)

// plainCardURL is a Bitbucket served over plain http, as a local one often
// is, whose links Claude does not open.
const plainCardURL = "http://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview"

// A host that does not say which display modes it has is asked for
// fullscreen: one that grants it gets the overview there, and one that
// answers with inline gets it opened in place, and is not asked again.
func TestDetailsAskAHostThatDoesNotSay(t *testing.T) {
	ctx := browser(t, fixtureFrames(t))

	clickButton(t, ctx, fixtureUnsaidGrants, "Overview")
	var header bool
	if err := chromedp.Run(ctx, chromedp.Poll(fmt.Sprintf(`window.bbHost.frames[%d].iframe.contentDocument.querySelector(".fullscreen-header") !== null`, fixtureUnsaidGrants), &header,
		chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Errorf("a host that grants fullscreen without saying so did not get the overview in fullscreen: %v", err)
	}

	clickButton(t, ctx, fixtureUnsaidInline, "Overview")
	var inPlace bool
	if err := chromedp.Run(ctx, chromedp.Poll(fmt.Sprintf(`window.bbHost.frames[%d].iframe.contentDocument.querySelector(".inline-details .markdown") !== null`, fixtureUnsaidInline), &inPlace,
		chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Errorf("a host that answers with inline did not get the overview in place: %v", err)
	}
	clickButton(t, ctx, fixtureUnsaidInline, "Hide overview")
	clickButton(t, ctx, fixtureUnsaidInline, "Overview")
	if requested := hostMessages(t, ctx, fixtureUnsaidInline, "ui/request-display-mode"); len(requested) != 1 {
		t.Errorf("the view asked a host that stayed inline for fullscreen %d times, want once", len(requested))
	}
}

// A link the host will not open is shown to open by hand, with why when it
// is not https, rather than a click that seems to do nothing.
func TestLinksTheHostRefusesAreShown(t *testing.T) {
	ctx := browser(t, fixtureFrames(t))

	clickButton(t, ctx, fixtureRefusesLinks, "Open in Bitbucket")
	var shown string
	err := chromedp.Run(ctx, chromedp.Poll(fmt.Sprintf(`(() => { const n = window.bbHost.frames[%d].iframe.contentDocument.querySelector(".link-notice"); return n ? n.textContent : ""; })()`, fixtureRefusesLinks), &shown,
		chromedp.WithPollingTimeout(5*time.Second)))
	if err != nil || !strings.Contains(shown, plainCardURL) || !strings.Contains(shown, "did not open") {
		t.Errorf("a refused link reads %q, want it shown to open by hand: %v", shown, err)
	}

	// A host that says it opens no links is not asked: the address shows at
	// once.
	clickButton(t, ctx, fixtureOpensNoLinks, "Open in Bitbucket")
	var address string
	inFrame(t, ctx, fixtureOpensNoLinks, `const n = d.querySelector(".link-notice"); return n ? n.textContent : "";`, &address)
	if !strings.Contains(address, "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview") || !strings.Contains(address, "does not open links") {
		t.Errorf("a host that opens no links shows %q, want the address to open by hand", address)
	}
	if asked := hostMessages(t, ctx, fixtureOpensNoLinks, "ui/open-link"); len(asked) != 0 {
		t.Errorf("the view asked a host that opens no links to open one: %v", asked)
	}
}

// browser opens the stand-in host with the frames in headless Chrome, and
// waits until every view has drawn its result.
func browser(t *testing.T, frames []viewhost.Frame) context.Context {
	t.Helper()

	page, err := viewhost.Page(viewPage(), frames, viewhost.Options{SameOrigin: true})
	if err != nil {
		t.Fatalf("render the host page: %v", err)
	}
	path := filepath.Join(t.TempDir(), "views.html")
	if err := os.WriteFile(path, []byte(page), 0o600); err != nil {
		t.Fatalf("write the host page: %v", err)
	}

	allocator, cancelAllocator := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.NoSandbox)...)
	t.Cleanup(cancelAllocator)
	ctx, cancelBrowser := chromedp.NewContext(allocator)
	t.Cleanup(cancelBrowser)
	ctx, cancelTimeout := context.WithTimeout(ctx, time.Minute)
	t.Cleanup(cancelTimeout)

	drawn := `window.bbHost && window.bbHost.frames.length === ` + fmt.Sprint(len(frames)) + ` && window.bbHost.frames.every((f) => {
		const d = f.iframe.contentDocument;
		return d && d.getElementById("app") && d.getElementById("app").childElementCount > 0 && !d.querySelector("[aria-busy]");
	})`
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1280, 900),
		chromedp.Navigate("file:///"+filepath.ToSlash(path)),
		chromedp.Poll(drawn, nil, chromedp.WithPollingTimeout(30*time.Second)),
	); err != nil {
		t.Fatalf("the views did not draw (is Chrome installed?): %v", err)
	}
	return ctx
}

// inFrame evaluates script with d bound to a frame's document and w to its
// window, and decodes what it returns.
func inFrame(t *testing.T, ctx context.Context, frame int, script string, result any) {
	t.Helper()
	wrapped := fmt.Sprintf(`(() => { const f = window.bbHost.frames[%d]; const d = f.iframe.contentDocument; const w = f.iframe.contentWindow; %s })()`, frame, script)
	if err := chromedp.Run(ctx, chromedp.Evaluate(wrapped, result)); err != nil {
		t.Fatalf("frame %d: %v\nscript: %s", frame, err, script)
	}
}

// hostMessages are the view's messages the stand-in host received, by method.
func hostMessages(t *testing.T, ctx context.Context, frame int, method string) []map[string]any {
	t.Helper()
	var messages []map[string]any
	script := fmt.Sprintf(`window.bbHost.frames[%d].messages.filter((m) => m.method === %q)`, frame, method)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &messages)); err != nil {
		t.Fatalf("read the host's messages: %v", err)
	}
	return messages
}

func messageText(message map[string]any) string {
	params, _ := message["params"].(map[string]any)
	if url, ok := params["url"].(string); ok {
		return url
	}
	content, _ := params["content"].([]any)
	var parts []string
	for _, part := range content {
		if block, ok := part.(map[string]any); ok {
			parts = append(parts, fmt.Sprint(block["text"]))
		}
	}
	return strings.Join(parts, "\n")
}

func TestViewsDrawTextOthersWroteAsText(t *testing.T) {
	ctx := browser(t, fixtureFrames(t))

	for frame := range len(fixtureFrames(t)) {
		var escaped bool
		inFrame(t, ctx, frame, `return !d.querySelector("img[src='x'], script:not(:first-of-type), b[onmouseover]");`, &escaped)
		if !escaped {
			t.Errorf("frame %d built an element from text someone wrote", frame)
		}
	}

	var card string
	inFrame(t, ctx, 0, `return d.body.textContent;`, &card)
	for _, want := range []string{hostileTitle, hostileName, "Carol Diaz requested changes", "1 build failed", "1 build in progress", "1 of 3 approved", "1 of 3 builds passed", "Changes requested", "Overview"} {
		if !strings.Contains(card, want) {
			t.Errorf("the card does not show %q:\n%s", want, card)
		}
	}

	var diff string
	inFrame(t, ctx, 3, `return d.body.textContent;`, &diff)
	if !strings.Contains(diff, hostileCode) {
		t.Errorf("the diff does not show the line as written:\n%s", diff)
	}

	var pwned any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.bbHost.pwned || null`, &pwned)); err != nil {
		t.Fatal(err)
	}
	if pwned != nil {
		t.Errorf("text someone wrote ran as a script: %v", pwned)
	}
}

func TestViewsDrawOnlyEmbeddedImages(t *testing.T) {
	ctx := browser(t, fixtureFrames(t))

	var sources []string
	inFrame(t, ctx, 0, `return [...d.querySelectorAll("img")].map((i) => i.getAttribute("src"));`, &sources)
	if len(sources) == 0 {
		t.Fatal("the card drew no avatar images")
	}
	for _, source := range sources {
		if !strings.HasPrefix(source, "data:image/png;base64,") {
			t.Errorf("the card draws an image from %q", source)
		}
	}

	var initials []string
	inFrame(t, ctx, 0, `return [...d.querySelectorAll(".initials")].map((i) => i.textContent);`, &initials)
	if !containsString(initials, "CD") {
		t.Errorf("carol, whose avatar was not an embedded image, is not drawn with initials: %v", initials)
	}
}

func TestInlineViewsReportTheirSize(t *testing.T) {
	ctx := browser(t, fixtureFrames(t))

	// The size arrives a frame after the card draws, and the host applies it
	// when the message lands.
	var sized bool
	if err := chromedp.Run(ctx, chromedp.Poll(`window.bbHost.frames[0].iframe.getBoundingClientRect().height > 150`, &sized,
		chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		var height float64
		inFrame(t, ctx, 0, `return f.iframe.getBoundingClientRect().height;`, &height)
		t.Errorf("the card's frame stayed %.0fpx tall, want it sized to the card: %v", height, err)
	}
	if len(hostMessages(t, ctx, 0, "ui/notifications/size-changed")) == 0 {
		t.Error("the card never reported its size")
	}
}

func TestViewsOpenLinksThroughTheHostAndNeverOthers(t *testing.T) {
	ctx := browser(t, fixtureFrames(t))

	inFrame(t, ctx, 1, `d.querySelector(".row-button").click(); return null;`, nil)
	clickButton(t, ctx, 0, "Open in Bitbucket")
	chromedp.Run(ctx, chromedp.Sleep(200*time.Millisecond))

	for frame, want := range map[int]string{
		0: "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview",
		1: "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview",
	} {
		opened := hostMessages(t, ctx, frame, "ui/open-link")
		if len(opened) != 1 || messageText(opened[0]) != want {
			t.Errorf("frame %d asked the host to open %v, want %s", frame, opened, want)
		}
	}
}

func TestDetailsGoFullscreenWhereTheHostHasIt(t *testing.T) {
	ctx := browser(t, fixtureFrames(t))

	inFrame(t, ctx, 0, `[...d.querySelectorAll("button")].find((b) => b.textContent.includes("Overview")).click(); return null;`, nil)
	var header bool
	if err := chromedp.Run(ctx, chromedp.Poll(`window.bbHost.frames[0].iframe.contentDocument.querySelector(".fullscreen-header") !== null`, &header,
		chromedp.WithPollingTimeout(5*time.Second))); err != nil {
		t.Fatalf("the card did not open to fullscreen: %v", err)
	}
	requested := hostMessages(t, ctx, 0, "ui/request-display-mode")
	if len(requested) != 1 {
		t.Fatalf("the card asked for %d display modes, want one", len(requested))
	}

	// A host without fullscreen: the card opens out in place, and asks for
	// nothing.
	inFrame(t, ctx, 4, `[...d.querySelectorAll("button")].find((b) => b.textContent.includes("Overview")).click(); return null;`, nil)
	var inline bool
	inFrame(t, ctx, 4, `return d.querySelector(".inline-details .markdown") !== null;`, &inline)
	if !inline {
		t.Error("the card did not open out in place in a host without fullscreen")
	}
	if requested := hostMessages(t, ctx, 4, "ui/request-display-mode"); len(requested) != 0 {
		t.Errorf("the card asked a host without fullscreen for a display mode: %v", requested)
	}

	// And the diff opens its files in place.
	inFrame(t, ctx, 2, `d.querySelector(".file-link").click(); return null;`, nil)
	var rows int
	inFrame(t, ctx, 2, `return d.querySelectorAll(".diff-table tr").length;`, &rows)
	if rows == 0 {
		t.Error("a file in the inline diff did not open in place")
	}
	if len(hostMessages(t, ctx, 2, "ui/request-display-mode")) != 0 {
		t.Error("the diff asked a host without fullscreen for fullscreen")
	}
}

func TestSelectedLinesGoToTheModelOnlyWhenAsked(t *testing.T) {
	ctx := browser(t, fixtureFrames(t))

	if sent := hostMessages(t, ctx, 3, "ui/update-model-context"); len(sent) != 0 {
		t.Fatalf("the diff sent the model context unasked: %v", sent)
	}

	inFrame(t, ctx, 3, `
		const numbers = [...d.querySelectorAll("tr.del .line-number, tr.add .line-number")].filter((b) => b.textContent !== "");
		numbers[0].click();
		numbers[2].dispatchEvent(new w.MouseEvent("click", { bubbles: true, shiftKey: true }));
		return null;`, nil)
	var selected int
	inFrame(t, ctx, 3, `return d.querySelectorAll("tr.selected").length;`, &selected)
	if selected != 3 {
		t.Fatalf("%d rows are selected, want the three from the first to the shift-click", selected)
	}

	inFrame(t, ctx, 3, `[...d.querySelectorAll(".selection-bar button")].find((b) => b.textContent.includes("chat context")).click(); return null;`, nil)
	chromedp.Run(ctx, chromedp.Sleep(200*time.Millisecond))
	sent := hostMessages(t, ctx, 3, "ui/update-model-context")
	if len(sent) != 1 {
		t.Fatalf("the diff sent the model context %d times, want once", len(sent))
	}
	text := messageText(sent[0])
	for _, want := range []string{"PAY/ledger#42", "internal/ledger/ledger.go", "math.Round", "```diff"} {
		if !strings.Contains(text, want) {
			t.Errorf("the model context lacks %q:\n%s", want, text)
		}
	}
	if len(hostMessages(t, ctx, 3, "ui/message")) != 0 {
		t.Error("adding to the context also sent a message")
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// A pull request larger than any view was drawn for. Thirty reviewers, of
// whom the two who requested changes sort after six approvals. 150 builds,
// of which the card lists the first hundred Bitbucket returns ordered by
// state, which leaves out the one of unknown state. A hundred pull requests.
// A diff of 1,201 files whose last is too large to carry. And titles, names,
// branches, paths and lines long enough to break a row.
const stressTitle = "Generate the payment provider client from its OpenAPI schema, with retries, idempotency keys, request telemetry, " +
	"typed errors, pagination helpers, webhook signature verification and a sandbox mode for the contract tests"

func stressFrames(t *testing.T) []viewhost.Frame {
	t.Helper()
	pr := fixturePullRequest()
	pr.Title = stressTitle
	pr.Author = "Alexandra Konstantinidou-Papadopoulou"
	pr.Repository = &pullrequestservice.RepositoryRef{ProjectKey: "PAYMENTS-PLATFORM", Slug: "payment-provider-client-generator-and-retry-middleware"}
	pr.SourceBranch = "feature/PAY-4821-generated-payment-provider-client-with-retries-idempotency-telemetry-and-typed-errors"
	pr.TargetBranch = "release/2026.09-payments-platform-hotfix-candidate"
	pr.Mergeability = &pullrequestservice.Mergeability{Outcome: "CONFLICTED", Conflicted: true}
	pr.Description = "## Why\n\nThe hand-written client drifted from its schema.\n\n" + strings.Repeat("A line of a long description.\n", 200)
	pr.Reviewers = nil
	for i := range 30 {
		reviewer := pullrequestservice.Reviewer{Name: fmt.Sprintf("reviewer%02d", i), DisplayName: fmt.Sprintf("Reviewer %02d", i), Role: "REVIEWER", Status: "UNAPPROVED"}
		switch {
		case i < 6:
			reviewer.Status, reviewer.Approved = "APPROVED", true
		case i >= 28:
			reviewer.Status = "NEEDS_WORK"
		}
		pr.Reviewers = append(pr.Reviewers, reviewer)
	}

	counts := &viewCheckCounts{Failed: 4, InProgress: 2, Cancelled: 1, Unknown: 1, Successful: 142}
	checks := []viewCheck{{Name: "Deploy preview", State: "CANCELLED"}}
	for i := range 4 {
		checks = append(checks, viewCheck{Name: fmt.Sprintf("Contract tests %d", i), State: "FAILED", URL: "https://ci.example.com/failed"})
	}
	for i := range 2 {
		checks = append(checks, viewCheck{Name: fmt.Sprintf("Integration tests %d", i), State: "INPROGRESS"})
	}
	for i := 0; len(checks) < maxViewChecks; i++ {
		checks = append(checks, viewCheck{Name: fmt.Sprintf("Unit tests %03d", i), State: "SUCCESSFUL"})
	}
	summary := pullrequestservice.BuildReviewSummary(pr, pullrequestservice.ReviewCounts{})
	url := "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview"
	card := viewPullRequest{PullRequest: pr, URL: url, ReviewSummary: &summary, Checks: checks, CheckCounts: counts, ChecksLimitReached: true}
	// Read three hours ago, so the view says it may be out of date.
	stale := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	cardPayload := viewPayload{Kind: showKindPullRequest, PullRequest: &card, GeneratedAt: stale}

	comments := 12345
	var list []viewPullRequest
	for i := range 100 {
		row := pr
		row.ID = int64(1000 - i)
		row.Description = ""
		row.Reviewers = pr.Reviewers[:i%31]
		row.CommentCount = &comments
		if i%2 == 0 {
			row.Repository = &pullrequestservice.RepositoryRef{ProjectKey: "PAY", Slug: "ledger"}
		}
		list = append(list, viewPullRequest{PullRequest: row, URL: url, CheckCounts: counts})
	}
	listPayload := viewPayload{Kind: showKindPullRequests, PullRequests: list, LimitReached: true, GeneratedAt: stale}

	var patch strings.Builder
	for i := range 1200 {
		fmt.Fprintf(&patch, "diff --git src://big/f_%04d.txt dst://big/f_%04d.txt\nnew file mode 100644\n--- /dev/null\n+++ dst://big/f_%04d.txt\n@@ -0,0 +1 @@\n+f%d\n", i, i, i, i)
	}
	patch.WriteString("diff --git src://big/huge.txt dst://big/huge.txt\nnew file mode 100644\n--- /dev/null\n+++ dst://big/huge.txt\n@@ -0,0 +1,30000 @@\n")
	for i := range 30000 {
		fmt.Fprintf(&patch, "+line %d of a file much longer than a view carries\n", i)
	}
	files, carried, truncated := splitPatch(patch.String(), maxViewFileBytes, maxViewPatchBytes)
	bigDiff := viewPayload{Kind: showKindDiff, PullRequest: &card, Diff: &viewDiff{Files: files, Patch: carried, Truncated: truncated}}

	deep := "services/payments/provider/internal/generated/clients/v2026_09/operations/refunds/partial/idempotency/handlers/"
	name := "retry_policy.go"
	minified := "var bundle=" + strings.Repeat("function a(b){return b*2},", 1000) + ";"
	long := strings.Join([]string{
		"diff --git src://" + deep + name + " dst://" + deep + name,
		"--- src://" + deep + name,
		"+++ dst://" + deep + name,
		"@@ -1,2 +1,2 @@",
		" package handlers",
		"-// old",
		"+// new",
		"diff --git src://web/static/bundle.min.js dst://web/static/bundle.min.js",
		"--- src://web/static/bundle.min.js",
		"+++ dst://web/static/bundle.min.js",
		"@@ -1 +1 @@",
		"-var bundle=1;",
		"+" + minified,
		"diff --git src://gen/big.go dst://gen/big.go",
		"new file mode 100644",
		"--- /dev/null",
		"+++ dst://gen/big.go",
		"@@ -0,0 +1,500 @@",
	}, "\n") + "\n" + strings.Repeat("+generated line\n", 500)
	longFiles, longCarried, longTruncated := splitPatch(long, maxViewFileBytes, maxViewPatchBytes)
	longDiff := viewPayload{Kind: showKindDiff, PullRequest: &card, Diff: &viewDiff{Files: longFiles, Patch: longCarried, Truncated: longTruncated}}

	arguments := map[string]any{"kind": "pull_request", "project": "PAY", "repo": "ledger", "id": "42"}
	frame := func(title, mode string, fullscreen bool, width, height int, payload viewPayload) viewhost.Frame {
		return viewhost.Frame{Title: title, Mode: mode, Fullscreen: fullscreen, Width: width, Height: height, Arguments: arguments, Result: fixtureResult(t, payload)}
	}
	return []viewhost.Frame{
		frame("card", "inline", true, 0, 0, cardPayload),
		frame("card on a phone", "inline", true, 360, 0, cardPayload),
		frame("card in a host without fullscreen", "inline", false, 0, 0, cardPayload),
		frame("overview in fullscreen", "fullscreen", true, 0, 0, cardPayload),
		frame("overview in fullscreen on a phone", "fullscreen", true, 390, 760, cardPayload),
		frame("list", "inline", true, 0, 0, listPayload),
		frame("list on a phone", "inline", true, 360, 0, listPayload),
		frame("list in fullscreen", "fullscreen", true, 0, 0, listPayload),
		frame("diff", "inline", true, 0, 0, bigDiff),
		frame("diff in a host without fullscreen", "inline", false, 0, 0, bigDiff),
		frame("diff of long lines", "inline", false, 0, 0, longDiff),
		frame("diff in fullscreen", "fullscreen", true, 0, 0, bigDiff),
	}
}

// The frames of stressFrames, by what they show.
const (
	stressCard = iota
	stressCardPhone
	stressCardInPlace
	stressOverview
	stressOverviewPhone
	stressList
	stressListPhone
	stressListFullscreen
	stressDiff
	stressDiffInPlace
	stressDiffLongLines
	stressDiffFullscreen
)

func clickButton(t *testing.T, ctx context.Context, frame int, text string) {
	t.Helper()
	var clicked bool
	inFrame(t, ctx, frame, fmt.Sprintf(`const b = [...d.querySelectorAll("button")].find((b) => b.textContent.includes(%q)); if (b) b.click(); return Boolean(b);`, text), &clicked)
	if !clicked {
		t.Fatalf("frame %d has no %q button", frame, text)
	}
}

func frameText(t *testing.T, ctx context.Context, frame int) string {
	t.Helper()
	var text string
	inFrame(t, ctx, frame, `return d.body.innerText;`, &text)
	return text
}

// A count is Bitbucket's count of the whole, however many items the view
// lists, and the model is told the same.
func TestViewsCountTheWhole(t *testing.T) {
	ctx := browser(t, stressFrames(t))

	card := frameText(t, ctx, stressCard)
	for _, want := range []string{"4 builds failed", "2 builds in progress", "142 of 150 builds passed", "6 of 30 approved"} {
		if !strings.Contains(card, want) {
			t.Errorf("the card does not say %q:\n%s", want, card)
		}
	}
	if strings.Contains(card, "93 of") {
		t.Errorf("the card counted the builds it lists rather than Bitbucket's totals:\n%s", card)
	}
	// Read three hours ago, the card says it may be out of date.
	if !strings.Contains(card, "may be out of date") {
		t.Errorf("a card read three hours ago does not say it may be out of date:\n%s", card)
	}

	// The fullscreen list counts only what it holds, and says so.
	list := frameText(t, ctx, stressListFullscreen)
	if !strings.Contains(list, "Counted among the first 100") || !strings.Contains(list, "There may be more than these in Bitbucket") {
		t.Errorf("a list that stopped at its limit does not say its counts are of part:\n%.600s", list)
	}
}

// What needs the person's attention is never behind a click or a count: the
// builds that failed or run, the reviewers who requested changes, the files
// too large to show.
func TestViewsNeverHideWhatNeedsAttention(t *testing.T) {
	ctx := browser(t, stressFrames(t))

	// The card names both reviewers who requested changes, with their
	// avatars, though six approvals sort before them.
	var asked string
	inFrame(t, ctx, stressCard, `const a = d.querySelector(".pr-card .attention .changes-requested"); return a ? a.querySelector(".who").textContent + "|" + [...a.querySelectorAll(".avatar")].map((v) => v.title).join(",") : "";`, &asked)
	if asked != "Reviewer 28 and Reviewer 29 requested changes|Reviewer 28,Reviewer 29" {
		t.Errorf("the card's requests for changes read %q, want both reviewers named and drawn", asked)
	}
	// And a conflict, in Bitbucket's words.
	var conflict string
	inFrame(t, ctx, stressCard, `const c = [...d.querySelectorAll(".pr-card .attention .attention-item")].find((i) => i.textContent === "Conflict"); return c ? c.title : "";`, &conflict)
	if conflict != "This pull request has conflicts that need to be resolved before it can be merged." {
		t.Errorf("the card does not flag the conflict as Bitbucket does: %q", conflict)
	}

	// A fullscreen row's avatars include those who requested changes, though
	// approvals sort before them.
	var votes []string
	inFrame(t, ctx, stressListFullscreen, `const row = [...d.querySelectorAll(".pr-list > li")].find((r) => r.querySelector(".meta-repo .nowrap").textContent === "#970");
		return [...row.querySelectorAll(".avatar-stack .avatar")].map((a) => a.title);`, &votes)
	requested := 0
	for _, vote := range votes {
		if strings.HasSuffix(vote, ": Changes requested") {
			requested++
		}
	}
	if len(votes) != 3 || requested != 2 {
		t.Errorf("the fullscreen row shows %v, want three avatars with both reviewers who requested changes", votes)
	}

	// An inline row flags its failed builds, and no state while it is plainly
	// open.
	var flags []string
	inFrame(t, ctx, stressList, `return [...d.querySelectorAll(".pr-list > li .row-side")].map((side) => side.textContent);`, &flags)
	if len(flags) != 6 || slices.ContainsFunc(flags, func(flag string) bool { return flag != "4" }) {
		t.Errorf("the inline rows flag %q, want each open row's 4 failed builds and nothing else", flags)
	}

	// The overview lists every build that failed, runs or was canceled without
	// a click; the passes fold to their count; the build it does not carry is
	// said to be missing.
	clickButton(t, ctx, stressCardInPlace, "Overview")
	var clamped bool
	inFrame(t, ctx, stressCardInPlace, `const c = d.querySelector(".details-main .clamp"); return Boolean(c) && c.classList.contains("overflowing") && !c.querySelector(".clamp-toggle").hidden;`, &clamped)
	if !clamped {
		t.Error("the long description opened in place is not held short, with a way to show the rest")
	}
	var listed map[string]int
	inFrame(t, ctx, stressCardInPlace, `const n = {}; for (const row of d.querySelectorAll(".check-list li[data-state]")) { n[row.dataset.state] = (n[row.dataset.state] || 0) + 1; } return n;`, &listed)
	if listed["failed"] != 4 || listed["inprogress"] != 2 || listed["cancelled"] != 1 || listed["successful"] != 0 {
		t.Errorf("the overview lists builds by state as %v, want 4 failed, 2 in progress and 1 canceled open, and the passes folded", listed)
	}
	overview := frameText(t, ctx, stressCardInPlace)
	if !strings.Contains(overview, "1 build has unknown state") || !strings.Contains(overview, "Not listed in this view") {
		t.Errorf("the overview does not say the build it does not carry is missing:\n%.2000s", overview)
	}
	clickButton(t, ctx, stressCardInPlace, "142 builds passed")
	inFrame(t, ctx, stressCardInPlace, `return { successful: d.querySelectorAll(".check-list li[data-state='successful']").length };`, &listed)
	if listed["successful"] != 93 {
		t.Errorf("the passes open to %d builds, want the 93 the view carries", listed["successful"])
	}
	var unlisted string
	inFrame(t, ctx, stressCardInPlace, `return [...d.querySelectorAll(".check-list .unlisted")].map((l) => l.textContent).join("|");`, &unlisted)
	if !strings.Contains(unlisted, "And 49 more, not listed in this view") {
		t.Errorf("the opened passes say %q, want the 49 the view does not carry counted", unlisted)
	}

	// The reviewers who requested changes come first and open; those still
	// reviewing fold to their count.
	var groups []string
	inFrame(t, ctx, stressCardInPlace, `return [...d.querySelectorAll(".details-side .group-title")].map((g) => g.textContent);`, &groups)
	if len(groups) < 3 || groups[0] != "Changes requested · 2" || groups[1] != "Approved · 6" || !strings.HasSuffix(groups[2], "Reviewing · 22") {
		t.Errorf("the reviewer groups are %v, want changes requested, approved, and reviewing folded", groups)
	}
	var people int
	inFrame(t, ctx, stressCardInPlace, `return d.querySelectorAll(".details-side .person-list:not(.details-list) li").length;`, &people)
	if people != 8 {
		t.Errorf("the overview names %d reviewers before the fold opens, want the 8 who decided", people)
	}

	// The diff names the file too large to show, wherever the view is.
	for _, frame := range []int{stressDiff, stressDiffFullscreen} {
		var named string
		inFrame(t, ctx, frame, `const n = d.querySelector(".notice.omitted"); return n ? n.textContent : "";`, &named)
		if !strings.Contains(named, "big/huge.txt") || !strings.Contains(named, "+30,000") || !strings.Contains(named, "You can view its diff in Bitbucket") {
			t.Errorf("frame %d does not name the file too large to show, with where to see it: %q", frame, named)
		}
	}
	// And links to the file's diff in Bitbucket, as Bitbucket links one.
	inFrame(t, ctx, stressDiff, `d.querySelector(".notice.omitted .omitted-link").click(); return null;`, nil)
	chromedp.Run(ctx, chromedp.Sleep(200*time.Millisecond))
	if opened := hostMessages(t, ctx, stressDiff, "ui/open-link"); len(opened) != 1 ||
		messageText(opened[0]) != "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/diff#big/huge.txt" {
		t.Errorf("the file too large to show links to %v, want its diff in Bitbucket", opened)
	}
	diff := frameText(t, ctx, stressDiff)
	if !strings.Contains(diff, "and 1,193 more files, in full screen") {
		t.Errorf("the inline diff does not say how many files it leaves out:\n%.800s", diff)
	}
}

// What identifies a pull request or a file survives a long neighbour: the
// number, the author, the file name, the title in fullscreen.
func TestViewsKeepWhatIdentifies(t *testing.T) {
	ctx := browser(t, stressFrames(t))

	for _, frame := range []int{stressCard, stressCardPhone} {
		var number string
		inFrame(t, ctx, frame, `const n = d.querySelector(".pr-card .repo-label .nowrap"); const b = n.getBoundingClientRect(); return b.width > 0 && n.scrollWidth <= n.clientWidth + 1 && b.right <= d.documentElement.clientWidth ? n.textContent : "";`, &number)
		if number != "#42" {
			t.Errorf("frame %d does not show the pull request's number whole: %q", frame, number)
		}
	}
	var author bool
	inFrame(t, ctx, stressCard, `const a = d.querySelector(".pr-card .byline-author strong"); return a.scrollWidth <= a.clientWidth + 1;`, &author)
	if !author {
		t.Error("the card cuts the author's name short for the branches")
	}

	for _, frame := range []int{stressList, stressListPhone} {
		var cut []string
		inFrame(t, ctx, frame, `return [...d.querySelectorAll(".pr-list > li")].filter((row, index) => {
			const n = row.querySelector(".meta-repo .nowrap"); const b = n.getBoundingClientRect();
			return !(n.textContent === "#" + (1000 - index) && b.width > 0 && n.scrollWidth <= n.clientWidth + 1 && b.right <= row.getBoundingClientRect().right + 1);
		}).map((row) => row.querySelector(".row-title").textContent);`, &cut)
		if len(cut) > 0 {
			t.Errorf("frame %d cuts the number off %d rows", frame, len(cut))
		}
	}

	// A long path gives way from its directory and keeps its file name.
	var path string
	inFrame(t, ctx, stressDiffLongLines, `const p = d.querySelector(".file-link .path"); const dir = p.querySelector(".dir"); const name = p.querySelector(".name");
		return (dir.scrollWidth > dir.clientWidth ? "directory cut, " : "directory whole, ") + (name.scrollWidth <= name.clientWidth + 1 ? name.textContent : "name cut");`, &path)
	if path != "directory cut, retry_policy.go" {
		t.Errorf("the long path reads %q, want its directory cut and its file name whole", path)
	}

	// Fullscreen shows the whole title, on a phone too, and the state beside
	// it is not under the buttons.
	for _, frame := range []int{stressOverview, stressOverviewPhone} {
		var whole bool
		inFrame(t, ctx, frame, fmt.Sprintf(`const h = d.querySelector(".fullscreen-header h1");
			const badge = d.querySelector(".fullscreen-header .badge").getBoundingClientRect();
			const actions = d.querySelector(".header-actions").getBoundingClientRect();
			const apart = badge.right <= actions.left || badge.bottom <= actions.top || badge.left >= actions.right || badge.top >= actions.bottom;
			return h.textContent === %q && h.scrollHeight <= h.clientHeight + 1 && h.scrollWidth <= h.clientWidth + 1 && apart;`, stressTitle), &whole)
		if !whole {
			t.Errorf("frame %d does not show the whole title clear of the buttons", frame)
		}
	}
}

// An inline view keeps a shape a chat can hold, whatever it shows: nothing
// scrolls sideways, and none grows past a budget until the person asks for
// more, and then a step at a time.
func TestInlineViewsStayInBounds(t *testing.T) {
	ctx := browser(t, stressFrames(t))
	chromedp.Run(ctx, chromedp.Sleep(300*time.Millisecond))

	for frame := range len(stressFrames(t)) {
		var wide bool
		inFrame(t, ctx, frame, `return d.documentElement.scrollWidth > d.documentElement.clientWidth + 1;`, &wide)
		if wide {
			t.Errorf("frame %d scrolls sideways: something long broke its layout", frame)
		}
	}
	const budget = 900
	for _, frame := range []int{stressCard, stressCardPhone, stressCardInPlace, stressList, stressListPhone, stressDiff, stressDiffInPlace, stressDiffLongLines} {
		var height float64
		inFrame(t, ctx, frame, `return f.iframe.getBoundingClientRect().height;`, &height)
		if height > budget || height < 100 {
			t.Errorf("inline frame %d is %.0fpx tall, want it drawn and within %dpx", frame, height, budget)
		}
	}

	// In a host without fullscreen, the diff grows by a step, and back.
	var files int
	inFrame(t, ctx, stressDiffInPlace, `return d.querySelectorAll(".file-list > li").length;`, &files)
	if files != 8 {
		t.Errorf("the inline diff lists %d files, want 8", files)
	}
	clickButton(t, ctx, stressDiffInPlace, "Show 50 more files (1,193 not shown)")
	inFrame(t, ctx, stressDiffInPlace, `return d.querySelectorAll(".file-list > li").length;`, &files)
	if files != 58 {
		t.Errorf("a step shows %d files, want 58", files)
	}
	clickButton(t, ctx, stressDiffInPlace, "Show fewer files")
	inFrame(t, ctx, stressDiffInPlace, `return d.querySelectorAll(".file-list > li").length;`, &files)
	if files != 8 {
		t.Errorf("Show fewer files leaves %d, want 8", files)
	}

	// A minified line is cut, and says how much more it has.
	clickButton(t, ctx, stressDiffLongLines, "bundle.min.js")
	var cut string
	inFrame(t, ctx, stressDiffLongLines, `const c = [...d.querySelectorAll("tr.add .code")].find((c) => c.textContent.startsWith("var bundle")); return c ? c.firstChild.textContent.length + "|" + c.querySelector(".cut").textContent : "";`, &cut)
	if !strings.HasPrefix(cut, "500| … ") || !strings.HasSuffix(cut, "more characters") {
		t.Errorf("the minified line is drawn as %q, want its first 500 characters and a count of the rest", cut)
	}
	var height float64
	inFrame(t, ctx, stressDiffLongLines, `return d.getElementById("app").getBoundingClientRect().height;`, &height)
	if height > budget {
		t.Errorf("the diff of long lines grew to %.0fpx with its files open, want it within %dpx", height, budget)
	}

	// A long file opens in place a little at a time.
	clickButton(t, ctx, stressDiffLongLines, "big.go")
	generated := func() int {
		var rows int
		inFrame(t, ctx, stressDiffLongLines, `return [...d.querySelectorAll("tr.add .code")].filter((c) => c.textContent === "generated line").length;`, &rows)
		return rows
	}
	if rows := generated(); rows != 60 {
		t.Errorf("a 500-line file opened in place draws %d lines, want 60", rows)
	}
	clickButton(t, ctx, stressDiffLongLines, "Show 200 more lines (440 not shown)")
	if rows := generated(); rows != 260 {
		t.Errorf("a step draws %d lines, want 260", rows)
	}

	// In a host with fullscreen, a file picked inline opens there, at that
	// file.
	inFrame(t, ctx, stressDiff, `d.querySelectorAll(".file-link")[5].click(); return null;`, nil)
	var at float64
	if err := chromedp.Run(ctx, chromedp.Poll(`(() => { const d = window.bbHost.frames[`+fmt.Sprint(stressDiff)+`].iframe.contentDocument; const m = d.getElementById("diff-main"); const f = d.getElementById("diff-file-5"); return m && f ? Math.abs(f.getBoundingClientRect().top - m.getBoundingClientRect().top) : false; })()`, &at,
		chromedp.WithPollingTimeout(5*time.Second))); err != nil || at > 20 {
		t.Errorf("a file picked inline did not open in fullscreen at that file (%.0fpx off): %v", at, err)
	}
}

// A description as people write them in Bitbucket, with a link, an image and
// raw HTML that would each run a script if the page trusted them.
const markdownDescription = "# Why\n" +
	"Some *emphasis*, **strong**, ~~gone~~, `code <b>`, and snake_case_name.\n\n" +
	"- [x] done\n- [ ] open\n  - nested\n\n" +
	"1. first\n2. second\n\n" +
	"> quoted **text**\n\n" +
	"```go\nfmt.Println(\"<script>\")\n```\n\n" +
	"| A | B |\n|:--|--:|\n| 1 | 2 |\n\n" +
	"[Design](https://confluence.example.com/x \"Design record\"), [evil](javascript:window.parent.bbHost.pwned='md-link'), " +
	"[local](/projects/PAY/repos/ledger/browse), <img src=x onerror=\"window.parent.bbHost.pwned='md-html'\">, https://example.com/bare.\n" +
	"![diagram](https://example.com/d.png) ![evil image](javascript:window.parent.bbHost.pwned='md-image')\n\n" +
	"---\n" +
	"Line one\nLine two\n"

// A description's Markdown becomes elements: headings, emphasis, lists and
// task lists, quotes, code, tables and links. HTML in it stays text, a link
// opens through the host and only to a web page, and an image is named, not
// fetched.
func TestDescriptionsDrawTheirMarkdown(t *testing.T) {
	pr := fixturePullRequest()
	pr.Description = markdownDescription
	card := viewPullRequest{PullRequest: pr, URL: "https://bitbucket.example.com/projects/PAY/repos/ledger/pull-requests/42/overview"}
	ctx := browser(t, []viewhost.Frame{{Title: "markdown", Mode: "fullscreen", Fullscreen: true,
		Arguments: map[string]any{"kind": "pull_request"}, Result: fixtureResult(t, viewPayload{Kind: showKindPullRequest, PullRequest: &card})}})

	var drawn struct {
		Heading   string   `json:"heading"`
		Em        []string `json:"em"`
		Strong    []string `json:"strong"`
		Del       []string `json:"del"`
		CodeSpans []string `json:"codeSpans"`
		Checked   []bool   `json:"checked"`
		Nested    int      `json:"nested"`
		Ordered   int      `json:"ordered"`
		Quote     string   `json:"quote"`
		Block     string   `json:"block"`
		Headers   []string `json:"headers"`
		Cells     []string `json:"cells"`
		Links     []string `json:"links"`
		Inert     []string `json:"inert"`
		Rules     int      `json:"rules"`
		Lines     bool     `json:"lines"`
		Text      string   `json:"text"`
		Images    int      `json:"images"`
	}
	inFrame(t, ctx, 0, `const m = d.querySelector(".details-main .markdown");
		const texts = (selector) => [...m.querySelectorAll(selector)].map((e) => e.textContent);
		return {
			heading: (m.querySelector("h3.md-h1") || {}).textContent || "",
			em: texts("em"), strong: texts("strong"), del: texts("del"), codeSpans: texts("code.md-code-span"),
			checked: [...m.querySelectorAll("li.md-task input")].map((i) => i.checked),
			nested: m.querySelectorAll("li .md-list li").length,
			ordered: m.querySelectorAll("ol > li").length,
			quote: (m.querySelector("blockquote strong") || {}).textContent || "",
			block: (m.querySelector("pre.md-code code") || {}).textContent || "",
			headers: [...m.querySelectorAll("th")].map((c) => c.className + ":" + c.textContent),
			cells: [...m.querySelectorAll("td")].map((c) => c.className + ":" + c.textContent),
			links: [...m.querySelectorAll("button.md-link")].map((b) => b.textContent + "|" + b.title),
			inert: texts(".md-link-text, span.md-image"),
			rules: m.querySelectorAll("hr.md-rule").length,
			lines: [...m.querySelectorAll("p")].some((p) => p.textContent === "Line oneLine two" && p.querySelector("br") !== null),
			text: m.textContent,
			images: m.querySelectorAll("img").length,
		};`, &drawn)

	if drawn.Heading != "Why" {
		t.Errorf("the heading is %q, want Why", drawn.Heading)
	}
	if !slices.Equal(drawn.Em, []string{"emphasis"}) || !slices.Contains(drawn.Strong, "strong") || !slices.Equal(drawn.Del, []string{"gone"}) {
		t.Errorf("the emphasis is em %v, strong %v, del %v; want emphasis, strong and gone, and snake_case left alone", drawn.Em, drawn.Strong, drawn.Del)
	}
	if !slices.Equal(drawn.CodeSpans, []string{"code <b>"}) || drawn.Block != `fmt.Println("<script>")` {
		t.Errorf("the code is %v and %q, want it as written", drawn.CodeSpans, drawn.Block)
	}
	if !slices.Equal(drawn.Checked, []bool{true, false}) || drawn.Nested != 1 || drawn.Ordered != 2 {
		t.Errorf("the lists are tasks %v, %d nested, %d ordered; want one task done and one open, one nested item, two ordered", drawn.Checked, drawn.Nested, drawn.Ordered)
	}
	if drawn.Quote != "text" || drawn.Rules != 1 || !drawn.Lines {
		t.Errorf("the quote is %q, %d rules, a break between lines: %v", drawn.Quote, drawn.Rules, drawn.Lines)
	}
	if !slices.Equal(drawn.Headers, []string{"align-left:A", "align-right:B"}) || !slices.Equal(drawn.Cells, []string{"align-left:1", "align-right:2"}) {
		t.Errorf("the table is %v over %v, want A and B aligned left and right", drawn.Headers, drawn.Cells)
	}
	wantLinks := []string{
		"Design|Design record (https://confluence.example.com/x)",
		"local|https://bitbucket.example.com/projects/PAY/repos/ledger/browse",
		"https://example.com/bare|https://example.com/bare",
		"diagram|Open the image: https://example.com/d.png",
	}
	if !slices.Equal(drawn.Links, wantLinks) {
		t.Errorf("the links are %q, want %q", drawn.Links, wantLinks)
	}
	if !slices.Equal(drawn.Inert, []string{"evil", "evil image"}) {
		t.Errorf("the links that lead nowhere a web page is are %q, want them as text", drawn.Inert)
	}
	if !strings.Contains(drawn.Text, `<img src=x onerror="window.parent.bbHost.pwned='md-html'">`) || drawn.Images != 0 {
		t.Errorf("the raw HTML became %d images rather than text:\n%s", drawn.Images, drawn.Text)
	}

	clickButton(t, ctx, 0, "Design")
	chromedp.Run(ctx, chromedp.Sleep(200*time.Millisecond))
	if opened := hostMessages(t, ctx, 0, "ui/open-link"); len(opened) != 1 || messageText(opened[0]) != "https://confluence.example.com/x" {
		t.Errorf("the link asked the host to open %v, want the design record", opened)
	}
	var pwned any
	if err := chromedp.Run(ctx, chromedp.Evaluate(`window.bbHost.pwned || null`, &pwned)); err != nil {
		t.Fatal(err)
	}
	if pwned != nil {
		t.Errorf("the description ran a script: %v", pwned)
	}
}
