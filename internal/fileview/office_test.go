package fileview

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport/filefixture"
)

// readDocument reads content and checks it came back as a document of the
// given type, returning the text extracted from it.
func readDocument(t *testing.T, path string, content []byte, mimeType string) (View, string) {
	t.Helper()

	view, err := Read(Request{Path: path}, content)
	if err != nil {
		t.Fatalf("Read(%s): %v", path, err)
	}
	if view.Kind != KindDocument || view.MIMEType != mimeType || view.Window == nil {
		t.Fatalf("%s came back as %s %s, want a document %s: %q", path, view.Kind, view.MIMEType, mimeType, view.Text)
	}

	return view, view.Window.Content
}

const (
	wordType       = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"
	powerPointType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	excelType      = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
)

func TestAWordDocumentComesBackAsItsParagraphsAndTableRowsInOrder(t *testing.T) {
	t.Parallel()

	content := filefixture.Word(
		filefixture.WordParagraph("Release plan") +
			filefixture.WordParagraph("Who does what:") +
			filefixture.WordTable([]string{"Name", "Role"}, []string{"Ada", "Engineer"}, []string{"Grace", "Admiral"}) +
			filefixture.WordParagraph("") +
			filefixture.WordParagraph("After the table, & escaped <text>."),
	)

	view, text := readDocument(t, "docs/plan.docx", content, wordType)
	if want := "Release plan\nWho does what:\nName\tRole\nAda\tEngineer\nGrace\tAdmiral\n\nAfter the table, & escaped <text>.\n"; text != want {
		t.Errorf("text:\n got %q\nwant %q", text, want)
	}

	header, _, _ := strings.Cut(view.Text, "\n")
	for _, want := range []string{
		"docs/plan.docx: text extracted from a Word document, 7 lines. ",
		"Each paragraph is a line, and each table row a line with its cells separated by tabs. ",
		"Lines 1-7 follow: the whole text.",
		"Formatting, pictures and embedded objects are not included.",
	} {
		if !strings.Contains(header, want) {
			t.Errorf("header does not say %q: %q", want, header)
		}
	}
}

// TestWordRunsBecomeTheTextAReaderSees covers what sits between the text in a
// paragraph: several runs make one line, a tab and a break are what they look
// like, and neither a tab stop, a field's code, deleted text nor a text box's
// fallback copy is text.
func TestWordRunsBecomeTheTextAReaderSees(t *testing.T) {
	t.Parallel()

	body := `<w:p><w:r><w:t xml:space="preserve">Hello </w:t></w:r><w:r><w:t>world</w:t></w:r>` +
		`<w:r><w:tab/><w:t>tabbed</w:t></w:r><w:r><w:br/><w:t>broken</w:t></w:r></w:p>` +
		`<w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="720"/></w:tabs></w:pPr><w:r><w:t>no tab stop</w:t></w:r></w:p>` +
		`<w:p><w:ins><w:r><w:t>kept</w:t></w:r></w:ins><w:del><w:r><w:delText>deleted</w:delText></w:r></w:del></w:p>` +
		`<w:p><w:r><w:t xml:space="preserve">Page </w:t></w:r><w:r><w:fldChar w:fldCharType="begin"/></w:r>` +
		`<w:r><w:instrText> PAGE </w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r>` +
		`<w:r><w:t>7</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>` +
		`<w:p><w:r><w:t>Anchor.</w:t></w:r><w:r><mc:AlternateContent>` +
		`<mc:Choice Requires="wps"><w:drawing><wps:txbx><w:txbxContent><w:p><w:r><w:t>In the box</w:t></w:r></w:p></w:txbxContent></wps:txbx></w:drawing></mc:Choice>` +
		`<mc:Fallback><w:pict><v:textbox><w:txbxContent><w:p><w:r><w:t>In the box</w:t></w:r></w:p></w:txbxContent></v:textbox></w:pict></mc:Fallback>` +
		`</mc:AlternateContent></w:r></w:p>` +
		`<w:tbl><w:tr><w:tc>` + filefixture.WordParagraph("two") + filefixture.WordParagraph("paragraphs") + `</w:tc>` +
		`<w:tc>` + filefixture.WordTable([]string{"nested", "table"}) + `</w:tc></w:tr></w:tbl>`

	_, text := readDocument(t, "notes.docx", filefixture.Word(body), wordType)
	want := "Hello world\ttabbed\nbroken\nno tab stop\nkept\nPage 7\nAnchor.\nIn the box\ntwo paragraphs\tnested table\n"
	if text != want {
		t.Errorf("text:\n got %q\nwant %q", text, want)
	}
}

