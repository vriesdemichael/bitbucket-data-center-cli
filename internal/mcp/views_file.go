package mcp

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/fileview"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp/highlight"
)

// The file viewer (#686): a file the person cannot open in their own editor,
// such as another repository's, read as get_file_content reads it and drawn
// as a file: code with line numbers, a picture, a player, a listing, or what
// the file is when it cannot be shown.

// fileViewLines is how many lines a view asks for at a time. fileview's
// window stops at WindowBytes whatever this says, so this only lets a file
// of short lines come back in fewer, longer windows.
const fileViewLines = fileview.MaxLineCount

// viewImageTypes are the pictures a view draws as they are stored, where the
// tool sends the model a smaller copy: the types a browser draws, and no
// SVG, which a view never draws from data.
var viewImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

// viewFile is a file as a view draws it.
type viewFile struct {
	Path string `json:"path"`
	At   string `json:"at,omitempty"`
	// URL is the file's page in Bitbucket.
	URL string `json:"url,omitempty"`
	// Kind is what the file turned out to be, in fileview's words: text,
	// document, archive, image, audio, video, binary or too_large.
	Kind     string `json:"kind"`
	MIMEType string `json:"mime_type,omitempty"`
	// Size is the file's size in bytes, or -1 when it is not known.
	Size int64 `json:"size"`
	// Lines is a window of the file's lines, for a file read as lines, from
	// StartLine to EndLine of TotalLines; NextLine is where the next window
	// starts, or 0 at the end.
	Lines      string `json:"lines,omitempty"`
	StartLine  int    `json:"start_line,omitempty"`
	EndLine    int    `json:"end_line,omitempty"`
	TotalLines int    `json:"total_lines,omitempty"`
	NextLine   int    `json:"next_line,omitempty"`
	// Highlight are the spans of each line of the window, for a language the
	// highlighter knows; a line past the end of it is drawn plain.
	Highlight []string `json:"highlight,omitempty"`
	// Data is a picture, audio or a video, as a data: URI, and Width and
	// Height a picture's; Scaled says the picture is smaller than stored.
	Data   string `json:"data,omitempty"`
	Width  int    `json:"width,omitempty"`
	Height int    `json:"height,omitempty"`
	Scaled bool   `json:"scaled,omitempty"`
	// Description is what bb says of a file whose bytes are not shown.
	Description string `json:"description,omitempty"`
}

// fileForView reads the file, or the window of it starting at in.StartLine.
func fileForView(ctx context.Context, c Clients, in ShowInput) (viewFile, viewSummary, error) {
	request, view, content, err := readFile(ctx, c, GetFileContentInput{
		Project: in.Project, Repo: in.Repo, Path: in.Path, At: in.At, StartLine: in.StartLine, LineCount: fileViewLines,
	})
	if err != nil {
		return viewFile{}, viewSummary{}, err
	}

	file := viewFile{Path: in.Path, At: in.At, URL: request.WebURL, Kind: string(view.Kind), MIMEType: view.MIMEType, Size: view.Size}
	state := ""
	switch {
	case view.Window != nil:
		file.Lines = view.Window.Content
		file.StartLine, file.EndLine = view.Window.StartLine, view.Window.EndLine
		file.TotalLines, file.NextLine = view.Window.TotalLines, view.Window.NextStartLine
		state = fmt.Sprintf("lines %d to %d of %d.", file.StartLine, file.EndLine, file.TotalLines)
		// Text is highlighted whole and the window's lines taken from it, so
		// a comment that opens above the window colours the lines in it.
		if view.Kind == fileview.KindText && file.StartLine >= 1 && file.EndLine >= file.StartLine {
			if lines, ok := highlight.Lines(in.Path, string(content), time.Now().Add(highlightBudget)); ok && len(lines) >= file.EndLine {
				file.Highlight = lines[file.StartLine-1 : file.EndLine]
			}
		}
	case view.Image != nil:
		data, mimeType := view.Image.Data, view.Image.MIMEType
		file.Scaled = view.Image.Scaled
		// The model gets a copy small enough for it; the person sees the
		// picture as stored, where a browser draws that and it fits.
		if stored := http.DetectContentType(content); view.Image.Scaled && viewImageTypes[stored] && len(content) <= fileview.ImageBytes {
			data, mimeType, file.Scaled = content, stored, false
		}
		file.Data = "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(data)
		file.Width, file.Height = view.Image.Width, view.Image.Height
		state = fmt.Sprintf("a picture, %d by %d.", file.Width, file.Height)
	case view.Media != nil:
		file.Data = "data:" + view.Media.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(view.Media.Data)
		state = fmt.Sprintf("%s, with a player.", view.Kind)
	default:
		file.Description = view.Text
		state = view.Text
	}

	at := ""
	if in.At != "" {
		at = " at " + in.At
	}
	return file, viewSummary{
		subject: fmt.Sprintf("%s in %s/%s%s", in.Path, in.Project, in.Repo, at),
		form:    "an interactive view",
		state:   state,
	}, nil
}
