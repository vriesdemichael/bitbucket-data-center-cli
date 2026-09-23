package fileview

import (
	"archive/zip"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// officeFamily is a kind of Office Open XML file this package reads the text
// of, recognised by where its main part lives.
type officeFamily struct {
	// directory holds the main part: word/, ppt/ or xl/.
	directory string
	// name is what the file is called in a sentence.
	name     string
	mimeType string
	// layout says how the text is laid out, for the header.
	layout  string
	extract func(document *officeDocument, mainPart string) error
}

var officeFamilies = []officeFamily{
	{
		directory: "word/",
		name:      "a Word document",
		mimeType:  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		layout:    "Each paragraph is a line, and each table row a line with its cells separated by tabs.",
		extract:   (*officeDocument).word,
	},
	{
		directory: "ppt/",
		name:      "a PowerPoint presentation",
		mimeType:  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		layout:    `The slides are in the order they are shown, each headed "Slide N" and followed by its speaker notes.`,
		extract:   (*officeDocument).powerPoint,
	},
	{
		directory: "xl/",
		name:      "an Excel workbook",
		mimeType:  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		layout: "The sheets are in the workbook's order, each headed with its name; each row is a line with its cells " +
			"separated by tabs, and a formula gives the value it last calculated.",
		extract: (*officeDocument).excel,
	},
}

// officeDocument is a Word, PowerPoint or Excel file open for its text.
type officeDocument struct {
	files map[string]*zip.File
	// budget is how much more XML may be read, decompressed.
	budget int64
	text   strings.Builder
	// full is set when the text reached DocumentTextBytes, and cut when the
	// XML ran past documentXMLBytes: either way, the rest is not included.
	full, cut bool
}

// errOutOfXML stops a read that has used up the XML budget.
var errOutOfXML = errors.New("the document holds more XML than is read")

// openOffice recognises an Office Open XML file among zip archives: by the
// main part its package relationships name, or failing those, by where each
// family keeps it.
func openOffice(archive *zip.Reader) (*officeDocument, officeFamily, string, bool) {
	document := &officeDocument{files: make(map[string]*zip.File, len(archive.File)), budget: documentXMLBytes}
	for _, file := range archive.File {
		document.files[file.Name] = file
	}

	mainPart := ""
	for _, link := range document.relationships("") {
		if strings.HasSuffix(link.kind, "/officeDocument") {
			mainPart = link.target
		}
	}
	if document.files[mainPart] == nil {
		mainPart = ""
		for _, candidate := range []string{"word/document.xml", "ppt/presentation.xml", "xl/workbook.xml"} {
			if document.files[candidate] != nil {
				mainPart = candidate
			}
		}
	}

	for _, family := range officeFamilies {
		if mainPart != "" && strings.HasPrefix(mainPart, family.directory) {
			return document, family, mainPart, true
		}
	}

	return nil, officeFamily{}, "", false
}

// readOffice views the text of an Office Open XML file as a window of lines.
func readOffice(request Request, size int64, document *officeDocument, family officeFamily, mainPart string) (View, error) {
	err := family.extract(document, mainPart)
	if err != nil && !errors.Is(err, errOutOfXML) {
		return describeBinary(subjectOf(request), request.WebURL, binaryType{
			mimeType: family.mimeType,
			name:     family.name,
			reason:   "Its text could not be read out of it (" + err.Error() + "), so it is not shown.",
		}, size), nil
	}

	note := "Formatting, pictures and embedded objects are not included."
	switch {
	case document.full:
		note += fmt.Sprintf(" The text stops at %s; the rest of the document is not included.", formatSize(DocumentTextBytes))
	case document.cut:
		note += fmt.Sprintf(" The text stops after %s of the document's XML; the rest of it is not included.", formatSize(documentXMLBytes))
	}

	return lines(request, KindDocument, family.mimeType, size, linedText{
		what:   "text extracted from " + family.name,
		layout: family.layout,
		noun:   "text",
		empty:  "text extracted from " + family.name + ", which has none.",
		note:   note,
	}, document.text.String())
}

// line adds text to the document's, as a line of its own. It reports false
// once the text is full, to stop the read.
func (document *officeDocument) line(text string) bool {
	if document.full {
		return false
	}
	if document.text.Len()+len(text)+1 > DocumentTextBytes {
		document.full = true

		return false
	}
	document.text.WriteString(text)
	document.text.WriteByte('\n')

	return true
}

// open starts reading a part's XML, drawing on the document's budget.
func (document *officeDocument) open(name string) (*xmlStream, func(), error) {
	file := document.files[name]
	if file == nil {
		return nil, nil, fmt.Errorf("it has no part %s", name)
	}
	reader, err := file.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("part %s: %w", name, err)
	}

	stream := &xmlStream{decoder: xml.NewDecoder(&budgeted{reader: reader, document: document})}

	return stream, func() { _ = reader.Close() }, nil
}

