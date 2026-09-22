package cli

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// Completion on a machine that cannot reach the instance, driven through the
// real command tree: the hidden __complete command, the words typed so far,
// and the word being completed last.
//
// This is the common case rather than an unhappy one -- a laptop off the VPN,
// a token that has expired, a directory that is not a checkout -- and it is
// the case a shell handles worst if bb gets it wrong: an error printed over
// the line being typed, a hang, or a list of the working directory offered in
// place of a branch.

// completeOffline runs one press against an instance that refuses every
// connection, and returns the candidates and the directive.
func completeOffline(t *testing.T, words ...string) ([]string, int) {
	t.Helper()

	// Port 9 is reserved as discard: nothing listens, and the refusal is
	// immediate rather than a hang the deadline has to cut short.
	command := NewRootCommandWithOverrides(config.Overrides{Host: "http://127.0.0.1:9", Token: "not-a-real-token"})

	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(append([]string{cobra.ShellCompRequestCmd}, words...))

	if err := command.Execute(); err != nil {
		t.Fatalf("completing %v failed instead of completing nothing: %v\noutput: %s", words, err, output.String())
	}

	var (
		candidates []string
		directive  = -1
	)

	for _, line := range strings.Split(strings.ReplaceAll(output.String(), "\r\n", "\n"), "\n") {
		switch {
		case strings.TrimSpace(line) == "":
		case strings.HasPrefix(line, ":"):
			parsed, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, ":")))
			if err != nil {
				t.Fatalf("directive %q is not a number: %v", line, err)
			}
			directive = parsed
		case strings.HasPrefix(line, "Completion ended with directive:"):
		default:
			value, _, _ := strings.Cut(line, "\t")
			candidates = append(candidates, strings.TrimSpace(value))
		}
	}

	if directive < 0 {
		t.Fatalf("completing %v carried no directive line: %q", words, output.String())
	}

	return candidates, directive
}

// TestAnUnreachableInstanceCompletesNothingAndOffersNoFiles is the behaviour
// every server-backed slot shares.
func TestAnUnreachableInstanceCompletesNothingAndOffersNoFiles(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")

	for _, words := range [][]string{
		{"pr", "merge", "--repo", "PRJ/nothing", ""},
		{"branch", "delete", "--repo", "PRJ/nothing", ""},
		{"project", "get", ""},
		{"repo", "clone", ""},
	} {
		candidates, directive := completeOffline(t, words...)

		// An Active Help line is allowed: it is how bash and zsh explain the
		// silence, and Cobra marks it so the other shells drop it.
		for _, candidate := range candidates {
			if !strings.HasPrefix(candidate, "_activeHelp_") {
				t.Errorf("completing %v offered %q from an instance that refuses connections", words, candidate)
			}
		}

		if directive&int(cobra.ShellCompDirectiveNoFileComp) == 0 {
			t.Errorf("completing %v did not stop the shell falling back to file names (directive %d)", words, directive)
		}
	}
}

// TestValuesThatNeedNoServerStillComplete is the other half.
//
// The wiring resolves configuration lazily, so a slot whose values are this
// machine's own must answer whether or not an instance can be reached. A
// completion package that resolved eagerly would fail here.
func TestValuesThatNeedNoServerStillComplete(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")

	for _, testCase := range []struct {
		words []string
		want  string
	}{
		{words: []string{"--log-level", ""}, want: "debug"},
		{words: []string{"pr", "list", "--state", ""}, want: "open"},
		{words: []string{"pr", "review", "set", "1", ""}, want: "APPROVED"},
	} {
		candidates, directive := completeOffline(t, testCase.words...)

		found := false
		for _, candidate := range candidates {
			if candidate == testCase.want {
				found = true
			}
		}
		if !found {
			t.Errorf("completing %v did not offer %q; got %v", testCase.words, testCase.want, candidates)
		}
		if directive&int(cobra.ShellCompDirectiveNoFileComp) == 0 {
			t.Errorf("completing %v did not stop the shell falling back to file names", testCase.words)
		}
	}
}

// TestASlotThatTakesAPathAsksTheShellForOne keeps the one exception honest.
//
// Refusing file completion everywhere would break the slots that really do
// take a local path, and those are the ones where the shell's own answer is
// the right one.
func TestASlotThatTakesAPathAsksTheShellForOne(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")

	if _, directive := completeOffline(t, "--ca-file", ""); directive != int(cobra.ShellCompDirectiveDefault) {
		t.Errorf("--ca-file should leave file completion to the shell, got directive %d", directive)
	}

	if _, directive := completeOffline(t, "ssh-key", "add", ""); directive != int(cobra.ShellCompDirectiveDefault) {
		t.Errorf("a key file argument should leave file completion to the shell, got directive %d", directive)
	}

	if _, directive := completeOffline(t, "repo", "clone", "PRJ/repo", ""); directive&int(cobra.ShellCompDirectiveFilterDirs) == 0 {
		t.Errorf("the clone directory should be completed as a directory, got directive %d", directive)
	}
}
