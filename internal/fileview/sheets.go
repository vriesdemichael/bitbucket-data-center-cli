package fileview

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

// excel reads a workbook's sheets in the workbook's order, each under its
// name: a row to a line, its cells separated by tabs and in their columns.
//
// A cell's value is what Excel last showed for it. A shared string is looked
// up, a formula gives the value it last calculated -- Excel stores it beside
// the formula -- and a number formatted as a date or a time is written as one,
// since the number Excel stores for a date means nothing to a reader.
func (document *officeDocument) excel(mainPart string) error {
	sheets, date1904, err := document.workbook(mainPart)
	if err != nil {
		return err
	}
	links := document.relationships(mainPart)

	var shared []string
	if link, ok := linked(links, "", "/sharedStrings"); ok {
		if shared, err = document.sharedStrings(link.target); err != nil {
			return err
		}
	}
	var styles []dateKind
	if link, ok := linked(links, "", "/styles"); ok {
		if styles, err = document.dateStyles(link.target); err != nil {
			return err
		}
	}

	for index, sheet := range sheets {
		if index > 0 && !document.line("") {
			return nil
		}
		heading := fmt.Sprintf("Sheet %d: %s", index+1, sheet.name)
		if sheet.hidden {
			heading += " (hidden)"
		}
		if !document.line(heading) {
			return nil
		}

		link, ok := linked(links, sheet.id, "")
		switch {
		case !ok:
			continue
		case strings.HasSuffix(link.kind, "/chartsheet"):
			if !document.line("(a chart, which has no cells)") {
				return nil
			}

			continue
		}
		if err := document.sheet(link.target, shared, styles, date1904); err != nil {
			return err
		}
		if document.full {
			return nil
		}
	}

	return nil
}

type workbookSheet struct {
	name, id string
	hidden   bool
}

// workbook reads a workbook's sheet list, in order, and which date system its
// numbers count from.
func (document *officeDocument) workbook(part string) ([]workbookSheet, bool, error) {
	decoder, done, err := document.open(part)
	if err != nil {
		return nil, false, err
	}
	defer done()

	var sheets []workbookSheet
	date1904 := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return sheets, date1904, nil
		}
		if err != nil {
			return sheets, date1904, err
		}

		element, ok := token.(xml.StartElement)
		switch {
		case !ok:
		case element.Name.Local == "workbookPr":
			value := attribute(element, "date1904")
			date1904 = value == "1" || value == "true"
		case element.Name.Local == "sheet":
			state := attribute(element, "state")
			sheets = append(sheets, workbookSheet{
				name:   attribute(element, "name"),
				id:     relationshipAttribute(element),
				hidden: state == "hidden" || state == "veryHidden",
			})
		}
	}
}

// sharedStrings reads a workbook's shared string table: every string a cell
// of type s points at by its index. A string in several runs is their text
// joined; a phonetic reading of one is not part of it.
func (document *officeDocument) sharedStrings(part string) ([]string, error) {
	decoder, done, err := document.open(part)
	if err != nil {
		return nil, err
	}
	defer done()

	var table []string
	var current strings.Builder
	inItem, inText := false, false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return table, nil
		}
		if err != nil {
			return table, err
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "si":
				inItem = true
				current.Reset()
			case "t":
				inText = inItem
			case "rPh":
				if err := decoder.Skip(); err != nil {
					return table, err
				}
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "t":
				inText = false
			case "si":
				table = append(table, current.String())
				inItem = false
			}
		case xml.CharData:
			if inText {
				current.Write(element)
			}
		}
	}
}

// dateKind is what a number format shows a number as.
type dateKind int

const (
	notADate dateKind = iota
	dateOnly
	timeOnly
	dateAndTime
)

