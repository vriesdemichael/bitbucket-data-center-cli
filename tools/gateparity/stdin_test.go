package gateparity

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stdinUseAllowed enumerates every place standard input enters the programme,
// with what makes the caller ask for it.
//
// The invariant is deliberately about stdin being *used* rather than being
// *read*. A read is not always visible where it happens: prompt.go consumes an
// io.Reader parameter, and clone.go's readCloneToken does the same, so a check
// looking for a consuming call beside a stdin expression sees neither. What is
// always visible is the point where cmd.InOrStdin() or os.Stdin is named, and
// that is the point where the decision is actually made.
var stdinUseAllowed = map[string]string{
	filepath.Join("internal", "cli", "cmd", "api", "api.go"):            "--input -",
	filepath.Join("internal", "cli", "cmd", "repo", "misc.go"):          "--content -",
	filepath.Join("internal", "cli", "cmd", "reviewer", "reviewer.go"):  "a - positional",
	filepath.Join("internal", "cli", "cmd", "repo", "admin.go"):         "handed to the prompter, which gates it",
	filepath.Join("internal", "cli", "cmd", "repo", "clone.go"):         "handed to the token prompt, behind interactive.Detect",
	filepath.Join("internal", "cli", "cmd", "auth", "gpg.go"):           "handed to the prompter, which gates it",
	filepath.Join("internal", "cli", "prompt", "prompt.go"):             "the prompter itself",
	filepath.Join("internal", "cli", "interactive", "interactive.go"):   "asking whether it is a terminal",
	filepath.Join("internal", "cli", "cmd", "ai", "mcp.go"):             "the MCP stdio transport, where stdin is the protocol channel",
	filepath.Join("internal", "cli", "cmd", "auth", "auth.go"):          "--token-stdin and --password-stdin",
	filepath.Join("internal", "cli", "cmd", "auth", "gitcredential.go"): "the git credential protocol, which writes its request on stdin",
	filepath.Join("internal", "cli", "webhookflags", "webhookflags.go"): "--secret-stdin and --credentials-password-stdin",
}

// TestEveryUseOfStandardInputIsAccountedFor fails when a new site names
// standard input.
//
// ADR-073 permits reading it only when the caller asked, with a - argument or
// a flag value. An implicit fallback blocks forever when stdin is an open pipe
// with nothing coming, which is what a CI runner provides, and returns nothing
// under an agent -- so bb repo browse edit committed an empty file over a real
// one.
func TestEveryUseOfStandardInputIsAccountedFor(t *testing.T) {
	root := repositoryRoot(t)

	found := standardInputUsers(t, root)
	if len(found) == 0 {
		t.Fatal("found no uses of standard input at all; the scan has stopped matching")
	}

	for _, path := range found {
		if _, ok := stdinUseAllowed[path]; !ok {
			t.Errorf(
				"%s names standard input.\n"+
					"ADR-073 permits reading it only when the caller asked for it, with a -\n"+
					"argument or a flag value. Gate the read, then add this file to\n"+
					"stdinUseAllowed with the token that requests it.",
				path,
			)
		}
	}
}

