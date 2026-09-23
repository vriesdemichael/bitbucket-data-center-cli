package fileview

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// decodedText is a file's bytes read as text.
type decodedText struct {
	text     string
	mimeType string
	// note says how the text was decoded when it was not UTF-8, for the header.
	note string
}

// decodeUnicode reads a file as text when it is: valid UTF-8 without a NUL,
// or UTF-16 that says so with a byte order mark.
func decodeUnicode(content []byte) (decodedText, bool) {
	if utf8.Valid(content) && bytes.IndexByte(content, 0) < 0 {
		return decodedText{text: string(content), mimeType: textMIMEType(content)}, true
	}

	return decodeUTF16(content)
}

// textMIMEType is the type of a file already known to be UTF-8 text.
// http.DetectContentType names HTML and XML, and otherwise says text/plain --
// unless a signature matched, which for text means a coincidence: a file that
// begins "BM" is sniffed as a bitmap.
func textMIMEType(content []byte) string {
	if sniffed := http.DetectContentType(content); strings.HasPrefix(sniffed, "text/") {
		return sniffed
	}

	return "text/plain; charset=utf-8"
}

// decodeUTF16 reads text written as UTF-16 with a byte order mark, which is
// how Windows saves "Unicode" text. Without the mark there is no telling it
// from binary, so it is not tried.
func decodeUTF16(content []byte) (decodedText, bool) {
	if len(content) < 2 || len(content)%2 != 0 {
		return decodedText{}, false
	}

	var littleEndian bool
	switch {
	case content[0] == 0xFF && content[1] == 0xFE:
		littleEndian = true
	case content[0] == 0xFE && content[1] == 0xFF:
	default:
		return decodedText{}, false
	}

	units := make([]uint16, 0, len(content)/2-1)
	for index := 2; index < len(content); index += 2 {
		if littleEndian {
			units = append(units, uint16(content[index])|uint16(content[index+1])<<8)
		} else {
			units = append(units, uint16(content[index])<<8|uint16(content[index+1]))
		}
	}

	decoded := string(utf16.Decode(units))
	for _, character := range decoded {
		if character < 0x20 && !textControl(character) {
			return decodedText{}, false
		}
	}

	byteOrder := "be"
	if littleEndian {
		byteOrder = "le"
	}

	return decodedText{text: decoded, mimeType: "text/plain; charset=utf-16" + byteOrder, note: ", decoded from UTF-16"}, true
}

// decodeWindows1252 reads text that is not UTF-8 as Windows-1252, the code page
// most older Western text was saved in, when every byte could be text in it:
// no NUL, no control character a text file does not use, and none of the five
// bytes the code page leaves undefined. A binary file almost always has one
// of those; one in another code page does not, and reads with a few wrong
// characters, which the header owns up to.
func decodeWindows1252(content []byte) (decodedText, bool) {
	var decoded strings.Builder
	decoded.Grow(len(content) + len(content)/8)

	for _, value := range content {
		switch {
		case value < 0x20:
			if !textControl(rune(value)) {
				return decodedText{}, false
			}
			decoded.WriteByte(value)
		case value < 0x80:
			decoded.WriteByte(value)
		case value < 0xA0:
			character := windows1252[value-0x80]
			if character == 0 {
				return decodedText{}, false
			}
			decoded.WriteRune(character)
		default:
			decoded.WriteRune(rune(value))
		}
	}

	return decodedText{
		text:     decoded.String(),
		mimeType: "text/plain; charset=windows-1252",
		note:     ", not UTF-8 and so decoded from Windows-1252, where a character from another code page may come out wrong",
	}, true
}

// textControl reports the control characters text files use: tab, the line
// and page breaks, escape for terminal colours, and the ^Z that ended DOS text.
func textControl(character rune) bool {
	switch character {
	case '\t', '\n', '\v', '\f', '\r', 0x1A, 0x1B:
		return true
	}

	return false
}

// windows1252 maps 0x80-0x9F, where Windows-1252 differs from Latin-1. Zero
// marks the five bytes it leaves undefined.
var windows1252 = [32]rune{
	0x20AC, 0, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021,
	0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0, 0x017D, 0,
	0, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014,
	0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0, 0x017E, 0x0178,
}

// linedText says how a text read as lines is named in its header.
type linedText struct {
	// what the text is: "text", or "text extracted from a Word document".
	what string
	// unit is what a line holds, "line" or "entry", for the count.
	unit string
	// layout is a sentence saying how the text is laid out in lines, when
	// that is not plain.
	layout string
	// noun is what "the whole ..." refers to: file, text or listing.
	noun string
	// empty is the header, after the subject, when there are no lines.
	empty string
	// note follows the window's sentence: what the text leaves out.
	note string
}

// lines views text as a window of numbered lines.
func lines(request Request, kind Kind, mimeType string, size int64, form linedText, text string) (View, error) {
	unit := form.unit
	if unit == "" {
		unit = "line"
	}

	cut, err := cutWindow(text, request.StartLine, request.LineCount)
	if err != nil {
		return View{}, apperrors.New(apperrors.KindValidation, fmt.Sprintf(
			"start_line %d is past the end of %s, which has %s", request.StartLine, request.Path, plural(cut.total, unit)), nil)
	}

	var header strings.Builder
	header.WriteString(subjectOf(request))
	header.WriteString(": ")
	if cut.total == 0 {
		header.WriteString(form.empty)
	} else {
		header.WriteString(form.what + ", " + plural(cut.total, unit) + ". ")
		if form.layout != "" {
			header.WriteString(form.layout + " ")
		}
		header.WriteString(windowSentence(cut, form.noun))
	}
	if form.note != "" {
		header.WriteString(" " + form.note)
	}

	return View{
		Kind:     kind,
		MIMEType: mimeType,
		Size:     size,
		Text:     header.String() + "\n" + cut.view,
		Window: &Window{
			Content:       cut.content,
			StartLine:     cut.start,
			EndLine:       cut.end,
			TotalLines:    cut.total,
			NextStartLine: cut.next,
		},
	}, nil
}

