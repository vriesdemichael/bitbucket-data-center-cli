// Command view-preview renders bb's MCP views from a real Bitbucket, outside
// an MCP client.
//
// It runs bb's MCP server in-process with bb's own configuration, calls the
// show tool as a client that renders views, and writes a page that mounts
// each answer in a stand-in host (internal/mcp/viewhost), light and dark:
//
//	go run ./tools/view-preview -project PAY -repo ledger -id 1 -out views.html
//
// Open the page in a browser. Nothing on it reaches Bitbucket: every view
// draws from the result it was handed.
//
// -screenshots writes a PNG of each frame; with -bare, of each view alone, as
// the docs show them, and -theme draws every frame light or dark:
//
//	go run ./tools/view-preview -project PAY -repo ledger -id 1 -screenshots out -bare -theme dark
//
// -from adds the pull request form for a new pull request from that branch.
// -kinds draws only the kinds it names, and -height draws each fullscreen
// frame that tall, to see a long view whole. The stand-in host passes tool
// calls, so the views offer what they do in a client; a view the person
// clicks in does nothing here.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	cdppage "github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/viewhost"
)

func main() {
	project := flag.String("project", "", "project key of the pull request and the list")
	repo := flag.String("repo", "", "repository slug")
	id := flag.String("id", "", "pull request ID for the card and the diff")
	state := flag.String("state", "ALL", "state of the pull requests in the list")
	out := flag.String("out", "views.html", "where to write the page")
	screenshots := flag.String("screenshots", "", "a directory to write a PNG of each frame into, taken with headless Chrome")
	bare := flag.Bool("bare", false, "capture each view alone, without its frame's title and the host's log")
	theme := flag.String("theme", "", "light or dark for every frame; empty draws each in its own")
	from := flag.String("from", "", "a branch to draft a new pull request from, in the pull request form")
	kinds := flag.String("kinds", "", "comma-separated kinds to draw, such as pull_request,diff; empty draws every kind")
	height := flag.Int("height", 0, "how tall to draw each fullscreen frame, in pixels; zero is the stand-in host's default")
	sameOrigin := flag.Bool("same-origin", false, "let the page reach into its frames, to drive the views from a script")
	flag.Parse()

	if *theme != "" && *theme != "light" && *theme != "dark" {
		fmt.Fprintln(os.Stderr, "view-preview: -theme is light or dark")
		os.Exit(2)
	}
	count, err := run(*project, *repo, *id, *state, *theme, *from, *kinds, *height, *sameOrigin, *out)
	if err == nil && *screenshots != "" {
		err = capture(*out, *screenshots, count, *bare)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "view-preview:", err)
		os.Exit(1)
	}
}

