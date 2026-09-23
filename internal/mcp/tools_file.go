package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/fileview"
	browseservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/browse"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/download"
)

// GetFileContentInput is the argument set for get_file_content.
type GetFileContentInput struct {
	Project string `json:"project" jsonschema:"Bitbucket project key"`
	Repo    string `json:"repo" jsonschema:"Repository slug"`
	Path    string `json:"path" jsonschema:"File path in the repository"`
	At      string `json:"at,omitempty" jsonschema:"Git ref or branch to read from (e.g. refs/heads/main)"`
	// The window's descriptions are set in specGetFileContent, built from the
	// limits fileview enforces rather than written out a second time here.
	StartLine int `json:"start_line,omitempty"`
	LineCount int `json:"line_count,omitempty"`
}

// GetFileContentOutput says what the file turned out to be and carries what
// came back of it. The path and ref travel with the content because a model
// that fetched several files needs to tell them apart in its own context.
//
// The window's fields are pointers so that they are present, zero or not, for
// a file read as lines -- an empty file has no lines, and says so -- and absent
// for one that is not.
type GetFileContentOutput struct {
	Path          string     `json:"path"`
	At            string     `json:"at,omitempty"`
	Kind          string     `json:"kind" jsonschema:"What the file is, which decides what came back: text, a window of its lines; document, a window of the text extracted from a Word, PowerPoint or Excel file; archive, a window of the listing of a zip or tar archive's entries; image, the image, in the content beside this; binary, a description of its type and size only; too_large, over the most this tool reads, so not read"`
	MIMEType      string     `json:"mime_type,omitempty" jsonschema:"The file's type, read from its bytes; absent when the file was not read"`
	Size          *int64     `json:"size,omitempty" jsonschema:"The file's size in bytes; absent when it was too large to read and Bitbucket did not say how large"`
	WebURL        string     `json:"web_url" jsonschema:"The file's page in Bitbucket, for a person to open"`
	Content       *string    `json:"content,omitempty" jsonschema:"The window's lines without their numbers: for text, as they are in the file, line endings included, so a window covering the whole file is the file; for a document or an archive, the extracted text or the listing"`
	StartLine     *int       `json:"start_line,omitempty" jsonschema:"The first line in the window, counting from 1"`
	EndLine       *int       `json:"end_line,omitempty" jsonschema:"The last line in the window; one less than start_line when there are no lines"`
	TotalLines    *int       `json:"total_lines,omitempty" jsonschema:"How many lines the whole text has"`
	NextStartLine *int       `json:"next_start_line,omitempty" jsonschema:"Where the next window starts: pass it as start_line for the lines that follow. Absent when this window reaches the end"`
	Image         *FileImage `json:"image,omitempty" jsonschema:"What the image returned is, beside what the file holds"`
}

// FileImage says how the image get_file_content returned compares with the
// one the file holds, which scaling makes worth knowing: small text may not
// have survived it.
type FileImage struct {
	Width            int    `json:"width" jsonschema:"The image's width in pixels, as stored"`
	Height           int    `json:"height" jsonschema:"The image's height in pixels, as stored"`
	Scaled           bool   `json:"scaled" jsonschema:"True when the image returned is smaller than the one stored, so small text in it may no longer be legible"`
	ReturnedWidth    int    `json:"returned_width" jsonschema:"The returned image's width in pixels"`
	ReturnedHeight   int    `json:"returned_height" jsonschema:"The returned image's height in pixels"`
	ReturnedMIMEType string `json:"returned_mime_type" jsonschema:"The returned image's type, which differs from mime_type when it was encoded again"`
	ReturnedSize     int    `json:"returned_size" jsonschema:"The returned image's size in bytes"`
	Frames           int    `json:"frames,omitempty" jsonschema:"How many frames an animated image has; only the first is returned"`
}

