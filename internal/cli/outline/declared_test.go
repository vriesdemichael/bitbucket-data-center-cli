package outline_test

import (
	"flag"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	// Imported for its init functions, which declare every command's result.
	_ "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outline"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// update rewrites the pinned outline from what the renderer prints, for when a
// change to it is intended: go test ./internal/cli/outline/ -update
var update = flag.Bool("update", false, "rewrite testdata from the outlines the renderer prints")

// TestThePullRequestMergeOutlineIsPinned holds one real command's whole outline
// to a copy in testdata, so that a change to how outlines look arrives as a
// diff a reviewer reads, rather than passing every unit test that happens not
// to cover it.
//
// pr merge because its result has most of what an outline shows: objects in
// objects, a list of objects, optional and required fields, enums whose
// description is dropped and one whose description is kept, and descriptions
// long enough to wrap. It also fails when one of those descriptions changes;
// rerun with -update and read the diff.
func TestThePullRequestMergeOutlineIsPinned(t *testing.T) {
	t.Parallel()

	schema, declared := result.SchemaFor("pr merge")
	if !declared {
		t.Fatal("pr merge declares no result, so there is nothing to outline")
	}
	got := write(t, outline.Member{Name: "data", Schema: schema})

	const pinned = "testdata/pr-merge.txt"
	if *update {
		if err := os.WriteFile(pinned, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	want, err := os.ReadFile(pinned)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("the outline of pr merge differs from %s; if that is intended, rerun with -update and read the diff\ngot:\n%s", pinned, got)
	}
}

// TestEveryDeclaredResultOutlinesEveryField renders the result each command
// declares, and finds each property on the row where it belongs: at its depth,
// in its order, marked ? exactly when its parent does not require it, with its
// type starting in the one type column.
//
// The expected rows come from a walk much simpler than the renderer, which is
// the point. A derived schema holds only objects, lists and maps of any, so the
// walk needs no branches, no map values and no guard, and cannot share a
// mistake with the code it checks.
func TestEveryDeclaredResultOutlinesEveryField(t *testing.T) {
	t.Parallel()

	paths := result.DeclaredPaths()
	if len(paths) == 0 {
		t.Fatal("no results are declared; importing internal/cli should have declared all of them")
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			schema, _ := result.SchemaFor(path)
			printed := write(t, outline.Member{Name: "data", Schema: schema})
			compareRows(t, printed, fieldRows(schema, 1, []namedRow{{depth: 0, name: "data"}}))
		})
	}
}

// namedRow is where a row sits: its depth and its name as printed.
type namedRow struct {
	depth int
	name  string
}

// fieldRows appends the rows the fields of a derived schema print as. A list's
// elements have no row of their own, so their fields sit where the list's
// would.
func fieldRows(schema *jsonschema.Schema, depth int, rows []namedRow) []namedRow {
	for schema.Items != nil {
		schema = schema.Items
	}

	for _, name := range schema.PropertyOrder {
		label := name
		if !slices.Contains(schema.Required, name) {
			label += "?"
		}
		rows = append(rows, namedRow{depth: depth, name: label})
		rows = fieldRows(schema.Properties[name], depth+1, rows)
	}

	return rows
}

// compareRows checks that the rows of a printed outline are exactly want, in
// order, and that every type starts in the same column. A line indented as far
// as the type column continues the description above it, and is not a row.
func compareRows(t *testing.T, printed string, want []namedRow) {
	t.Helper()

	typeColumn := 0
	for _, row := range want {
		typeColumn = max(typeColumn, 2*row.depth+len(row.name)+2)
	}

	var got []namedRow
	for _, line := range strings.Split(strings.TrimSuffix(printed, "\n"), "\n") {
		indent := len(line) - len(strings.TrimLeft(line, " "))
		if indent >= typeColumn {
			continue
		}
		if indent%2 != 0 {
			t.Errorf("a row is indented by an odd %d spaces: %q", indent, line)
		}

		name, _, _ := strings.Cut(line[indent:], "  ")
		got = append(got, namedRow{depth: indent / 2, name: name})

		if rest := line[indent+len(name):]; rest != "" && len(line)-len(strings.TrimLeft(rest, " ")) != typeColumn {
			t.Errorf("the type of %s does not start at column %d: %q", name, typeColumn, line)
		}
	}

	for index := range max(len(got), len(want)) {
		var gotRow, wantRow namedRow
		if index < len(got) {
			gotRow = got[index]
		}
		if index < len(want) {
			wantRow = want[index]
		}
		if gotRow != wantRow {
			t.Fatalf("row %d is %+v, want %+v, in:\n%s", index+1, gotRow, wantRow, printed)
		}
	}
}
