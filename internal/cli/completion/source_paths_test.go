package completion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
)

func TestSplitPathNamesTheDirectoryToList(t *testing.T) {
	t.Parallel()

	tests := []struct {
		word          string
		wantDirectory string
		wantLeaf      string
	}{
		{"", "", ""},
		{"int", "", "int"},
		{"internal/", "internal/", ""},
		{"internal/cli/comp", "internal/cli/", "comp"},
		{"internal/cli/", "internal/cli/", ""},
		// The separator stays with the directory, so a candidate is the
		// directory plus the entry and nothing has to be re-joined.
		{"a/b", "a/", "b"},
	}

	for _, test := range tests {
		t.Run(test.word, func(t *testing.T) {
			t.Parallel()

			directory, leaf := splitPath(test.word)
			if directory != test.wantDirectory || leaf != test.wantLeaf {
				t.Fatalf("splitPath(%q) = (%q, %q), want (%q, %q)",
					test.word, directory, leaf, test.wantDirectory, test.wantLeaf)
			}
		})
	}
}

// TestPathResultMarksADirectory is the behaviour that makes a path completable
// at all: the trailing slash and the directive that keeps the cursor on it.
func TestPathResultMarksADirectory(t *testing.T) {
	t.Parallel()

	result := pathResult("internal/", []entry{
		{name: "cli", directory: true},
		{name: "doc.md"},
	}, "internal/")

	values := valuesOf(result.Candidates)
	if strings.Join(values, ",") != "internal/cli/,internal/doc.md" {
		t.Fatalf("pathResult = %v, want the typed directory carried and the directory marked", values)
	}
	if !result.NoSpace {
		t.Error("a directory was offered without NoSpace, so the next press would start after a space")
	}
	if result.KeepOrder {
		t.Error("a directory listing asked the shell to keep an order it has no ranking for")
	}
}

func TestPathResultLeavesTheCursorFreeWhenNothingIsADirectory(t *testing.T) {
	t.Parallel()

	// NoSpace is a property of the whole answer, so it has to be decided from
	// the candidates the shell will be shown rather than from the directory
	// that was read: "doc" matches no directory here, and gluing the cursor to
	// the completed file name because some sibling is one is the bug this
	// pins.
	result := pathResult("", []entry{
		{name: "internal", directory: true},
		{name: "doc.md"},
	}, "doc")

	if values := valuesOf(result.Candidates); strings.Join(values, ",") != "doc.md" {
		t.Fatalf("pathResult filtered to %v, want only what starts with what was typed", values)
	}
	if result.NoSpace {
		t.Error("a file completed with NoSpace because another entry of the directory was one")
	}
}

func TestPathResultMatchesTheWholeTypedWord(t *testing.T) {
	t.Parallel()

	// The shell replaces the whole word, and run.go drops any candidate that
	// does not start with it. A candidate offered as the bare entry name would
	// therefore never reach the user at all.
	result := pathResult("internal/cli/", []entry{{name: "completion", directory: true}}, "internal/cli/comp")

	if values := valuesOf(result.Candidates); strings.Join(values, ",") != "internal/cli/completion/" {
		t.Fatalf("pathResult = %v", values)
	}
}

func TestCheckoutRefsFallBackToTheRemoteCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		at     string
		remote string
		want   []string
	}{
		{
			// The default branch, as the checkout records it. Nothing else is
			// safe: the commit this checkout stands on is a branch nobody
			// named.
			name:   "nothing typed",
			remote: "origin",
			want:   []string{"refs/remotes/origin/HEAD"},
		},
		{
			name: "nothing typed and no remote",
			want: nil,
		},
		{
			name:   "a branch this checkout may only track",
			at:     "release/2.1",
			remote: "origin",
			want:   []string{"release/2.1", "refs/remotes/origin/release/2.1"},
		},
		{
			name: "a commit needs no remote",
			at:   "0a943a2",
			want: []string{"0a943a2"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := checkoutRefs(test.at, test.remote)
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Fatalf("checkoutRefs(%q, %q) = %v, want %v", test.at, test.remote, got, test.want)
			}
		})
	}
}

