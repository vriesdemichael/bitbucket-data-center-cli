package codeowners

import (
	"reflect"
	"testing"
)

func TestParseAndMatch(t *testing.T) {
	content := `
# CODEOWNERS file comment
*                   @global-lead

# Specific directory
docs/               @docs-team alice

# Recursive directory
src/**/*.go         @backend-team bob

# Exact file
/README.md          @readme-owner

# Bitbucket suffix modifier and escaped spaces
*.js                @js-team:all @frontend\ lead:random(2)
`

	co := Parse(content)
	if len(co.Rules) != 5 {
		t.Fatalf("expected 5 rules, got %d", len(co.Rules))
	}

	tests := []struct {
		path     string
		expected []string
	}{
		{
			path:     "README.md",
			expected: []string{"@readme-owner"},
		},
		{
			path:     "docs/index.md",
			expected: []string{"@docs-team", "alice"},
		},
		{
			path:     "docs/sub/tutorial.md",
			expected: []string{"@docs-team", "alice"},
		},
		{
			path:     "src/pkg/server/main.go",
			expected: []string{"@backend-team", "bob"},
		},
		{
			path:     "web/app.js",
			expected: []string{"@js-team", "@frontend lead"},
		},
		{
			path:     "other/random.txt",
			expected: []string{"@global-lead"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := co.MatchFile(tt.path)
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("MatchFile(%q) = %v, want %v", tt.path, got, tt.expected)
			}
		})
	}
}

func TestFineGrainedStrategiesAndEscapes(t *testing.T) {
	content := `
*.go   @backend\\ engineers:random(2) @lead:all
*.py   @python\\ dev:least_busy(3)
`
	co := Parse(content)
	refs := co.MatchFileRefs("main.go")
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}

	if refs[0].Name != "backend engineers" || refs[0].Strategy != StrategyRandom || refs[0].Count != 2 || !refs[0].IsGroup {
		t.Errorf("unexpected ref 0: %+v", refs[0])
	}
	if refs[1].Name != "lead" || refs[1].Strategy != StrategyAll || refs[1].Count != 0 || !refs[1].IsGroup {
		t.Errorf("unexpected ref 1: %+v", refs[1])
	}

	pyRefs := co.MatchFileRefs("script.py")
	if len(pyRefs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(pyRefs))
	}
	if pyRefs[0].Name != "python dev" || pyRefs[0].Strategy != StrategyLeastBusy || pyRefs[0].Count != 3 {
		t.Errorf("unexpected pyRef: %+v", pyRefs[0])
	}
}

func TestMatchFileRefsUnion(t *testing.T) {
	content := `
pkg/a/* @team:random(1)
pkg/b/* @team:all
`
	co := Parse(content)
	union := co.MatchFileRefsUnion([]string{"pkg/a/file.go", "pkg/b/file.go"})
	if len(union) != 1 {
		t.Fatalf("expected 1 union ref, got %d", len(union))
	}
	if union[0].Strategy != StrategyAll {
		t.Errorf("expected StrategyAll after upgrade, got %v", union[0].Strategy)
	}
}

func TestMatchFiles(t *testing.T) {
	content := `
*           @global-owner
docs/       @docs-team alice
src/*.go    @backend-team alice
`
	co := Parse(content)

	files := []string{
		"docs/index.md",
		"src/main.go",
		"other.txt",
	}

	want := []string{"@docs-team", "alice", "@backend-team", "@global-owner"}
	got := co.MatchFiles(files)

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MatchFiles() = %v, want %v", got, want)
	}
}

func TestPrecedenceLastMatchWins(t *testing.T) {
	content := `
*.js            @general-js
/src/special.js @special-team
`
	co := Parse(content)

	got := co.MatchFile("src/special.js")
	want := []string{"@special-team"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MatchFile() = %v, want %v", got, want)
	}

	gotOther := co.MatchFile("src/other.js")
	wantOther := []string{"@general-js"}
	if !reflect.DeepEqual(gotOther, wantOther) {
		t.Fatalf("MatchFile() = %v, want %v", gotOther, wantOther)
	}
}