// dateStyles reads which of a workbook's cell styles show a number as a date
// or a time, by the index a cell's s attribute gives.
func (document *officeDocument) dateStyles(part string) ([]dateKind, error) {
	decoder, done, err := document.open(part)
	if err != nil {
		return nil, err
	}
	defer done()

	formats := map[int]string{}
	var kinds []dateKind
	inCellFormats := false
	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return kinds, nil
		}
		if err != nil {
			return kinds, err
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "numFmt":
				if id, err := strconv.Atoi(attribute(element, "numFmtId")); err == nil {
					formats[id] = attribute(element, "formatCode")
				}
			case "cellXfs":
				inCellFormats = true
			case "xf":
				if inCellFormats {
					id, _ := strconv.Atoi(attribute(element, "numFmtId"))
					kinds = append(kinds, formatKind(id, formats))
				}
			}
		case xml.EndElement:
			if element.Name.Local == "cellXfs" {
				inCellFormats = false
			}
		}
	}
}

// formatKind is what number format id shows: one the workbook defines, or one
// of the formats Excel builds in, whose ids for dates and times are fixed.
func formatKind(id int, formats map[int]string) dateKind {
	if code, ok := formats[id]; ok {
		return codeKind(code)
	}

	switch {
	case id >= 14 && id <= 17, id >= 27 && id <= 36, id >= 50 && id <= 58:
		return dateOnly
	case id >= 18 && id <= 21, id >= 45 && id <= 47:
		return timeOnly
	case id == 22:
		return dateAndTime
	}

	return notADate
}

// codeKind reads a format code for date and time parts: y, m and d are a date,
// h and s a time. What is not a part is left out first -- a literal in quotes,
// an escaped character, a colour or a locale in brackets -- and only the first
// section counts, the one for a positive number.
func codeKind(code string) dateKind {
	var parts strings.Builder
	elapsed := false
	for index := 0; index < len(code); index++ {
		switch code[index] {
		case '"':
			end := strings.IndexByte(code[index+1:], '"')
			if end < 0 {
				index = len(code)
			} else {
				index += end + 1
			}
		case '\\', '_', '*':
			index++
		case '[':
			end := strings.IndexByte(code[index+1:], ']')
			if end < 0 {
				index = len(code)

				continue
			}
			switch strings.ToLower(code[index+1 : index+1+end]) {
			case "h", "hh", "m", "mm", "s", "ss":
				elapsed = true
			}
			index += end + 1
		case ';':
			index = len(code)
		default:
			parts.WriteByte(code[index] | 0x20)
		}
	}

	shown := parts.String()
	hasTime := elapsed || strings.ContainsAny(shown, "hs")
	hasDate := strings.ContainsAny(shown, "yd") || (strings.Contains(shown, "m") && !hasTime)

	switch {
	case hasDate && hasTime:
		return dateAndTime
	case hasDate:
		return dateOnly
	case hasTime:
		return timeOnly
	}

	return notADate
}

