package auth

import (
	"fmt"
	"io"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/secretinput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// maxSecretLength bounds a secret read from stdin.
const maxSecretLength = secretinput.MaxLength

// readSecretFromStdin reads a single credential from reader.
//
// The reading is shared with every other command that takes a secret; only the
// example in the rejection message is login's own.
func readSecretFromStdin(reader io.Reader, flagName string) (string, error) {
	return secretinput.FromStdin(reader, flagName,
		fmt.Sprintf("printf '%%s' \"$TOKEN\" | bb auth login <host> %s", flagName))
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
