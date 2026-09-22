package completion

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/execgit"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git/gittest"
)

// TestMain fails this package when its tests reconfigure the repository they
// run inside. The tests below build checkouts and run git in them, which is
// exactly the shape that has rewritten a developer's own configuration
// before.
func TestMain(m *testing.M) { gittest.Guard(m) }

func TestBranchFromRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		ref    string
		remote string
		want   string
		wantOK bool
	}{
		{"local branch", "refs/heads/main", "origin", "main", true},
		{"local branch with slashes", "refs/heads/feature/login", "origin", "feature/login", true},
		{"remote-tracking branch", "refs/remotes/origin/main", "origin", "main", true},
		{"remote-tracking branch with slashes", "refs/remotes/origin/feature/login", "origin", "feature/login", true},
		// refs/remotes/<remote>/HEAD is the default branch recorded as a
		// symbolic ref. Offered as a branch it would complete to a name no
		// command accepts.
		{"the remote's HEAD is not a branch", "refs/remotes/origin/HEAD", "origin", "", false},
		// A second remote is a different repository. Its branches are not the
		// scoped repository's, and a command told to act on one would be
		// acting on a branch that may not exist there.
		{"another remote's branch", "refs/remotes/fork/main", "origin", "", false},
		{"no remote inferred", "refs/remotes/origin/main", "", "", false},
		{"a local branch needs no remote", "refs/heads/main", "", "main", true},
		{"tag", "refs/tags/v1.0.0", "origin", "", false},
		{"note", "refs/notes/commits", "origin", "", false},
		{"empty", "", "origin", "", false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := branchFromRef(test.ref, test.remote)
			if got != test.want || ok != test.wantOK {
				t.Fatalf("branchFromRef(%q, %q) = (%q, %t), want (%q, %t)",
					test.ref, test.remote, got, ok, test.want, test.wantOK)
			}
		})
	}
}

func TestTagFromRef(t *testing.T) {
	t.Parallel()

	if got, ok := tagFromRef("refs/tags/v1.0.0"); got != "v1.0.0" || !ok {
		t.Fatalf("tagFromRef(refs/tags/v1.0.0) = (%q, %t)", got, ok)
	}
	if _, ok := tagFromRef("refs/heads/main"); ok {
		t.Fatal("a branch was read as a tag")
	}
	if _, ok := tagFromRef("refs/tags/"); ok {
		t.Fatal("an empty tag name was accepted")
	}
}

func TestDefaultBranchFromTheRemoteHead(t *testing.T) {
	t.Parallel()

	refs := []execgit.Ref{
		{Name: "refs/heads/feature"},
		{Name: "refs/remotes/origin/HEAD", Target: "refs/remotes/origin/main"},
		{Name: "refs/remotes/origin/main"},
	}

	if got := defaultBranchFrom(refs, "origin"); got != "main" {
		t.Fatalf("defaultBranchFrom = %q, want main", got)
	}
	if got := defaultBranchFrom(refs, ""); got != "" {
		t.Fatalf("defaultBranchFrom with no remote = %q, want empty", got)
	}
	if got := defaultBranchFrom(refs, "fork"); got != "" {
		t.Fatalf("another remote's HEAD answered: %q", got)
	}
}

// TestRankBranchesPutsTheBranchYouAreOnFirst pins the order Result.KeepOrder
// exists to protect.
func TestRankBranchesPutsTheBranchYouAreOnFirst(t *testing.T) {
	t.Parallel()

	// The branch that is checked out comes last and the default one second,
	// so neither lands in place by having been listed there.
	refs := []execgit.Ref{
		{Name: "refs/remotes/origin/release"},
		{Name: "refs/remotes/origin/HEAD", Target: "refs/remotes/origin/main"},
		{Name: "refs/heads/main"},
		// The same branch twice, once local and once remote-tracking. A shell
		// offering it twice is a shell offering a duplicate.
		{Name: "refs/remotes/origin/main"},
		{Name: "refs/tags/v1.0.0"},
		{Name: "refs/heads/spike", Checked: true},
	}

	candidates := rankBranches(refs, "origin")

	wantValues := []string{"spike", "main", "release"}
	gotValues := valuesOf(candidates)
	if strings.Join(gotValues, ",") != strings.Join(wantValues, ",") {
		t.Fatalf("rankBranches = %v, want %v", gotValues, wantValues)
	}

	if description := descriptionOf(candidates, "main"); description != "default branch" {
		t.Errorf("the default branch was described as %q", description)
	}
	if description := descriptionOf(candidates, "spike"); description != "" {
		t.Errorf("an ordinary branch carried the description %q", description)
	}
}

