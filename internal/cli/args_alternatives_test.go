package cli

import (
	"reflect"
	"testing"
)

// A bracketed group is one placeholder. Split on spaces, browse's
// [<number> | <path> | <commit-sha>] lost the bars and read as three
// arguments rather than one of three.
func TestAPlaceholderWithAlternativesStaysWhole(t *testing.T) {
	t.Parallel()

	for use, want := range map[string][]string{
		"browse [<number> | <path> | <commit-sha>]": {"[<number> | <path> | <commit-sha>]"},
		"compare <from> <to>":                       {"<from>", "<to>"},
		"list <commit> [key]":                       {"<commit>", "[key]"},
		"add <username>... [--flag]":                {"<username>...", "[--flag]"},
		"status":                                    nil,
	} {
		if got := positionalPlaceholders(use); !reflect.DeepEqual(got, want) {
			t.Errorf("positionalPlaceholders(%q) = %q, want %q", use, got, want)
		}
	}
}
