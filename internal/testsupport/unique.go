package testsupport

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// uniqueAlphabet is base32 without padding, lowercased: digits and letters
// only, which Bitbucket identifiers accept -- repository slugs, branch and tag
// names, build keys, label names. A project key is stored upper-cased, so a
// test naming a project upper-cases the result.
var uniqueAlphabet = base32.StdEncoding.WithPadding(base32.NoPadding)

// UniqueSuffix returns a random identifier for a test fixture's name.
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
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		// crypto/rand does not fail on any platform this runs on, and a test
		// helper has nowhere to report an error to. Panicking is honest: a
		// fixture name that is not unique produces failures that look like
		// product bugs, which is what this exists to prevent.
		panic("testsupport: no randomness available for a unique fixture name: " + err.Error())
	}

	return strings.ToLower(uniqueAlphabet.EncodeToString(raw))
}

// UniqueName joins a readable prefix to a unique suffix.
//
// The prefix is what a person reads when a fixture is left behind on the
// server and somebody has to work out which test made it.
func UniqueName(prefix string) string {
	return prefix + UniqueSuffix()
}
