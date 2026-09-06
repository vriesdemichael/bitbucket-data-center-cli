package execgit

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
)

// newCommittedRepository builds a repository with one commit on master, which
// is the minimum state in which branches, checkouts and merges mean anything.
func newCommittedRepository(t *testing.T, backend *Backend) string {
	t.Helper()

	repositoryDirectory := filepath.Join(t.TempDir(), "repo")
	if _, err := backend.run(context.Background(), runOptions{args: []string{"init", "--initial-branch", "master", repositoryDirectory}}); err != nil {
		t.Fatalf("git init failed: %v", err)
	}
	for _, args := range [][]string{
		{"config", "user.email", "test@example.local"},
		{"config", "user.name", "Test"},
		{"commit", "--allow-empty", "-m", "first"},
	} {
		if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: args}); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	return repositoryDirectory
}

func TestWorkingTreeStateReportsTrackedChangesOnly(t *testing.T) {
	t.Parallel()

	backend := New()
	backend.Timeout = 20 * time.Second

	repositoryDirectory := newCommittedRepository(t, backend)

	status, err := backend.WorkingTreeState(context.Background(), repositoryDirectory)
	if err != nil {
		t.Fatalf("working tree state failed: %v", err)
	}
	if status.Dirty {
		t.Fatalf("expected a clean tree, got %#v", status)
	}

	// An untracked file cannot be overwritten by a branch switch, so it must
	// not count as dirty. Counting it would block every checkout over build
	// output.
	if err := os.WriteFile(filepath.Join(repositoryDirectory, "untracked.txt"), []byte("scratch\n"), 0o600); err != nil {
		t.Fatalf("write untracked file failed: %v", err)
	}
	status, err = backend.WorkingTreeState(context.Background(), repositoryDirectory)
	if err != nil {
		t.Fatalf("working tree state failed: %v", err)
	}
	if status.Dirty {
		t.Fatalf("expected an untracked file to leave the tree clean, got %#v", status)
	}

	if err := os.WriteFile(filepath.Join(repositoryDirectory, "tracked.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatalf("write tracked file failed: %v", err)
	}
	for _, args := range [][]string{
		{"add", "tracked.txt"},
		{"commit", "-m", "add tracked"},
	} {
		if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: args}); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(repositoryDirectory, "tracked.txt"), []byte("two\n"), 0o600); err != nil {
		t.Fatalf("modify tracked file failed: %v", err)
	}

	status, err = backend.WorkingTreeState(context.Background(), repositoryDirectory)
	if err != nil {
		t.Fatalf("working tree state failed: %v", err)
	}
	if !status.Dirty {
		t.Fatal("expected a modified tracked file to make the tree dirty")
	}
	if len(status.Entries) != 1 || !strings.Contains(status.Entries[0], "tracked.txt") {
		t.Fatalf("expected the modified file to be named, got %#v", status.Entries)
	}
}

func TestWorkingTreeStateRejectsBadInput(t *testing.T) {
	t.Parallel()

	backend := New()
	backend.Timeout = 10 * time.Second

	if _, err := backend.WorkingTreeState(context.Background(), "  "); err == nil {
		t.Fatal("expected an empty repository directory to be rejected")
	}
	if _, err := backend.WorkingTreeState(context.Background(), t.TempDir()); err == nil {
		t.Fatal("expected a non-repository directory to fail")
	}
}

func TestBranchExists(t *testing.T) {
	t.Parallel()

	backend := New()
	backend.Timeout = 20 * time.Second

	repositoryDirectory := newCommittedRepository(t, backend)

	exists, err := backend.BranchExists(context.Background(), repositoryDirectory, "master")
	if err != nil {
		t.Fatalf("branch exists failed: %v", err)
	}
	if !exists {
		t.Fatal("expected master to exist")
	}

	// An absent branch is an answer, not a failure.
	exists, err = backend.BranchExists(context.Background(), repositoryDirectory, "feature/nope")
	if err != nil {
		t.Fatalf("branch exists for an absent branch should not error: %v", err)
	}
	if exists {
		t.Fatal("expected feature/nope to be absent")
	}

	if _, err := backend.BranchExists(context.Background(), "  ", "master"); err == nil {
		t.Fatal("expected an empty repository directory to be rejected")
	}
	if _, err := backend.BranchExists(context.Background(), repositoryDirectory, "  "); err == nil {
		t.Fatal("expected an empty branch name to be rejected")
	}
}