// maxXMLDepth is the deepest a part's elements are read nested. Office nests a
// few dozen levels; the decoder keeps every open element, so a part nested far
// deeper is one built to take the memory of the process reading it.
const maxXMLDepth = 1000

// errTooDeep stops a read of XML nested past maxXMLDepth.
var errTooDeep = errors.New("its XML is nested deeper than any document's")

// xmlStream is a part's XML, read token by token as xml.Decoder reads it, and
// no deeper than maxXMLDepth: the decoder itself has no such bound outside
// Unmarshal.
type xmlStream struct {
	decoder *xml.Decoder
	depth   int
}

// Token is the next token, or errTooDeep past maxXMLDepth.
func (stream *xmlStream) Token() (xml.Token, error) {
	token, err := stream.decoder.Token()
	switch token.(type) {
	case xml.StartElement:
		stream.depth++
		if stream.depth > maxXMLDepth {
			return nil, errTooDeep
		}
	case xml.EndElement:
		stream.depth--
	}

	return token, err
}

// Skip reads past the rest of the element just started, as xml.Decoder's Skip
// does, and within the same bound: its own would read any depth.
func (stream *xmlStream) Skip() error {
	for level := 1; level > 0; {
		token, err := stream.Token()
		if err != nil {
			return err
		}
		switch token.(type) {
		case xml.StartElement:
			level++
		case xml.EndElement:
			level--
		}
	}

	return nil
}

// budgeted reads a part until the document's XML budget runs out.
type budgeted struct {
	reader   io.Reader
	document *officeDocument
}

func (reader *budgeted) Read(buffer []byte) (int, error) {
	if reader.document.budget <= 0 {
		reader.document.cut = true

		return 0, errOutOfXML
	}
	if int64(len(buffer)) > reader.document.budget {
		buffer = buffer[:reader.document.budget]
	}
	count, err := reader.reader.Read(buffer)
	reader.document.budget -= int64(count)

	return count, err
}

// officeLink is one relationship of a part: to another part, by id.
type officeLink struct {
	id, kind string
	// target is the part it names, resolved against the part it is from.
	target string
}

// relationships reads the relationships of part, or of the package when part
// is empty. A part without any has none, which is not an error.
func (document *officeDocument) relationships(part string) []officeLink {
	directory, base := path.Split(part)
	decoder, done, err := document.open(directory + "_rels/" + base + ".rels")
	if err != nil {
		return nil
	}
	defer done()

	var links []officeLink
	for {
		token, err := decoder.Token()
		if err != nil {
			return links
		}
		element, ok := token.(xml.StartElement)
		if !ok || element.Name.Local != "Relationship" || attribute(element, "TargetMode") == "External" {
			continue
		}
		links = append(links, officeLink{
			id:     attribute(element, "Id"),
			kind:   attribute(element, "Type"),
			target: resolvePart(directory, attribute(element, "Target")),
		})
	}
}

// linked finds a relationship by id, or by the end of its type when id is
// empty.
func linked(links []officeLink, id, kind string) (officeLink, bool) {
	for _, link := range links {
		if (id != "" && link.id == id) || (id == "" && strings.HasSuffix(link.kind, kind)) {
			return link, true
		}
	}

	return officeLink{}, false
}

// resolvePart turns a relationship's target into a part name: relative to the
// directory of the part it is from, or to the package root when it starts
// with a slash.
func resolvePart(directory, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(path.Clean(target), "/")
	}

	return strings.TrimPrefix(path.Clean("/"+directory+target), "/")
}

// attribute is an element's attribute by local name, whatever its namespace.
func attribute(element xml.StartElement, name string) string {
	for _, attr := range element.Attr {
		if attr.Name.Local == name {
			return attr.Value
		}
	}

	return ""
}

// relationshipAttribute is the r:id-style attribute that names a relationship:
// the one called id in a namespace, as opposed to a plain id.
func relationshipAttribute(element xml.StartElement) string {
	for _, attr := range element.Attr {
		if attr.Name.Local == "id" && attr.Name.Space != "" {
			return attr.Value
		}
	}

	return ""
}

// word reads a Word document's body, in order.
func (document *officeDocument) word(mainPart string) error {
	decoder, done, err := document.open(mainPart)
	if err != nil {
		return err
	}
	defer done()

	return (&textWalker{emit: document.line}).walk(decoder)
}

// textWalker turns the paragraphs and tables of WordprocessingML or DrawingML
// into lines: a paragraph to a line, and a table row to a line with its cells
// separated by tabs. The two share the element names this reads -- p, r, t,
// tbl, tr, tc -- so one walker serves a Word document, a slide and its notes.
type textWalker struct {
	emit   func(string) bool
	frames []*textFrame
	// runs is how many runs the walk is inside: a tab or a break counts only
	// there, and not among a paragraph's properties.
	runs   int
	inText bool
	done   bool

	// bodyOnly keeps only the text of shapes that are a body placeholder: the
	// notes on a notes page, and not the slide number beside them.
	bodyOnly    bool
	inShape     bool
	shapeIsBody bool
	shapeLines  []string
}

