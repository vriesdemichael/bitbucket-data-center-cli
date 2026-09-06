package auth

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// Each of these commands has to report a failed request rather than a result.
// A key listing that prints nothing on failure reads as an account with no
// keys, and a clear that reports success without one is worse.
//
// The failure is a dropped connection rather than a 500: the subject is that
// the error reaches the caller, and answering 500 to everything would say
// besides that Bitbucket does, which it has no reason to.
//
// mock-inventory: transport-fault — nothing is listening; the subject is that each command surfaces the failure instead of rendering an empty result.
func TestAuthGpgKeyCommandsErrors(t *testing.T) {
	t.Parallel()

	unreachable := testsupport.ClosedListenerURL(t)

	deps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL: unreachable,
			}, nil
		},
	}

	execute := func(args ...string) (string, error) {
		cmd := New(deps)
		buffer := &bytes.Buffer{}
		cmd.SetOut(buffer)
		cmd.SetErr(buffer)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return buffer.String(), err
	}

	if _, err := execute("gpg-key", "list"); err == nil {
		t.Fatal("a failed key listing was reported as an empty one")
	}
	if _, err := execute("gpg-key", "add", "gpg-key-text"); err == nil {
		t.Fatal("a failed add was reported as a success")
	}
	if _, err := execute("gpg-key", "remove", "keyid"); err == nil {
		t.Fatal("a failed remove was reported as a success")
	}
	if _, err := execute("gpg-key", "clear", "-y"); err == nil {
		t.Fatal("a failed clear was reported as a success")
	}
}

func TestAuthGpgKeyCommandsAdditionalCoverage(t *testing.T) {
	t.Parallel()

	// 1. deps.LoadConfig returns error
	{
		deps := Dependencies{
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{}, fmt.Errorf("config error")
			},
		}
		cmd := New(deps)
		cmd.SetArgs([]string{"gpg-key", "list"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error when LoadConfig fails")
		}
	}

	// 2. Client creation fails (invalid URL)
	{
		deps := Dependencies{
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{
					BitbucketURL: "://invalid",
				}, nil
			},
		}
		cmd := New(deps)
		cmd.SetArgs([]string{"gpg-key", "list"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error when client creation fails")
		}
	}
	// 3. Listing an empty set of keys is live now, in
	// TestLiveGPGKeyLifecycle, against a list that is empty because clear
	// emptied it. It used to be here, against a page written to be empty.

	// 4. Add key with empty key text
	{
		deps := Dependencies{}
		cmd := New(deps)
		cmd.SetArgs([]string{"gpg-key", "add", "   "})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error when adding empty key")
		}
	}

	// 5. Clear keys cancellation ("n")
	{
		deps := Dependencies{}
		cmd := New(deps)
		oldStdin := os.Stdin
		r, w, _ := os.Pipe()
		os.Stdin = r
		_, _ = w.Write([]byte("n\n"))
		_ = w.Close()
		cmd.SetArgs([]string{"gpg-key", "clear"})
		err := cmd.Execute()
		os.Stdin = oldStdin
		if err == nil {
			t.Fatal("expected error (cancelled) when clearing keys with 'n'")
		}
	}

	// 6. Clear keys confirmation ("y")
	{
		// A listener that fails the test if it is reached, which is the
		// assertion: the confirmation is refused before a request exists.
		guard := httptest.NewServer(testsupport.UnreachedHandler(t))
		defer guard.Close()

		deps := Dependencies{
			LoadConfig: func() (config.AppConfig, error) {
				return config.AppConfig{
					BitbucketURL: guard.URL,
				}, nil
			},
		}
		// This used to swap os.Stdin for a pipe carrying "y\n", because the
		// command read the process's stdin with a bare fmt.Scanln rather than
		// its own. It now goes through prompt.ConfirmAction, which refuses when
		// nobody is there to answer -- so what is asserted here is the refusal
		// and the flag it names. Reading a typed answer is covered in
		// internal/cli/prompt, where the environment is injectable.
		cmd := New(deps)
		out := new(bytes.Buffer)
		cmd.SetOut(out)
		cmd.SetErr(out)
		cmd.SetIn(strings.NewReader("y\n"))
		cmd.SetArgs([]string{"gpg-key", "clear"})

		err := cmd.Execute()
		if err == nil {
			t.Fatal("all GPG keys were cleared with no confirmation and no --yes")
		}
		if !strings.Contains(err.Error(), "--yes is required") {
			t.Errorf("error = %q, want it to name the flag that would have confirmed", err.Error())
		}
	}
}