func TestRankBranchesHoistsTheDefaultWhenNothingIsCheckedOut(t *testing.T) {
	t.Parallel()

	refs := []execgit.Ref{
		{Name: "refs/heads/spike"},
		{Name: "refs/remotes/origin/HEAD", Target: "refs/remotes/origin/main"},
		{Name: "refs/heads/main"},
		{Name: "refs/heads/zeta"},
	}

	if got := valuesOf(rankBranches(refs, "origin")); strings.Join(got, ",") != "main,spike,zeta" {
		t.Fatalf("rankBranches on a detached HEAD = %v, want [main spike zeta]", got)
	}
}

func TestHoistLeavesTheRestAlone(t *testing.T) {
	t.Parallel()

	candidates := []Candidate{{Value: "a"}, {Value: "b"}, {Value: "c"}}

	if got := valuesOf(hoist(candidates, "c")); strings.Join(got, ",") != "c,a,b" {
		t.Fatalf("hoist(c) = %v", got)
	}
	if got := valuesOf(hoist(candidates, "a")); strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("hoist of the first candidate reordered it: %v", got)
	}
	if got := valuesOf(hoist(candidates, "absent")); strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("hoist of an absent value changed the order: %v", got)
	}
	if got := valuesOf(hoist(candidates, "")); strings.Join(got, ",") != "a,b,c" {
		t.Fatalf("hoist of nothing changed the order: %v", got)
	}
}

