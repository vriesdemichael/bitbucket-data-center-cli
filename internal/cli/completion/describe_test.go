package completion

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestADescriptionIsCutBetweenCharacters covers the description of a pull
// request whose title is prose rather than ASCII.
//
// Cutting a fixed number of bytes can land inside a character, which hands the
// shell a byte sequence that is not text -- and a title long enough to be cut
// is exactly the one worth showing.
func TestADescriptionIsCutBetweenCharacters(t *testing.T) {
	t.Parallel()

	cut := describe(strings.Repeat("é", 200))

	if !utf8.ValidString(cut) {
		t.Fatalf("the cut description is not valid text: %q", cut)
	}
	if utf8.RuneCountInString(cut) > maxDescription {
		t.Errorf("expected at most %d characters, got %d", maxDescription, utf8.RuneCountInString(cut))
	}
	if !strings.HasSuffix(cut, "…") {
		t.Errorf("expected the cut to be marked, got %q", cut)
	}
}