// TestSlidesComeBackInTheOrderTheyAreShown is the ordering the owner named: a
// presentation's order is its slide list. The parts are named, and zipped, in
// another order, so reading either would put the slides out of order.
func TestSlidesComeBackInTheOrderTheyAreShown(t *testing.T) {
	t.Parallel()

	table := `<p:graphicFrame><a:graphic><a:graphicData><a:tbl>` +
		`<a:tr><a:tc><a:txBody><a:p><a:r><a:t>Q1</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>Q2</a:t></a:r></a:p></a:txBody></a:tc></a:tr>` +
		`<a:tr><a:tc><a:txBody><a:p><a:r><a:t>10</a:t></a:r></a:p></a:txBody></a:tc><a:tc><a:txBody><a:p><a:r><a:t>12</a:t></a:r></a:p></a:txBody></a:tc></a:tr>` +
		`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`

	content := filefixture.PowerPoint(
		filefixture.Slide{Part: "slide3.xml", Texts: []string{"Opening", "Two lines\nin one box"}},
		filefixture.Slide{Part: "slide1.xml", Texts: []string{"Results"}, Shapes: table, Notes: []string{"Mention the dip.", "Then the recovery."}},
		filefixture.Slide{Part: "slide2.xml", Texts: []string{"Backup"}, Hidden: true},
	)

	view, text := readDocument(t, "talk.pptx", content, powerPointType)
	want := "Slide 1\nOpening\nTwo lines\nin one box\n\n" +
		"Slide 2\nResults\nQ1\tQ2\n10\t12\nSpeaker notes:\nMention the dip.\nThen the recovery.\n\n" +
		"Slide 3 (hidden)\nBackup\n"
	if text != want {
		t.Errorf("text:\n got %q\nwant %q", text, want)
	}
	if !strings.Contains(view.Text, `text extracted from a PowerPoint presentation, 15 lines. The slides are in the order they are shown`) {
		t.Errorf("header does not say what the text is: %q", view.Text)
	}
}

// TestSheetsComeBackInWorkbookOrderWithTheValuesExcelShows covers a sheet as
// Excel stores it: strings shared by index, formulas beside the value they
// last calculated, dates as day counts, and rows and cells only where there
// is something in them.
func TestSheetsComeBackInWorkbookOrderWithTheValuesExcelShows(t *testing.T) {
	t.Parallel()

	workbook := filefixture.Workbook{
		SharedStrings: []string{
			filefixture.SharedString("Item"),
			filefixture.SharedString("Cost"),
			`<r><t xml:space="preserve">Rich </t></r><r><rPr><b/></rPr><t>text</t></r><rPh sb="0" eb="1"><t>reading</t></rPh>`,
		},
		Styles:  []int{0, 14, 164, 20},
		Formats: map[int]string{164: "yyyy-mm-dd hh:mm"},
	}
	budget := filefixture.Sheet{Name: "Budget", Part: "sheet2.xml", Rows: `<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>1</v></c></row>` +
		`<row r="2"><c r="A2" t="s"><v>2</v></c><c r="B2"><v>1200.5</v></c></row>` +
		`<row r="3"><c r="A3" s="1"/></row>` +
		`<row r="5"><c r="A5" t="inlineStr"><is><t>Total</t></is></c><c r="B5"><f>SUM(B2:B4)</f><v>1200.5</v></c></row>`}
	flags := filefixture.Sheet{Name: "Flags & dates", Part: "sheet1.xml", Hidden: true, Rows: `<row r="2"><c r="C2" t="b"><v>1</v></c>` +
		`<c r="D2" t="e"><v>#DIV/0!</v></c><c r="E2" t="str"><f>A1&amp;"x"</f><v>joined</v></c></row>` +
		`<row r="3"><c r="A3" s="1"><v>45292</v></c><c r="B3" s="2"><v>45292.5</v></c><c r="C3" s="3"><v>0.75</v></c></row>` +
		`<row r="4"><c r="A4" t="inlineStr"><is><t>two
lines</t></is></c></row>`}

	view, text := readDocument(t, "budget.xlsx", filefixture.Excel(workbook, budget, flags), excelType)
	want := "Sheet 1: Budget\nItem\tCost\nRich text\t1200.5\n(rows 3-4 are empty)\nTotal\t1200.5\n\n" +
		"Sheet 2: Flags & dates (hidden)\n(row 1 is empty)\n\t\tTRUE\t#DIV/0!\tjoined\n2024-01-01\t2024-01-01 12:00:00\t18:00:00\ntwo lines\n"
	if text != want {
		t.Errorf("text:\n got %q\nwant %q", text, want)
	}
	if !strings.Contains(view.Text, "text extracted from an Excel workbook, 11 lines. The sheets are in the workbook's order") {
		t.Errorf("header does not say what the text is: %q", view.Text)
	}

	older, text := readDocument(t, "mac.xlsx", filefixture.Excel(filefixture.Workbook{Styles: []int{0, 14}, Date1904: true},
		filefixture.Sheet{Name: "Only", Part: "sheet1.xml", Rows: `<row r="1"><c r="A1" s="1"><v>0</v></c></row>`}), excelType)
	if text != "Sheet 1: Only\n1904-01-01\n" {
		t.Errorf("a workbook in the 1904 date system: %q (%q)", text, older.Text)
	}
}