// window is a run of lines cut out of a text.
type window struct {
	// start and end are the first and last line held, counting from 1.
	start, end int
	total      int
	// next is the start of the following window, or 0 at the end.
	next int
	// view is the lines numbered as cat -n numbers them.
	view string
	// content is the lines as they are in the text, endings included.
	content string
	// shown and length are set when the one line held was longer than a window
	// and cut to fit: how many of its bytes are shown, of how many.
	shown, length int
	// full is set when the window stopped at WindowBytes rather than at its
	// line count.
	full bool
}

// errPastEnd is a window that starts after the last line.
var errPastEnd = errors.New("the window starts past the end of the text")

// cutWindow cuts the window of lineCount lines from startLine out of text.
//
// Lines are counted as cat -n and wc -l count them: each ends at a newline, and
// text after the last newline is a line of its own. A carriage return before
// the newline is left out of the numbered view, where it is noise, and kept in
// content, which is the text as it is.
//
// The window stops at WindowBytes of numbered view. A first line that is
// longer than that on its own is cut to fit, at a character boundary, so every
// window holds at least one line and the next always starts further on.
func cutWindow(text string, startLine, lineCount int) (window, error) {
	if startLine == 0 {
		startLine = 1
	}
	if lineCount == 0 {
		lineCount = DefaultLineCount
	}
	lineCount = min(lineCount, MaxLineCount)

	result := window{start: startLine, total: countLines(text)}
	if startLine > result.total && (result.total > 0 || startLine > 1) {
		return result, errPastEnd
	}

	var view, content strings.Builder
	offset := lineOffset(text, startLine)
	used := 0
	number := startLine

	for number <= result.total && number-startLine < lineCount {
		raw := text[offset:]
		if end := strings.IndexByte(raw, '\n'); end >= 0 {
			raw = raw[:end+1]
		}
		shown := strings.TrimSuffix(strings.TrimSuffix(raw, "\n"), "\r")
		prefix := fmt.Sprintf("%6d\t", number)
		cost := len(prefix) + len(shown) + 1

		if used+cost > WindowBytes {
			if number > startLine {
				result.full = true

				break
			}

			// One line longer than a window: as much of it as fits.
			fits := WindowBytes - len(prefix) - 1
			for fits > 0 && !utf8.RuneStart(shown[fits]) {
				fits--
			}
			view.WriteString(prefix + shown[:fits] + "\n")
			content.WriteString(shown[:fits])
			result.shown, result.length = fits, len(shown)
			number++

			break
		}

		view.WriteString(prefix + shown + "\n")
		content.WriteString(raw)
		used += cost
		offset += len(raw)
		number++
	}

	result.end = number - 1
	if number <= result.total {
		result.next = number
	}
	result.view = view.String()
	result.content = content.String()

	return result, nil
}

// countLines counts the lines in text as wc -l would, plus a last line that
// has no newline after it.
func countLines(text string) int {
	if text == "" {
		return 0
	}

	count := strings.Count(text, "\n")
	if !strings.HasSuffix(text, "\n") {
		count++
	}

	return count
}

// lineOffset is where line starts in text, counting from 1.
func lineOffset(text string, line int) int {
	offset := 0
	for range line - 1 {
		end := strings.IndexByte(text[offset:], '\n')
		if end < 0 {
			return len(text)
		}
		offset += end + 1
	}

	return offset
}

// windowSentence says which lines a window holds and where the next starts.
func windowSentence(cut window, noun string) string {
	held := fmt.Sprintf("Lines %d-%d follow", cut.start, cut.end)
	if cut.start == cut.end {
		held = fmt.Sprintf("Line %d follows", cut.start)
	}

	switch {
	case cut.shown > 0 && cut.next == 0:
		return fmt.Sprintf("%s, cut to its first %s of %s; it is the last line of the %s, and the rest of it is not returned.",
			held, formatSize(int64(cut.shown)), formatSize(int64(cut.length)), noun)
	case cut.shown > 0:
		return fmt.Sprintf("%s, cut to its first %s of %s; for the next, pass start_line=%d.",
			held, formatSize(int64(cut.shown)), formatSize(int64(cut.length)), cut.next)
	case cut.next == 0 && cut.start == 1:
		return held + ": the whole " + noun + "."
	case cut.next == 0:
		return held + ": the end of the " + noun + "."
	case cut.full:
		return fmt.Sprintf("%s, as many as fit in %s; for the next, pass start_line=%d.", held, formatSize(WindowBytes), cut.next)
	default:
		return fmt.Sprintf("%s; for the next, pass start_line=%d.", held, cut.next)
	}
}

// plural counts a unit: "1 line", "3 lines", "1 entry", "48 entries".
func plural(count int, unit string) string {
	if count == 1 {
		return "1 " + unit
	}
	if strings.HasSuffix(unit, "y") {
		return fmt.Sprintf("%d %sies", count, strings.TrimSuffix(unit, "y"))
	}

	return fmt.Sprintf("%d %ss", count, unit)
}
