// Package filefixture builds the kinds of file get_file_content converts --
// Word, PowerPoint and Excel files, zip and tar archives -- from strings, so the
// converters' unit tests and the live suite work from the same bytes without a
// binary fixture committed beside them.
//
// Each builder writes what the format needs and no more: the parts an Office
// application reads a document by, in a zip. A test that wants a shape the
// helpers do not make passes its own XML for the body.
package filefixture

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/xml"
	"fmt"
	"sort"
	"strings"
)

// Entry is one file in an archive.
type Entry struct {
	Name string
	Body []byte
	// Directory makes the entry a directory; Body is ignored.
	Directory bool
	// Link makes the entry a symbolic link to this target, in a tar.
	Link string
}

// Zip packs entries, in the order given, into a zip archive.
func Zip(entries ...Entry) []byte {
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	for _, entry := range entries {
		name := entry.Name
		if entry.Directory && !strings.HasSuffix(name, "/") {
			name += "/"
		}
		file, err := writer.Create(name)
		if err != nil {
			panic(fmt.Sprintf("zip %s: %v", name, err))
		}
		if !entry.Directory {
			if _, err := file.Write(entry.Body); err != nil {
				panic(fmt.Sprintf("zip %s: %v", name, err))
			}
		}
	}
	if err := writer.Close(); err != nil {
		panic(fmt.Sprintf("close zip: %v", err))
	}

	return archive.Bytes()
}

// Tar packs entries, in the order given, into a tar archive.
func Tar(entries ...Entry) []byte {
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for _, entry := range entries {
		header := &tar.Header{Name: entry.Name, Mode: 0o644, Size: int64(len(entry.Body)), Typeflag: tar.TypeReg}
		switch {
		case entry.Directory:
			header.Typeflag, header.Mode, header.Size = tar.TypeDir, 0o755, 0
		case entry.Link != "":
			header.Typeflag, header.Linkname, header.Size = tar.TypeSymlink, entry.Link, 0
		}
		if err := writer.WriteHeader(header); err != nil {
			panic(fmt.Sprintf("tar %s: %v", entry.Name, err))
		}
		if header.Typeflag == tar.TypeReg {
			if _, err := writer.Write(entry.Body); err != nil {
				panic(fmt.Sprintf("tar %s: %v", entry.Name, err))
			}
		}
	}
	if err := writer.Close(); err != nil {
		panic(fmt.Sprintf("close tar: %v", err))
	}

	return archive.Bytes()
}

// Gzip compresses content as gzip does.
func Gzip(content []byte) []byte {
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if _, err := writer.Write(content); err != nil {
		panic(fmt.Sprintf("gzip: %v", err))
	}
	if err := writer.Close(); err != nil {
		panic(fmt.Sprintf("close gzip: %v", err))
	}

	return compressed.Bytes()
}

// Escape makes text safe to put between XML tags.
func Escape(text string) string {
	var escaped strings.Builder
	_ = xml.EscapeText(&escaped, []byte(text))

	return escaped.String()
}

const (
	relationshipsNS   = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	packageRelsNS     = "http://schemas.openxmlformats.org/package/2006/relationships"
	contentTypesNS    = "http://schemas.openxmlformats.org/package/2006/content-types"
	officeDocumentRel = relationshipsNS + "/officeDocument"
)

// part is one file inside an Office document.
type part struct {
	name, body string
}

// office zips an Office document's parts behind the two every document has:
// the content types, and the package relationship naming the main part.
//
// The parts go in by name, as a tool that zips a directory would put them, so
// the order in the zip is the order of the names: a test showing that a
// document's order is not its file names' shows it is not the zip's either.
func office(mainPart, mainType string, parts ...part) []byte {
	sort.Slice(parts, func(left, right int) bool { return parts[left].name < parts[right].name })

	entries := []Entry{
		{Name: "[Content_Types].xml", Body: []byte(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<Types xmlns="` + contentTypesNS + `">` +
			`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
			`<Default Extension="xml" ContentType="application/xml"/>` +
			`<Override PartName="/` + mainPart + `" ContentType="` + mainType + `"/></Types>`)},
		{Name: "_rels/.rels", Body: []byte(relationships(relationship{id: "rId1", kind: officeDocumentRel, target: mainPart}))},
	}
	for _, part := range parts {
		entries = append(entries, Entry{Name: part.name, Body: []byte(part.body)})
	}

	return Zip(entries...)
}

