// Package usage reads a command's declared signature out of its Use line.
//
// The Use line is where a command already names its positional arguments: it
// prints in the help text, and internal/cli/args.go builds the message for a
// missing argument from it rather than from a second list that could disagree
// with it. Shell completion needs the same names to know what a slot accepts,
// so it reads them from here instead of keeping a third copy.
package usage

import "strings"

// Placeholders are the <required> and [optional] words in a Use line.
//
// A bracketed group is one placeholder however many spaces it holds. browse
// declares [<number> | <path> | <commit-sha>], one argument that is any of
// three, and splitting it on spaces dropped the bars that say so.
func Placeholders(use string) []string {
	var words []string
	var word strings.Builder
	depth := 0

	for _, character := range use {
		switch {
		case character == ' ' && depth == 0:
			if word.Len() > 0 {
				words = append(words, word.String())
				word.Reset()
			}

			continue
		case character == '[' || character == '<':
			depth++
		case (character == ']' || character == '>') && depth > 0:
			depth--
		}
		word.WriteRune(character)
	}
	if word.Len() > 0 {
		words = append(words, word.String())
	}

	if len(words) <= 1 {
		return nil
	}

	var placeholders []string
	for _, candidate := range words[1:] {
		if strings.HasPrefix(candidate, "<") || strings.HasPrefix(candidate, "[") {
			placeholders = append(placeholders, candidate)
		}
	}

	return placeholders
}

// Variadic reports a placeholder that repeats, as <commit>... and [alias...]
// both do, so the last declared kind keeps applying to every further argument.
func Variadic(placeholder string) bool {
	trimmed := strings.TrimSpace(placeholder)

	return strings.HasSuffix(trimmed, "...") || strings.HasSuffix(unwrap(trimmed), "...")
}

// Name is the placeholder with its brackets and any repetition marker
// removed, which is the form the completion vocabulary is keyed by.
//
// The marker can sit on either side of the bracket -- <commit>... outside,
// [alias...] inside -- so the brackets come off first and the dots after.
func Name(placeholder string) string {
	return strings.TrimSpace(strings.TrimSuffix(unwrap(placeholder), "..."))
}

// unwrap removes the outer brackets, and the repetition marker that sits
// outside them, leaving what the placeholder is called.
func unwrap(placeholder string) string {
	trimmed := strings.TrimSuffix(strings.TrimSpace(placeholder), "...")

	if len(trimmed) >= 2 {
		first, last := trimmed[0], trimmed[len(trimmed)-1]
		if (first == '<' && last == '>') || (first == '[' && last == ']') {
			trimmed = trimmed[1 : len(trimmed)-1]
		}
	}

	return strings.TrimSpace(trimmed)
}
