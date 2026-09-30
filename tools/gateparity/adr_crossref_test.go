package gateparity

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/adr"
)

// TestEveryADRMentionHasARecord fails when anything in the repository names a
// decision record that does not exist.
//
// A record that no longer holds is deleted rather than kept as a tombstone, so
// deleting one leaves a gap in the numbers, and every mention of the gap has to
// go with it: a comment, a doc page, a test, an error message or another record
// that cites it would otherwise send a reader to nothing. The same check
// catches a number mistyped, or a link to a record by a slug it no longer has.
//
// Both forms a reader follows are checked: ADR-NNN (or ADR NNN) in text, and a
// link to adr/NNN-slug.md.
func TestEveryADRMentionHasARecord(t *testing.T) {
	t.Parallel()

	root := repositoryRoot(t)
	records, err := adr.Load(filepath.Join(root, adr.Directory))
	if err != nil {
		t.Fatalf("load the records: %v", err)
	}

	mentions := adrMentionsIn(t, root)
	if len(mentions) < 200 {
		t.Fatalf("found %d mentions of a record; the walk has stopped reaching the tree", len(mentions))
	}

	for _, dangling := range unresolvedMentions(records, mentions) {
		t.Errorf("%s:%d names %s, and there is no such record.\n"+
			"A record that was deleted takes its mentions with it; a number or slug that is wrong is fixed.",
			dangling.file, dangling.line, dangling.form)
	}
}

// TestADRMentionCheckCatchesAGap is the sabotage, kept as a test: a mention of
// a number no record has, and a link by a slug no record has, are both caught.
func TestADRMentionCheckCatchesAGap(t *testing.T) {
	t.Parallel()

	records := []adr.Record{{Path: "docs/site/adr/001-present.md", Number: 1}}
	mentions := []adrMention{
		{file: "a.go", line: 1, number: 1, form: "ADR-001"},
		{file: "b.md", line: 2, number: 99, form: "ADR-099"},
		{file: "c.md", line: 3, number: 1, slug: "001-renamed.md", form: "adr/001-renamed.md"},
		{file: "d.md", line: 4, number: 1, slug: "001-present.md", form: "adr/001-present.md"},
	}

	dangling := unresolvedMentions(records, mentions)
	if len(dangling) != 2 || dangling[0].form != "ADR-099" || dangling[1].form != "adr/001-renamed.md" {
		t.Errorf("caught %+v, want the missing number and the wrong slug", dangling)
	}
}

type adrMention struct {
	file   string
	line   int
	number int
	// slug is the file a link names, empty for a mention by number.
	slug string
	form string
}

func unresolvedMentions(records []adr.Record, mentions []adrMention) []adrMention {
	numbers, files := map[int]bool{}, map[string]bool{}
	for _, record := range records {
		numbers[record.Number] = true
		files[filepath.Base(record.Path)] = true
	}

	var dangling []adrMention
	for _, mention := range mentions {
		if !numbers[mention.number] || (mention.slug != "" && !files[mention.slug]) {
			dangling = append(dangling, mention)
		}
	}

	return dangling
}

var (
	adrByNumber = regexp.MustCompile(`\bADR[- ]([0-9]{3})\b`)
	adrByLink   = regexp.MustCompile(`adr/(([0-9]{3})-[a-z0-9-]+\.md)`)
)

// adrSkippedDirectories hold nothing a reader follows: git's own data, scratch
// output, other checkouts, and the vendored Atlassian specification.
var adrSkippedDirectories = map[string]bool{
	".git":         true,
	".tmp":         true,
	".claude":      true,
	"node_modules": true,
	filepath.FromSlash("docs/reference/atlassian"): true,
}

func adrMentionsIn(t *testing.T, root string) []adrMention {
	t.Helper()

	var mentions []adrMention
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(root, path)
		if entry.IsDir() {
			if adrSkippedDirectories[relative] || adrSkippedDirectories[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.IndexByte(content[:min(len(content), 8000)], 0) >= 0 {
			return nil // binary
		}

		// A test holds fixtures that name records which do not exist on
		// purpose, so in a test only its comments are read.
		isTest := strings.HasSuffix(path, "_test.go")
		for index, line := range strings.Split(string(content), "\n") {
			if isTest && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				continue
			}
			for _, match := range adrByNumber.FindAllStringSubmatch(line, -1) {
				number, _ := strconv.Atoi(match[1])
				mentions = append(mentions, adrMention{file: filepath.ToSlash(relative), line: index + 1, number: number, form: match[0]})
			}
			for _, match := range adrByLink.FindAllStringSubmatch(line, -1) {
				number, _ := strconv.Atoi(match[2])
				mentions = append(mentions, adrMention{file: filepath.ToSlash(relative), line: index + 1, number: number, slug: match[1], form: "adr/" + match[1]})
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	sort.Slice(mentions, func(i, j int) bool {
		if mentions[i].file != mentions[j].file {
			return mentions[i].file < mentions[j].file
		}
		return mentions[i].line < mentions[j].line
	})

	return mentions
}
