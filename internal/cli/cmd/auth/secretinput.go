package auth

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// maxSecretLength bounds a secret read from stdin.
//
// Without a bound, `bb auth login --token-stdin < /dev/urandom` would buffer
// without limit. Bitbucket personal access tokens are well under 200 bytes; the
// ceiling is generous enough that no legitimate credential reaches it.
const maxSecretLength = 8192

// readSecretFromStdin reads a single credential from reader.
//
// Trailing newlines are stripped so the common `echo "$TOKEN" | bb ...` form
// works, and interior whitespace is rejected rather than silently accepted:
// a value that arrived with a stray space is far more likely to be a piping
// mistake than a real credential, and storing it produces an authentication
// failure much later, far from its cause.
func readSecretFromStdin(reader io.Reader, flagName string) (string, error) {
	if reader == nil {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s was given but stdin is not available", flagName), nil)
	}

	limited := io.LimitReader(reader, maxSecretLength+1)

	raw, err := io.ReadAll(bufio.NewReader(limited))
	if err != nil {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("failed to read %s from stdin", flagName), err)
	}

	if len(raw) > maxSecretLength {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s input exceeds %d bytes; that is not a credential", flagName, maxSecretLength), nil)
	}

	secret := strings.TrimRight(string(raw), "\r\n")

	if strings.TrimSpace(secret) == "" {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s was given but stdin was empty", flagName), nil)
	}

	if strings.ContainsAny(secret, " \t\r\n") {
		return "", apperrors.New(apperrors.KindValidation, fmt.Sprintf("%s input contains whitespace; pipe exactly one credential, e.g. printf '%%s' \"$TOKEN\" | bb auth login <host> %s", flagName, flagName), nil)
	}

	return secret, nil
}

// resolveLoginSecret picks between a flag-supplied secret and one piped on
// stdin, rejecting the ambiguous case where both are given.
//
// There is no value form to reconcile with any more. --token and --password
// were retired in #464 because a flag value lands in the process argument list,
// which is world-readable on Linux, and in shell history.
func resolveLoginSecret(fromStdin bool, reader io.Reader, stdinFlagName string) (string, error) {
	if !fromStdin {
		return "", nil
	}

	return readSecretFromStdin(reader, stdinFlagName)
}

// storedConfigLocation renders the config file path for a warning message,
// falling back to a description when the path cannot be resolved. A warning
// that cannot name the file is still worth printing.
func storedConfigLocation() string {
	path, err := config.ConfigPath()
	if err != nil || strings.TrimSpace(path) == "" {
		return "the bb config file"
	}

	return path
}

// reportInsecureStorage announces that a secret was written to the config file
// in plaintext, naming the file so the reader can act on it.
//
// Always to stderr: under --json stdout carries the machine contract, and prose
// there makes the envelope unparseable.
func reportInsecureStorage(writer io.Writer, host string) {
	fmt.Fprintf(writer, "Warning: OS keyring unavailable; credentials for %s were written in plaintext to %s.\n", host, storedConfigLocation())
	fmt.Fprintln(writer, "         Use --require-keyring (or BB_REQUIRE_KEYRING=1) to fail instead of falling back.")
}

// describeCredentialStorage renders where the credential in use is held, naming
// the file when it is on disk.
//
// auth status is where an operator comes for detail, so it names the path
// rather than repeating the generic warning already emitted for every command.
func describeCredentialStorage(cfg config.AppConfig) string {
	storage := cfg.CredentialStorage()
	if !cfg.UsedInsecureStorage {
		return storage
	}

	return fmt.Sprintf("%s (%s)", storage, storedConfigLocation())
}
