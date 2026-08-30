package main

import "testing"

// TestSkipDirectoryExcludesForeignCheckouts pins the rule that this tool only
// reads the source of the checkout it is run in.
//
// It walks the working tree rather than a fixed list of source roots, so
// anything on disk that looks like Go is a candidate. Agent tooling keeps full
// checkouts of other branches under .claude/worktrees/ -- gitignored, but still
// present and still full of .go files. Without the dot-directory rule the tool
// counted their call sites as this branch's, reported the committed artifact as
// 468 operations out of date on a clean main, and the artifact was regenerated
// wrongly on the strength of it.
func TestSkipDirectoryExcludesForeignCheckouts(t *testing.T) {
	cases := []struct {
		path string
		name string
		want bool
	}{
		// The case this exists for.
		{path: ".claude", name: ".claude", want: true},
		{path: ".claude/worktrees", name: "worktrees", want: false},

		{path: ".git", name: ".git", want: true},
		{path: ".idea", name: ".idea", want: true},
		{path: ".tmp", name: ".tmp", want: true},
		{path: "docs", name: "docs", want: true},
		{path: "node_modules", name: "node_modules", want: true},
		{path: "vendor", name: "vendor", want: true},

		// The walk root arrives as "." and must not skip the whole tree.
		{path: ".", name: ".", want: false},

		// Real source stays in scope.
		{path: "internal", name: "internal", want: false},
		{path: "internal/openapi", name: "openapi", want: false},
		{path: "cmd", name: "cmd", want: false},
		{path: "tools", name: "tools", want: false},
		{path: "tests", name: "tests", want: false},
	}

	for _, testCase := range cases {
		if got := skipDirectory(testCase.path, testCase.name); got != testCase.want {
			t.Errorf("skipDirectory(%q, %q) = %v, want %v", testCase.path, testCase.name, got, testCase.want)
		}
	}
}
