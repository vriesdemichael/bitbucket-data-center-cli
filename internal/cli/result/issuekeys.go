package result

import (
	"regexp"
	"slices"
)

// issueKeyPattern is the shape of an issue key: a project key of capital
// letters and digits, a hyphen, a number.
var issueKeyPattern = regexp.MustCompile(`[A-Z][A-Z0-9]+-\d+`)

// issueKeys are the issue keys the texts mention, in the order they appear and
// each once, or nil when there are none.
//
// Matched by shape alone, which is what linking a pull request to its issue by
// naming convention is. It also matches what merely looks like one -- UTF-8 in
// a title -- and is not checked against any tracker.
func issueKeys(texts ...string) []string {
	var keys []string
	for _, text := range texts {
		for _, span := range issueKeyPattern.FindAllStringIndex(text, -1) {
			// A key stands on its own: not the tail of a longer word, nor the
			// head of one. An underscore does set it apart, as in a branch
			// named PAY-12_refunds, which a word boundary would not allow.
			if (span[0] > 0 && alphanumeric(text[span[0]-1])) || (span[1] < len(text) && alphanumeric(text[span[1]])) {
				continue
			}
			if key := text[span[0]:span[1]]; !slices.Contains(keys, key) {
				keys = append(keys, key)
			}
		}
	}

	return keys
}

func alphanumeric(character byte) bool {
	return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9')
}
