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
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
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
	flag.Parse()

	if *theme != "" && *theme != "light" && *theme != "dark" {
		fmt.Fprintln(os.Stderr, "view-preview: -theme is light or dark")
		os.Exit(2)
	}
	count, err := run(*project, *repo, *id, *state, *theme, *out)
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

	const viewportHeight = 6000
	if err := chromedp.Run(ctx,
		chromedp.EmulateViewport(1400, viewportHeight, chromedp.EmulateScale(2)),
		chromedp.Navigate("file:///"+filepath.ToSlash(absolute)),
		chromedp.WaitVisible("section.frame"),
		// The views draw once the stand-in host has answered them.
		chromedp.Sleep(2*time.Second),
	); err != nil {
		return err
	}
	selector := "#frames > section"
	if bare {
		selector = "#frames > section > .stage"
	}
	var boxes []struct{ X, Y, Width, Height float64 }
	if err := chromedp.Run(ctx, chromedp.Evaluate(fmt.Sprintf(`[...document.querySelectorAll(%q)].map((frame) => {
		const box = frame.getBoundingClientRect();
		return { X: box.left, Y: box.top, Width: box.width, Height: box.height };
	})`, selector), &boxes)); err != nil {
		return err
	}
	if len(boxes) != count {
		return fmt.Errorf("the page has %d frames, want %d", len(boxes), count)
	}
	for i, box := range boxes {
		if box.Y+box.Height > viewportHeight {
			return fmt.Errorf("frame %d ends %.0fpx down, past the %dpx the capture holds", i+1, box.Y+box.Height, viewportHeight)
		}
		var shot []byte
		if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			shot, err = cdppage.CaptureScreenshot().
				WithClip(&cdppage.Viewport{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height, Scale: 1}).
				Do(ctx)
			return err
		})); err != nil {
			return fmt.Errorf("frame %d: %w", i+1, err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("view-%02d.png", i+1)), shot, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func run(project, repo, id, state, theme, out string) (int, error) {
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
	threads := map[string]any{"kind": "threads", "project": project, "repo": repo, "id": id}

	var frames []viewhost.Frame
	for _, want := range []struct {
		title      string
		theme      string
		mode       string
		fullscreen bool
		arguments  map[string]any
	}{
		{"Pull request card, inline", "light", "inline", true, card},
		{"Pull request card, dark", "dark", "inline", true, card},
		{"Pull request list", "light", "inline", true, list},
		{"Diff, inline in a host without fullscreen", "light", "inline", false, diff},
		{"Pull request, fullscreen", "light", "fullscreen", true, card},
		{"Diff, fullscreen", "dark", "fullscreen", true, diff},
		{"Comment threads, inline", "light", "inline", true, threads},
		{"Comment threads, fullscreen", "light", "fullscreen", true, threads},
	} {
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
		frames = append(frames, viewhost.Frame{
			Title:      want.title,
			Theme:      want.theme,
			Mode:       want.mode,
			Fullscreen: want.fullscreen,
			Arguments:  want.arguments,
			Result:     encoded,
		})
	}

	html, err := viewhost.Page(page.Contents[0].Text, frames, viewhost.Options{Heading: "bb views: " + project + "/" + repo + " #" + id})
	if err != nil {
		return 0, err
	}
	return len(frames), os.WriteFile(out, []byte(html), 0o600)
}