func TestCheckoutCreatesBranchesAndDetaches(t *testing.T) {
	t.Parallel()

	backend := New()
	backend.Timeout = 30 * time.Second

	repositoryDirectory := newCommittedRepository(t, backend)

	if err := backend.Checkout(context.Background(), repositoryDirectory, git.CheckoutOptions{
		Ref:       "master",
		NewBranch: "feature/x",
	}); err != nil {
		t.Fatalf("create branch failed: %v", err)
	}
	branch, err := backend.CurrentBranch(context.Background(), repositoryDirectory)
	if err != nil {
		t.Fatalf("current branch failed: %v", err)
	}
	if branch != "feature/x" {
		t.Fatalf("expected feature/x, got %q", branch)
	}

	// -b rather than -B: recreating an existing branch must fail rather than
	// silently move it off whatever it pointed at.
	if err := backend.Checkout(context.Background(), repositoryDirectory, git.CheckoutOptions{
		Ref:       "master",
		NewBranch: "feature/x",
	}); err == nil {
		t.Fatal("expected recreating an existing branch to fail")
	}

	if err := backend.Checkout(context.Background(), repositoryDirectory, git.CheckoutOptions{Ref: "master", Detach: true}); err != nil {
		t.Fatalf("detached checkout failed: %v", err)
	}
	branch, err = backend.CurrentBranch(context.Background(), repositoryDirectory)
	if err != nil {
		t.Fatalf("current branch failed: %v", err)
	}
	if branch != "" {
		t.Fatalf("expected no branch after a detached checkout, got %q", branch)
	}

	if err := backend.Checkout(context.Background(), repositoryDirectory, git.CheckoutOptions{
		Ref:       "master",
		NewBranch: "feature/y",
		Detach:    true,
	}); err == nil {
		t.Fatal("expected creating a branch while detaching to be rejected")
	}

	// Force is what --force on the command reaches: a modified tracked file
	// stops a plain checkout and does not stop a forced one.
	if err := backend.Checkout(context.Background(), repositoryDirectory, git.CheckoutOptions{Ref: "master"}); err != nil {
		t.Fatalf("checkout master failed: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repositoryDirectory, "conflict.txt"), []byte("master\n"), 0o600); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	for _, args := range [][]string{
		{"add", "conflict.txt"},
		{"commit", "-m", "add conflict"},
		{"checkout", "feature/x"},
	} {
		if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: args}); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}
	if err := os.WriteFile(filepath.Join(repositoryDirectory, "conflict.txt"), []byte("local\n"), 0o600); err != nil {
		t.Fatalf("write conflicting file failed: %v", err)
	}
	if err := backend.Checkout(context.Background(), repositoryDirectory, git.CheckoutOptions{Ref: "master", Force: true}); err != nil {
		t.Fatalf("forced checkout over a conflicting file failed: %v", err)
	}

	if err := backend.Checkout(context.Background(), repositoryDirectory, git.CheckoutOptions{Ref: "  "}); err == nil {
		t.Fatal("expected an empty ref to be rejected")
	}
	if err := backend.Checkout(context.Background(), "  ", git.CheckoutOptions{Ref: "master"}); err == nil {
		t.Fatal("expected an empty repository directory to be rejected")
	}
}

func TestFastForwardRefusesToDiverge(t *testing.T) {
	t.Parallel()

	backend := New()
	backend.Timeout = 30 * time.Second

	repositoryDirectory := newCommittedRepository(t, backend)

	for _, args := range [][]string{
		{"checkout", "-b", "ahead"},
		{"commit", "--allow-empty", "-m", "second"},
		{"checkout", "master"},
	} {
		if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: args}); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	if err := backend.FastForward(context.Background(), repositoryDirectory, "ahead"); err != nil {
		t.Fatalf("fast-forward to a descendant failed: %v", err)
	}

	// A diverged branch must fail rather than quietly produce a merge commit.
	for _, args := range [][]string{
		{"checkout", "-b", "diverged", "HEAD~1"},
		{"commit", "--allow-empty", "-m", "other"},
	} {
		if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: args}); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	if err := backend.FastForward(context.Background(), repositoryDirectory, "master"); err == nil {
		t.Fatal("expected a diverged fast-forward to fail")
	}

	if err := backend.FastForward(context.Background(), "  ", "master"); err == nil {
		t.Fatal("expected an empty repository directory to be rejected")
	}
	if err := backend.FastForward(context.Background(), repositoryDirectory, "  "); err == nil {
		t.Fatal("expected an empty ref to be rejected")
	}
}