// capture opens the page in headless Chrome and saves each frame as it is
// drawn, at twice the pixel density so text stays sharp.
//
// The viewport is tall enough to hold every frame at once, and each frame is
// cut from what is on screen. Scrolling to a frame, or capturing past the
// viewport, makes Chrome lay the page out again mid-capture, and a view in a
// sandboxed frame redraws late: the capture caught views half laid out.
func capture(page, dir string, count int, bare bool) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	absolute, err := filepath.Abs(page)
	if err != nil {
		return err
	}
	ctx, cancel := chromedp.NewContext(context.Background())
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	viewportHeight := 6000
	if err := chromedp.Do(ctx,
		chromedp.EmulateViewport(1400, int64(viewportHeight), chromedp.EmulateScale(2)),
		chromedp.Navigate("file:///"+filepath.ToSlash(absolute)),
		chromedp.WaitVisible("section.frame"),
		// The views draw once the stand-in host has answered them.
		chromedp.Sleep(2*time.Second),
	); err != nil {
		return err
	}
	pageHeight, err := chromedp.Run(ctx, chromedp.Evaluate[int](`document.documentElement.scrollHeight`))
	if err != nil {
		return err
	}
	// Tall frames make a page longer than the viewport: it grows to hold them.
	if pageHeight > viewportHeight {
		viewportHeight = pageHeight + 400
		if err := chromedp.Do(ctx,
			chromedp.EmulateViewport(1400, int64(viewportHeight), chromedp.EmulateScale(2)),
			chromedp.Sleep(2*time.Second),
		); err != nil {
			return err
		}
	}
	selector := "#frames > section"
	if bare {
		selector = "#frames > section > .stage"
	}
	boxes, err := chromedp.Run(ctx, chromedp.Evaluate[[]struct{ X, Y, Width, Height float64 }](fmt.Sprintf(`[...document.querySelectorAll(%q)].map((frame) => {
		const box = frame.getBoundingClientRect();
		return { X: box.left, Y: box.top, Width: box.width, Height: box.height };
	})`, selector)))
	if err != nil {
		return err
	}
	if len(boxes) != count {
		return fmt.Errorf("the page has %d frames, want %d", len(boxes), count)
	}
	for i, box := range boxes {
		if box.Y+box.Height > float64(viewportHeight) {
			return fmt.Errorf("frame %d ends %.0fpx down, past the %dpx the capture holds", i+1, box.Y+box.Height, viewportHeight)
		}
		shot, err := chromedp.Call(ctx, cdppage.CaptureScreenshot, cdppage.CaptureScreenshotParams{
			Clip: &cdppage.Viewport{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height, Scale: 1},
		})
		if err != nil {
			return fmt.Errorf("frame %d: %w", i+1, err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("view-%02d.png", i+1)), shot.Data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func run(project, repo, id, state, theme, from, kinds string, height int, sameOrigin bool, out string) (int, error) {
	if project == "" || repo == "" || id == "" {
		return 0, fmt.Errorf("-project, -repo and -id are required")
	}
	ctx := context.Background()

	cfg, err := config.LoadWithOverrides(config.Overrides{})
	if err != nil {
		return 0, err
	}
	clients, err := bbmcp.ClientsFromConfig(cfg)
	if err != nil {
		return 0, err
	}

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	server := bbmcp.NewServer(bbmcp.ServerOptions{Name: "bb", Version: "preview", Clients: clients})
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = serverSession.Close() }()

	// A client that renders views, as Claude Desktop and VS Code declare it.
	capabilities := &mcp.ClientCapabilities{}
	capabilities.AddExtension("io.modelcontextprotocol/ui", map[string]any{"mimeTypes": []string{"text/html;profile=mcp-app"}})
	client := mcp.NewClient(&mcp.Implementation{Name: "view-preview", Version: "1"}, &mcp.ClientOptions{Capabilities: capabilities})
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = session.Close() }()

	page, err := session.ReadResource(ctx, &mcp.ReadResourceParams{URI: "ui://bb/view"})
	if err != nil {
		return 0, fmt.Errorf("read the view page: %w", err)
	}

	card := map[string]any{"kind": "pull_request", "project": project, "repo": repo, "id": id}
	list := map[string]any{"kind": "pull_requests", "project": project, "repo": repo, "state": state}
	diff := map[string]any{"kind": "diff", "project": project, "repo": repo, "id": id}

	type frame struct {
		title      string
		theme      string
		mode       string
		fullscreen bool
		arguments  map[string]any
	}
	wanted := []frame{
		{"Pull request card, inline", "light", "inline", true, card},
		{"Pull request card, dark", "dark", "inline", true, card},
		{"Pull request list", "light", "inline", true, list},
		{"Diff, inline in a host without fullscreen", "light", "inline", false, diff},
		{"Pull request, fullscreen", "light", "fullscreen", true, card},
		{"Diff, fullscreen", "dark", "fullscreen", true, diff},
		{"Pull request in a host without fullscreen", "light", "inline", false, card},
	}
	if from != "" {
		wanted = append(wanted, frame{"Pull request form", "light", "inline", true, map[string]any{
			"kind": "pull_request_form", "project": project, "repo": repo, "from_ref": from,
			"title":       "Retry transient payment failures",
			"description": "Retries a charge the provider refused with a transient error, **twice**, with backoff.\n\n- Caps the time a charge spends retrying\n- Leaves declined cards alone",
			"reviewers":   "bob,carol",
		}})
	}
	if kinds != "" {
		drawn := strings.Split(kinds, ",")
		wanted = slices.DeleteFunc(wanted, func(want frame) bool {
			kind, _ := want.arguments["kind"].(string)
			return !slices.Contains(drawn, kind)
		})
	}

	// An answer for the views' own tool, so the stand-in host says it passes
	// tool calls and the views offer what they offer in a client. A view
	// whose data was read just now does not call it while the page is taken.
	unchanged, err := json.Marshal(&mcp.CallToolResult{
		Content:           []mcp.Content{&mcp.TextContent{Text: "Unchanged."}},
		StructuredContent: map[string]any{"changed": false},
	})
	if err != nil {
		return 0, err
	}

	var frames []viewhost.Frame
	for _, want := range wanted {
		result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "show", Arguments: want.arguments})
		if err != nil {
			return 0, fmt.Errorf("show %v: %w", want.arguments["kind"], err)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			return 0, err
		}
		if theme != "" {
			want.theme = theme
		}
		frameHeight := 0
		if want.mode == "fullscreen" {
			frameHeight = height
		}
		frames = append(frames, viewhost.Frame{
			Title:       want.title,
			Theme:       want.theme,
			Mode:        want.mode,
			Fullscreen:  want.fullscreen,
			Height:      frameHeight,
			Arguments:   want.arguments,
			Result:      encoded,
			ToolResults: map[string][]json.RawMessage{"refresh_view": {unchanged}},
		})
	}

	html, err := viewhost.Page(page.Contents[0].Text, frames, viewhost.Options{Heading: "bb views: " + project + "/" + repo + " #" + id, SameOrigin: sameOrigin})
	if err != nil {
		return 0, err
	}
	return len(frames), os.WriteFile(out, []byte(html), 0o600)
}