func TestEncodeDirectoryKeepsTheSeparators(t *testing.T) {
	t.Parallel()

	// Escaped whole, the separators become %2F and the browse endpoint
	// refuses the request; escaped per segment they survive and a half-typed
	// word still cannot reach a different endpoint.
	got, err := encodeDirectory("src/main resources/a?b/")
	if err != nil {
		t.Fatalf("encodeDirectory returned %v", err)
	}
	if got != "src/main%20resources/a%3Fb" {
		t.Fatalf("encodeDirectory = %q", got)
	}

	if got, err := encodeDirectory(""); err != nil || got != "" {
		t.Fatalf("encodeDirectory of the root = (%q, %v), want the repository root", got, err)
	}

	if _, err := encodeDirectory("internal/../../etc"); err == nil {
		t.Fatal("a traversal was encoded instead of refused")
	}
}

func TestBrowsePathIsTheDirectoryEndpoint(t *testing.T) {
	t.Parallel()

	repository := Repository{ProjectKey: "~alice", Slug: "my service"}

	if got := browsePath(repository, "internal/cli"); got != "/rest/api/latest/projects/~alice/repos/my%20service/browse/internal/cli" {
		t.Fatalf("browsePath = %q", got)
	}
	// The repository root, which the endpoint answers with a trailing slash.
	if got := browsePath(repository, ""); got != "/rest/api/latest/projects/~alice/repos/my%20service/browse/" {
		t.Fatalf("browsePath of the root = %q", got)
	}
}

func TestChangeCandidatesSayWhatThePullRequestDidToEachFile(t *testing.T) {
	t.Parallel()

	candidates := changeCandidates([]pullrequestservice.Change{
		{Path: "internal/cli/root.go", Type: "MODIFY"},
		{Path: "docs/new.md", Type: "ADD"},
		{Path: "internal/moved.go", Type: "MOVE", SrcPath: "internal/old.go"},
		{Path: "  ", Type: "ADD"},
	})

	values := valuesOf(candidates)
	if strings.Join(values, ",") != "internal/cli/root.go,docs/new.md,internal/moved.go" {
		t.Fatalf("changeCandidates = %v", values)
	}
	if got := descriptionOf(candidates, "docs/new.md"); got != "add" {
		t.Errorf("an added file was described as %q", got)
	}
	// A comment cannot be anchored to a path the pull request renamed away
	// from, so which one this is has to be visible.
	if got := descriptionOf(candidates, "internal/moved.go"); got != "move from internal/old.go" {
		t.Errorf("a moved file was described as %q", got)
	}
}

// TestLocalEntriesReadOneDirectoryOfTheCheckout is the local path against a
// real repository rather than a description of one.
//
// The point of reading the checkout is that it answers with the same entries
// Bitbucket would have listed, which only holds if the tree listing stops at
// one directory, names each entry the way the candidate is rebuilt from it,
// and resolves the ref the way the branch source offers them.
func TestLocalEntriesReadOneDirectoryOfTheCheckout(t *testing.T) {
	t.Parallel()

	scope := refScope{
		repository: Repository{ProjectKey: "PROJ", Slug: "service", RemoteName: "origin"},
		directory:  buildTreeCheckout(t),
	}

	ctx := context.Background()

	top := localEntries(ctx, scope, "", "")
	if !hasEntry(top, "dir", true) {
		t.Fatalf("the top level did not offer the directory: %v", top)
	}
	if !hasEntry(top, "top.txt", false) {
		t.Fatalf("the top level did not offer the file beside it: %v", top)
	}
	// One segment at a time: a listing that walked the tree would reach this,
	// and a repository of any size would answer a keystroke with all of it.
	for _, item := range top {
		if strings.Contains(item.name, "/") {
			t.Fatalf("the top level offered a path from deeper in the tree: %v", top)
		}
	}

	inside := localEntries(ctx, scope, "", "dir/")
	if !hasEntry(inside, "inner.txt", false) || !hasEntry(inside, "deeper", true) {
		t.Fatalf("typing into the directory did not offer its contents: %v", inside)
	}
	if hasEntry(inside, "top.txt", false) {
		t.Errorf("listing a directory came back with the level above it: %v", inside)
	}

	// The whole press, from the word to the candidates: the prefix names the
	// directory to read, and what comes back carries it.
	directory, _ := splitPath("dir/in")
	result := pathResult(directory, localEntries(ctx, scope, "", directory), "dir/in")
	if values := valuesOf(result.Candidates); strings.Join(values, ",") != "dir/inner.txt" {
		t.Fatalf("completing dir/in offered %v", values)
	}
}

