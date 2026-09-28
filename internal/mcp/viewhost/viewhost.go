// Package viewhost is a stand-in for an MCP Apps host, for looking at bb's
// views outside a real client: the browser tests render through it, and so
// does tools/view-preview.
//
// It writes one self-contained page that mounts the view page in sandboxed
// frames and answers it the way a host does: it completes the handshake,
// passes the host context and the tool's input and result, sizes an inline
// frame to what the view reports, switches display modes, and records what
// the view asks for, which a real host would act on.
package viewhost

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

// Frame is one view to mount.
type Frame struct {
	// Title labels the frame on the page.
	Title string `json:"title"`
	// Theme is light or dark.
	Theme string `json:"theme"`
	// Mode is the display mode it opens in: inline or fullscreen.
	Mode string `json:"mode"`
	// Fullscreen says whether the host offers fullscreen at all. A host
	// without it, as VS Code is, leaves the view to open out in place.
	Fullscreen bool `json:"fullscreen"`
	// Width is the view's width in pixels, such as a phone's; zero leaves it
	// to the page's grid.
	Width int `json:"width,omitempty"`
	// Height is a fullscreen view's height in pixels; zero is the default.
	Height int `json:"height,omitempty"`
	// Arguments are the tool's input, as the host passes them.
	Arguments map[string]any `json:"arguments"`
	// Result is the tools/call result, as the client received it.
	Result json.RawMessage `json:"result"`
}

// Options shape the page.
type Options struct {
	// Heading titles the page.
	Heading string
	// SameOrigin lets the page reach into its frames, which a test needs to
	// read what a view drew. A real host keeps a view on an origin of its
	// own, which the default keeps.
	SameOrigin bool
}

//go:embed host.html
var hostTemplate string

// Page renders the frames around the view page, which is the HTML bb serves
// as ui://bb/view.
func Page(viewPage string, frames []Frame, options Options) (string, error) {
	for i := range frames {
		if frames[i].Theme == "" {
			frames[i].Theme = "light"
		}
		if frames[i].Mode == "" {
			frames[i].Mode = "inline"
		}
	}
	// json.Marshal escapes <, > and &, so the view page and the results can
	// sit inside a script element without closing it.
	view, err := json.Marshal(viewPage)
	if err != nil {
		return "", fmt.Errorf("encode the view page: %w", err)
	}
	encodedFrames, err := json.Marshal(frames)
	if err != nil {
		return "", fmt.Errorf("encode the frames: %w", err)
	}
	sandbox := "allow-scripts"
	if options.SameOrigin {
		sandbox = "allow-scripts allow-same-origin"
	}
	heading, err := json.Marshal(options.Heading)
	if err != nil {
		return "", fmt.Errorf("encode the heading: %w", err)
	}

	page := hostTemplate
	for placeholder, value := range map[string]string{
		"/*BB_HOST_VIEW*/":    string(view),
		"/*BB_HOST_FRAMES*/":  string(encodedFrames),
		"/*BB_HOST_SANDBOX*/": fmt.Sprintf("%q", sandbox),
		"/*BB_HOST_HEADING*/": string(heading),
	} {
		if !strings.Contains(page, placeholder) {
			return "", fmt.Errorf("host.html has lost %s", placeholder)
		}
		page = strings.Replace(page, placeholder, value, 1)
	}
	return page, nil
}