func TestANumberFormatSaysWhetherItShowsADate(t *testing.T) {
	t.Parallel()

	cases := map[string]dateKind{
		"yyyy-mm-dd":           dateOnly,
		"d/m/yyyy":             dateOnly,
		"mmm":                  dateOnly,
		"h:mm AM/PM":           timeOnly,
		"mm:ss":                timeOnly,
		"[h]:mm":               timeOnly,
		"d/m/yyyy h:mm":        dateAndTime,
		"General":              notADate,
		"0.00":                 notADate,
		`#,##0 "days"`:         notADate,
		`0.0\d`:                notADate,
		"#,##0.00 [$€-1]":      notADate,
		"[Red]0.00;[Blue]yyyy": notADate,
		"0.00E+00":             notADate,
	}
	for code, want := range cases {
		if got := codeKind(code); got != want {
			t.Errorf("codeKind(%q) = %d, want %d", code, got, want)
		}
	}

	if formatKind(14, nil) != dateOnly || formatKind(22, nil) != dateAndTime || formatKind(20, nil) != timeOnly || formatKind(2, nil) != notADate {
		t.Error("a built-in format id is not read as Excel builds it in")
	}
	if formatKind(14, map[int]string{14: "0.00"}) != notADate {
		t.Error("a format the workbook defines does not win over the built-in one of the same id")
	}
}

func TestExcelDaysAreWrittenAsTheDatesTheyAre(t *testing.T) {
	t.Parallel()

	cases := []struct {
		value    string
		kind     dateKind
		date1904 bool
		want     string
	}{
		{value: "1", kind: dateOnly, want: "1900-01-01"},
		{value: "59", kind: dateOnly, want: "1900-02-28"},
		{value: "61", kind: dateOnly, want: "1900-03-01"},
		{value: "45292", kind: dateOnly, want: "2024-01-01"},
		{value: "45292.75", kind: dateAndTime, want: "2024-01-01 18:00:00"},
		{value: "0", kind: dateOnly, date1904: true, want: "1904-01-01"},
		{value: "0.5", kind: timeOnly, want: "12:00:00"},
		{value: "1.5", kind: timeOnly, want: "36:00:00"},
		{value: "not a number", kind: dateOnly, want: "not a number"},
		{value: "-1", kind: dateOnly, want: "-1"},
	}
	for _, testCase := range cases {
		if got := excelDate(testCase.value, testCase.kind, testCase.date1904); got != testCase.want {
			t.Errorf("excelDate(%q, %d, %v) = %q, want %q", testCase.value, testCase.kind, testCase.date1904, got, testCase.want)
		}
	}
}

func TestColumnIndex(t *testing.T) {
	t.Parallel()

	cases := map[string]int{"A1": 0, "Z9": 25, "AA3": 26, "ab7": 27, "XFD1": 16383, "XFE1": -1, "12": -1, "": -1}
	for reference, want := range cases {
		if got := columnIndex(reference); got != want {
			t.Errorf("columnIndex(%q) = %d, want %d", reference, got, want)
		}
	}
}

func TestADocumentWithNoTextSaysSo(t *testing.T) {
	t.Parallel()

	view, err := Read(Request{Path: "blank.docx"}, filefixture.Word(""))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.Kind != KindDocument || view.Window.TotalLines != 0 ||
		!strings.HasPrefix(view.Text, "blank.docx: text extracted from a Word document, which has none.") {
		t.Errorf("an empty document: %s %d lines, %q", view.Kind, view.Window.TotalLines, view.Text)
	}
}

