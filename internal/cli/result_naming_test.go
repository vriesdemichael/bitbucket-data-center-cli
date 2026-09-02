package cli

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"unicode"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// TestEveryPublishedFieldIsCamelCase enforces ADR-076 instead of trusting a
// sweep to have been thorough.
//
// The rule was applied by hand, command group by command group, and a hand pass
// misses things: bb admin health published status_code beside healthy and
// authenticated, because its model was written before the pass and the sweep
// keyed on the payload sites rather than the declared types. Nothing failed,
// because nothing was checking.
//
// A field an open map holds -- a comment's properties, a diff's stats, an
// activity's raw entry -- is not checked, and cannot be: those are Bitbucket's
// keys, not bb's, which is exactly why they are published as open objects.
func TestEveryPublishedFieldIsCamelCase(t *testing.T) {
	t.Parallel()

	// Loading the root command is what runs every package's init and fills the
	// registry; without it this test passes by having nothing to look at.
	_ = NewRootCommand()

	declared := result.DeclaredPaths()
	if len(declared) == 0 {
		t.Fatal("no results are declared; this test is checking nothing")
	}

	offenders := map[string][]string{}
	for _, path := range declared {
		schema, ok := result.SchemaFor(path)
		if !ok {
			t.Errorf("%q is listed as declared but has no schema", path)
			continue
		}

		encoded, err := json.Marshal(schema)
		if err != nil {
			t.Errorf("%q: encode schema: %v", path, err)
			continue
		}
		var document map[string]any
		if err := json.Unmarshal(encoded, &document); err != nil {
			t.Errorf("%q: decode schema: %v", path, err)
			continue
		}

		for _, field := range badlyNamedFields(document) {
			offenders[field] = append(offenders[field], path)
		}
	}

	for field, paths := range offenders {
		sort.Strings(paths)
		t.Errorf("%q is not camelCase (ADR-076); published by %s", field, strings.Join(paths, ", "))
	}
}

// badlyNamedFields walks a schema and returns every property name that is not
// camelCase, deduplicated.
func badlyNamedFields(schema map[string]any) []string {
	found := map[string]struct{}{}

	var walk func(node any)
	walk = func(node any) {
		object, ok := node.(map[string]any)
		if !ok {
			return
		}

		if properties, ok := object["properties"].(map[string]any); ok {
			for name, child := range properties {
				if !isCamelCase(name) {
					found[name] = struct{}{}
				}
				walk(child)
			}
		}
		// Arrays carry their element schema under items, and a payload's
		// interesting names are usually one list down.
		walk(object["items"])
	}
	walk(schema)

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// isCamelCase accepts a name that starts lowercase and carries only letters and
// digits, which rejects snake_case, kebab-case and the PascalCase an untagged
// Go field publishes.
func isCamelCase(name string) bool {
	if name == "" {
		return false
	}

	for index, character := range name {
		if index == 0 && !unicode.IsLower(character) {
			return false
		}
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) {
			return false
		}
	}

	return true
}
