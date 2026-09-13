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

// limitRegistration matches a command being given --limit, through either of
// the paging package's registrations. The paging options are named for what
// they page -- listPaging, commentPaging, statusPaging -- so the receiver is
// recognised by its suffix.
var limitRegistration = regexp.MustCompile(`\b(\w+[Pp]aging)\.(?:Register|RegisterPersistent)\((\w+),`)

// jsonWrite matches a command writing its --json document.
var jsonWrite = regexp.MustCompile(`\b(?:d|deps)\.(WriteJSONList|WriteJSON)\(`)

// reportsTruncation matches the command working out whether it hit that limit.
var reportsTruncation = regexp.MustCompile(`paging\.LimitReached\(`)

// hintsInText matches the command telling a person reading text output that it
// stopped at the limit.
var hintsInText = regexp.MustCompile(`paging\.Hint\(`)

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
// Every JSON write is checked, not the command as a whole. pr comment list
// writes its document from two places, threaded and under --full, and a version
// of this test that passed once a command reported anywhere would have let one
// of them stay silent. The version before that looked only at commands calling
// ServiceLimit() and never saw pr comment list at all.
//
// A registration is matched to the command declared nearest before it. A file
// can declare several commands under the same variable name; taking the first
// one attributes the limit to the wrong command.
func TestACappedListingSaysSoIsEnforced(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "internal", "cli", "cmd")

	var silent []string
	var registrations, writes int

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
		at := func(line int, what string) string {
			return filepath.ToSlash(path) + ":" + strconv.Itoa(line+1) + " " + what
		}

		for _, match := range limitRegistration.FindAllStringSubmatchIndex(text, -1) {
			registrations++
			command := text[match[4]:match[5]]
			registeredAt := strings.Count(text[:match[0]], "\n")

			start, end, found := runEFor(lines, command, registeredAt)
			if !found {
				silent = append(silent, at(registeredAt, "("+command+"): no RunE found"))
				continue
			}

			block := strings.Join(lines[start:end], "\n")
			if notAListing.MatchString(block) {
				continue
			}

			sites := 0
			for index := start; index < end; index++ {
				write := jsonWrite.FindStringSubmatch(lines[index])
				if write == nil {
					continue
				}
				sites++
				writes++

				call := callFrom(lines, index)
				switch {
				case strings.Contains(call, "dryrunpreview."):
					// A preview answers what would change, not a page of results.
				case write[1] == "WriteJSON":
					silent = append(silent, at(index, "("+command+"): WriteJSON carries no meta.limitReached"))
				case !reportsTruncation.MatchString(call) && !computedInBlock(block, call):
					silent = append(silent, at(index, "("+command+"): WriteJSONList not given paging.LimitReached"))
				}
			}

			if sites == 0 {
				// A document written through a helper is invisible here, which is
				// exactly how a silent listing would hide.
				silent = append(silent, at(start, "("+command+"): no JSON write in its RunE to check"))
			}

			// A person reading 25 rows has the same problem a pipeline reading
			// 25 results had, so the text output owes them the same answer.
			if !hintsInText.MatchString(block) {
				silent = append(silent, at(start, "("+command+"): text output never says it stopped at --limit; call paging.Hint"))
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walk the command tree: %v", err)
	}

	// A walk that stopped matching would report perfect compliance, which is the
	// failure mode ADR-067 exists to catch.
	if registrations < 25 || writes < 25 {
		t.Fatalf("found only %d --limit registrations and %d JSON writes under them, expected dozens of each.\nThe walk is probably broken, not the commands.", registrations, writes)
	}

	if len(silent) > 0 {
		sort.Strings(silent)
		t.Fatalf(
			"%d JSON write(s) under a command that takes --limit do not report meta.limitReached:\n  %s\n\n"+
				"Write the payload with WriteJSONList and paging.LimitReached(options, len(items)).\n"+
				"An absent field reads as \"not truncated\", so a caller cannot tell a full page\n"+
				"from all there is. A command that takes --limit to find one thing rather than to\n"+
				"return a page says so with a limit-not-reported: comment giving the reason.",
			len(silent), strings.Join(silent, "\n  "),
		)
	}
}

// runEFor returns the line range of the RunE body of the command declared
// under name, nearest before the line that registered its limit.
func runEFor(lines []string, name string, before int) (int, int, bool) {
	declaration := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\s*:?=\s*&cobra\.Command\{`)

	declared := -1
	for index := before; index >= 0; index-- {
		if declaration.MatchString(lines[index]) {
			declared = index
			break
		}
	}
	if declared == -1 {
		return 0, 0, false
	}

	for index := declared; index < before; index++ {
		if !strings.Contains(lines[index], "RunE: func(") {
			continue
		}

		indent := len(lines[index]) - len(strings.TrimLeft(lines[index], "\t"))
		for end := index + 1; end < len(lines); end++ {
			trimmed := strings.TrimSpace(lines[end])
			if strings.HasPrefix(trimmed, "},") && len(lines[end])-len(strings.TrimLeft(lines[end], "\t")) == indent {
				return index, end, true
			}
		}

		return index, len(lines), true
	}

	return 0, 0, false
}

// lastArgument matches a call ending in a plain variable.
var lastArgument = regexp.MustCompile(`,\s*(\w+)\s*\)\s*$`)

// computedInBlock reports whether a write's truncation argument is a variable
// the block assigns from paging.LimitReached. pr status pages two listings and
// reports either one reaching the limit, so it works that out before writing.
func computedInBlock(block, call string) bool {
	variable := lastArgument.FindStringSubmatch(call)
	if variable == nil {
		return false
	}

	assigned := regexp.MustCompile(`\b` + regexp.QuoteMeta(variable[1]) + `\s*:?=\s*paging\.LimitReached\(`)

	return assigned.MatchString(block)
}

// callFrom returns a call's source, from the line it starts on until its
// parentheses balance.
func callFrom(lines []string, start int) string {
	var call strings.Builder
	depth := 0
	for index := start; index < len(lines); index++ {
		call.WriteString(lines[index])
		call.WriteString("\n")
		depth += strings.Count(lines[index], "(") - strings.Count(lines[index], ")")
		if depth <= 0 {
			break
		}
	}

	return call.String()
}
