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

	index := lexerState().index
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

// A lexer that lexes the whole text before its first token stands aside: a
// template is highlighted by the lexer of its markup, which stops at the
// deadline, and so is a Markdown code block naming it, where Svelte's own
// lexer takes seconds on a crafted one. PHP, which chroma lexes on its own
// as well as inside HTML, keeps its lexer.
func TestTemplatesHaveStandIns(t *testing.T) {
	t.Parallel()

	lexerState()
	delegating := reflect.TypeOf(chroma.DelegatingLexer(nil, nil))
	for _, lexer := range lexers.GlobalLexerRegistry.Lexers {
		if reflect.TypeOf(lexer) == delegating {
			t.Errorf("%s still lexes a whole text before its first token", lexer.Config().Name)
		}
	}
	for name, template := range map[string]string{"App.svelte": "Svelte", "index.html.erb": "ERB", "page.phtml": "PHTML", "top.sls": "YAML+Jinja"} {
		lexer := lexerFor(name, false)
		if _, stands := lexer.(standIn); !stands || lexer.Config().Name != template {
			t.Errorf("%s has lexer %v, want a stand-in for %s", name, lexer, template)
		}
	}
	if lexer := lexerFor("index.php", false); lexer == nil || lexer.Config().Name != "PHP" {
		t.Errorf("index.php has lexer %v, want PHP", lexer)
	}

	// An ordinary Svelte file has its markup and its script highlighted.
	svelte, ok := Lines("App.svelte", "<script>\n  let total = 0;\n</script>\n<p class=\"total\">{total}</p>\n", Options{Deadline: time.Now().Add(time.Second)})
	if !ok || !strings.Contains(svelte[1], string(ClassKeyword)) || !strings.Contains(svelte[3], string(ClassTag)) {
		t.Errorf("an ordinary Svelte file is highlighted as %q, ok %v; want its script's keyword and its tags", svelte, ok)
	}
	// A crafted one slows the markup's lexer too, which stops at the deadline.
	started := time.Now()
	Lines("App.svelte", craftedSvelte(), Options{Deadline: started.Add(100 * time.Millisecond)})
	if waited := time.Since(started); waited > 500*time.Millisecond {
		t.Errorf("a crafted Svelte file held the answer %v, past its 100ms deadline", waited)
	}

	block := "```svelte\n<div class=\"total\">{total}</div>\n```\n"
	spans, ok := Lines("README.md", block, Options{Deadline: time.Now().Add(time.Second)})
	if !ok {
		t.Fatal("a Markdown file with a Svelte block was not highlighted within a second")
	}
	if !strings.Contains(spans[1], string(ClassTag)) {
		t.Errorf("the Svelte block's markup is %q, want its tags highlighted", spans[1])
	}
}

// Asked for, chroma's own lexer highlights a template, the code in it too;
// on a crafted file that keeps it busy for seconds, nothing waits for it past
// the deadline.
func TestTemplatesOwnLexerOnRequestDoesNotHoldTheAnswer(t *testing.T) {
	t.Parallel()

	own := lexerFor("App.svelte", true)
	if reflect.TypeOf(own) != reflect.TypeOf(chroma.DelegatingLexer(nil, nil)) {
		t.Fatalf("asked for chroma's own lexer, App.svelte has %T", own)
	}
	spans, ok := Lines("App.svelte", "<script>\nlet total = 1;\n</script>\n", Options{Deadline: time.Now().Add(time.Second), Templates: true})
	if !ok || !strings.HasPrefix(spans[1], "k3") {
		t.Errorf("chroma's own lexer highlighted the script as %q, ok %v; want let a keyword", spans, ok)
	}

	crafted := craftedSvelte()
	started := time.Now()
	if _, ok := Lines("App.svelte", crafted, Options{Deadline: started.Add(100 * time.Millisecond), Templates: true}); ok {
		t.Error("a crafted Svelte file was highlighted by chroma's own lexer within its deadline; the test no longer shows what it means to")
	}
	if waited := time.Since(started); waited > 500*time.Millisecond {
		t.Errorf("the answer waited %v for chroma's own lexer, past its 100ms deadline", waited)
	}
}

// craftedSvelte is 16 KiB of Svelte its own lexer backtracks through for
// seconds.
func craftedSvelte() string {
	return `<script lang="ts">` + strings.Repeat("{", 16<<10) + "\n"
}
