package conventionalcommits

import (
	"fmt"
	"testing"
)

// buildCommits makes commits from (subject, body) pairs so a bump case reads as
// the list of commits it is about.
func buildCommits(pairs ...[2]string) []Commit {
	built := make([]Commit, 0, len(pairs))
	for index, pair := range pairs {
		built = append(built, Classify(fmt.Sprintf("%040x", index), pair[0], pair[1]))
	}

	return built
}

func subjects(list ...string) [][2]string {
	pairs := make([][2]string, 0, len(list))
	for _, subject := range list {
		pairs = append(pairs, [2]string{subject, ""})
	}

	return pairs
}

func TestClassifyReadsTypeScopeAndDescription(t *testing.T) {
	t.Parallel()

	commit := Classify("abc", "feat(auth): add a thing", "")
	if !commit.Conventional {
		t.Fatalf("expected a conventional commit, got %+v", commit)
	}
	if commit.Type != "feat" {
		t.Errorf("type: got %q, want feat", commit.Type)
	}
	if commit.Scope == nil || *commit.Scope != "auth" {
		t.Errorf("scope: got %v, want auth", commit.Scope)
	}
	if commit.Description != "add a thing" {
		t.Errorf("description: got %q, want %q", commit.Description, "add a thing")
	}
	if commit.Breaking {
		t.Error("expected not breaking")
	}
}

func TestClassifyScopeIsOptional(t *testing.T) {
	t.Parallel()

	commit := Classify("abc", "fix: a thing", "")
	if commit.Type != "fix" {
		t.Errorf("type: got %q, want fix", commit.Type)
	}
	if commit.Scope != nil {
		t.Errorf("scope: got %q, want none", *commit.Scope)
	}
}

func TestClassifyBangMarksBreakingWithAndWithoutScope(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{"feat!: a thing", "feat(api)!: a thing"} {
		if !Classify("a", subject, "").Breaking {
			t.Errorf("%q: expected breaking", subject)
		}
	}
}

func TestClassifyUnconventionalSubjectIsOtherAndNeverConventional(t *testing.T) {
	t.Parallel()

	commit := Classify("abc", "Revert to a project per test", "")
	if commit.Conventional {
		t.Error("expected not conventional")
	}
	if commit.Type != "other" {
		t.Errorf("type: got %q, want other", commit.Type)
	}
	if commit.Description != "Revert to a project per test" {
		t.Errorf("description: got %q", commit.Description)
	}
}

func TestBreakingFooterBothSpellingsCount(t *testing.T) {
	t.Parallel()

	for _, body := range []string{"BREAKING CHANGE: gone", "BREAKING-CHANGE: gone"} {
		if !Classify("a", "fix: x", body).Breaking {
			t.Errorf("%q: expected breaking", body)
		}
	}
}

func TestBreakingFooterMayFollowABody(t *testing.T) {
	t.Parallel()

	body := "Some prose about the change.\n\nBREAKING CHANGE: the flag is gone"
	if !Classify("a", "fix: x", body).Breaking {
		t.Error("expected breaking")
	}
}

// The reason this package exists. Bump detection used to ask whether the body
// contained the string anywhere, so a sentence mentioning it cut a major
// release while the changelog -- which matched a footer anchored to a line --
// did not list it as breaking. A footer is what the specification describes,
// and it is what the notes have reported.
func TestBreakingFooterMidLineMentionIsNotAFooter(t *testing.T) {
	t.Parallel()

	body := "This is deliberately not a BREAKING CHANGE: it only looks like one."
	if Classify("a", "fix: x", body).Breaking {
		t.Error("expected a mid-line mention not to count")
	}
}

func TestBreakingFooterAnUnconventionalSubjectStillCarriesAFooter(t *testing.T) {
	t.Parallel()

	commit := Classify("a", "Merge branch whatever", "BREAKING CHANGE: gone")
	if commit.Conventional {
		t.Error("expected not conventional")
	}
	if !commit.Breaking {
		t.Error("expected breaking")
	}
}

// A footer wrapped at the usual commit width has to arrive whole. Reading only
// its first line is what once truncated five of six breaking changes in a
// release, one of them mid-word, and those notes are the migration
// instructions.
func TestBreakingFooterIsCollectedToTheBlankLineAndCollapsed(t *testing.T) {
	t.Parallel()

	body := "Some prose.\n\n" +
		"BREAKING CHANGE: --token is gone because a flag value reaches the\n" +
		"process table. Read the token from stdin instead.\n" +
		"\n" +
		"Refs: #123"

	commit := Classify("a", "feat: x", body)
	want := "--token is gone because a flag value reaches the process table. Read the token from stdin instead."
	if commit.BreakingNote != want {
		t.Errorf("note:\n got %q\nwant %q", commit.BreakingNote, want)
	}
}

func TestBreakingFooterNoteIsEmptyWithoutOne(t *testing.T) {
	t.Parallel()

	if note := Classify("a", "feat!: x", "").BreakingNote; note != "" {
		t.Errorf("note: got %q, want empty", note)
	}
}

