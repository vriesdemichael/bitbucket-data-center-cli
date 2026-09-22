package execgit

import (
	"context"
	"strings"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TreeEntry is one entry of one directory in a checkout.
//
// Like Ref it is not on git.Backend, for the reason given there: every
// consumer of that interface would have to stub a method only shell
// completion asks for.
type TreeEntry struct {
	// Path is the entry's path from the repository root, which is what git
	// prints -- listing internal/ gives internal/cli, not cli.
	Path string
	// Directory marks a tree. A submodule is a commit entry rather than a
	// tree: it is a path that exists, with nothing underneath it in this
	// repository, so it is not one.
	Directory bool
}

// recordSeparator divides ls-tree's records under -z.
//
// -z rather than the default, because without it git quotes any name holding
// a space, a quote or a non-ASCII byte -- and a quoted name completes to a
// path that does not exist.
const recordSeparator = "\x00"

// ListTree reads one directory of a checkout at a commit-ish.
//
// One directory rather than the tree under it: `git ls-tree <ref> dir/` lists
// the entries of dir alone, where -r would walk everything beneath it. A
// repository of any size has more paths than a shell can show, and the
// directory being typed is the only part of it a press is asking about.
//
// An empty directory means the repository root.
func (backend *Backend) ListTree(
	ctx context.Context,
	repositoryDirectory string,
	ref string,
	directory string,
	limit int,
) ([]TreeEntry, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, apperrors.New(apperrors.KindValidation, "a tree listing needs a commit-ish", nil)
	}
	// A ref is a positional argument here, and git reads a leading dash as an
	// option however the caller meant it.
	if strings.HasPrefix(ref, "-") {
		return nil, apperrors.New(apperrors.KindValidation, "a commit-ish cannot start with a dash", nil)
	}

	args := []string{"ls-tree", "-z", ref, "--"}
	// The trailing slash is what asks for the directory's contents; without
	// it git prints the directory entry itself.
	if trimmed := strings.Trim(strings.TrimSpace(directory), "/"); trimmed != "" {
		args = append(args, trimmed+"/")
	}

	result, err := backend.run(ctx, runOptions{cwd: strings.TrimSpace(repositoryDirectory), args: args})
	if err != nil {
		return nil, err
	}

	entries := make([]TreeEntry, 0, 32)
	for _, record := range strings.Split(result.stdout, recordSeparator) {
		if strings.TrimSpace(record) == "" {
			continue
		}

		// <mode> SP <type> SP <object> TAB <path>, and a path cannot hold a
		// tab: git refuses a control character in a file name.
		header, path, found := strings.Cut(record, "\t")
		if !found || strings.TrimSpace(path) == "" {
			continue
		}

		fields := strings.Fields(header)
		if len(fields) < 2 {
			continue
		}

		entries = append(entries, TreeEntry{Path: path, Directory: fields[1] == "tree"})

		if limit > 0 && len(entries) == limit {
			break
		}
	}

	return entries, nil
}
