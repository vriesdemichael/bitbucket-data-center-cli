package fileview

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"
)

// numberedText is count lines, each naming its own number, so a window that
// holds the wrong lines cannot hold the right text.
func numberedText(count int) string {
	var text strings.Builder
	for number := 1; number <= count; number++ {
		fmt.Fprintf(&text, "line %04d of the fixture\n", number)
	}

	return text.String()
}

func TestAWindowFromTheMiddleHoldsThoseLinesAndSaysWhereTheNextStarts(t *testing.T) {
	t.Parallel()

	view, err := Read(t.Context(), Request{Path: "src/long.txt", StartLine: 1200, LineCount: 100}, []byte(numberedText(3000)))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	window := view.Window
	if window == nil {
		t.Fatalf("a text file came back without a window: %+v", view)
	}
	if window.StartLine != 1200 || window.EndLine != 1299 || window.TotalLines != 3000 || window.NextStartLine != 1300 {
		t.Fatalf("window = lines %d-%d of %d, next %d; want 1200-1299 of 3000, next 1300",
			window.StartLine, window.EndLine, window.TotalLines, window.NextStartLine)
	}

	var wantContent, wantView strings.Builder
	for number := 1200; number <= 1299; number++ {
		fmt.Fprintf(&wantContent, "line %04d of the fixture\n", number)
		fmt.Fprintf(&wantView, "%6d\tline %04d of the fixture\n", number, number)
	}
	if window.Content != wantContent.String() {
		t.Errorf("content is not lines 1200-1299 as they are in the file:\n%s", window.Content)
	}

	header, body, _ := strings.Cut(view.Text, "\n")
	if body != wantView.String() {
		t.Errorf("the numbered view is not lines 1200-1299 numbered as cat -n numbers them:\n%s", body)
	}
	for _, want := range []string{"src/long.txt: text, 3000 lines.", "Lines 1200-1299 follow", "pass start_line=1300"} {
		if !strings.Contains(header, want) {
			t.Errorf("header %q does not say %q", header, want)
		}
	}
}

