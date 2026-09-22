package usage

import (
	"reflect"
	"testing"
)

// TestNameReadsThePlaceholderTheVocabularyIsKeyedBy covers the two spellings
// of a repeated argument.
//
// The marker sits outside the bracket in <commit>... and inside it in
// [alias...], and the completion vocabulary is keyed by the name alone. Taking
// the dots off only one of them leaves "alias..." as a name no table knows,
// which reads as an argument nobody declared rather than as a parsing bug.
func TestNameReadsThePlaceholderTheVocabularyIsKeyedBy(t *testing.T) {
	t.Parallel()

	for placeholder, want := range map[string]string{
		"<pr-id>":                            "pr-id",
		"[directory]":                        "directory",
		"<commit>...":                        "commit",
		"[alias...]":                         "alias",
		"[<number> | <path> | <commit-sha>]": "<number> | <path> | <commit-sha>",
		"<-- <gitflags>...>":                 "-- <gitflags>",
		"plain":                              "plain",
	} {
		if got := Name(placeholder); got != want {
			t.Errorf("Name(%q) = %q, want %q", placeholder, got, want)
		}
	}
}

// TestVariadicFindsTheMarkerOnEitherSideOfTheBracket is the same distinction,
// for the question of whether the last declared kind keeps applying.
func TestVariadicFindsTheMarkerOnEitherSideOfTheBracket(t *testing.T) {
	t.Parallel()

	for placeholder, want := range map[string]bool{
		"<commit>...": true,
		"[alias...]":  true,
		"<commit>":    false,
		"[directory]": false,
	} {
		if got := Variadic(placeholder); got != want {
			t.Errorf("Variadic(%q) = %v, want %v", placeholder, got, want)
		}
	}
}

// TestPlaceholdersReadsTheCommandsOwnSignature covers what the Use line
// declares, including the bracketed alternation that is one argument.
func TestPlaceholdersReadsTheCommandsOwnSignature(t *testing.T) {
	t.Parallel()

	for use, want := range map[string][]string{
		"merge <pr-id>":                                     {"<pr-id>"},
		"compare <from> <to>":                               {"<from>", "<to>"},
		"browse [<number> | <path> | <commit-sha>]":         {"[<number> | <path> | <commit-sha>]"},
		"clone <repository> [directory] [-- <gitflags>...]": {"<repository>", "[directory]", "[-- <gitflags>...]"},
		"status": nil,
	} {
		if got := Placeholders(use); !reflect.DeepEqual(got, want) {
			t.Errorf("Placeholders(%q) = %q, want %q", use, got, want)
		}
	}
}