type relationship struct {
	id, kind, target string
}

func relationships(links ...relationship) string {
	var document strings.Builder
	document.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="` + packageRelsNS + `">`)
	for _, link := range links {
		fmt.Fprintf(&document, `<Relationship Id="%s" Type="%s" Target="%s"/>`, link.id, link.kind, link.target)
	}
	document.WriteString(`</Relationships>`)

	return document.String()
}

// Word namespaces, as Word writes them.
const (
	wordNS   = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	wordMain = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"
)

// Word is a .docx whose body holds the given WordprocessingML: the children of
// w:body, with the w: prefix bound.
func Word(body string) []byte {
	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="` + wordNS + `" xmlns:r="` + relationshipsNS + `"` +
		` xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"` +
		` xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"` +
		` xmlns:v="urn:schemas-microsoft-com:vml">` +
		`<w:body>` + body + `<w:sectPr/></w:body></w:document>`

	return office("word/document.xml", wordMain, part{name: "word/document.xml", body: document})
}

// WordParagraph is a paragraph of one run holding text.
func WordParagraph(text string) string {
	return `<w:p><w:r><w:t xml:space="preserve">` + Escape(text) + `</w:t></w:r></w:p>`
}

// WordTable is a table, a row of cells for each row given.
func WordTable(rows ...[]string) string {
	var table strings.Builder
	table.WriteString(`<w:tbl><w:tblPr/>`)
	for _, row := range rows {
		table.WriteString(`<w:tr>`)
		for _, cell := range row {
			table.WriteString(`<w:tc><w:tcPr/>` + WordParagraph(cell) + `</w:tc>`)
		}
		table.WriteString(`</w:tr>`)
	}
	table.WriteString(`</w:tbl>`)

	return table.String()
}

// PowerPoint namespaces.
const (
	presentationNS   = "http://schemas.openxmlformats.org/presentationml/2006/main"
	drawingNS        = "http://schemas.openxmlformats.org/drawingml/2006/main"
	presentationMain = "application/vnd.openxmlformats-officedocument.presentationml.presentation.main+xml"
	slideRel         = relationshipsNS + "/slide"
	notesSlideRel    = relationshipsNS + "/notesSlide"
)

// Slide is one slide of a presentation.
type Slide struct {
	// Part is the slide's file name under ppt/slides, such as slide2.xml. A
	// presentation's order is its slide list, not these names, and a test
	// that means to show that gives them out of order.
	Part string
	// Texts are the slide's text boxes, one shape each.
	Texts []string
	// Shapes is PresentationML added to the slide's shape tree after them, for
	// a shape the helpers do not make -- a table.
	Shapes string
	// Notes are the speaker notes, a paragraph each; none makes no notes page.
	Notes []string
	// Hidden hides the slide in a slide show.
	Hidden bool
}

// SlideShape is a text box holding text, a paragraph for each line of it.
func SlideShape(text string) string {
	var shape strings.Builder
	shape.WriteString(`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Text"/><p:cNvSpPr/><p:nvPr/></p:nvSpPr><p:spPr/><p:txBody><a:bodyPr/>`)
	for _, line := range strings.Split(text, "\n") {
		shape.WriteString(`<a:p><a:r><a:t>` + Escape(line) + `</a:t></a:r></a:p>`)
	}
	shape.WriteString(`</p:txBody></p:sp>`)

	return shape.String()
}

