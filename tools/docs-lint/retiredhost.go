package main

import (
	"bufio"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// The project was renamed from bitbucket-server-cli. GitHub redirects a renamed
// repository, so github.com links survive, but it does not redirect the renamed
// project's Pages site: every link to the former host is a hard 404.
//
// They lasted long enough to reach the published output schemas, where the dead
// host became the $id of every machine contract bb publishes. This rule exists
// so that cannot happen a second time.
const retiredPagesHost = "https://vriesdemichael.github.io/bitbucket-server-cli"

// retiredHostExtensions are the file types a URL can hide in. Binaries and
// generated Go clients are not among them; the vendored OpenAPI spec is far too
// large to scan for a string that could never appear in it.
var retiredHostExtensions = map[string]bool{
	".go":   true,
	".json": true,
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
// tools/docs-lint is skipped because the rule is stated here, in this file and
// its test, and a rule that fails on its own definition can never pass.
var retiredHostSkippedDirs = map[string]bool{
	".git":            true,
	".claude":         true,
	"node_modules":    true,
	"vendor":          true,
	"tools/docs-lint": true,
}

// lintRetiredHost reports every reference to the retired Pages host under root.
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
		if !strings.Contains(scanner.Text(), retiredPagesHost) {
			continue
		}
		findings = append(findings, finding{
			File:    displayPath,
			Line:    line,
			Command: retiredPagesHost,
			Problem: "References the retired Pages host. GitHub redirects the renamed repository but not its Pages site, so this URL is a hard 404. Use https://vriesdemichael.github.io/bitbucket-data-center-cli.",
		})
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