func TestFetchPassesRefspecs(t *testing.T) {
	t.Parallel()

	backend := New()
	backend.Timeout = 45 * time.Second

	source := newCommittedRepository(t, backend)
	if _, err := backend.run(context.Background(), runOptions{cwd: source, args: []string{"checkout", "-b", "feature/shared"}}); err != nil {
		t.Fatalf("git checkout -b failed: %v", err)
	}

	target := newCommittedRepository(t, backend)
	if err := backend.AddRemote(context.Background(), target, git.Remote{Name: "src", URL: source}); err != nil {
		t.Fatalf("add remote failed: %v", err)
	}

	// The explicit refspec is what a pull request checkout relies on: the
	// remote was just added, so it has no configured fetch refspec of its own.
	// The blank entry is there because a caller building refspecs by
	// concatenation can produce one, and it must be dropped rather than passed.
	if err := backend.Fetch(context.Background(), target, git.FetchOptions{
		Remote:   "src",
		Refspecs: []string{"+refs/heads/feature/shared:refs/remotes/src/feature/shared", "   "},
	}); err != nil {
		t.Fatalf("fetch with refspec failed: %v", err)
	}

	if _, err := backend.run(context.Background(), runOptions{
		cwd:  target,
		args: []string{"rev-parse", "--verify", "refs/remotes/src/feature/shared"},
	}); err != nil {
		t.Fatalf("expected the refspec to have created the remote-tracking ref: %v", err)
	}

	if err := backend.Fetch(context.Background(), "  ", git.FetchOptions{Remote: "src"}); err == nil {
		t.Fatal("expected an empty repository directory to be rejected")
	}
}

// TestCredentialArgsScopesToTheHost is a security property, not a formatting
// one.
//
// git applies an unscoped http.extraHeader to every request it makes from a
// repository — including to unrelated remotes and to redirect targets — so an
// unscoped Bitbucket token would be handed to whatever else that repository
// talks to. Where there is no host to scope to, nothing is sent at all.
func TestCredentialArgsScopesToTheHost(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		credentials *git.Credentials
		wantArgs    bool
		wantScope   string
		wantSecret  string
	}{
		{
			name:        "nil credentials",
			credentials: nil,
		},
		{
			name:        "token is scoped to scheme, host and port",
			credentials: &git.Credentials{URL: "https://bitbucket.example.com:7990/scm/PRJ/repo.git", Token: "tok"},
			wantArgs:    true,
			wantScope:   "http.https://bitbucket.example.com:7990/.extraHeader=",
			wantSecret:  "Authorization: Bearer tok",
		},
		{
			name:        "basic auth is base64 encoded",
			credentials: &git.Credentials{URL: "https://bitbucket.example.com/scm/PRJ/repo.git", Username: "alice", Password: "hunter2"},
			wantArgs:    true,
			wantScope:   "http.https://bitbucket.example.com/.extraHeader=",
			wantSecret:  "Authorization: Basic " + base64.StdEncoding.EncodeToString([]byte("alice:hunter2")),
		},
		{
			// An SSH remote has no scheme to scope an HTTP header to. Sending it
			// unscoped would be worse than sending nothing.
			name:        "unparseable URL sends nothing",
			credentials: &git.Credentials{URL: "git@bitbucket.example.com:PRJ/repo.git", Token: "tok"},
		},
		{
			name:        "no secret sends nothing",
			credentials: &git.Credentials{URL: "https://bitbucket.example.com/scm/PRJ/repo.git"},
		},
		{
			name:        "username without password sends nothing",
			credentials: &git.Credentials{URL: "https://bitbucket.example.com/scm/PRJ/repo.git", Username: "alice"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			args := credentialArgs(testCase.credentials)

			if !testCase.wantArgs {
				if len(args) != 0 {
					t.Fatalf("expected no arguments, got %v", args)
				}
				return
			}

			if len(args) != 2 || args[0] != "-c" {
				t.Fatalf("expected a single -c pair, got %v", args)
			}
			if !strings.HasPrefix(args[1], testCase.wantScope) {
				t.Fatalf("expected scope %q, got %q", testCase.wantScope, args[1])
			}
			if !strings.HasSuffix(args[1], testCase.wantSecret) {
				t.Fatalf("expected the rendered header to end with %q, got %q", testCase.wantSecret, args[1])
			}
		})
	}
}