// TestForkScopeMovesOnlyTheSourceRef is the decision behind `bb pr create
// --from-repo`.
func TestForkScopeMovesOnlyTheSourceRef(t *testing.T) {
	t.Parallel()

	target := Repository{Host: "https://bitbucket.example.com", ProjectKey: "PROJ", Slug: "service", RemoteName: "origin"}

	tests := []struct {
		name     string
		flag     string
		fromRepo string
		want     Repository
		wantMove bool
	}{
		{
			name:     "--from-ref follows --from-repo",
			flag:     "from-ref",
			fromRepo: "~alice/service",
			want:     Repository{Host: target.Host, ProjectKey: "~alice", Slug: "service"},
			wantMove: true,
		},
		{
			name:     "--to-ref stays with the target",
			flag:     "to-ref",
			fromRepo: "~alice/service",
			want:     target,
		},
		{
			name:     "a positional stays with the target",
			flag:     "",
			fromRepo: "~alice/service",
			want:     target,
		},
		{
			name:     "no --from-repo is no move",
			flag:     "from-ref",
			fromRepo: "",
			want:     target,
		},
		{
			// Naming the target explicitly is the same-repository case
			// spelled out. Treating it as a move would discard the checkout
			// those branches could have been read from.
			name:     "--from-repo naming the target is not a move",
			flag:     "from-ref",
			fromRepo: "proj/SERVICE",
			want:     target,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, moved, err := forkScope(test.flag, test.fromRepo, target)
			if err != nil {
				t.Fatalf("forkScope returned %v", err)
			}
			if moved != test.wantMove {
				t.Fatalf("forkScope moved = %t, want %t", moved, test.wantMove)
			}
			if got != test.want {
				t.Fatalf("forkScope = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestForkScopeRefusesASelectorItCannotParse(t *testing.T) {
	t.Parallel()

	if _, _, err := forkScope("from-ref", "not-a-selector", Repository{ProjectKey: "PROJ", Slug: "service"}); err == nil {
		t.Fatal("an unparsable --from-repo completed against the target repository instead of failing")
	}
}

func TestDescriptions(t *testing.T) {
	t.Parallel()

	if got := describeCommit("fix the thing\n\nwith a body nobody reads", "Ada Lovelace"); got != "fix the thing (Ada Lovelace)" {
		t.Errorf("describeCommit = %q", got)
	}
	if got := describeCommit("fix the thing", ""); got != "fix the thing" {
		t.Errorf("describeCommit without an author = %q", got)
	}
	if got := describeTag("0a943a29376f2336b78312d99e65da17048951db"); got != "at 0a943a2" {
		t.Errorf("describeTag = %q", got)
	}
	if got := describeTag(""); got != "tag" {
		t.Errorf("describeTag of an unknown commit = %q", got)
	}
	if got := describeBranch("main", "main"); got != "default branch" {
		t.Errorf("describeBranch of the default = %q", got)
	}
	if got := describeBranch("main", ""); got != "" {
		t.Errorf("describeBranch with no default known = %q", got)
	}
}

func TestNameOfPrefersTheDisplayID(t *testing.T) {
	t.Parallel()

	display, id := "main", "refs/heads/main"
	if got := nameOf(&display, &id); got != "main" {
		t.Errorf("nameOf = %q", got)
	}
	if got := nameOf(nil, &id); got != "main" {
		t.Errorf("nameOf without a display id = %q", got)
	}

	tagID := "refs/tags/v1.0.0"
	if got := nameOf(nil, &tagID); got != "v1.0.0" {
		t.Errorf("nameOf of a tag = %q", got)
	}
	if got := nameOf(nil, nil); got != "" {
		t.Errorf("nameOf of nothing = %q", got)
	}
}

func TestLocalCommitCandidatesSayWhichOneIsHead(t *testing.T) {
	t.Parallel()

	candidates := localCommitCandidates([]execgit.Commit{
		{ID: "abc1234", Subject: "latest", Author: "Ada"},
		{ID: "def5678", Subject: "older", Author: "Ada"},
	})

	if candidates[0].Description != "HEAD: latest (Ada)" {
		t.Errorf("the first commit was described as %q", candidates[0].Description)
	}
	if candidates[1].Description != "older (Ada)" {
		t.Errorf("a later commit was described as %q", candidates[1].Description)
	}
}

// TestLocalRefsAnswerFromTheCheckout is the local path end to end, against a
// real repository rather than a description of one.
//
// The whole point of reading the checkout is that it is the same set of
// branches Bitbucket would have listed, which only holds if the ref names,
// the remote-tracking mapping and the annotated-tag dereference are all read
// the way git writes them.
func TestLocalRefsAnswerFromTheCheckout(t *testing.T) {
	t.Parallel()

	checkout, headCommit := buildCheckout(t)

	scope := refScope{
		repository: Repository{ProjectKey: "PROJ", Slug: "service", RemoteName: "origin"},
		directory:  checkout,
	}

	ctx := context.Background()

	branches := rankBranches(localRefs(ctx, scope, headPatterns...), "origin")
	values := valuesOf(branches)
	if len(values) == 0 {
		t.Fatalf("the checkout offered no branches at all")
	}
	if values[0] != "zeta" {
		t.Errorf("the checked-out branch was not offered first: %v", values)
	}
	if len(values) < 2 || values[1] != "main" {
		t.Errorf("the default branch was not offered next: %v", values)
	}
	if !contains(values, "alpha") {
		t.Errorf("a pushed branch was not offered: %v", values)
	}
	if description := descriptionOf(branches, "main"); description != "default branch" {
		t.Errorf("the remote's default branch was described as %q", description)
	}
	if contains(values, "HEAD") {
		t.Errorf("refs/remotes/origin/HEAD was offered as a branch: %v", values)
	}

	tags := localTags(localRefs(ctx, scope, tagPatterns...))
	tagValues := valuesOf(tags)
	if !contains(tagValues, "v1.0.0") || !contains(tagValues, "v0.9.0") {
		t.Fatalf("the checkout's tags were not offered: %v", tagValues)
	}

	// An annotated tag's own object is not the commit. Reading it without the
	// dereference would describe the tag by a hash no commit has.
	wantDescription := "at " + headCommit[:shortCommitLength]
	if got := descriptionOf(tags, "v1.0.0"); got != wantDescription {
		t.Errorf("the annotated tag was described as %q, want %q", got, wantDescription)
	}
	if got := descriptionOf(tags, "v0.9.0"); got != wantDescription {
		t.Errorf("the lightweight tag was described as %q, want %q", got, wantDescription)
	}
}

func TestLocalCommitishOffersHeadAndTheNamesAroundIt(t *testing.T) {
	t.Parallel()

	checkout, _ := buildCheckout(t)

	scope := refScope{
		repository: Repository{ProjectKey: "PROJ", Slug: "service", RemoteName: "origin"},
		directory:  checkout,
	}

	candidates := localCommitish(context.Background(), scope)
	values := valuesOf(candidates)

	if !contains(values, "main") || !contains(values, "v1.0.0") {
		t.Errorf("a commit-ish slot was not offered the names that resolve to a commit: %v", values)
	}

	headDescribed := false
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate.Description, "HEAD") {
			headDescribed = true
		}
	}
	if !headDescribed {
		t.Errorf("no candidate was marked as HEAD: %v", candidates)
	}
}

// TestLocalRefsStaySilentAwayFromTheCheckout is the rule that keeps one
// repository's branches out of another's completion.
func TestLocalRefsStaySilentAwayFromTheCheckout(t *testing.T) {
	t.Parallel()

	checkout, _ := buildCheckout(t)

	// A repository the caller named rather than one this checkout implies:
	// RemoteName is empty, so scopeFor would never have set a directory.
	scope := refScope{repository: Repository{ProjectKey: "OTHER", Slug: "service"}}
	if refs := localRefs(context.Background(), scope, headPatterns...); len(refs) != 0 {
		t.Fatalf("a repository that is not this checkout was answered from it: %v", refs)
	}

	// And a directory that is not a repository fails silently rather than
	// taking the press down with it.
	empty := refScope{repository: scope.repository, directory: t.TempDir()}
	if refs := localRefs(context.Background(), empty, headPatterns...); len(refs) != 0 {
		t.Fatalf("a directory that is not a checkout answered: %v", refs)
	}

	_ = checkout
}

// buildCheckout makes a repository with a remote, two branches, two tags and a
// recorded remote HEAD, and returns the checkout and the commit main points
// at.
func buildCheckout(t *testing.T) (checkout string, headCommit string) {
	t.Helper()

	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	checkout = filepath.Join(root, "work")

	if err := os.MkdirAll(origin, 0o750); err != nil {
		t.Fatalf("create the origin directory: %v", err)
	}
	if err := os.MkdirAll(checkout, 0o750); err != nil {
		t.Fatalf("create the checkout directory: %v", err)
	}

	git(t, origin, "init", "--bare")
	git(t, checkout, "init")
	git(t, checkout, "symbolic-ref", "HEAD", "refs/heads/main")
	git(t, checkout, "config", "user.name", "bb completion test")
	git(t, checkout, "config", "user.email", "bb-completion-test@example.local")

	write(t, filepath.Join(checkout, "first.txt"), "one\n")
	git(t, checkout, "add", "first.txt")
	git(t, checkout, "commit", "-m", "first commit")

	git(t, checkout, "remote", "add", "origin", filepath.ToSlash(origin))
	git(t, checkout, "push", "-u", "origin", "main")

	headCommit = strings.TrimSpace(gitOutput(t, checkout, "rev-parse", "HEAD"))

	git(t, checkout, "tag", "-a", "v1.0.0", "-m", "release one")
	git(t, checkout, "tag", "v0.9.0")
	git(t, checkout, "push", "origin", "--tags")

	// zeta is the branch this checkout ends up on, and it is deliberately
	// neither the newest nor the first alphabetically: a ranking that did
	// nothing would leave alpha at the top under either tie-break git applies.
	git(t, checkout, "checkout", "-b", "zeta")
	write(t, filepath.Join(checkout, "second.txt"), "two\n")
	git(t, checkout, "add", "second.txt")
	git(t, checkout, "commit", "-m", "second commit")
	git(t, checkout, "push", "-u", "origin", "zeta")

	git(t, checkout, "checkout", "-b", "alpha", "main")
	write(t, filepath.Join(checkout, "third.txt"), "three\n")
	git(t, checkout, "add", "third.txt")
	git(t, checkout, "commit", "-m", "third commit")
	git(t, checkout, "push", "-u", "origin", "alpha")

	git(t, checkout, "checkout", "zeta")

	// What a clone records for itself, and the only place the default branch
	// is written down locally.
	git(t, checkout, "remote", "set-head", "origin", "main")

	return checkout, headCommit
}

func git(t *testing.T, directory string, args ...string) {
	t.Helper()

	gitOutput(t, directory, args...)
}

func gitOutput(t *testing.T, directory string, args ...string) string {
	t.Helper()

	// The same scoping execgit runs with, plus the two settings a developer's
	// own configuration can otherwise impose on a commit made here.
	scoped := append([]string{"-c", "credential.helper=", "-c", "commit.gpgsign=false"}, args...)
	command := exec.Command("git", scoped...)
	command.Dir = directory
	command.Env = append(execgit.ScopeFreeEnv(), "GIT_TERMINAL_PROMPT=0")

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v\n%s", strings.Join(args, " "), directory, err, output)
	}

	return string(output)
}

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func valuesOf(candidates []Candidate) []string {
	values := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		values = append(values, candidate.Value)
	}

	return values
}

func descriptionOf(candidates []Candidate, value string) string {
	for _, candidate := range candidates {
		if candidate.Value == value {
			return candidate.Description
		}
	}

	return ""
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}
