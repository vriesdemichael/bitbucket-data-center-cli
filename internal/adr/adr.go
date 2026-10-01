// Package adr reads the architecture decision records in docs/site/adr.
//
// A record is a Markdown page, and the page the site renders is the source:
//
//	# ADR-084: A removed flag warns for one major before it stops working
//
//	The rule, in the present tense, then why it holds.
//
//	## Not chosen
//
//	- **An alternative**: why it was not taken.
//
// The number and the title are the only structure. Everything a record decides
// and why is one piece of prose, and the alternatives it turned down are the
// one part kept apart, because they stop a rejected idea coming back, and the
// guards that read a record's rule skip them.
//
// A record states the rule as it holds now, so it carries no status and no
// line naming the records that replaced or changed it: a record that no longer
// holds is changed or deleted, and how it came to be is in git. Parse refuses a
// record that opens with a quoted line, which is where such a line went.
package adr

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Directory is where the records live, relative to the repository root.
const Directory = "docs/site/adr"

// notChosenHeading opens the list of alternatives a record turned down.
const notChosenHeading = "## Not chosen"

// Record is one decision record.
type Record struct {
	// Path is the file the record was read from.
	Path string
	// Number and Title are what the record's heading says.
	Number int
	Title  string
	// Body is the prose after the heading, up to the alternatives.
	Body string
	// NotChosen is the list of alternatives, without its heading.
	NotChosen string
}

var (
	fileName = regexp.MustCompile(`^([0-9]{3})-[a-z0-9]+(?:-[a-z0-9]+)*\.md$`)
	heading  = regexp.MustCompile(`^# ADR-([0-9]{3}): (\S.*)$`)
)

// Load reads every record in the directory, in number order.
func Load(directory string) ([]Record, error) {
	paths, err := filepath.Glob(filepath.Join(directory, "*.md"))
	if err != nil {
		return nil, fmt.Errorf("list decision records: %w", err)
	}

	var records []Record
	seen := map[int]string{}
	for _, path := range paths {
		if filepath.Base(path) == "index.md" {
			continue
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		record, err := Parse(path, content)
		if err != nil {
			return nil, err
		}
		if other, taken := seen[record.Number]; taken {
			return nil, fmt.Errorf("%s and %s both claim ADR-%03d", filepath.Base(other), filepath.Base(path), record.Number)
		}
		seen[record.Number] = path
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("no decision records in %s", directory)
	}

	sort.Slice(records, func(i, j int) bool { return records[i].Number < records[j].Number })

	return records, nil
}

// Parse reads one record, and refuses a file whose name and heading do not
// agree.
func Parse(path string, content []byte) (Record, error) {
	name := filepath.Base(path)
	named := fileName.FindStringSubmatch(name)
	if named == nil {
		return Record{}, fmt.Errorf("%s: a record is named NNN-slug.md", name)
	}

	lines := strings.Split(strings.ReplaceAll(string(content), "\r\n", "\n"), "\n")
	lines = withoutFrontMatter(lines)

	index := 0
	for index < len(lines) && strings.TrimSpace(lines[index]) == "" {
		index++
	}
	if index == len(lines) {
		return Record{}, fmt.Errorf("%s: empty", name)
	}
	titled := heading.FindStringSubmatch(lines[index])
	if titled == nil {
		return Record{}, fmt.Errorf("%s: the first line is %q, and a record opens with # ADR-NNN: Title", name, lines[index])
	}
	if titled[1] != named[1] {
		return Record{}, fmt.Errorf("%s: its heading says ADR-%s", name, titled[1])
	}
	number, _ := strconv.Atoi(titled[1])

	record := Record{Path: path, Number: number, Title: strings.TrimSpace(titled[2])}
	rest := strings.Join(lines[index+1:], "\n")

	body, notChosen, _ := strings.Cut(rest, "\n"+notChosenHeading+"\n")
	record.NotChosen = strings.TrimSpace(notChosen)

	body = strings.TrimSpace(body)
	if strings.HasPrefix(body, ">") {
		return Record{}, fmt.Errorf("%s: opens with a quoted line; a record states the rule as it holds now, "+
			"and which records replaced or changed it is in git", name)
	}
	if body == "" {
		return Record{}, fmt.Errorf("%s: states no rule", name)
	}
	record.Body = body

	return record, nil
}

// withoutFrontMatter drops a YAML front matter block, which carries settings
// for the site rather than anything the record says.
func withoutFrontMatter(lines []string) []string {
	if len(lines) == 0 || lines[0] != "---" {
		return lines
	}
	for index := 1; index < len(lines); index++ {
		if lines[index] == "---" {
			return lines[index+1:]
		}
	}

	return lines
}
