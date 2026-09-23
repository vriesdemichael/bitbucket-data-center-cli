package fileview

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// powerPoint reads a presentation's slides in the order they are shown. That
// is the order of the presentation's slide list, which the slides' file names
// stop following as soon as a slide is moved: slide3.xml may be the first.
func (document *officeDocument) powerPoint(mainPart string) error {
	ids, err := document.slideList(mainPart)
	if err != nil {
		return err
	}
	links := document.relationships(mainPart)

	for index, id := range ids {
		link, ok := linked(links, id, "")
		if !ok {
			continue
		}
		if index > 0 && !document.line("") {
			return nil
		}
		if err := document.slide(index+1, link.target); err != nil {
			return err
		}
		if document.full {
			return nil
		}
	}

	return nil
}

// slideList reads the relationship ids of a presentation's slides, in order.
func (document *officeDocument) slideList(mainPart string) ([]string, error) {
	decoder, done, err := document.open(mainPart)
	if err != nil {
		return nil, err
	}
	defer done()

	var ids []string
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return ids, nil
		}
		if err != nil {
			return ids, err
		}
		if element, ok := token.(xml.StartElement); ok && element.Name.Local == "sldId" {
			ids = append(ids, relationshipAttribute(element))
		}
	}
}

// slide reads one slide under its heading, and then its speaker notes.
func (document *officeDocument) slide(number int, part string) error {
	decoder, done, err := document.open(part)
	if err != nil {
		return err
	}
	defer done()

	root, err := firstElement(decoder)
	if err != nil {
		return err
	}
	heading := fmt.Sprintf("Slide %d", number)
	if attribute(root, "show") == "0" {
		heading += " (hidden)"
	}
	if !document.line(heading) {
		return nil
	}
	if err := (&textWalker{emit: document.line}).walk(decoder); err != nil {
		return err
	}

	notes, err := document.speakerNotes(part)
	if err != nil || len(notes) == 0 {
		return err
	}
	if !document.line("Speaker notes:") {
		return nil
	}
	for _, line := range notes {
		if !document.line(line) {
			return nil
		}
	}

	return nil
}

// speakerNotes reads the notes on the notes page a slide links to: the text of
// its body placeholder, and not the slide number and picture beside it.
func (document *officeDocument) speakerNotes(slidePart string) ([]string, error) {
	link, ok := linked(document.relationships(slidePart), "", "/notesSlide")
	if !ok {
		return nil, nil
	}
	decoder, done, err := document.open(link.target)
	if err != nil {
		// A notes page the slide names and the file does not have is no
		// notes, not a document that cannot be read.
		return nil, nil
	}
	defer done()

	var notes []string
	walker := &textWalker{bodyOnly: true, emit: func(line string) bool {
		notes = append(notes, line)

		return true
	}}
	if err := walker.walk(decoder); err != nil {
		return nil, err
	}
	if strings.TrimSpace(strings.Join(notes, "")) == "" {
		return nil, nil
	}

	return notes, nil
}

// firstElement reads up to a part's root element.
func firstElement(decoder *xmlStream) (xml.StartElement, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		if element, ok := token.(xml.StartElement); ok {
			return element, nil
		}
	}
}
