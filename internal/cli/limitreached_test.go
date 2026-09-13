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

// limitRegistration matches a command being given --limit. The paging options
// are named for what they page -- listPaging, commentPaging, statusPaging -- so
// the receiver is recognised by its suffix.
var limitRegistration = regexp.MustCompile(`\b(\w+[Pp]aging)\.Register\((\w+),`)

// reportsTruncation matches the command saying whether it hit that limit.
var reportsTruncation = regexp.MustCompile(`paging\.LimitReached\(`)

// notAListing is the way out for a command that takes --limit in order to find
// something rather than to return a page. It has to carry a reason, because the
// whole failure mode here was a field quietly missing.
var notAListing = regexp.MustCompile(`limit-not-reported:\s*\S+`)

// TestACappedListingSaysSoIsEnforced is #573.
//
// Commands that accept --limit emitted no meta.limitReached, so a pipeline that
// asked for 25 repositories and received 25 could not tell whether that was all
// of them. An absent field reads as "not truncated" rather than "not answered".
//
// Keyed on the --limit registration, not on how the command caps. An earlier
// version looked only at RunE blocks that called ServiceLimit(), and so never
// saw pr comment list, which caps with Truncate() instead -- and which was
// silent while this test reported compliance.
//
// A registration is matched to the command declared nearest before it. A file
// can declare several commands under the same variable name; taking the first
// one attributes the limit to the wrong command.
func TestACappedListingSaysSoIsEnforced(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "internal", "cli", "cmd")

	var silent []string
	var registrations int

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
		text := string(contents)
		lines := strings.Split(text, "\n")

		for _, match := range limitRegistration.FindAllStringSubmatchIndex(text, -1) {
			registrations++
			command := text[match[4]:match[5]]
			registeredAt := lineOf(text, match[0])

			block, found := runEBlockFor(lines, command, registeredAt)
			if !found {
				silent = append(silent, filepath.ToSlash(path)+":"+strconv.Itoa(registeredAt+1)+" ("+command+": no RunE found)")
				continue
			}
			if notAListing.MatchString(block) || reportsTruncation.MatchString(block) {
				continue
			}
			if !strings.Contains(block, "WriteJSON") {
				// No envelope to carry the field.
				continue
			}

			silent = append(silent, filepath.ToSlash(path)+":"+strconv.Itoa(registeredAt+1)+" ("+command+")")
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the command tree: %v", err)
	}

	// A walk that stopped matching would report perfect compliance, which is the
	// failure mode ADR-067 exists to catch.
	if registrations < 25 {
		t.Fatalf("found only %d --limit registrations, expected dozens.\nThe walk is probably broken, not the commands.", registrations)
	}

	if len(silent) > 0 {
		sort.Strings(silent)
		t.Fatalf(
			"%d of %d commands that take --limit do not report meta.limitReached:\n  %s\n\n"+
				"Write the payload with WriteJSONList and paging.LimitReached(options, len(items)).\n"+
				"An absent field reads as \"not truncated\", so a caller cannot tell a full page\n"+
				"from all there is. A command that takes --limit to find one thing rather than to\n"+
				"return a page says so with a limit-not-reported: comment giving the reason.",
			len(silent), registrations, strings.Join(silent, "\n  "),
		)
	}
}

// runEBlockFor returns the RunE body of the command declared under name,
// nearest before the line that registered its limit.
func runEBlockFor(lines []string, name string, before int) (string, bool) {
	declaration := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*:?=\s*&cobra\.Command\{`)

	declared := -1
	for index := before; index >= 0; index-- {
		if declaration.MatchString(lines[index]) {
			declared = index
			break
		}
	}
	if declared == -1 {
		return "", false
	}

	for index := declared; index < before; index++ {
		if !strings.Contains(lines[index], "RunE: func(") {
			continue
		}

		indent := len(lines[index]) - len(strings.TrimLeft(lines[index], "\t"))
		for end := index + 1; end < len(lines); end++ {
			trimmed := strings.TrimSpace(lines[end])
			if strings.HasPrefix(trimmed, "},") && len(lines[end])-len(strings.TrimLeft(lines[end], "\t")) == indent {
				return strings.Join(lines[index:end], "\n"), true
			}
		}

		return strings.Join(lines[index:], "\n"), true
	}

	return "", false
}

func lineOf(text string, offset int) int {
	return strings.Count(text[:offset], "\n")
}
