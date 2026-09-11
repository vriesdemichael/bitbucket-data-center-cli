package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// cappedResultSet matches a command that asks a service for a bounded number of
// results, which is what makes its answer potentially incomplete.
var cappedResultSet = regexp.MustCompile(`\w+\.ServiceLimit\(\)`)

// reportsTruncation matches the command saying whether it hit that bound.
var reportsTruncation = regexp.MustCompile(`paging\.LimitReached\(`)

// notAListing is the way out for a command that bounds a read in order to
// find something rather than to return a page. It has to carry a reason,
// because the whole failure mode here was a field quietly missing.
var notAListing = regexp.MustCompile(`limit-not-reported:\s*\S+`)

// TestACappedListingSaysSoIsEnforced is #573.
//
// 24 of 37 commands that accept --limit emitted no meta.limitReached, so a
// pipeline that asked for 25 repositories and received 25 could not tell
// whether that was all of them. The envelope was there; the field was absent,
// which reads as "not truncated" rather than "not answered".
//
// Per RunE block rather than per file: a file can hold a listing that reports
// and another that does not, which is exactly how the gap grew.
func TestACappedListingSaysSoIsEnforced(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "internal", "cli", "cmd")

	var silent []string
	var capped int

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		lines := strings.Split(string(contents), "\n")
		for index, line := range lines {
			if !strings.Contains(line, "RunE: func(") {
				continue
			}

			indent := len(line) - len(strings.TrimLeft(line, "\t"))
			end := len(lines)
			for scan := index + 1; scan < len(lines); scan++ {
				trimmed := strings.TrimSpace(lines[scan])
				if strings.HasPrefix(trimmed, "},") && len(lines[scan])-len(strings.TrimLeft(lines[scan], "\t")) == indent {
					end = scan
					break
				}
			}

			block := strings.Join(lines[index:end], "\n")
			if !cappedResultSet.MatchString(block) {
				continue
			}
			// A command that writes no JSON has no envelope to carry the field.
			if !strings.Contains(block, "WriteJSON") {
				continue
			}

			if notAListing.MatchString(block) {
				continue
			}

			capped++
			if !reportsTruncation.MatchString(block) {
				silent = append(silent, filepath.ToSlash(path)+":"+strconv.Itoa(index+1))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the command tree: %v", err)
	}

	// A walk that stopped matching would report perfect compliance, which is
	// the failure mode ADR-067 exists to catch.
	if capped < 20 {
		t.Fatalf("found only %d capped listings, expected dozens.\nThe walk is probably broken, not the commands.", capped)
	}

	if len(silent) > 0 {
		sort.Strings(silent)
		t.Fatalf(
			"%d of %d capped listings do not report meta.limitReached:\n  %s\n\n"+
				"Write the payload with WriteJSONList and paging.LimitReached(options, len(items)).\n"+
				"An absent field reads as \"not truncated\", so a caller cannot tell a full page\n"+
				"from all there is.",
			len(silent), capped, strings.Join(silent, "\n  "),
		)
	}
}
