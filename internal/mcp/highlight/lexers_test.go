package highlight

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// sampleName is a name glob matches: each * as x, each ? as q, and each class
// as its first member.
func sampleName(glob string) string {
	var name strings.Builder
	for i := 0; i < len(glob); i++ {
		switch c := glob[i]; c {
		case '*':
			name.WriteByte('x')
		case '?':
			name.WriteByte('q')
		case '[':
			name.WriteByte(glob[i+1])
			i += strings.IndexByte(glob[i:], ']')
		case '\\':
			i++
			name.WriteByte(glob[i])
		default:
			name.WriteByte(c)
		}
	}
	return name.String()
}

// The index picks the lexer lexers.Match picks, for a name each glob of each
// lexer matches, for backup names chroma looks through, and for names near
// them that it does not.
func TestNameIndexAgreesWithChroma(t *testing.T) {
	t.Parallel()

	index := lexerIndex()
	variants := []func(string) string{
		func(name string) string { return name + "~" },
		func(name string) string { return "a" + name },
		func(name string) string { return name + "x" },
		func(name string) string { return strings.ToUpper(name) },
		func(name string) string { return "dir/" + name },
		func(name string) string { return name + ".swp" },
	}
	for _, suffix := range ignoredSuffixes {
		variants = append(variants, func(name string) string { return name + suffix })
	}
	names := []string{
		"", ".", "x", "README", "Makefile", "makefile", "GNUmakefile", "CMakeLists.txt", "Dockerfile.dev",
		".bashrc", ".env.local", "a.go.in", "Makefile.in", "go.mod", "go.sum", "package.json", "a.min.js",
		"Jenkinsfile", "x.tar.gz", "a.php5", "a.1", "a.xbm", "Caddyfile.prod", "notes.txt", "a.svelte",
	}
	seen := 0
	for _, lexer := range lexers.GlobalLexerRegistry.Lexers {
		config := lexer.Config()
		for _, glob := range append(append([]string{}, config.Filenames...), config.AliasFilenames...) {
			name := sampleName(glob)
			if matched, err := filepath.Match(glob, name); err != nil || !matched {
				t.Errorf("sample %q does not match %s's glob %q", name, config.Name, glob)
				continue
			}
			names = append(names, name, variants[seen%len(variants)](name))
			seen++
		}
	}
	for _, name := range names {
		if got, want := index.match(name), lexers.Match(name); got != want {
			t.Errorf("%q: index picks %v, lexers.Match %v", name, lexerName(got), lexerName(want))
		}
	}
}

// The index picks as Match picks in a registry with what chroma's own lacks:
// AliasFilenames, tried only when no Filenames glob matches, and priorities
// deciding between lexers matching one name.
func TestNameIndexAgreesWithMatchInAnyRegistry(t *testing.T) {
	t.Parallel()

	registry := chroma.NewLexerRegistry()
	for _, config := range []*chroma.Config{
		{Name: "low", Filenames: []string{"*.x", "Build"}},
		{Name: "high", Filenames: []string{"*.x", "*.y"}, Priority: 2},
		{Name: "alias", Filenames: []string{"*.w"}, AliasFilenames: []string{"*.z", "*.x", "Build*"}, Priority: 3},
		{Name: "class", Filenames: []string{"*.[ab]c", "Build.*", "?.q"}},
	} {
		registry.Register(chroma.MustNewLexer(config, lexers.PlaintextRules))
	}
	index := newNameIndex(registry, nil)
	for _, name := range []string{
		"a.x", "a.y", "a.z", "a.w", "a.x~", "a.z.bak", "a.z.orig", "a.ac", "a.bc", "a.cc", "Build", "Build.gradle",
		"Builder", "a.q", "ab.q", "dir/a.z", "a.x.in", "a", "",
	} {
		if got, want := index.match(name), registry.Match(name); got != want {
			t.Errorf("%q: index picks %v, Match %v", name, lexerName(got), lexerName(want))
		}
	}
}

func lexerName(lexer chroma.Lexer) string {
	if lexer == nil {
		return "nothing"
	}
	return lexer.Config().Name
}

// A lexer that lexes the whole text before its first token is gone from the
// registry: a file of its language has no lexer, and a Markdown code block
// naming it is plain text, where Svelte's own lexer takes seconds on it.
func TestLexAheadLexersAreReplaced(t *testing.T) {
	t.Parallel()

	lexerIndex()
	delegating := reflect.TypeOf(chroma.DelegatingLexer(nil, nil))
	for _, lexer := range lexers.GlobalLexerRegistry.Lexers {
		if reflect.TypeOf(lexer) == delegating {
			t.Errorf("%s still lexes a whole text before its first token", lexer.Config().Name)
		}
	}
	for _, name := range []string{"App.svelte", "index.html.erb", "page.phtml", "top.sls"} {
		if lexer := lexerFor(name); lexer != nil {
			t.Errorf("%s has lexer %s", name, lexer.Config().Name)
		}
	}

	block := "```svelte\n<script lang=\"ts\">" + strings.Repeat("{", 16<<10) + "\n```\n"
	spans, ok := Lines("README.md", block, time.Now().Add(time.Second))
	if !ok {
		t.Fatal("a Markdown file with a Svelte block was not highlighted within a second")
	}
	if got := classes(t, spans[1])[0]; got != ClassText {
		t.Errorf("the Svelte block's text is %c, want %c", got, ClassText)
	}
}
