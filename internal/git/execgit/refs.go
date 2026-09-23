package execgit

import (
	"context"
	"strconv"
	"strings"
)

// Ref is one reference in a checkout.
//
// It is not on git.Backend. Every consumer of the interface stubs it, and a
// method there would be a method each of those stubs has to grow for a
// capability only shell completion asks for; a caller that wants this declares
// the two methods it needs and takes a *Backend.
type Ref struct {
	// Name is the full reference: refs/heads/main, refs/tags/v1.0.0,
	// refs/remotes/origin/main.
	Name string
	// Target is what a symbolic reference points at, empty for an ordinary
	// one. refs/remotes/<remote>/HEAD carries the remote's default branch
	// here, which is the only place a checkout records which branch that is.
	Target string
	// Object is the abbreviated commit the reference resolves to. An
	// annotated tag is dereferenced, so this is the commit it marks rather
	// than the tag object that marks it.
	Object string
	// Checked marks the reference HEAD is on.
	Checked bool
	// Subject is the first line of the message of what the reference names:
	// the commit's for a branch, the tag's own for an annotated tag.
	Subject string
}

// Commit is one commit in a checkout, as git abbreviates it.
type Commit struct {
	// ID is the abbreviated hash, which is what a person types and what
	// Bitbucket resolves.
	ID      string
	Subject string
	Author  string
}

// unitSeparator divides the fields of a log record.
//
// A tab would do for references, which cannot contain one, but a commit
// subject can contain anything; ASCII 31 is the separator that exists for
// this and is the one byte neither a name nor a subject carries.
const unitSeparator = "\x1f"

// ListRefs reads a checkout's references, most recently written first.
//
// Ordered by creatordate rather than committerdate because the set is mixed:
// creatordate is the commit's date for a branch and the tag's own date for an
// annotated tag, where committerdate is empty for the second and would sort
// every annotated tag to one end.
//
// Fields are tab-separated because a reference name cannot contain a tab --
// git refuses control characters in one -- and records are newline-separated
// for the same reason. The subject can hold a tab, so it is the last field and
// takes the rest of the line; it cannot hold a newline, because git folds the
// first paragraph of a message onto one line to make it.
func (backend *Backend) ListRefs(ctx context.Context, repositoryDirectory string, limit int, patterns ...string) ([]Ref, error) {
	args := []string{
		"for-each-ref",
		"--sort=-creatordate",
		"--format=%(refname)\t%(HEAD)\t%(symref)\t%(objectname:short)\t%(*objectname:short)\t%(contents:subject)",
	}
	if limit > 0 {
		args = append(args, "--count="+strconv.Itoa(limit))
	}
	args = append(args, patterns...)

	result, err := backend.run(ctx, runOptions{cwd: strings.TrimSpace(repositoryDirectory), args: args})
	if err != nil {
		return nil, err
	}

	refs := make([]Ref, 0, 32)
	for _, line := range strings.Split(result.stdout, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		name, rest, _ := strings.Cut(line, "\t")
		head, rest := cut(rest)
		target, rest := cut(rest)
		object, rest := cut(rest)
		dereferenced, subject := cut(rest)

		// The dereferenced field is empty for everything but an annotated
		// tag, where it is the commit rather than the tag object.
		if strings.TrimSpace(dereferenced) != "" {
			object = dereferenced
		}

		refs = append(refs, Ref{
			Name:    strings.TrimSpace(name),
			Target:  strings.TrimSpace(target),
			Object:  strings.TrimSpace(object),
			Checked: strings.TrimSpace(head) == "*",
			Subject: strings.TrimSpace(subject),
		})
	}

	return refs, nil
}

// cut takes the next tab-separated field and returns the remainder, so a
// record with a field missing at the end reads as empty rather than as a
// short slice.
func cut(record string) (field, rest string) {
	field, rest, _ = strings.Cut(record, "\t")

	return field, rest
}

// ListCommits reads the checkout's history from HEAD, newest first.
//
// The first record is the commit HEAD is on, which is the one a caller
// completing a commit-ish most often wants.
func (backend *Backend) ListCommits(ctx context.Context, repositoryDirectory string, limit int) ([]Commit, error) {
	if limit <= 0 {
		limit = 20
	}

	result, err := backend.run(ctx, runOptions{
		cwd: strings.TrimSpace(repositoryDirectory),
		args: []string{
			"log",
			"--max-count=" + strconv.Itoa(limit),
			"--format=%h" + unitSeparator + "%s" + unitSeparator + "%an",
		},
	})
	if err != nil {
		return nil, err
	}

	commits := make([]Commit, 0, limit)
	for _, line := range strings.Split(result.stdout, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		id, rest, _ := strings.Cut(line, unitSeparator)
		subject, author, _ := strings.Cut(rest, unitSeparator)

		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}

		commits = append(commits, Commit{
			ID:      id,
			Subject: strings.TrimSpace(subject),
			Author:  strings.TrimSpace(author),
		})
	}

	return commits, nil
}
