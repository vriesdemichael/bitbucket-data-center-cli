package testsupport

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
)

// uniqueAlphabet is base32 without padding, lowercased: digits and letters
// only, which every Bitbucket identifier accepts -- project keys, repository
// slugs, branch and tag names, build keys, label names.
var uniqueAlphabet = base32.StdEncoding.WithPadding(base32.NoPadding)

// UniqueSuffix returns a random identifier for a test fixture's name.
//
// Not a timestamp. Names built from the clock collide in three ways this suite
// has actually hit:
//
//   - Truncated. 65 names used UnixNano()%100000, which repeats every 100
//     microseconds while the suite runs eight ways in parallel. Bitbucket
//     answered the resulting duplicate label with 500 "A database error has
//     occurred".
//   - Coarse. time.Now() is not guaranteed finer than a millisecond, and on
//     Windows often is not, so two goroutines can read the same nanosecond
//     value however many digits it has.
//   - Restarted. A process-local counter beside a low-resolution clock repeats
//     from the beginning on the next run, so a run that crashed and left
//     fixtures behind collides with the run that follows it.
//
// 40 bits of randomness, which is enough that a suite creating thousands of
// fixtures a day will not see a collision, and short enough to read in a
// failure message.
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
