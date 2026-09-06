// Package secretinput reads a credential from somewhere that is not the
// command line.
//
// ADR-047 forbids a flag whose value is a secret: a flag value lands in the
// process argument list, which is world-readable on Linux, and in shell
// history. What is left is stdin and the environment, and both have a shape
// that is easy to get subtly wrong -- a trailing newline that becomes part of
// the credential, an unbounded read, an empty pipe accepted as an empty secret.
// `bb auth login` worked this out once for its token and its password; this is
// that reasoning, in one place, for every command that needs it.
package secretinput

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// MaxLength bounds a secret read from stdin.
//
// Without a bound, `bb auth login --token-stdin < /dev/urandom` would buffer
// without limit. Bitbucket personal access tokens are well under 200 bytes; the
// ceiling is generous enough that no legitimate credential reaches it.
const MaxLength = 8192

// Resolved is a secret and where it came from.
//
// The origin travels with the value because a dry run has to say that a secret
// will be set without saying what it is, and "from $BB_WEBHOOK_SECRET" is the
// most useful thing it can say. Nothing here renders Value; that is the
// caller's job, and no caller should.
type Resolved struct {
	Value  string
	Origin string
	Given  bool
}

// FromStdin reads a single credential from reader.
//
// Trailing newlines are stripped so the common `echo "$TOKEN" | bb ...` form
// works, and interior whitespace is rejected rather than silently accepted: a
// value that arrived with a stray space is far more likely to be a piping
// mistake than a real credential, and storing it produces an authentication
// failure much later, far from its cause. A secret that genuinely contains
// whitespace has to come from the environment instead.
//
// usageExample completes the message that rejection produces, because a caller
// who piped wrongly needs the right form, not the diagnosis.
func FromStdin(reader io.Reader, flagName string, usageExample string) (string, error) {
	if reader == nil {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s was given but stdin is not available", flagName), nil)
	}

	limited := io.LimitReader(reader, MaxLength+1)

	raw, err := io.ReadAll(bufio.NewReader(limited))
	if err != nil {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read %s from stdin", flagName), err)
	}

	if len(raw) > MaxLength {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s input exceeds %d bytes; that is not a credential", flagName, MaxLength), nil)
	}

	secret := strings.TrimRight(string(raw), "\r\n")

	if strings.TrimSpace(secret) == "" {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s was given but stdin was empty", flagName), nil)
	}

	if strings.ContainsAny(secret, " \t\r\n") {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s input contains whitespace; pipe exactly one credential, e.g. %s", flagName, usageExample), nil)
	}

	return secret, nil
}

// Resolve picks between a secret piped on stdin and one held in the
// environment, in that order.
//
// stdin wins because it is the explicit act: a variable can be set by a shell
// profile the caller has forgotten, and silently preferring it over what the
// caller just piped would be the wrong way round. Neither being present is not
// an error here -- the command decides whether a missing secret means "leave it
// alone" or "this was required".
func Resolve(fromStdin bool, reader io.Reader, stdinFlagName string, envName string, usageExample string) (Resolved, error) {
	if fromStdin {
		secret, err := FromStdin(reader, stdinFlagName, usageExample)
		if err != nil {
			return Resolved{}, err
		}

		return Resolved{Value: secret, Origin: stdinFlagName, Given: true}, nil
	}

	// An empty variable is not a secret, and treating it as one would set an
	// empty credential from a typo in a variable name two files away.
	if value := os.Getenv(envName); strings.TrimSpace(value) != "" {
		return Resolved{Value: value, Origin: "$" + envName, Given: true}, nil
	}

	return Resolved{}, nil
}
