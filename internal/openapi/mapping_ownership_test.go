package openapi_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The status-to-kind mapping belongs to this package, and so do its tests.
//
// It did not stay here. Ten packages had grown their own copy of the same
// table -- 400 is exit 2, 403 is exit 3, 409 is exit 5 -- four of them sharing
// a helper named testMapStatusErrors that had been pasted from one service to
// the next. Every copy asserted the same thing about the same function, and a
// change to the mapping would have had to be made eleven times or been caught
// eleven times.
//
// That duplication is what made the mapping look like a per-service concern
// worth a per-service test, when it is one pure function with 204 call sites.
// A service test that wants an error of some kind should build one with
// apperrors.New rather than route a status through the mapper to get it.
//
// This does not police whether services use the mapping -- see issue #535 for
// the question that actually matters, which is whether the kind it produces is
// right for a bare 400 or 5xx.
func TestTheStatusMappingIsOnlyTestedWhereItLives(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..", "internal")

	offenders := make([]string, 0)
	fileSet := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		// The owning package is where it is supposed to be tested.
		if filepath.Dir(path) == filepath.Join("..", "..", "internal", "openapi") {
			return nil
		}

		parsed, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}

		ast.Inspect(parsed, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "MapStatusError" {
				return true
			}
			receiver, ok := selector.X.(*ast.Ident)
			if !ok || receiver.Name != "openapi" {
				return true
			}

			offenders = append(offenders,
				filepath.ToSlash(path)+":"+strconv.Itoa(fileSet.Position(selector.Pos()).Line))

			return true
		})

		return nil
	})
	if err != nil {
		t.Fatalf("walk internal: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("openapi.MapStatusError is tested outside the package that owns it:\n  %s\n\n"+
			"The mapping is one function. Testing it from a service package asserts nothing about\n"+
			"that service and duplicates TestMapStatusError. To build an error of a given kind, use\n"+
			"apperrors.New.",
			strings.Join(offenders, "\n  "))
	}
}
