package main

import (
	"strings"
)

// bbInvocation is a single documented `bb ...` command, with enough location to
// point a reader at it.
type bbInvocation struct {
	File string
	Line int
	Raw  string
	Args []string
	// FlagsToVerify are flags whose value was a placeholder, so the value was
	// dropped and only the flag's existence can be checked.
	FlagsToVerify []string
}

// extractBBInvocations pulls every `bb ...` command out of one shell snippet.
//
// The snippet is documentation, not a script to run: nothing here executes, and
// the goal is to recover the argument vector a reader would end up passing.
//
// Line numbers are relative to the snippet; the caller offsets them to the file.
func extractBBInvocations(snippet string) []bbInvocation {
	var found []bbInvocation

	for _, line := range joinContinuations(strings.Split(snippet, "\n")) {
		for _, segment := range splitShellSegments(line.text) {
			args, ok := parseBBSegment(segment)
			if !ok {
				continue
			}

			found = append(found, bbInvocation{
				Line: line.number,
				Raw:  strings.TrimSpace(segment),
				Args: args,
			})
		}
	}

	return found
}

type numberedLine struct {
	number int
	text   string
}

// joinContinuations folds backslash-continued lines into one logical line,
// keeping the line number of where the command started.
func joinContinuations(lines []string) []numberedLine {
	var joined []numberedLine

	for index := 0; index < len(lines); index++ {
		text := lines[index]
		startedAt := index

		for strings.HasSuffix(strings.TrimRight(text, " \t"), "\\") && index+1 < len(lines) {
			trimmed := strings.TrimRight(text, " \t")
			text = strings.TrimSuffix(trimmed, "\\") + " " + strings.TrimSpace(lines[index+1])
			index++
		}

		joined = append(joined, numberedLine{number: startedAt, text: text})
	}

	return joined
}

// splitShellSegments breaks a line on the operators that start a new command, so
// `printf ... | bb auth login ...` and `bb x && bb y` both yield their bb parts.
//
// Operators inside quotes are ignored; a documented message containing a pipe
// must not be mistaken for a pipeline.
func splitShellSegments(line string) []string {
	var (
		segments []string
		current  strings.Builder
		quote    rune
		escaped  bool
	)

	runes := []rune(line)
	for index := 0; index < len(runes); index++ {
		char := runes[index]

		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}

		switch {
		case char == '\\' && quote != '\'':
			current.WriteRune(char)
			escaped = true
			continue
		case quote != 0:
			if char == quote {
				quote = 0
			}
			current.WriteRune(char)
			continue
		case char == '\'' || char == '"':
			quote = char
			current.WriteRune(char)
			continue
		case char == '#' && (current.Len() == 0 || runes[index-1] == ' ' || runes[index-1] == '\t'):
			// If preceded by whitespace, a hash followed by a digit (e.g. #42) is a PR/issue identifier argument rather than a comment.
			if current.Len() > 0 && index+1 < len(runes) && runes[index+1] >= '0' && runes[index+1] <= '9' {
				current.WriteRune(char)
				continue
			}
			// A comment runs to end of line.
			index = len(runes)
			continue
		case char == '|' || char == ';' || char == '&':
			segments = append(segments, current.String())
			current.Reset()
			// Skip the second character of || and &&.
			if index+1 < len(runes) && runes[index+1] == char {
				index++
			}
			continue
		}

		current.WriteRune(char)
	}

	segments = append(segments, current.String())

	return segments
}

// parseBBSegment returns the argument vector for a segment that invokes bb.
//
// Leading environment assignments and `$` prompts are stripped, because both
// appear routinely in documentation.
func parseBBSegment(segment string) ([]string, bool) {
	fields, ok := splitFields(segment)
	if !ok || len(fields) == 0 {
		return nil, false
	}

	for len(fields) > 0 {
		head := fields[0]

		switch {
		case head == "$" || head == ">":
			fields = fields[1:]
		case head == "sudo":
			fields = fields[1:]
		case isEnvironmentAssignment(head):
			fields = fields[1:]
		default:
			if head != "bb" && !strings.HasSuffix(head, "/bb") && head != "bb.exe" {
				return nil, false
			}
			rawArgs := fields[1:]
			var cleanArgs []string
			for i := 0; i < len(rawArgs); i++ {
				arg := rawArgs[i]
				if isShellRedirection(arg) {
					if arg == ">" || arg == ">>" || arg == "<" || arg == "2>" || arg == "2>>" || arg == "&>" {
						i++
					}
					continue
				}
				cleanArgs = append(cleanArgs, arg)
			}
			return cleanArgs, true
		}
	}

	return nil, false
}

func isShellRedirection(arg string) bool {
	// A placeholder like <foo> or <commit-sha> or [arg] is not redirection.
	if strings.HasPrefix(arg, "<") && strings.HasSuffix(arg, ">") {
		return false
	}
	if arg == ">" || arg == ">>" || arg == "<" || arg == "2>" || arg == "2>>" || arg == "&>" {
		return true
	}
	if strings.HasPrefix(arg, ">") || strings.HasPrefix(arg, "2>") {
		return true
	}
	return false
}

func isEnvironmentAssignment(field string) bool {
	index := strings.Index(field, "=")
	if index <= 0 {
		return false
	}

	for position, char := range field[:index] {
		isUpper := char >= 'A' && char <= 'Z'
		isLower := char >= 'a' && char <= 'z'
		isDigit := char >= '0' && char <= '9'

		if !isUpper && !isLower && char != '_' && !(isDigit && position > 0) {
			return false
		}
	}

	return true
}