// TestTheTextOfALargeDocumentStopsAndSaysSo covers both bounds: the text kept,
// and the XML read to find it.
func TestTheTextOfALargeDocumentStopsAndSaysSo(t *testing.T) {
	t.Parallel()

	// Lines of 1024 bytes with their newline, so exactly this many fit.
	fit := DocumentTextBytes / 1024
	paragraph := filefixture.WordParagraph(strings.Repeat("x", 1023))
	content := filefixture.Word(strings.Repeat(paragraph, fit+10))

	view, err := Read(Request{Path: "huge.docx", StartLine: fit - 5}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.Window.TotalLines != fit || view.Window.NextStartLine != 0 {
		t.Errorf("the text holds %d lines, next %d; want the %d that fit in %d bytes", view.Window.TotalLines, view.Window.NextStartLine, fit, DocumentTextBytes)
	}
	if !strings.Contains(view.Text, "The text stops at 8.0 MiB; the rest of the document is not included.") {
		t.Errorf("the header does not say the text stops: %.300q", view.Text)
	}

	archive, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		t.Fatalf("open the zip: %v", err)
	}
	document, family, mainPart, ok := openOffice(archive)
	if !ok {
		t.Fatal("the document was not recognised")
	}
	document.budget = 4096
	cut, err := readOffice(Request{Path: "huge.docx"}, int64(len(content)), document, family, mainPart)
	if err != nil {
		t.Fatalf("readOffice: %v", err)
	}
	if cut.Kind != KindDocument || !strings.Contains(cut.Text, "The text stops after 128.0 MiB of the document's XML") {
		t.Errorf("a document past the XML budget: %s %.300q", cut.Kind, cut.Text)
	}
}

// TestALegacyOfficeFileIsDescribedByItsName: a compound file's first bytes do
// not say which application wrote it, so the extension does.
func TestALegacyOfficeFileIsDescribedByItsName(t *testing.T) {
	t.Parallel()

	content := append([]byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}, make([]byte, 504)...)
	cases := map[string]string{
		"old.doc":  "a Word 97-2003 document (application/msword)",
		"old.XLS":  "an Excel 97-2003 workbook (application/vnd.ms-excel)",
		"old.ppt":  "a PowerPoint 97-2003 presentation (application/vnd.ms-powerpoint)",
		"mail.msg": "an Outlook message (application/vnd.ms-outlook)",
		"thumbs":   "a Microsoft compound file (application/x-ole-storage)",
	}
	for path, want := range cases {
		view, err := Read(Request{Path: path}, content)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		if view.Kind != KindBinary || !strings.Contains(view.Text, want) {
			t.Errorf("%s: %s %q, want it described as %s", path, view.Kind, view.Text, want)
		}
	}
}

// TestXMLNestedPastAnyDocumentIsRefused: the decoder keeps every open element,
// so nesting is a way to make a small file take a large amount of memory. The
// bound is well past any document an Office application writes.
func TestXMLNestedPastAnyDocumentIsRefused(t *testing.T) {
	t.Parallel()

	nested := strings.Repeat("<w:sdt><w:sdtContent>", maxXMLDepth) + filefixture.WordParagraph("deep") +
		strings.Repeat("</w:sdtContent></w:sdt>", maxXMLDepth)
	view, err := Read(Request{Path: "deep.docx"}, filefixture.Word(nested))
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.Kind != KindBinary || !strings.Contains(view.Text, "its XML is nested deeper than any document's") {
		t.Errorf("a document nested %d levels deep: %s %q", 2*maxXMLDepth, view.Kind, view.Text)
	}

	// Deep, but as deep as documents get: read.
	shallow := strings.Repeat("<w:sdt><w:sdtContent>", 40) + filefixture.WordParagraph("deep") + strings.Repeat("</w:sdtContent></w:sdt>", 40)
	if _, text := readDocument(t, "nested.docx", filefixture.Word(shallow), wordType); text != "deep\n" {
		t.Errorf("a document nested 80 levels deep: %q", text)
	}
}

func TestADocumentWhosePartsCannotBeReadIsDescribed(t *testing.T) {
	t.Parallel()

	content := filefixture.Zip(
		filefixture.Entry{Name: "[Content_Types].xml", Body: []byte(`<Types/>`)},
		filefixture.Entry{Name: "word/document.xml", Body: []byte(`<w:document xmlns:w="w"><w:body><w:p>unclosed`)},
	)
	view, err := Read(Request{Path: "broken.docx"}, content)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if view.Kind != KindBinary || view.MIMEType != wordType || !strings.Contains(view.Text, "a Word document") ||
		!strings.Contains(view.Text, "Its text could not be read out of it") {
		t.Errorf("a broken document: %s %s %q", view.Kind, view.MIMEType, view.Text)
	}
}
