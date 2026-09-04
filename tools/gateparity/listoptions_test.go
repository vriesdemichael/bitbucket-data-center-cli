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

// TestNoServiceOptionIsCalledLimit fails when a list option reintroduces the
// name that meant two opposite things.
//
// Eleven services capped their results and truncated; eight passed the value
// through as a page size and paged to exhaustion. The field was called Limit in
// all nineteen, so a call site could not tell which it was getting, and the
// wrong guess failed silently and in the direction that loses data: fewer
// results, no error, no flag. Two user-visible bugs came from reading it as a
// page size, which is the natural reading.
//
// The check is on the name rather than on the behaviour because the behaviour
// is not statically decidable, and because the name is what the call site
// reads. ADR-074 records the rule.
func TestNoServiceOptionIsCalledLimit(t *testing.T) {
	root := repositoryRoot(t)

	fields := limitNamedFields(t, filepath.Join(root, servicesDirectory))
	for _, field := range fields {
		t.Errorf(
			"%s is named for a limit that does not say which one it is.\n"+
				"Name it MaxResults if it caps the total and truncates, or PageSize if it is the\n"+
				"per-request size and the call pages to exhaustion. ADR-074 explains why: the two\n"+
				"behaviours were indistinguishable at the call site, and guessing wrong silently\n"+
				"returned fewer results.",
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
			name:   "MaxResults is fine",
			source: "package p\ntype ListOptions struct {\n\tMaxResults int\n}\n",
			want:   false,
		},
		{
			name:   "PageSize is fine",
			source: "package p\ntype ListOptions struct {\n\tPageSize int\n}\n",
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

// limitNamedFields finds every struct field named Limit under a directory.
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
						if name.Name == "Limit" {
							found = append(found, relative+" (field Limit)")
						}
					}
				}

				return true
			}

			// A parameter is read at the call site exactly as a field is, and
			// the same two meanings had grown there: eighteen exported methods
			// took a bare `limit`, six capping and twelve paging to exhaustion.
			// The guard watched only the fields, so the half it could not see
			// went on doing what the rule was written to stop -- and `bb branch
			// restriction list --limit` shipped returning everything.
			function, ok := node.(*ast.FuncDecl)
			if !ok || function.Type.Params == nil || !function.Name.IsExported() {
				return true
			}
			for _, parameter := range function.Type.Params.List {
				for _, name := range parameter.Names {
					if name.Name == "limit" || name.Name == "Limit" {
						found = append(found, relative+" ("+function.Name.Name+" parameter limit)")
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