// splitFields tokenises a segment the way a shell would, honouring quotes.
//
// Reports false for input with an unterminated quote: that is a snippet this
// linter cannot reason about, and guessing would produce a misleading error.
func splitFields(segment string) ([]string, bool) {
	var (
		fields  []string
		current strings.Builder
		inField bool
		quote   rune
		escaped bool
	)

	flush := func() {
		if inField {
			fields = append(fields, current.String())
			current.Reset()
			inField = false
		}
	}

	for _, char := range segment {
		if escaped {
			current.WriteRune(char)
			escaped = false
			continue
		}

		switch {
		case char == '\\' && quote != '\'':
			inField = true
			escaped = true
		case quote != 0:
			if char == quote {
				quote = 0
			} else {
				current.WriteRune(char)
			}
			inField = true
		case char == '\'' || char == '"':
			quote = char
			inField = true
		case char == ' ' || char == '\t':
			flush()
		default:
			current.WriteRune(char)
			inField = true
		}
	}

	if quote != 0 {
		return nil, false
	}

	flush()

	return fields, true
}

// inlineIgnoreDirective suppresses inline checking for the line that follows.
//
// It exists for documentation that names a command on purpose that does not
// exist: server-side-hooks.md documents the removed `bb hook list` (ADR-051).
const inlineIgnoreDirective = "docs-lint: ignore-inline"

// placeholderMarkers are the ways documentation stands in for a value the
// reader substitutes.
var placeholderMarkers = []string{"...", "…", "<", "{", "*", "["}

// extractInlineBBInvocations pulls every `bb ...` written in an inline code
// span, outside any fenced block.
//
// Only fenced blocks were checked, so a command in backticks was invisible to
// the linter. That is not a rare style here: cheatsheet.md is entirely markdown
// tables -- 50 inline spans, no fenced blocks -- so none of the page was
// checked, and invocations in the tree had drifted.
//
// Fenced blocks are skipped rather than re-read: they are already validated,
// with their own language rules and expect-invalid handling.
func extractInlineBBInvocations(contents string) []bbInvocation {
	var found []bbInvocation

	inFence := false
	fence := ""
	ignoreNext := false

	for index, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)

		if inFence {
			if strings.HasPrefix(trimmed, fence) {
				inFence = false
			}
			continue
		}
		if marker := fenceMarker(trimmed); marker != "" {
			inFence = true
			fence = marker
			continue
		}

		if strings.Contains(line, inlineIgnoreDirective) {
			ignoreNext = true
			continue
		}
		// Suppression runs to the next blank line rather than one line, so a
		// table of commands that deliberately do not exist takes one directive
		// instead of one per row -- and an HTML comment between table rows does
		// not render.
		if ignoreNext {
			if trimmed == "" {
				ignoreNext = false
			}
			continue
		}

		for _, span := range inlineCodeSpans(line) {
			args, ok := parseBBSegment(span)
			if !ok {
				continue
			}
			cleaned, flagsToVerify, usable := resolvePlaceholders(args)
			if !usable {
				continue
			}
			found = append(found, bbInvocation{Line: index, Raw: span, Args: cleaned, FlagsToVerify: flagsToVerify})
		}
	}

	return found
}

// resolvePlaceholders substitutes placeholder values and reports whether the
// span is checkable at all.
//
// Where a placeholder stands is what decides. Following a flag it is a value
// the reader supplies, so `bb auth login --host ...` still says something
// checkable -- whether --host exists on that command, which it does not. In
// subcommand position it means the span is a shape rather than an invocation,
// and `bb project permissions users …` is documentation doing its job.
// The placeholder-valued flag is dropped rather than given a made-up value: a
// substitute has to satisfy the flag's type, and `--expiry-days <N>` would
// otherwise be reported as an invalid integer, which is the documentation being
// blamed for the linter's stand-in. The flag name is returned instead, so the
// caller can check it exists without pretending to know its value.
func resolvePlaceholders(args []string) (cleaned []string, flagsToVerify []string, usable bool) {
	cleaned = make([]string, 0, len(args))

	for index, arg := range args {
		if !isPlaceholder(arg) {
			cleaned = append(cleaned, arg)
			continue
		}

		if index == 0 || !strings.HasPrefix(args[index-1], "-") {
			return nil, nil, false
		}

		// Drop the flag that precedes it along with the value.
		flagsToVerify = append(flagsToVerify, cleaned[len(cleaned)-1])
		cleaned = cleaned[:len(cleaned)-1]
	}

	return cleaned, flagsToVerify, true
}

// isPlaceholder reports whether a token stands in for something.
func isPlaceholder(arg string) bool {
	for _, marker := range placeholderMarkers {
		if strings.Contains(arg, marker) {
			return true
		}
	}
	return false
}

// inlineCodeSpans returns the backtick-delimited spans in a line.
func inlineCodeSpans(line string) []string {
	var spans []string

	rest := line
	for {
		open := strings.Index(rest, "`")
		if open < 0 {
			return spans
		}
		rest = rest[open+1:]

		closing := strings.Index(rest, "`")
		if closing < 0 {
			return spans
		}
		spans = append(spans, strings.TrimSpace(rest[:closing]))
		rest = rest[closing+1:]
	}
}

// fenceMarker returns the fence a line opens, or "".
func fenceMarker(trimmed string) string {
	for _, marker := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, marker) {
			return marker
		}
	}
	return ""
}
