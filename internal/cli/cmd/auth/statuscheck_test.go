package auth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
)

// TestRemedyForAuthFailureUsesTheErrorKind is the reason this branches on the
// taxonomy rather than on message text.
//
// "Your credential is wrong" and "the host did not answer" have genuinely
// different fixes, and ADR-011 already draws that line. Re-deriving it from
// wording would get it wrong the first time a message changed.
func TestRemedyForAuthFailureUsesTheErrorKind(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		expected string
	}{
		{
			name:     "rejected credential points at login",
			err:      apperrors.New(apperrors.KindAuthentication, "401", nil),
			expected: "bb auth login",
		},
		{
			name:     "forbidden also points at login",
			err:      apperrors.New(apperrors.KindAuthorization, "403", nil),
			expected: "bb auth login",
		},
		{
			name:     "unreachable host points at the network docs",
			err:      apperrors.New(apperrors.KindTransient, "dial tcp: i/o timeout", nil),
			expected: "proxy",
		},
		{
			name:     "anything else stays generic",
			err:      errors.New("something unexpected"),
			expected: "BITBUCKET_URL",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			remedy := remedyForAuthFailure(testCase.err, "https://bitbucket.example.com")
			if !strings.Contains(remedy, testCase.expected) {
				t.Fatalf("expected remedy to mention %q, got %q", testCase.expected, remedy)
			}
		})
	}
}

// gitConfigStub answers the one config read the helper check makes.
type gitConfigStub struct {
	value string
	err   error
	// seenKey records what was asked for. Scoping matters: a bare
	// credential.helper would be consulted for every host git talks to.
	seenKey string
}

func (stub *gitConfigStub) GetConfig(_ context.Context, options git.ConfigOptions) (string, error) {
	stub.seenKey = options.Key
	return stub.value, stub.err
}

func (stub *gitConfigStub) Version(context.Context) (string, error) { return "", nil }
func (stub *gitConfigStub) Clone(context.Context, string, git.CloneOptions) error {
	return nil
}
func (stub *gitConfigStub) AddRemote(context.Context, string, git.Remote) error { return nil }
func (stub *gitConfigStub) Fetch(context.Context, string, git.FetchOptions) error {
	return nil
}
func (stub *gitConfigStub) Checkout(context.Context, string, git.CheckoutOptions) error {
	return nil
}
func (stub *gitConfigStub) RepositoryRoot(context.Context, string) (string, error) {
	return "", nil
}
func (stub *gitConfigStub) CurrentBranch(context.Context, string) (string, error) {
	return "", nil
}
func (stub *gitConfigStub) WorkingTreeState(context.Context, string) (git.WorkingTreeStatus, error) {
	return git.WorkingTreeStatus{}, nil
}
func (stub *gitConfigStub) BranchExists(context.Context, string, string) (bool, error) {
	return false, nil
}
func (stub *gitConfigStub) FastForward(context.Context, string, string) error { return nil }
func (stub *gitConfigStub) ListRemotes(context.Context, string) ([]git.Remote, error) {
	return nil, nil
}
func (stub *gitConfigStub) SetConfig(context.Context, git.ConfigOptions) error   { return nil }
func (stub *gitConfigStub) UnsetConfig(context.Context, git.ConfigOptions) error { return nil }

