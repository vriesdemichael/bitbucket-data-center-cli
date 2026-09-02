package cli

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestTheServiceLayerDoesNotImportTheCLI pins the direction of the dependency
// between the two halves of the tree.
//
// It is not an abstract tidiness rule. internal/cli/safederef was created by
// consolidating 34 copies of a pointer helper, and two files under
// internal/services reached for it -- the first production files ever to
// import from internal/cli. Nothing reported it: golangci-lint has no depguard
// configured, and a leaf package makes no import cycle for the compiler to
// refuse. It was only ever going to be noticed by someone reading the diff.
//
// The helper now lives at internal/safederef, which both layers may use. This
// test is what stops the next one landing in internal/cli instead.
func TestTheServiceLayerDoesNotImportTheCLI(t *testing.T) {
	const (
		serviceRoot = "../services"
		forbidden   = "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	)

	fileSet := token.NewFileSet()
	checked := 0

	err := filepath.WalkDir(serviceRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Tests may reach across the layer to build a command tree; the rule
		// is about what ships.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}

		parsed, err := parser.ParseFile(fileSet, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		checked++

		for _, spec := range parsed.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return err
			}
			if imported == forbidden || strings.HasPrefix(imported, forbidden+"/") {
				t.Errorf("%s imports %s. The service layer must not depend on the CLI layer -- "+
					"if the two need to share something, it belongs outside both (internal/safederef "+
					"is the precedent).", filepath.ToSlash(path), imported)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", serviceRoot, err)
	}

	// Guards the walk: a wrong root would report nothing and pass.
	if checked == 0 {
		t.Fatalf("no service files were parsed under %s", serviceRoot)
	}
	t.Logf("%d service files checked", checked)
}