func specGetFileContent() Spec {
	tool := &mcp.Tool{
		Name: "get_file_content",
		Description: "Read a file in a repository. Text comes back as a window of numbered lines: start_line and line_count " +
			"choose it, and each answer says which lines it holds and where the next window starts. A Word, PowerPoint or " +
			"Excel file comes back as the text extracted from it, and an archive (zip, jar, tar, tar.gz) as a listing of its " +
			"entries, both in the same windows. An image (PNG, JPEG, GIF, WebP) comes back as an image, scaled down when it " +
			"is large, with a note saying so. Any other file is described by its type and size rather than shown, and a file " +
			"over " + fmt.Sprintf("%d MiB", fileview.MaxFileBytes>>20) + " is described without being read.",
		Annotations: readOnly(),
		InputSchema: describedInputSchema[GetFileContentInput](map[string]string{
			"start_line": "First line of the window, counting from 1 (default 1). An answer that stops short of the end gives " +
				"next_start_line, the value to pass here for the lines that follow",
			"line_count": fmt.Sprintf("How many lines the window holds (default %d, at most %d). A window also stops at %d KiB "+
				"of text, so a file of long lines comes back in shorter windows",
				fileview.DefaultLineCount, fileview.MaxLineCount, fileview.WindowBytes>>10),
		}),
	}
	return toolSpec(tool, true, func(c Clients) mcp.ToolHandlerFor[GetFileContentInput, GetFileContentOutput] {
		svc := browseservice.NewService(c.OpenAPI, c.HTTP)
		return func(ctx context.Context, _ *mcp.CallToolRequest, in GetFileContentInput) (*mcp.CallToolResult, GetFileContentOutput, error) {
			request := fileview.Request{
				Path:      in.Path,
				At:        in.At,
				WebURL:    fileWebURL(c.BaseURL, in.Project, in.Repo, in.Path, in.At),
				StartLine: in.StartLine,
				LineCount: in.LineCount,
			}
			// Refused before the file is fetched, since no file makes a
			// negative line number mean anything.
			if err := request.Validate(); err != nil {
				return nil, GetFileContentOutput{}, fmt.Errorf("get_file_content: %w", err)
			}

			// Held in memory, because it is converted as a whole, so capped. A
			// file over the cap is described rather than refused: an error
			// would leave the model with nothing, when what it needs is to know
			// the file is there and too large.
			var held download.Memory
			err := svc.RawTo(ctx, browseservice.RepositoryRef{ProjectKey: in.Project, Slug: in.Repo}, in.Path, in.At, &held, fileview.MaxFileBytes)

			var view fileview.View
			var limit *download.LimitError
			switch {
			case errors.As(err, &limit):
				view = fileview.TooLarge(request, limit.Limit, limit.Size)
			case err != nil:
				return nil, GetFileContentOutput{}, fmt.Errorf("get_file_content failed: %w", err)
			default:
				if view, err = fileview.Read(request, held.Bytes()); err != nil {
					return nil, GetFileContentOutput{}, fmt.Errorf("get_file_content: %w", err)
				}
			}

			// Both values are named rather than returned as literals: a return
			// of two multi-line composite literals is the one shape successive
			// gofmt releases indent differently, and the tree is read with
			// whichever gofmt the reader has installed.
			result, structured := fileContentResult(in, request.WebURL, view)

			return result, structured, nil
		}
	})
}

// fileContentResult puts a view of a file into a tool result.
//
// The text is what a model reads, so it is the content itself rather than the
// JSON of the envelope the SDK would supply by default, as with get_pr_diff.
// structuredContent carries the same facts for a client that parses them.
func fileContentResult(in GetFileContentInput, webURL string, view fileview.View) (*mcp.CallToolResult, GetFileContentOutput) {
	structured := GetFileContentOutput{
		Path:     in.Path,
		At:       in.At,
		Kind:     string(view.Kind),
		MIMEType: view.MIMEType,
		WebURL:   webURL,
	}
	if view.Size >= 0 {
		size := view.Size
		structured.Size = &size
	}
	if window := view.Window; window != nil {
		content, start, end, total := window.Content, window.StartLine, window.EndLine, window.TotalLines
		structured.Content, structured.StartLine, structured.EndLine, structured.TotalLines = &content, &start, &end, &total
		if next := window.NextStartLine; next > 0 {
			structured.NextStartLine = &next
		}
	}

	// The text first, so a model reads what the image is -- and whether it
	// was scaled -- before it sees it.
	content := []mcp.Content{&mcp.TextContent{Text: view.Text}}
	if image := view.Image; image != nil {
		content = append(content, &mcp.ImageContent{Data: image.Data, MIMEType: image.MIMEType})
		structured.Image = &FileImage{
			Width:            image.Width,
			Height:           image.Height,
			Scaled:           image.Scaled,
			ReturnedWidth:    image.ReturnedWidth,
			ReturnedHeight:   image.ReturnedHeight,
			ReturnedMIMEType: image.MIMEType,
			ReturnedSize:     len(image.Data),
			Frames:           image.Frames,
		}
	}

	return &mcp.CallToolResult{Content: content}, structured
}

// fileWebURL is a file's page in Bitbucket's web interface, which a person can
// open: <base>/projects/<P>/repos/<r>/browse/<path>?at=<ref>.
//
// The path is escaped a segment at a time, and trimmed as the fetch trims it,
// so the page is the file that was read.
func fileWebURL(baseURL, project, repo, path, at string) string {
	segments := make([]string, 0, strings.Count(path, "/")+1)
	for _, segment := range strings.Split(path, "/") {
		if trimmed := strings.TrimSpace(segment); trimmed != "" && trimmed != "." {
			segments = append(segments, url.PathEscape(trimmed))
		}
	}

	webURL := fmt.Sprintf("%s/projects/%s/repos/%s/browse/%s", strings.TrimRight(baseURL, "/"),
		url.PathEscape(strings.TrimSpace(project)), url.PathEscape(strings.TrimSpace(repo)), strings.Join(segments, "/"))
	if ref := strings.TrimSpace(at); ref != "" {
		webURL += "?at=" + url.QueryEscape(ref)
	}

	return webURL
}