// TestTheStdinScannerFindsARealRead is the sabotage, and it exercises the
// scanner rather than the map.
//
// The earlier version of this guard matched a stdin expression and a consuming
// call on one line, so the two-statement form below passed silently -- which is
// how anyone would write it without thinking about the guard at all. The
// membership test could not have caught that; only running the scanner over
// source that contains the shape can.
func TestTheStdinScannerFindsARealRead(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   bool
	}{
		{
			name:   "direct",
			source: "package p\nimport (\"io\"; \"github.com/spf13/cobra\")\nfunc f(cmd *cobra.Command) { _, _ = io.ReadAll(cmd.InOrStdin()) }\n",
			want:   true,
		},
		{
			name:   "through a local variable, on two lines",
			source: "package p\nimport (\"io\"; \"github.com/spf13/cobra\")\nfunc f(cmd *cobra.Command) {\n\tin := cmd.InOrStdin()\n\t_, _ = io.ReadAll(in)\n}\n",
			want:   true,
		},
		{
			name:   "handed to another function",
			source: "package p\nimport \"github.com/spf13/cobra\"\nfunc g(r interface{}) {}\nfunc f(cmd *cobra.Command) { g(cmd.InOrStdin()) }\n",
			want:   true,
		},
		{
			name:   "the process's own stdin",
			source: "package p\nimport (\"bufio\"; \"os\")\nfunc f() { _ = bufio.NewReader(os.Stdin) }\n",
			want:   true,
		},
		{
			name:   "a scan, which always reads the process's stdin",
			source: "package p\nimport \"fmt\"\nfunc f() { var s string; _, _ = fmt.Scanln(&s) }\n",
			want:   true,
		},
		{
			name:   "installing a stream is not reading one",
			source: "package p\nimport (\"strings\"; \"github.com/spf13/cobra\")\nfunc f(cmd *cobra.Command) { cmd.SetIn(strings.NewReader(\"x\")) }\n",
			want:   false,
		},
		{
			name:   "reading an unrelated reader",
			source: "package p\nimport (\"io\"; \"strings\")\nfunc f() { _, _ = io.ReadAll(strings.NewReader(\"x\")) }\n",
			want:   false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			path := filepath.Join(directory, "sabotage.go")
			if err := os.WriteFile(path, []byte(testCase.source), 0o600); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}

			found := standardInputUsers(t, directory)
			if got := len(found) == 1; got != testCase.want {
				t.Errorf("scanner found %v, want detected=%v for:\n%s", found, testCase.want, testCase.source)
			}
		})
	}
}

// standardInputUsers finds every file that names standard input.
//
// It parses rather than matching text, so a mention in a comment or a string
// is not a use, and a use split across statements still is.
func standardInputUsers(t *testing.T, root string) []string {
	t.Helper()

	users := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			// Agent tooling keeps checkouts of other branches under dot
			// directories; their call sites are not this tree's.
			if path != root && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		if !fileUsesStandardInput(t, path) {
			return nil
		}
		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		users = append(users, relative)
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	return normalise(users)
}

// fileUsesStandardInput reports whether a file names standard input anywhere
// other than to install one.
func fileUsesStandardInput(t *testing.T, path string) bool {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		// A file this tree cannot parse is a problem for the compiler, not for
		// this guard, and skipping it is better than failing every run.
		return false
	}

	// A stream installed with SetIn is a test or a caller supplying input, not
	// a read of the process's own. Collect those first so they can be excluded.
	installed := map[ast.Node]struct{}{}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel.Name == "SetIn" {
			for _, argument := range call.Args {
				installed[argument] = struct{}{}
			}
		}
		return true
	})

	uses := false
	ast.Inspect(file, func(node ast.Node) bool {
		if uses {
			return false
		}
		if _, skip := installed[node]; skip {
			return false
		}

		switch typed := node.(type) {
		case *ast.SelectorExpr:
			// os.Stdin
			if identifier, ok := typed.X.(*ast.Ident); ok && identifier.Name == "os" && typed.Sel.Name == "Stdin" {
				uses = true
			}
		case *ast.CallExpr:
			selector, ok := typed.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// cmd.InOrStdin()
			if selector.Sel.Name == "InOrStdin" {
				uses = true
			}
			// fmt.Scan family reads the process's stdin whatever the command
			// was given, which is what made gpg-key clear hang.
			if identifier, ok := selector.X.(*ast.Ident); ok && identifier.Name == "fmt" {
				if strings.HasPrefix(selector.Sel.Name, "Scan") || strings.HasPrefix(selector.Sel.Name, "Fscan") {
					uses = true
				}
			}
		}
		return true
	})

	return uses
}
