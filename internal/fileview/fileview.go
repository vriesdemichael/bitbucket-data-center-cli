// Package fileview turns the bytes of a repository file into something a
// model can use inside a client that has nothing else: no file system, no
// shell, no bb. What it can use is text or an image, so a file becomes one of
// those, converted here, or a description of what it is.
//
// Text comes back as numbered lines in windows, so a long file is read a part
// at a time and one enormous line cannot flood a context. What the file is
// decides everything else, and is read from its bytes; the extension is only a
// hint where the bytes cannot tell.
//
// Nothing here makes a request. The caller fetches the file and hands over its
// bytes, and ADR-094 has the reasoning.
package fileview

import (
	"bytes"
	"net/http"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// Kind is what a file turned out to be, and so what comes back of it.
type Kind string

const (
	// KindText is text, returned as a window of numbered lines.
	KindText Kind = "text"
	// KindDocument is a Word, PowerPoint or Excel file, returned as a window
	// of the text extracted from it.
	KindDocument Kind = "document"
	// KindArchive is a zip or tar archive, returned as a window of the
	// listing of its entries.
	KindArchive Kind = "archive"
	// KindImage is an image, returned as an image: scaled down when it is
	// larger than a client takes, and an animation's first frame.
	KindImage Kind = "image"
	// KindAudio is audio, returned as audio when it is within MediaBytes and
	// described when it is not.
	KindAudio Kind = "audio"
	// KindVideo is a video, returned as an embedded resource when it is within
	// MediaBytes and described when it is not.
	KindVideo Kind = "video"
	// KindBinary is a file whose bytes are not shown: only a description of
	// its type and size comes back.
	KindBinary Kind = "binary"
	// KindTooLarge is a file over MaxFileBytes, which was not read.
	KindTooLarge Kind = "too_large"
)

// Request is one file to view.
type Request struct {
	// Path and At name the file as the caller did.
	Path string
	At   string
	// WebURL is the file's page in Bitbucket, which a person can open. A
	// description of a file whose bytes are not shown ends with it.
	WebURL string
	// StartLine is the first line of the window, counting from 1; zero is 1.
	StartLine int
	// LineCount is how many lines the window holds; zero is DefaultLineCount,
	// and more than MaxLineCount is MaxLineCount.
	LineCount int
}

// View is what comes back of a file.
type View struct {
	Kind     Kind
	MIMEType string
	// Size is the file's size in bytes, or -1 when it is not known: a file
	// too large to read whose size Bitbucket did not declare.
	Size int64
	// Text is what the model reads: a header and the numbered lines of a
	// window, or the whole description of a file whose bytes are not shown.
	Text string
	// Window is the window of lines, for a kind read as lines.
	Window *Window
	// Image is the image returned, for an image.
	Image *Image
	// Media is the audio or video returned, when it is within MediaBytes.
	Media *Media
}

// Image is an image returned as an image.
type Image struct {
	// Data is the image, in MIMEType: the file itself when it fits what a
	// client takes, and otherwise the image encoded again.
	Data     []byte
	MIMEType string
	// Width and Height are the image's as it is seen: as stored, or once
	// turned upright when Turned is set.
	Width  int
	Height int
	// ReturnedWidth and ReturnedHeight are the image's as returned.
	ReturnedWidth  int
	ReturnedHeight int
	// Turned is set when the image was stored turned or mirrored, as its
	// orientation tag says, and comes back turned upright.
	Turned bool
	// Scaled is set when the image returned is smaller than the one stored.
	Scaled bool
	// Frames is how many frames an animation has, of which the first is
	// returned; zero for a still image.
	Frames int
	// Pages is how many pages a TIFF has, of which the first is returned;
	// zero for one of a single page.
	Pages int
}

// Window is a run of lines out of a file's text.
type Window struct {
	// Content is the lines as they are in the text, line endings included, so
	// a window that covers the whole file is the file.
	Content string
	// StartLine and EndLine are the first and last line held, counting from 1.
	// EndLine is StartLine-1 when the text has no lines.
	StartLine int
	EndLine   int
	// TotalLines is how many lines the whole text has.
	TotalLines int
	// NextStartLine is where the following window starts, or 0 when this one
	// reaches the end.
	NextStartLine int
}

// Validate refuses a window no file can serve, so a caller can refuse it
// before fetching anything.
func (request Request) Validate() error {
	if request.StartLine < 0 {
		return apperrors.New(apperrors.KindValidation, "start_line counts from 1; omit it to start at the first line", nil)
	}
	if request.LineCount < 0 {
		return apperrors.New(apperrors.KindValidation, "line_count must be 1 or more; omit it for the default", nil)
	}

	return nil
}

// Read views a file from its bytes.
//
// The only error is a window that cannot be served: one Validate refuses, or
// one that starts past the end of the text.
func Read(request Request, content []byte) (View, error) {
	if err := request.Validate(); err != nil {
		return View{}, err
	}

	size := int64(len(content))

	// Text first: a signature is a few bytes at the start, and a text file
	// can begin with any of them -- "BM" is a bitmap's -- while no real file
	// of those kinds is valid UTF-8 without a NUL all the way through.
	if text, ok := decodeUnicode(content); ok {
		return readText(request, text, size)
	}

	sniffed := sniff(content)
	if _, ok := imageFormats[sniffed]; ok {
		return readImage(request, sniffed, content, defaultImageLimits), nil
	}
	if view, ok, err := readArchive(request, sniffed, content); ok || err != nil {
		return view, err
	}
	if view, ok := readMedia(request, sniffed, content); ok {
		return view, nil
	}

	// Last before giving up, and after every signature, because it accepts
	// almost any byte: it is what is left of text that is not Unicode, and a
	// binary file nearly always has a NUL or a control character it refuses.
	if text, ok := decodeWindows1252(content); ok {
		return readText(request, text, size)
	}

	return describeBinary(subjectOf(request), request.WebURL, detectBinary(request.Path, sniffed, content), size), nil
}

// sniff reads a file's type from its first bytes: http.DetectContentType's
// answer, or for a signature it does not know, this package's.
func sniff(content []byte) string {
	if bytes.HasPrefix(content, []byte("II*\x00")) || bytes.HasPrefix(content, []byte("MM\x00*")) {
		return "image/tiff"
	}

	return http.DetectContentType(content)
}

// readText views decoded text as a window of its lines.
func readText(request Request, text decodedText, size int64) (View, error) {
	return lines(request, KindText, text.mimeType, size, linedText{
		what:  "text" + text.note,
		noun:  "file",
		empty: "an empty text file.",
	}, text.text)
}

// TooLarge views a file that was not read because it is over limit. size is
// what Bitbucket declared the file to be, or -1 when it did not say.
func TooLarge(request Request, limit, size int64) View {
	var text strings.Builder
	text.WriteString(subjectOf(request))
	if size >= 0 {
		text.WriteString(": " + formatSize(size))
		text.WriteString(", larger than the " + formatSize(limit) + " this tool reads, so it was not read.")
	} else {
		text.WriteString(": larger than the " + formatSize(limit) + " this tool reads, so it was not read.")
	}
	text.WriteString(personCanOpen(request.WebURL))

	return View{Kind: KindTooLarge, Size: size, Text: text.String()}
}

// subjectOf names the file the way a header does: its path, and the ref it
// was read at when one was given.
func subjectOf(request Request) string {
	if request.At == "" {
		return request.Path
	}

	return request.Path + " at " + request.At
}