// TestLocalEntriesResolveABranchTheCheckoutOnlyTracks is why checkoutRefs
// tries twice.
//
// The branch source offers branches this checkout has never had checked out,
// so `--at that-branch --path <tab>` names something that exists here only as
// a remote-tracking reference.
func TestLocalEntriesResolveABranchTheCheckoutOnlyTracks(t *testing.T) {
	t.Parallel()

	scope := refScope{
		repository: Repository{ProjectKey: "PROJ", Slug: "service", RemoteName: "origin"},
		directory:  buildTreeCheckout(t),
	}

	ctx := context.Background()

	onFeature := localEntries(ctx, scope, "feature", "")
	if !hasEntry(onFeature, "only-on-feature.txt", false) {
		t.Fatalf("the tracked branch's own file was not offered: %v", onFeature)
	}

	// And the default branch does not carry it, which is what proves the ref
	// was applied rather than ignored.
	if onDefault := localEntries(ctx, scope, "", ""); hasEntry(onDefault, "only-on-feature.txt", false) {
		t.Errorf("a file that exists only on a branch was offered for the default one: %v", onDefault)
	}

	if unknown := localEntries(ctx, scope, "no-such-ref", ""); len(unknown) != 0 {
		t.Errorf("a ref the checkout does not have answered from it: %v", unknown)
	}
}

// TestLocalEntriesStaySilentAwayFromTheCheckout keeps one repository's paths
// out of another's completion.
func TestLocalEntriesStaySilentAwayFromTheCheckout(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// A repository the caller named rather than one this checkout implies:
	// scopeFor leaves the directory empty, and the paths have to come from
	// the server.
	elsewhere := refScope{repository: Repository{ProjectKey: "OTHER", Slug: "service"}}
	if entries := localEntries(ctx, elsewhere, "", ""); len(entries) != 0 {
		t.Fatalf("a repository that is not this checkout was answered from it: %v", entries)
	}

	// A directory that is not a checkout fails silently rather than taking
	// the press down with it.
	notARepository := refScope{
		repository: Repository{ProjectKey: "PROJ", Slug: "service", RemoteName: "origin"},
		directory:  t.TempDir(),
	}
	if entries := localEntries(ctx, notARepository, "", ""); len(entries) != 0 {
		t.Fatalf("a directory that is not a checkout answered: %v", entries)
	}
}

// buildTreeCheckout makes a repository with a nested tree, a remote, a
// recorded remote HEAD and a branch this checkout tracks without holding.
func buildTreeCheckout(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	checkout := filepath.Join(root, "work")

	if err := os.MkdirAll(origin, 0o750); err != nil {
		t.Fatalf("create the origin directory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(checkout, "dir", "deeper"), 0o750); err != nil {
		t.Fatalf("create the checkout directory: %v", err)
	}

	git(t, origin, "init", "--bare")
	git(t, checkout, "init")
	git(t, checkout, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, checkout, "config", "user.name", "bb completion test")
	git(t, checkout, "config", "user.email", "bb-completion-test@example.local")

	write(t, filepath.Join(checkout, "top.txt"), "top\n")
	write(t, filepath.Join(checkout, "dir", "inner.txt"), "inner\n")
	write(t, filepath.Join(checkout, "dir", "deeper", "leaf.txt"), "leaf\n")
	git(t, checkout, "add", ".")
	git(t, checkout, "commit", "-m", "first commit")

	git(t, checkout, "remote", "add", "origin", filepath.ToSlash(origin))
	git(t, checkout, "push", "-u", "origin", "main")

	git(t, checkout, "checkout", "-b", "feature")
	write(t, filepath.Join(checkout, "only-on-feature.txt"), "feature\n")
	git(t, checkout, "add", "only-on-feature.txt")
	git(t, checkout, "commit", "-m", "second commit")
	git(t, checkout, "push", "-u", "origin", "feature")

	// Held only as refs/remotes/origin/feature from here on, which is what a
	// clone looks like for every branch somebody else pushed.
	git(t, checkout, "checkout", "main")
	git(t, checkout, "branch", "-D", "feature")

	// What a clone records for itself, and the only place the default branch
	// is written down locally.
	git(t, checkout, "remote", "set-head", "origin", "main")

	return checkout
}

func hasEntry(entries []entry, name string, directory bool) bool {
	for _, item := range entries {
		if item.name == name {
			return item.directory == directory
		}
	}

	return false
}
