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
		// Not an embedded image, so never a source: bob is drawn with initials.
		"bob": "javascript:window.parent.bbHost.pwned='avatar'",
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
	for _, want := range []string{hostileTitle, hostileName, "1 approved", "1 changes requested", "1 build failed", "1 build in progress", "1 build passed", "Changes requested", "Builds", "Overview"} {
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
	if !containsString(initials, "BC") {
		t.Errorf("bob, whose avatar was not an embedded image, is not drawn with initials: %v", initials)
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
	inFrame(t, ctx, 0, `d.querySelector(".button.primary").click(); return null;`, nil)
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
	inFrame(t, ctx, 4, `return d.querySelector(".inline-details .description") !== null;`, &inline)
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
