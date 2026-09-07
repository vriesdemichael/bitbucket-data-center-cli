// Package conventionalcommits reads commit messages the way the release
// machinery reads them, in one place.
//
// Two things have to agree about a commit: the release workflow, which decides
// from it whether a release is cut and how the version moves, and the
// release-flow gate, which refuses a breaking change on a pull request into
// main. Disagreement fails in the one direction the gate exists to prevent --
// the gate saying "not breaking, allow it" while the releaser says "breaking,
// cut a major" -- so they read this package and not their own copies (ADR-065).
//
// They did not, before this existed. The bump detection asked whether the body
// contained the string "BREAKING CHANGE:" anywhere; the changelog matched a
// footer anchored to the start of a line. A body mentioning the phrase
// mid-sentence therefore cut a major release and was not listed as a breaking
// change in the notes it cut. The line-anchored form is kept, because a footer
// is what the Conventional Commits specification describes and what the release
// notes have always reported.
package conventionalcommits

import (
	"regexp"
	"strings"
)

// subjectPattern is the subject line: type, optional (scope), optional !
// marker, description.
var subjectPattern = regexp.MustCompile(`^([a-z]+)(?:\(([^)]+)\))?(!)?: (.+)$`)

// footerPattern matches the opening line of a breaking-change footer. The rest
// of the footer is collected by reading on to the next blank line rather than
// by regexp: a footer wrapped at the usual commit width spans several lines,
// and a pattern that stopped at the first newline is what once truncated five
// of six breaking changes in a release -- one of them mid-word. Since
// CHANGELOG.md only points at the generated notes, that truncation was the
// migration instructions.
var footerPattern = regexp.MustCompile(`^BREAKING(?:-| )CHANGE:[ \t]*(.*)$`)

// releasingPatchTypes are the types that cut a patch release on their own.
//
// Only user-visible change releases. Every valid Conventional Commit type used
// to cut one, which produced 95 releases in six months -- a rate adopters
// running binaries through change approval cannot consume, and which left the
// version number carrying no signal about whether anything changed for them.
// ci/chore/docs/style/refactor/test/build accumulate and ship with the next
// feat or fix.
var releasingPatchTypes = map[string]bool{"fix": true, "perf": true, "revert": true}

// Bump is how far the version moves.
type Bump int

const (
	BumpNone Bump = iota
	BumpPatch
	BumpMinor
	BumpMajor
)

// LogFormat is what git is asked to print, and what ParseLog reads. Field and
// record separators rather than newlines, because a commit body contains
// newlines and a subject can contain almost anything.
const LogFormat = "%H%x1f%s%x1f%b%x1e"

const (
	fieldSep  = "\x1f"
	recordSep = "\x1e"
)

// LogArgs is the git invocation whose output ParseLog reads.
func LogArgs(rangeSpec string) []string {
	args := []string{"log", "--no-merges", "--pretty=format:" + LogFormat}
	if rangeSpec != "" {
		args = append(args, rangeSpec)
	}

	return args
}

// Commit is one commit, as the release machinery understands it.
type Commit struct {
	SHA     string
	Subject string
	Body    string

	// Type is "other" when the subject is not a Conventional Commit;
	// Conventional says which it was, because the two are treated differently
	// -- an unconventional subject never contributes to the version but can
	// still carry a breaking footer into the notes.
	Type string
	// Scope is nil when the subject carried no (scope).
	Scope        *string
	Description  string
	Breaking     bool
	Conventional bool
	// BreakingNote is the footer's text collapsed onto one line, empty when
	// there is no footer. Collapsed here because every reader puts it in a
	// Markdown list item, where the commit's own hard wrapping would break the
	// bullet.
	BreakingNote string
}

// ShortSHA is the abbreviation used in release notes.
func (c Commit) ShortSHA() string {
	if len(c.SHA) < 7 {
		return c.SHA
	}

	return c.SHA[:7]
}

// breakingFooter returns the footer's collapsed text and whether one is
// present. It reads from the footer's opening line to the next blank line, so a
// wrapped footer arrives whole.
func breakingFooter(body string) (string, bool) {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		match := footerPattern.FindStringSubmatch(strings.TrimRight(line, "\r"))
		if match == nil {
			continue
		}

		collected := []string{match[1]}
		for _, rest := range lines[i+1:] {
			if strings.TrimSpace(rest) == "" {
				break
			}
			collected = append(collected, rest)
		}

		return strings.Join(strings.Fields(strings.Join(collected, " ")), " "), true
	}

	return "", false
}

// Classify reads one commit the way the release workflow reads it.
func Classify(sha, subject, body string) Commit {
	note, hasFooter := breakingFooter(body)

	commit := Commit{
		SHA:          sha,
		Subject:      subject,
		Body:         body,
		Breaking:     hasFooter,
		BreakingNote: note,
	}

	match := subjectPattern.FindStringSubmatch(strings.TrimSpace(subject))
	if match == nil {
		commit.Type = "other"
		commit.Description = subject

		return commit
	}

	commit.Type = match[1]
	if match[2] != "" {
		scope := match[2]
		commit.Scope = &scope
	}
	commit.Description = match[4]
	commit.Breaking = match[3] == "!" || hasFooter
	commit.Conventional = true

	return commit
}

// ParseLog turns the output of git log --pretty=format:LogFormat into commits.
func ParseLog(raw string) []Commit {
	var commits []Commit
	for _, record := range strings.Split(raw, recordSep) {
		if strings.TrimSpace(record) == "" {
			continue
		}

		parts := strings.SplitN(record, fieldSep, 3)
		if len(parts) < 2 {
			continue
		}

		body := ""
		if len(parts) > 2 {
			body = parts[2]
		}

		commits = append(commits, Classify(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), body))
	}

	return commits
}

// BumpLevel is how far the version moves for these commits.
//
// BumpNone when nothing here releases, which is not the same as "no
// conventional commits": a run of chore and docs commits is well formed and
// still releases nothing.
func BumpLevel(commits []Commit) Bump {
	level := BumpNone
	for _, commit := range commits {
		if !commit.Conventional {
			continue
		}

		switch {
		case commit.Breaking:
			level = max(level, BumpMajor)
		case commit.Type == "feat":
			level = max(level, BumpMinor)
		case releasingPatchTypes[commit.Type]:
			level = max(level, BumpPatch)
		}
	}

	return level
}

// HasConventional reports whether anything here was a Conventional Commit at
// all.
func HasConventional(commits []Commit) bool {
	for _, commit := range commits {
		if commit.Conventional {
			return true
		}
	}

	return false
}
