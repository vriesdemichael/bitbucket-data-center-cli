package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/git"
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