// excelDate writes the number Excel stores for a date or a time as one. The
// number counts days, from the end of 1899 or, in the 1904 system, from the
// start of 1904; the fraction is the time of day. Excel counts a 29 February
// 1900 that never was, so dates from March 1900 are a day further on.
func excelDate(value string, kind dateKind, date1904 bool) string {
	serial, err := strconv.ParseFloat(value, 64)
	if err != nil || serial < 0 || serial >= 2958466 { // 31 December 9999
		return value
	}

	seconds := int64(math.Round(serial * 86400))
	if kind == timeOnly {
		// A time with no date can run past a day, as an elapsed time does.
		return fmt.Sprintf("%02d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
	}

	base := time.Date(1899, time.December, 30, 0, 0, 0, 0, time.UTC)
	switch {
	case date1904:
		base = time.Date(1904, time.January, 1, 0, 0, 0, 0, time.UTC)
	case serial < 61:
		base = time.Date(1899, time.December, 31, 0, 0, 0, 0, time.UTC)
	}
	moment := base.Add(time.Duration(seconds) * time.Second)

	if kind == dateOnly {
		return moment.Format("2006-01-02")
	}

	return moment.Format("2006-01-02 15:04:05")
}

// sheet reads one worksheet's rows. A row without a value in it is left out,
// and a line in brackets says which rows were, so a row can still be found by
// its number; a cell is put in its column, so a row's values line up with the
// row above.
func (document *officeDocument) sheet(part string, shared []string, styles []dateKind, date1904 bool) error {
	decoder, done, err := document.open(part)
	if err != nil {
		return err
	}
	defer done()

	var (
		written, number, next int
		row                   []string
		cell                  sheetCell
	)
	flatten := strings.NewReplacer("\t", " ", "\r\n", " ", "\n", " ", "\r", " ")

	for {
		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "row":
				number++
				if given, err := strconv.Atoi(attribute(element, "r")); err == nil && given > 0 {
					number = given
				}
				row, next = row[:0], 0
			case "c":
				cell = sheetCell{column: next, kind: attribute(element, "t")}
				if column := columnIndex(attribute(element, "r")); column >= 0 {
					cell.column = column
				}
				cell.style, _ = strconv.Atoi(attribute(element, "s"))
			case "v":
				cell.inValue = true
			case "is":
				cell.inInline = true
			case "t":
				cell.inText = cell.inInline
			case "f", "rPh":
				// A formula's text, and a phonetic reading: the value is
				// what is shown.
				if err := decoder.Skip(); err != nil {
					return err
				}
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "v":
				cell.inValue = false
			case "t":
				cell.inText = false
			case "is":
				cell.inInline = false
			case "c":
				if value := flatten.Replace(cell.display(shared, styles, date1904)); value != "" {
					for len(row) <= cell.column {
						row = append(row, "")
					}
					row[cell.column] = value
				}
				next = cell.column + 1
			case "row":
				if len(row) == 0 {
					continue
				}
				if skipped := skippedRows(written, number); skipped != "" && !document.line(skipped) {
					return nil
				}
				if !document.line(strings.Join(row, "\t")) {
					return nil
				}
				written = number
			}
		case xml.CharData:
			switch {
			case cell.inValue:
				cell.value.Write(element)
			case cell.inText:
				cell.inline.Write(element)
			}
		}
	}
}

// skippedRows is the line that says which rows between the last one written
// and number were empty, or nothing when none were.
func skippedRows(written, number int) string {
	switch {
	case number <= written+1:
		return ""
	case number == written+2:
		return fmt.Sprintf("(row %d is empty)", written+1)
	default:
		return fmt.Sprintf("(rows %d-%d are empty)", written+1, number-1)
	}
}

// sheetCell is one cell as it is read.
type sheetCell struct {
	column                    int
	kind                      string
	style                     int
	value, inline             strings.Builder
	inValue, inInline, inText bool
}

// display is what Excel shows in a cell.
func (cell *sheetCell) display(shared []string, styles []dateKind, date1904 bool) string {
	value := cell.value.String()
	switch cell.kind {
	case "s":
		if index, err := strconv.Atoi(value); err == nil && index >= 0 && index < len(shared) {
			return shared[index]
		}

		return ""
	case "inlineStr":
		return cell.inline.String()
	case "b":
		if value == "1" {
			return "TRUE"
		}
		if value == "0" {
			return "FALSE"
		}

		return value
	case "e", "str", "d":
		return value
	}

	if value != "" && cell.style >= 0 && cell.style < len(styles) && styles[cell.style] != notADate {
		return excelDate(value, styles[cell.style], date1904)
	}

	return value
}

// maxColumns is how many columns a sheet can have: XFD is the last.
const maxColumns = 16384

// columnIndex reads the column out of a cell reference, B3 being column 1,
// or -1 when the reference does not give one a sheet can have.
func columnIndex(reference string) int {
	column := 0
	for index := 0; index < len(reference); index++ {
		letter := reference[index] | 0x20
		if letter < 'a' || letter > 'z' {
			break
		}
		column = column*26 + int(letter-'a') + 1
		if column > maxColumns {
			return -1
		}
	}

	return column - 1
}