func TestAWindowSaysWhenItHoldsTheWholeFileOrItsEnd(t *testing.T) {
	t.Parallel()

	whole, err := Read(t.Context(), Request{Path: "a.txt"}, []byte("one\ntwo\nthree\n"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if want := "a.txt: text, 3 lines. Lines 1-3 follow: the whole file.\n     1\tone\n     2\ttwo\n     3\tthree\n"; whole.Text != want {
		t.Errorf("whole file:\n got %q\nwant %q", whole.Text, want)
	}
	if whole.Window.Content != "one\ntwo\nthree\n" || whole.Window.NextStartLine != 0 {
		t.Errorf("a window over the whole file is %q with next %d, want the file and no next", whole.Window.Content, whole.Window.NextStartLine)
	}

	end, err := Read(t.Context(), Request{Path: "a.txt", At: "refs/heads/main", StartLine: 2}, []byte("one\ntwo\nthree\n"))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if header, _, _ := strings.Cut(end.Text, "\n"); header != "a.txt at refs/heads/main: text, 3 lines. Lines 2-3 follow: the end of the file." {
		t.Errorf("end of file header = %q", header)
	}
}

// TestLinesAreCountedAsCatCountsThem pins what a line is: text after the last
// newline is a line, a final newline does not start another, and a carriage
// return is left out of the numbered view but kept in the content.
func TestLinesAreCountedAsCatCountsThem(t *testing.T) {
	t.Parallel()

	cases := []struct {
		text  string
		total int
		view  string
	}{
		{text: "", total: 0, view: ""},
		{text: "\n", total: 1, view: "     1\t\n"},
		{text: "a", total: 1, view: "     1\ta\n"},
		{text: "a\n", total: 1, view: "     1\ta\n"},
		{text: "a\nb", total: 2, view: "     1\ta\n     2\tb\n"},
		{text: "a\r\nb\r\n", total: 2, view: "     1\ta\n     2\tb\n"},
		{text: "\n\n\n", total: 3, view: "     1\t\n     2\t\n     3\t\n"},
	}

	for _, testCase := range cases {
		cut, err := cutWindow(testCase.text, 0, 0)
		if err != nil {
			t.Errorf("%q: %v", testCase.text, err)

			continue
		}
		if cut.total != testCase.total || cut.view != testCase.view || cut.content != testCase.text {
			t.Errorf("%q: %d lines, view %q, content %q; want %d lines, view %q, content the text",
				testCase.text, cut.total, cut.view, cut.content, testCase.total, testCase.view)
		}
	}
}

func TestAWindowDefaultsItsStartAndLengthAndHoldsNoMoreThanTheMaximum(t *testing.T) {
	t.Parallel()

	text := strings.Repeat("x\n", MaxLineCount+500)

	defaulted, err := cutWindow(text, 0, 0)
	if err != nil {
		t.Fatalf("cutWindow: %v", err)
	}
	if defaulted.start != 1 || defaulted.end != DefaultLineCount || defaulted.next != DefaultLineCount+1 {
		t.Errorf("an unspecified window is lines %d-%d, next %d; want 1-%d", defaulted.start, defaulted.end, defaulted.next, DefaultLineCount)
	}

	capped, err := cutWindow(text, 1, MaxLineCount*10)
	if err != nil {
		t.Fatalf("cutWindow: %v", err)
	}
	if capped.end != MaxLineCount || capped.next != MaxLineCount+1 {
		t.Errorf("a window asked for %d lines holds %d, next %d; want the %d a window holds at most", MaxLineCount*10, capped.end, capped.next, MaxLineCount)
	}
}

func TestAWindowThatStartsPastTheEndIsRefused(t *testing.T) {
	t.Parallel()

	_, err := Read(t.Context(), Request{Path: "a.txt", StartLine: 4}, []byte("one\ntwo\nthree\n"))
	if err == nil || !strings.Contains(err.Error(), "start_line 4 is past the end of a.txt, which has 3 lines") {
		t.Fatalf("a window past the end: %v, want it refused with the file's length", err)
	}

	empty, err := Read(t.Context(), Request{Path: "empty.txt"}, nil)
	if err != nil {
		t.Fatalf("the first window of an empty file was refused: %v", err)
	}
	window := empty.Window
	if window == nil || window.StartLine != 1 || window.EndLine != 0 || window.TotalLines != 0 || window.Content != "" {
		t.Fatalf("empty file window = %+v, want lines 1-0 of 0", window)
	}
	if empty.Text != "empty.txt: an empty text file.\n" {
		t.Errorf("empty file text = %q", empty.Text)
	}

	for _, request := range []Request{{Path: "a.txt", StartLine: -1}, {Path: "a.txt", LineCount: -5}} {
		if _, err := Read(t.Context(), request, []byte("one\n")); err == nil {
			t.Errorf("%+v was accepted", request)
		}
	}
}

// TestAWindowStopsAtTheByteCeiling is what keeps a file of long lines from
// filling a context: the line count is not reached, and the next window starts
// where this one stopped.
func TestAWindowStopsAtTheByteCeiling(t *testing.T) {
	t.Parallel()

	line := strings.Repeat("w", 100)
	text := strings.Repeat(line+"\n", 1000)

	view, err := Read(t.Context(), Request{Path: "wide.txt", LineCount: 1000}, []byte(text))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	// Each line costs its number, a tab, its text and a newline.
	perLine := len(fmt.Sprintf("%6d\t", 1)) + len(line) + 1
	fits := WindowBytes / perLine
	window := view.Window
	if window.EndLine != fits || window.NextStartLine != fits+1 {
		t.Fatalf("window holds lines 1-%d, next %d; want 1-%d, the lines that fit in %d bytes", window.EndLine, window.NextStartLine, fits, WindowBytes)
	}

	header, body, _ := strings.Cut(view.Text, "\n")
	if len(body) > WindowBytes {
		t.Errorf("the numbered view is %d bytes, over the %d a window holds", len(body), WindowBytes)
	}
	if !strings.Contains(header, "as many as fit in 32.0 KiB") || !strings.Contains(header, fmt.Sprintf("pass start_line=%d", fits+1)) {
		t.Errorf("header %q does not say the window stopped at its size", header)
	}
}

// TestALineLongerThanAWindowIsCutToFit covers the one enormous line: minified
// code, a data blob. It is cut at a character boundary, and the window after
// it starts on the next line, so paging always advances.
func TestALineLongerThanAWindowIsCutToFit(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("é", 60_000) // two bytes each: 120,000 bytes
	text := "first\n" + long + "\nthird\n"

	view, err := Read(t.Context(), Request{Path: "app.min.js", StartLine: 2}, []byte(text))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	window := view.Window
	if window.StartLine != 2 || window.EndLine != 2 || window.NextStartLine != 3 {
		t.Fatalf("window = lines %d-%d, next %d; want line 2 alone, next 3", window.StartLine, window.EndLine, window.NextStartLine)
	}
	header, body, _ := strings.Cut(view.Text, "\n")
	if len(body) > WindowBytes {
		t.Errorf("the cut line's view is %d bytes, over the %d a window holds", len(body), WindowBytes)
	}
	if !utf8.ValidString(body) || !utf8.ValidString(window.Content) {
		t.Error("the line was cut inside a character")
	}
	if !strings.HasPrefix(long, window.Content) || len(window.Content) < WindowBytes-16 {
		t.Errorf("content is %d bytes that do not start the line, want nearly %d of it", len(window.Content), WindowBytes)
	}
	if !strings.Contains(header, "Line 2 follows, cut to its first") || !strings.Contains(header, "of 117.2 KiB") ||
		!strings.Contains(header, "pass start_line=3") {
		t.Errorf("header %q does not say line 2 was cut and where the next starts", header)
	}

	last, err := Read(t.Context(), Request{Path: "blob.txt"}, []byte(long))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if last.Window.NextStartLine != 0 || !strings.Contains(last.Text, "it is the last line of the file, and the rest of it is not returned") {
		t.Errorf("a cut last line: next %d, text %.200q", last.Window.NextStartLine, last.Text)
	}
}

func TestTextThatIsNotUTF8IsDecodedWhenItCanBe(t *testing.T) {
	t.Parallel()

	utf16le := []byte{0xFF, 0xFE, 'h', 0, 'i', 0, '\n', 0, 0xE9, 0}
	utf16be := []byte{0xFE, 0xFF, 0, 'h', 0, 'i', 0, '\n', 0, 0xE9}
	windows := []byte("caf\xe9 \x93quoted\x94 \x80 5\r\n")

	cases := []struct {
		name     string
		content  []byte
		mimeType string
		text     string
		note     string
	}{
		{name: "UTF-16 little-endian", content: utf16le, mimeType: "text/plain; charset=utf-16le", text: "hi\né", note: "decoded from UTF-16"},
		{name: "UTF-16 big-endian", content: utf16be, mimeType: "text/plain; charset=utf-16be", text: "hi\né", note: "decoded from UTF-16"},
		{name: "Windows-1252", content: windows, mimeType: "text/plain; charset=windows-1252", text: "café “quoted” € 5\r\n", note: "decoded from Windows-1252"},
	}

	for _, testCase := range cases {
		view, err := Read(t.Context(), Request{Path: "legacy.txt"}, testCase.content)
		if err != nil {
			t.Fatalf("%s: %v", testCase.name, err)
		}
		if view.Kind != KindText || view.MIMEType != testCase.mimeType || view.Window.Content != testCase.text {
			t.Errorf("%s: %s %s %q, want text %s %q", testCase.name, view.Kind, view.MIMEType, view.Window.Content, testCase.mimeType, testCase.text)
		}
		if !strings.Contains(view.Text, testCase.note) {
			t.Errorf("%s: the header does not say it was %s: %q", testCase.name, testCase.note, view.Text)
		}
	}
}

func TestUTF8TextIsTextWhateverItsFirstBytesLookLike(t *testing.T) {
	t.Parallel()

	for _, content := range []string{
		"BM is also how a bitmap starts\n",
		"ID3 tags are an MP3's\n",
		"a bell \a in the text, which only a NUL would make binary\n",
		string(rune(0xFEFF)) + "a byte order mark\n",
	} {
		view, err := Read(t.Context(), Request{Path: "notes.txt"}, []byte(content))
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if view.Kind != KindText || !strings.HasPrefix(view.MIMEType, "text/") || view.Window.Content != content {
			t.Errorf("%q came back as %s %s", content, view.Kind, view.MIMEType)
		}
	}
}
