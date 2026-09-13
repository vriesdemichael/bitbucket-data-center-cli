package testsupport

import (
	"crypto/rand"
	"strings"
)

// UniqueSuffix returns a random identifier for a test fixture's name.
//
// Lowercase base32: digits and letters only, which Bitbucket identifiers
// accept -- repository slugs, branch and tag names, build keys, label names. A
// project key is stored upper-cased, so a test naming a project upper-cases the
// result.
//
// Not a timestamp (ADR-085). Names built from the clock collide in three ways
// this suite has hit: truncated, they repeat within a run; the clock is coarser
// than the suite is parallel, so two tests read the same value; and a counter
// beside the clock restarts with the process, so a run collides with what a
// crashed run left behind.
//
// 40 bits of randomness: enough that a suite creating thousands of fixtures a
// day will not see a collision, and short enough to read in a failure message.
func UniqueSuffix() string {
	// rand.Text is base32 from crypto/rand and cannot fail, so there is no
	// error path to leave untested. Eight of its characters are 40 bits.
	return strings.ToLower(rand.Text()[:8])
}

// UniqueName joins a readable prefix to a unique suffix.
//
// The prefix is what a person reads when a fixture is left behind on the
// server and somebody has to work out which test made it.
func UniqueName(prefix string) string {
	return prefix + UniqueSuffix()
}
