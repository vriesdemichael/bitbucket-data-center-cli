package main

import (
	"os"
	"path/filepath"
	"testing"
)

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
	t.Parallel()

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

// TestBareClientCallsAreRecordedWithoutFalsePositives covers the third call
// shape and the reason it is anchored on the receiver.
//
// A raw client method with no suffix -- client.GetComments(...) -- has to be
// recorded, or an operation drops out of the report and a spec bump can
// retarget it silently. But the operation names are ordinary words, so
// matching the name alone also records this project's own service methods,
// putting operations in the report that nothing calls.
func TestBareClientCallsAreRecordedWithoutFalsePositives(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	source := `package example

func (service *Service) List() {
	service.client.GetComments(ctx, "P", "r", "abc", nil)
	service.client.UpdateCommentWithResponse(ctx, "P", "r", "abc", "1", body)
	service.repository.Update(ctx)
	other.Create(ctx)
}
`
	if err := os.WriteFile(filepath.Join(root, "example.go"), []byte(source), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}

	declared := map[string]operation{
		"GetComments":   {Method: "GET", Path: "/comments"},
		"UpdateComment": {Method: "PUT", Path: "/comments/%s"},
		"Update":        {Method: "PUT", Path: "/repos/%s"},
		"Create":        {Method: "POST", Path: "/repos"},
	}

	called, err := collectCalledOperations(root, filepath.Join(root, "absent.go"), declared)
	if err != nil {
		t.Fatalf("collect: %v", err)
	}

	for _, name := range []string{"GetComments", "UpdateComment"} {
		if !called[name] {
			t.Errorf("%q is called through the client and was not recorded", name)
		}
	}
	// Update and Create are generated operation names too, and this file calls
	// neither of them on the client.
	for _, name := range []string{"Update", "Create"} {
		if called[name] {
			t.Errorf("%q was recorded from a call on something other than the client", name)
		}
	}
}
