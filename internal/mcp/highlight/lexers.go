package highlight

import (
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
)

// lexerFor returns the lexer for a file, chosen by its name alone: sniffing
// the content is slow, and wrong on the fragment of a file a hunk holds. It
// returns nil for a name no lexer matches, and for a plain-text file, whose
// every span would be ClassText.
func lexerFor(name string) chroma.Lexer {
	index := lexerIndex()
	lexer := index.match(name)
	if lexer == nil || index.plain[lexer.Config().Name] {
		return nil
	}
	return lexer
}

// lexerIndex prepares chroma's registry once, on first use, and indexes it.
var lexerIndex = sync.OnceValue(func() *nameIndex {
	plain := map[string]bool{"plaintext": true}
	for _, name := range replaceLexAhead(lexers.GlobalLexerRegistry) {
		plain[name] = true
	}
	return newNameIndex(lexers.GlobalLexerRegistry, plain)
})

// replaceLexAhead replaces each lexer in the registry that lexes a whole text
// before it yields a token, and returns their names.
//
// A DelegatingLexer, which chroma uses where one language is embedded in
// another (ERB, Svelte, PHTML, YAML+Jinja, Go HTML templates), lexes the whole
// text before it yields its first token, so no deadline checked between
// tokens can stop it, and Svelte's and ERB's patterns are quadratic: 16 KiB of
// crafted Svelte takes seconds. A Markdown code block, an org-mode source
// block and the like name their language, so a file of any language can reach
// one. Each is therefore replaced by a plain-text lexer of the same name and
// file names: a file of such a language, or a block naming one, is left
// uncoloured. bb uses chroma for nothing else.
func replaceLexAhead(registry *chroma.LexerRegistry) []string {
	var replaced []string
	delegating := reflect.TypeOf(chroma.DelegatingLexer(nil, nil))
	for _, lexer := range registry.Lexers {
		if reflect.TypeOf(lexer) != delegating {
			continue
		}
		config := *lexer.Config()
		replaced = append(replaced, config.Name)
		registry.Register(chroma.MustNewLexer(&config, lexers.PlaintextRules))
	}
	return replaced
}

// nameIndex answers what lexers.Match answers, faster. Match tries every glob
// of every lexer with filepath.Match, and tries each again with every backup
// suffix it ignores appended: some ten thousand calls, 1.4 ms a name, paid for
// every file of a patch. Nearly every glob is a file name or a * followed by
// a suffix, which a comparison matches; nameIndex compares those and matches
// the few others as Match does. Matching a glob with a suffix appended is
// matching the glob against the name without that suffix, which is how the
// backup names are tried. TestNameIndexAgreesWithChroma holds it to Match.
type nameIndex struct {
	primary   []glob // the lexers' Filenames, in registry order
	secondary []glob // their AliasFilenames, tried when no Filenames glob matches
	plain     map[string]bool
}

type glob struct {
	lexer   chroma.Lexer
	pattern string
	kind    byte // '=' for a name, '*' for a * and a suffix, 0 for anything else
}

// newNameIndex indexes the registry's lexers by their globs. plain names the
// lexers lexerFor declines.
func newNameIndex(registry *chroma.LexerRegistry, plain map[string]bool) *nameIndex {
	index := &nameIndex{plain: plain}
	for _, lexer := range registry.Lexers {
		config := lexer.Config()
		index.primary = appendGlobs(index.primary, lexer, config.Filenames)
		index.secondary = appendGlobs(index.secondary, lexer, config.AliasFilenames)
	}
	return index
}

func appendGlobs(globs []glob, lexer chroma.Lexer, patterns []string) []glob {
	for _, pattern := range patterns {
		entry := glob{lexer: lexer, pattern: pattern}
		suffix, star := strings.CutPrefix(pattern, "*")
		switch {
		case !strings.ContainsAny(pattern, `*?[\`):
			entry.kind = '='
		case star && !strings.ContainsAny(suffix, `*?[\`):
			entry.kind, entry.pattern = '*', suffix
		}
		globs = append(globs, entry)
	}
	return globs
}

func (g glob) matches(name string) bool {
	switch g.kind {
	case '=':
		return name == g.pattern
	case '*':
		// A * matches no path separator, and a base name has none.
		return strings.HasSuffix(name, g.pattern)
	}
	matched, err := filepath.Match(g.pattern, name)
	return err == nil && matched
}

// ignoredSuffixes are the backup and template suffixes lexers.Match looks
// through, so that main.go.orig is lexed as Go: chroma's list, which it does
// not export.
var ignoredSuffixes = []string{
	"~", ".bak", ".old", ".orig",
	".dpkg-dist", ".dpkg-old", ".ucf-dist", ".ucf-new", ".ucf-old",
	".rpmnew", ".rpmorig", ".rpmsave",
	".in",
}

// match is lexers.Match: the matching lexer of the highest priority, sorted
// as Match sorts, from the Filenames globs and else from the AliasFilenames.
func (x *nameIndex) match(filename string) chroma.Lexer {
	name := filepath.Base(filename)
	names := []string{name}
	for _, suffix := range ignoredSuffixes {
		if stem, ok := strings.CutSuffix(name, suffix); ok {
			names = append(names, stem)
		}
	}
	for _, globs := range [][]glob{x.primary, x.secondary} {
		var matched chroma.PrioritisedLexers
		for _, g := range globs {
			if slices.ContainsFunc(names, g.matches) {
				matched = append(matched, g.lexer)
			}
		}
		if len(matched) > 0 {
			sort.Sort(matched)
			return matched[0]
		}
	}
	return nil
}
