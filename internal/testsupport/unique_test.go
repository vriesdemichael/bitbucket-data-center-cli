package testsupport

import (
	"regexp"
	"strings"
	"testing"
)

// A suffix goes into slugs, branch names, build keys and label names, so it
// must be characters all of them accept, and two draws must not collide.
func TestUniqueSuffixesAreIdentifiersThatDoNotRepeat(t *testing.T) {
	t.Parallel()

	identifier := regexp.MustCompile(`^[a-z2-7]{8}$`)
	seen := map[string]bool{}
	for range 1000 {
		suffix := UniqueSuffix()
		if !identifier.MatchString(suffix) {
			t.Fatalf("suffix %q is not eight lowercase base32 characters", suffix)
		}
		if seen[suffix] {
			t.Fatalf("suffix %q repeated within 1000 draws", suffix)
		}
		seen[suffix] = true
	}

	if name := UniqueName("LT-"); !strings.HasPrefix(name, "LT-") || !identifier.MatchString(strings.TrimPrefix(name, "LT-")) {
		t.Fatalf("UniqueName(%q) = %q, want the prefix and a suffix", "LT-", name)
	}
}
