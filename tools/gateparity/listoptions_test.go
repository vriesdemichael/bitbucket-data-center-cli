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

// servicesDirectory holds the packages this guard reads.
const servicesDirectory = "internal/services"

// bannedName reports the offending name, or "" when it is allowed.
//
// Limit says nothing about which of the two behaviours it selects. PageSize
// says exactly which, and is the one no caller can use.
func bannedName(name string) string {
	switch name {
	case "Limit", "PageSize":
		return name
	case "pageSize":
		return name
	}

	return ""
}

// TestNoServiceOptionIsCalledLimit fails when a list option names a limit the
// call site cannot act on.
//
// It began as a rule about ambiguity. Eleven services capped their results and
// truncated; eight passed the value through as a page size and paged to
// exhaustion. The field was called Limit in all nineteen, so a call site could
// not tell which it was getting, and the wrong guess failed silently and in the
// direction that loses data: fewer results, no error, no flag.
//
// Renaming was not enough. Seven `--limit` flags shipped doing nothing --
// branch restriction list, commit compare, pr commits, pr files, repo browse
// tree, branch model inspect, and the pr comment listings -- and in every one of
// them the field was honestly called PageSize. The CLI handed a cap to something
// that took a window, and no name could have said that was wrong, because both
// halves were correctly named.
//
// So the rule is now that a service does not take a page size at all. Paging is
// an HTTP detail openapi.PageThrough owns; a caller has no cursor to advance
// and nothing to do with the window, and the only number worth accepting from
// outside is a cap. With page size gone from the surface, a cap can no longer
// land on one. ADR-074 records it.
func TestNoServiceOptionIsCalledLimit(t *testing.T) {
	root := repositoryRoot(t)

	fields := limitNamedFields(t, filepath.Join(root, servicesDirectory))
	for _, field := range fields {
		t.Errorf(
			"%s names a limit a caller cannot act on.\n"+
				"Call it MaxResults and let openapi.PageThrough size the requests. A service does\n"+
				"not take Limit, because it does not say whether it caps or paginates, and it does\n"+
				"not take PageSize, because a caller has no cursor to advance and every caller that\n"+
				"passed one was passing a cap. ADR-074 explains what each of those cost.",
			field,
		)
	}
}

// TestTheLimitScannerFindsAField is the sabotage, and it runs the scanner over
// source rather than checking a list.
func TestTheLimitScannerFindsAField(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   bool
	}{
		{
			name:   "a field called Limit",
			source: "package p\ntype ListOptions struct {\n\tLimit int\n\tStart int\n}\n",
			want:   true,
		},
		{
			name:   "a field called Limit with a tag",
			source: "package p\ntype ListOptions struct {\n\tLimit int `json:\"limit\"`\n}\n",
			want:   true,
		},
		{
			name:   "a field called PageSize",
			source: "package p\ntype ListOptions struct {\n\tPageSize int\n}\n",
			want:   true,
		},
		{
			name:   "an exported method taking a page size",
			source: "package p\nimport \"context\"\nfunc (s *S) List(ctx context.Context, pageSize int) error { return nil }\n",
			want:   true,
		},
		{
			name:   "MaxResults is fine",
			source: "package p\ntype ListOptions struct {\n\tMaxResults int\n}\n",
			want:   false,
		},
		{
			// The window still exists inside a service; what it must not do is
			// reach the surface.
			name:   "an unexported helper may still speak of pages",
			source: "package p\nfunc fetch(pageSize int) int { return pageSize }\n",
			want:   false,
		},
		{
			name:   "a local variable called limit is not a field",
			source: "package p\nfunc f() int { limit := 25; return limit }\n",
			want:   false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "s.go"), []byte(testCase.source), 0o600); err != nil {
				t.Fatalf("write: %v", err)
			}

			found := limitNamedFields(t, directory)
			if got := len(found) == 1; got != testCase.want {
				t.Errorf("scanner found %v, want detected=%v for:\n%s", found, testCase.want, testCase.source)
			}
		})
	}
}

// limitNamedFields finds every struct field and exported parameter under a
// directory that names a limit the caller cannot act on.
func limitNamedFields(t *testing.T, root string) []string {
	t.Helper()

	found := []string{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			if path != root && strings.HasPrefix(info.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return nil
		}

		relative, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}

		ast.Inspect(file, func(node ast.Node) bool {
			if structure, ok := node.(*ast.StructType); ok && structure.Fields != nil {
				for _, field := range structure.Fields.List {
					for _, name := range field.Names {
						if banned := bannedName(name.Name); banned != "" {
							found = append(found, relative+" (field "+banned+")")
						}
					}
				}

				return true
			}

			// A parameter is read at the call site exactly as a field is, and
			// the same meanings had grown there: eighteen exported methods took
			// a bare `limit`, six capping and twelve paging to exhaustion. The
			// guard watched only the fields, so the half it could not see went
			// on doing what the rule was written to stop -- and `bb branch
			// restriction list --limit` shipped returning everything.
			//
			// Unexported functions are left alone. A window is a real thing
			// inside a service; the rule is about what a caller is offered.
			function, ok := node.(*ast.FuncDecl)
			if !ok || function.Type.Params == nil || !function.Name.IsExported() {
				return true
			}
			for _, parameter := range function.Type.Params.List {
				for _, name := range parameter.Names {
					if banned := bannedName(name.Name); banned != "" {
						found = append(found, relative+" ("+function.Name.Name+" parameter "+banned+")")
					}
				}
			}

			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	return normalise(found)
}