type frameKind int

const (
	paragraphFrame frameKind = iota
	rowFrame
	cellFrame
)

type textFrame struct {
	kind  frameKind
	text  strings.Builder
	cells []string
	// after holds the paragraphs inside this one -- a text box anchored in
	// it -- which follow it once it ends.
	after []string
}

func (walker *textWalker) walk(decoder *xmlStream) error {
	for !walker.done {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		switch element := token.(type) {
		case xml.StartElement:
			if err := walker.start(decoder, element); err != nil {
				return err
			}
		case xml.EndElement:
			walker.end(element.Name.Local)
		case xml.CharData:
			if walker.inText {
				walker.write(string(element))
			}
		}
	}

	return nil
}

func (walker *textWalker) start(decoder *xmlStream, element xml.StartElement) error {
	switch element.Name.Local {
	case "Fallback":
		// Markup compatibility: the same content again for an application
		// that cannot read the choice before it -- a text box, twice.
		return decoder.Skip()
	case "instrText", "delText", "delInstrText":
		// A field's code, and text a tracked change deleted.
		return decoder.Skip()
	case "p":
		walker.frames = append(walker.frames, &textFrame{kind: paragraphFrame})
	case "tr":
		walker.frames = append(walker.frames, &textFrame{kind: rowFrame})
	case "tc":
		walker.frames = append(walker.frames, &textFrame{kind: cellFrame})
	case "r":
		walker.runs++
	case "t":
		walker.inText = true
	case "tab", "ptab":
		if walker.runs > 0 {
			walker.write("\t")
		}
	case "br", "cr":
		walker.write("\n")
	case "noBreakHyphen":
		walker.write("-")
	case "sp":
		walker.inShape, walker.shapeIsBody, walker.shapeLines = true, false, nil
	case "ph":
		if walker.inShape && attribute(element, "type") == "body" {
			walker.shapeIsBody = true
		}
	}

	return nil
}

func (walker *textWalker) end(name string) {
	switch name {
	case "t":
		walker.inText = false
	case "r":
		walker.runs--
	case "p", "tc", "tr":
		walker.close()
	case "sp":
		if walker.bodyOnly && walker.shapeIsBody {
			for _, line := range walker.shapeLines {
				if !walker.emit(line) {
					walker.done = true

					break
				}
			}
		}
		walker.inShape, walker.shapeLines = false, nil
	}
}

// write adds text to the paragraph it is in.
func (walker *textWalker) write(text string) {
	for index := len(walker.frames) - 1; index >= 0; index-- {
		if walker.frames[index].kind == paragraphFrame {
			walker.frames[index].text.WriteString(text)

			return
		}
	}
}

// close ends the innermost paragraph, cell or row, and hands on what it held.
func (walker *textWalker) close() {
	if len(walker.frames) == 0 {
		return
	}
	frame := walker.frames[len(walker.frames)-1]
	walker.frames = walker.frames[:len(walker.frames)-1]

	switch frame.kind {
	case paragraphFrame:
		walker.deliver(frame.text.String())
		for _, nested := range frame.after {
			walker.deliver(nested)
		}
	case cellFrame:
		for index := len(walker.frames) - 1; index >= 0; index-- {
			if walker.frames[index].kind == rowFrame {
				walker.frames[index].cells = append(walker.frames[index].cells, frame.text.String())

				return
			}
		}
		walker.deliver(frame.text.String())
	case rowFrame:
		walker.deliver(strings.Join(frame.cells, "\t"))
	}
}

// deliver hands on a finished paragraph or row: into the table cell it sits
// in, on one line with the cell's other paragraphs; after the paragraph it
// sits in, when that is a text box's anchor; or out as a line of its own.
func (walker *textWalker) deliver(text string) {
	for index := len(walker.frames) - 1; index >= 0; index-- {
		switch frame := walker.frames[index]; frame.kind {
		case paragraphFrame:
			frame.after = append(frame.after, text)

			return
		case cellFrame:
			if frame.text.Len() > 0 {
				frame.text.WriteByte(' ')
			}
			frame.text.WriteString(strings.NewReplacer("\t", " ", "\n", " ").Replace(text))

			return
		}
	}

	walker.emitLine(text)
}

func (walker *textWalker) emitLine(text string) {
	if walker.bodyOnly {
		if walker.inShape {
			walker.shapeLines = append(walker.shapeLines, text)
		}

		return
	}
	if !walker.emit(text) {
		walker.done = true
	}
}