// TestGitCredentialHelperState covers the check that explains the most common
// confusion there is: bb works, and then git prompts for a password anyway.
func TestGitCredentialHelperState(t *testing.T) {
	t.Run("configured", func(t *testing.T) {
		stub := &gitConfigStub{value: `!"/usr/local/bin/bb" auth git-credential`}
		check := gitCredentialHelperState(context.Background(), stub, "https://bitbucket.example.com/context")

		if !check.OK {
			t.Fatalf("expected a configured helper to pass, got %#v", check)
		}
		// Scoped to scheme and host, without the path: git matches config on
		// the host, and a path would simply never match.
		if stub.seenKey != "credential.https://bitbucket.example.com.helper" {
			t.Fatalf("unexpected config key %q", stub.seenKey)
		}
		if check.Remedy != "" {
			t.Fatalf("expected no remedy when it passes, got %q", check.Remedy)
		}
	})

	t.Run("not configured names the fix", func(t *testing.T) {
		check := gitCredentialHelperState(context.Background(), &gitConfigStub{}, "https://bitbucket.example.com")

		if check.OK {
			t.Fatal("expected an unconfigured helper to fail the check")
		}
		if !strings.Contains(check.Remedy, "bb auth setup-git") {
			t.Fatalf("expected the remedy to name setup-git, got %q", check.Remedy)
		}
	})

	t.Run("unreadable git config still reports", func(t *testing.T) {
		check := gitCredentialHelperState(context.Background(), &gitConfigStub{err: errors.New("git missing")}, "https://bitbucket.example.com")

		if check.OK {
			t.Fatal("expected a config read failure to fail the check")
		}
		if !strings.Contains(check.Detail, "git missing") {
			t.Fatalf("expected the underlying error in the detail, got %q", check.Detail)
		}
	})

	t.Run("unusable host is reported, not guessed at", func(t *testing.T) {
		check := gitCredentialHelperState(context.Background(), &gitConfigStub{}, "not a url")

		if check.OK {
			t.Fatal("expected an unusable host to fail the check")
		}
		if !strings.Contains(check.Detail, "no usable host") {
			t.Fatalf("expected the detail to say why, got %q", check.Detail)
		}
	})
}

// TestGitCredentialHelperCheckIsAdvisory guards the distinction a CI run
// exposed. The helper is needed to git push and irrelevant to anything that
// only calls the API, so its absence must not report a broken setup to the CI
// pipelines and agents that never run git at all.
func TestGitCredentialHelperCheckIsAdvisory(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		value string
	}{
		{name: "configured", value: `!"/usr/local/bin/bb" auth git-credential`},
		{name: "not configured", value: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			check := gitCredentialHelperState(context.Background(), &gitConfigStub{value: testCase.value}, "https://bitbucket.example.com")
			if !check.Advisory {
				t.Fatalf("the git helper check must be advisory whatever it finds, got %#v", check)
			}
		})
	}

	// The authentication check is the opposite: a rejected credential is a
	// broken setup, and must count.
	identity := statusCheck{Name: "authentication"}
	if identity.Advisory {
		t.Fatal("authentication must not be advisory")
	}
}

// TestStatusCommandUsesTheInjectedGitBackend closes a gap the unit tests above
// did not: they exercise gitCredentialHelperState directly, but the command
// wired it to a real execgit backend, so running `bb auth status` in a test
// shelled out to git and read whatever global configuration the machine had.
//
// Nothing asserted on the result, so it passed either way — which is the
// problem. The outcome depended on whether the developer had run
// bb auth setup-git, not on the code.
func TestStatusCommandUsesTheInjectedGitBackend(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "https://bitbucket.example.com")

	stub := &gitConfigStub{value: `!"/usr/local/bin/bb" auth git-credential`}

	cmd := New(Dependencies{
		JSONEnabled: func() bool { return true },
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{BitbucketURL: "https://bitbucket.example.com"}, nil
		},
		WriteJSON: func(writer io.Writer, payload any) error {
			return jsonoutput.Write(writer, payload)
		},
		NewUsersClient: func(config.AppConfig) (usersClient, error) {
			return nil, errors.New("users client unavailable in test")
		},
		GitBackend: func() git.Backend { return stub },
	})

	buffer := &bytes.Buffer{}
	cmd.SetOut(buffer)
	cmd.SetErr(buffer)
	cmd.SetArgs([]string{"status"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// The injected backend was consulted, and with the host-scoped key: proof
	// the command no longer reaches for the machine's real git configuration.
	if stub.seenKey != "credential.https://bitbucket.example.com.helper" {
		t.Fatalf("expected the injected backend to be asked for the scoped key, got %q", stub.seenKey)
	}

	if !strings.Contains(buffer.String(), "git credential helper") {
		t.Fatalf("expected the helper check in the output, got %q", buffer.String())
	}
}
