package main

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/docsite"
)

// retiredSlug is the name the project carried before the rename, read from the
// package that owns it so this file does not become another copy.
//
// The dead Pages host is derived from it, so the two cannot disagree about what
// is retired. GitHub redirects a renamed repository, so github.com links
// survive, but it does not redirect the renamed project's Pages site: every link
// to the former host is a hard 404, and they lasted long enough to reach the
// published output schemas, where the dead host became the $id of every machine
// contract bb publishes.
//
// The host rule catches the URL. The slug rule catches the name wherever else it
// surfaces -- a heading, a module path, a test fixture, a workflow comment --
// because that is how the URL kept coming back: the retired name sat in front of
// contributors and agents, who then repeated it. It reached the AGENT NOTICE,
// the installation docs and the Codecov upload slug, and the last of those
// discarded coverage reports for two months before anyone noticed.
//
// There is no allowlist, because a rule with holes in it is how the name
// survived this long. internal/docsite declares it and this rule reads it from
// there, so those two directories are skipped rather than excused -- a rule
// that fails on its own definition can never pass.
const retiredSlug = docsite.RetiredSlug

const retiredPagesHost = "https://vriesdemichael.github.io/" + retiredSlug

// retiredHostExtensions are the file types the retired name can hide in.
// Binaries and generated Go clients are not among them; the vendored OpenAPI
// spec is far too large to scan for a string that could never appear in it.
var retiredHostExtensions = map[string]bool{
	".go":   true,
	".json": true,
	// go.mod is where the name lived longest, so the rule that exists to keep it
	// gone has to be able to see it.
	".mod":  true,
	".md":   true,
	".ps1":  true,
	".py":   true,
	".sh":   true,
	".toml": true,
	".txt":  true,
	".yaml": true,
	".yml":  true,
}

// retiredHostSkippedDirs are directories the rule does not walk.
//
// internal/docsite is skipped because it declares the retired name, and
// tools/docs-lint because its test writes examples containing it. A rule that
// fails on its own definition can never pass. In the tracked tree those two are
// the whole of it: everywhere else the name is simply gone.
//
// The rest is build output git already ignores, listed here because the rule
// walks the filesystem rather than the index. A coverage profile records import
// paths, so one written before the module was renamed still carries the old one;
// a docs virtualenv embeds the absolute path of whatever directory the checkout
// happens to sit in. Neither is a documentation problem, and a lint that fails
// on a developer's own leftovers is a lint they turn off.
var retiredHostSkippedDirs = map[string]bool{
	".git":             true,
	".claude":          true,
	".tmp":             true,
	".venv":            true,
	"node_modules":     true,
	"vendor":           true,
	"tools/docs-lint":  true,
	"internal/docsite": true,
}

// lintRetiredHost reports every reference to the retired Pages host, and to the
// retired name on its own, under root.
func lintRetiredHost(root string) ([]finding, error) {
	var findings []finding

	walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			// A tree that disappears mid-walk is a working copy being edited,
			// not a documentation problem.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}

		relative := filepath.ToSlash(mustRelative(root, path))

		if entry.IsDir() {
			if retiredHostSkippedDirs[entry.Name()] || retiredHostSkippedDirs[relative] {
				return fs.SkipDir
			}
			return nil
		}

		if !retiredHostExtensions[strings.ToLower(filepath.Ext(entry.Name()))] {
			return nil
		}

		fileFindings, scanErr := scanForRetiredHost(path, relative)
		if scanErr != nil {
			return scanErr
		}
		findings = append(findings, fileFindings...)

		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	return findings, nil
}

func scanForRetiredHost(path, displayPath string) ([]finding, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var findings []finding

	scanner := bufio.NewScanner(file)
	// Generated schemas put a whole document on one line often enough that the
	// default 64KiB limit would stop the scan without saying so.
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()

		// The host is reported on its own terms when it is there, because the
		// remedy differs: a dead URL has a working replacement, while a stray
		// slug has to be read in context. A line carrying the host also carries
		// the slug, so reporting both would name one mistake twice.
		switch {
		case strings.Contains(text, retiredPagesHost):
			findings = append(findings, finding{
				File:    displayPath,
				Line:    line,
				Command: retiredPagesHost,
				Problem: "References the retired Pages host. GitHub redirects the renamed repository but not its Pages site, so this URL is a hard 404. Use https://vriesdemichael.github.io/bitbucket-data-center-cli.",
			})
		case strings.Contains(text, retiredSlug):
			findings = append(findings, finding{
				File:    displayPath,
				Line:    line,
				Command: retiredSlug,
				Problem: "Names the slug the project carried before it was renamed. It is retired everywhere -- module path, repository, Pages site -- and every place it has resurfaced was copied from another place it had already resurfaced. Use bitbucket-data-center-cli.",
			})
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return findings, nil
}

func mustRelative(root, path string) string {
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return relative
}