func TestBumpLevelNothingReleasingIsNone(t *testing.T) {
	t.Parallel()

	got := BumpLevel(buildCommits(subjects("chore: x", "docs: y", "test: z", "refactor: w")...))
	if got != BumpNone {
		t.Errorf("got %v, want BumpNone", got)
	}
}

func TestBumpLevelFixPerfAndRevertArePatch(t *testing.T) {
	t.Parallel()

	for _, subject := range []string{"fix: x", "perf: x", "revert: x"} {
		if got := BumpLevel(buildCommits([2]string{subject, ""})); got != BumpPatch {
			t.Errorf("%q: got %v, want BumpPatch", subject, got)
		}
	}
}

func TestBumpLevelFeatIsMinor(t *testing.T) {
	t.Parallel()

	if got := BumpLevel(buildCommits([2]string{"feat: x", ""})); got != BumpMinor {
		t.Errorf("got %v, want BumpMinor", got)
	}
}

func TestBumpLevelBreakingIsMajorHoweverItIsSpelled(t *testing.T) {
	t.Parallel()

	if got := BumpLevel(buildCommits([2]string{"feat!: x", ""})); got != BumpMajor {
		t.Errorf("bang: got %v, want BumpMajor", got)
	}
	if got := BumpLevel(buildCommits([2]string{"fix: x", "BREAKING CHANGE: gone"})); got != BumpMajor {
		t.Errorf("footer: got %v, want BumpMajor", got)
	}
}

func TestBumpLevelTheHighestWins(t *testing.T) {
	t.Parallel()

	if got := BumpLevel(buildCommits(subjects("chore: a", "fix: b", "feat: c")...)); got != BumpMinor {
		t.Errorf("got %v, want BumpMinor", got)
	}
	if got := BumpLevel(buildCommits(subjects("chore: a", "feat: b", "fix!: c")...)); got != BumpMajor {
		t.Errorf("got %v, want BumpMajor", got)
	}
}

// Not even with a breaking footer: a subject the parser cannot read has no type
// to release under, and the non-conventional revert commit on the v4 branch is
// exactly this shape.
func TestBumpLevelAnUnconventionalSubjectDoesNotRelease(t *testing.T) {
	t.Parallel()

	got := BumpLevel(buildCommits([2]string{"Revert something", "BREAKING CHANGE: gone"}))
	if got != BumpNone {
		t.Errorf("got %v, want BumpNone", got)
	}
}

func TestParseLogReadsTheLogFormatItDeclares(t *testing.T) {
	t.Parallel()

	raw := "1111111\x1ffeat(x): first\x1f\x1e" +
		"2222222\x1ffix: second\x1fBREAKING CHANGE: gone\x1e"

	parsed := ParseLog(raw)
	if len(parsed) != 2 {
		t.Fatalf("got %d commits, want 2", len(parsed))
	}
	if parsed[0].SHA != "1111111" || parsed[1].SHA != "2222222" {
		t.Errorf("shas: got %q and %q", parsed[0].SHA, parsed[1].SHA)
	}
	if parsed[0].Type != "feat" {
		t.Errorf("type: got %q, want feat", parsed[0].Type)
	}
	if !parsed[1].Breaking {
		t.Error("expected the second commit to be breaking")
	}
}

// git writes a newline between records, so every record after the first arrives
// with one in front of its sha.
func TestParseLogToleratesTheNewlineGitPutsBetweenRecords(t *testing.T) {
	t.Parallel()

	raw := "1111111\x1ffeat: first\x1f\x1e\n2222222\x1ffix: second\x1f\x1e"

	parsed := ParseLog(raw)
	if len(parsed) != 2 {
		t.Fatalf("got %d commits, want 2", len(parsed))
	}
	if parsed[1].SHA != "2222222" {
		t.Errorf("sha: got %q, want 2222222", parsed[1].SHA)
	}
}

func TestParseLogIgnoresEmptyRecords(t *testing.T) {
	t.Parallel()

	if got := ParseLog(""); len(got) != 0 {
		t.Errorf("empty: got %d commits, want 0", len(got))
	}
	if got := ParseLog("\x1e\x1e"); len(got) != 0 {
		t.Errorf("separators only: got %d commits, want 0", len(got))
	}
}

func TestHasConventionalDistinguishesNoneFromNonReleasing(t *testing.T) {
	t.Parallel()

	if HasConventional(buildCommits([2]string{"Merge pull request #1", ""})) {
		t.Error("expected an unconventional subject not to count")
	}
	if !HasConventional(buildCommits([2]string{"chore: x", ""})) {
		t.Error("expected chore to count as conventional")
	}
}

func TestShortSHAAbbreviatesAndToleratesShortInput(t *testing.T) {
	t.Parallel()

	if got := (Commit{SHA: "0123456789abcdef"}).ShortSHA(); got != "0123456" {
		t.Errorf("got %q, want 0123456", got)
	}
	if got := (Commit{SHA: "abc"}).ShortSHA(); got != "abc" {
		t.Errorf("got %q, want abc", got)
	}
}