// PowerPoint is a .pptx of slides in the order given.
func PowerPoint(slides ...Slide) []byte {
	namespaces := ` xmlns:p="` + presentationNS + `" xmlns:a="` + drawingNS + `" xmlns:r="` + relationshipsNS + `"`

	var list strings.Builder
	links := make([]relationship, 0, len(slides))
	parts := make([]part, 0, 2*len(slides)+2)
	for index, slide := range slides {
		id := fmt.Sprintf("rId%d", index+10)
		fmt.Fprintf(&list, `<p:sldId id="%d" r:id="%s"/>`, 256+index, id)
		links = append(links, relationship{id: id, kind: slideRel, target: "slides/" + slide.Part})

		show := ""
		if slide.Hidden {
			show = ` show="0"`
		}
		var shapes strings.Builder
		for _, text := range slide.Texts {
			shapes.WriteString(SlideShape(text))
		}
		shapes.WriteString(slide.Shapes)
		parts = append(parts, part{name: "ppt/slides/" + slide.Part, body: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<p:sld` + namespaces + show + `><p:cSld><p:spTree><p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr>` +
			`<p:grpSpPr/>` + shapes.String() + `</p:spTree></p:cSld></p:sld>`})

		if len(slide.Notes) == 0 {
			continue
		}
		notesPart := "notes" + slide.Part
		parts = append(parts,
			part{name: "ppt/slides/_rels/" + slide.Part + ".rels", body: relationships(relationship{id: "rId1", kind: notesSlideRel, target: "../notesSlides/" + notesPart})},
			part{name: "ppt/notesSlides/" + notesPart, body: notesSlide(namespaces, index+1, slide.Notes)},
		)
	}

	parts = append(parts,
		part{name: "ppt/presentation.xml", body: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<p:presentation` + namespaces + `><p:sldIdLst>` + list.String() + `</p:sldIdLst><p:sldSz cx="9144000" cy="6858000"/></p:presentation>`},
		part{name: "ppt/_rels/presentation.xml.rels", body: relationships(links...)},
	)

	return office("ppt/presentation.xml", presentationMain, parts...)
}

// notesSlide is a notes page as PowerPoint writes one: a picture of the slide,
// the notes in the body placeholder, and the slide number in a placeholder of
// its own -- which is not a note, and which an extractor has to leave out.
func notesSlide(namespaces string, number int, notes []string) string {
	var body strings.Builder
	for _, note := range notes {
		body.WriteString(`<a:p><a:r><a:t>` + Escape(note) + `</a:t></a:r></a:p>`)
	}

	return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><p:notes` + namespaces + `><p:cSld><p:spTree>` +
		`<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr/>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Slide Image"/><p:cNvSpPr/><p:nvPr><p:ph type="sldImg"/></p:nvPr></p:nvSpPr><p:spPr/></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="3" name="Notes"/><p:cNvSpPr/><p:nvPr><p:ph type="body" idx="1"/></p:nvPr></p:nvSpPr><p:spPr/>` +
		`<p:txBody><a:bodyPr/>` + body.String() + `</p:txBody></p:sp>` +
		`<p:sp><p:nvSpPr><p:cNvPr id="4" name="Slide Number"/><p:cNvSpPr/><p:nvPr><p:ph type="sldNum" idx="5"/></p:nvPr></p:nvSpPr><p:spPr/>` +
		fmt.Sprintf(`<p:txBody><a:bodyPr/><a:p><a:fld id="{1}" type="slidenum"><a:t>%d</a:t></a:fld></a:p></p:txBody></p:sp>`, number) +
		`</p:spTree></p:cSld></p:notes>`
}

// Excel namespaces.
const (
	spreadsheetNS    = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	spreadsheetMain  = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"
	worksheetRel     = relationshipsNS + "/worksheet"
	sharedStringsRel = relationshipsNS + "/sharedStrings"
	stylesRel        = relationshipsNS + "/styles"
)

// Sheet is one worksheet of a workbook.
type Sheet struct {
	Name string
	// Part is the sheet's file name under xl/worksheets, such as sheet2.xml.
	// A workbook's order is its sheet list, not these names.
	Part string
	// Rows is the SpreadsheetML inside sheetData: row elements, with the c
	// elements in them.
	Rows   string
	Hidden bool
}

// Workbook is what a workbook holds beside its sheets.
type Workbook struct {
	// SharedStrings is the shared string table: a cell of type s holds an
	// index into it. Each entry is SpreadsheetML inside an si element, which
	// SharedString makes from plain text.
	SharedStrings []string
	// Styles is the cellXfs list: the number format of each style, by the
	// index a cell's s attribute names.
	Styles []int
	// Formats are custom number formats, by id, as numFmt elements declare them.
	Formats map[int]string
	// Date1904 is the older date system Excel for Mac used.
	Date1904 bool
}

// SharedString is a shared string entry holding plain text.
func SharedString(text string) string {
	return `<t xml:space="preserve">` + Escape(text) + `</t>`
}

// Excel is a .xlsx of sheets in the order given.
func Excel(workbook Workbook, sheets ...Sheet) []byte {
	namespaces := ` xmlns="` + spreadsheetNS + `" xmlns:r="` + relationshipsNS + `"`

	var list strings.Builder
	links := make([]relationship, 0, len(sheets)+2)
	parts := make([]part, 0, len(sheets)+4)
	for index, sheet := range sheets {
		id := fmt.Sprintf("rId%d", index+10)
		state := ""
		if sheet.Hidden {
			state = ` state="hidden"`
		}
		fmt.Fprintf(&list, `<sheet name="%s" sheetId="%d"%s r:id="%s"/>`, Escape(sheet.Name), index+1, state, id)
		links = append(links, relationship{id: id, kind: worksheetRel, target: "worksheets/" + sheet.Part})
		parts = append(parts, part{name: "xl/worksheets/" + sheet.Part, body: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<worksheet` + namespaces + `><sheetData>` + sheet.Rows + `</sheetData></worksheet>`})
	}

	if len(workbook.SharedStrings) > 0 {
		var table strings.Builder
		fmt.Fprintf(&table, `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><sst xmlns="%s" count="%d" uniqueCount="%d">`,
			spreadsheetNS, len(workbook.SharedStrings), len(workbook.SharedStrings))
		for _, entry := range workbook.SharedStrings {
			table.WriteString(`<si>` + entry + `</si>`)
		}
		table.WriteString(`</sst>`)
		links = append(links, relationship{id: "rId1", kind: sharedStringsRel, target: "sharedStrings.xml"})
		parts = append(parts, part{name: "xl/sharedStrings.xml", body: table.String()})
	}

	if len(workbook.Styles) > 0 {
		var styles strings.Builder
		styles.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="` + spreadsheetNS + `">`)
		if len(workbook.Formats) > 0 {
			fmt.Fprintf(&styles, `<numFmts count="%d">`, len(workbook.Formats))
			for id, code := range workbook.Formats {
				fmt.Fprintf(&styles, `<numFmt numFmtId="%d" formatCode="%s"/>`, id, Escape(code))
			}
			styles.WriteString(`</numFmts>`)
		}
		fmt.Fprintf(&styles, `<cellXfs count="%d">`, len(workbook.Styles))
		for _, format := range workbook.Styles {
			fmt.Fprintf(&styles, `<xf numFmtId="%d" fontId="0" fillId="0" borderId="0" xfId="0"/>`, format)
		}
		styles.WriteString(`</cellXfs></styleSheet>`)
		links = append(links, relationship{id: "rId2", kind: stylesRel, target: "styles.xml"})
		parts = append(parts, part{name: "xl/styles.xml", body: styles.String()})
	}

	properties := ""
	if workbook.Date1904 {
		properties = `<workbookPr date1904="1"/>`
	}
	parts = append(parts,
		part{name: "xl/workbook.xml", body: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
			`<workbook` + namespaces + `>` + properties + `<sheets>` + list.String() + `</sheets></workbook>`},
		part{name: "xl/_rels/workbook.xml.rels", body: relationships(links...)},
	)

	return office("xl/workbook.xml", spreadsheetMain, parts...)
}
