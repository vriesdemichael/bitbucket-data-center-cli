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
// every span would be ClassText. templates asks for chroma's own lexer for a
// template, in place of the stand-in that highlights its markup.
func lexerFor(name string, templates bool) chroma.Lexer {
	state := lexerState()
	lexer := state.index.match(name)
	if lexer == nil {
		return nil
	}
	if own, ok := state.templates[lexer.Config().Name]; ok && templates {
		return own
	}
	if state.index.plain[lexer.Config().Name] {
		return nil
	}
	return lexer
}

// registryState is chroma's registry as prepared here: its index, and
// chroma's own lexers for the templates, kept aside by name.
type registryState struct {
	index     *nameIndex
	templates map[string]chroma.Lexer
}

// lexerState prepares chroma's registry once, on first use, and indexes it.
var lexerState = sync.OnceValue(func() registryState {
	templates, plain := standInForTemplates(lexers.GlobalLexerRegistry)
	plain["plaintext"] = true
	return registryState{index: newNameIndex(lexers.GlobalLexerRegistry, plain), templates: templates}
})

// markupOfTemplates names, for each template language, the language it is
// written in around the code: the lexer that stands in for it.
var markupOfTemplates = map[string]string{
	"ERB":              "HTML",
	"Go HTML Template": "HTML",
	"PHTML":            "HTML",
	"Svelte":           "HTML",
	"YAML+Jinja":       "YAML",
}

// standInForTemplates replaces each lexer in the registry that lexes a whole
// text before it yields a token, and returns those lexers by name, and the
// names of those it replaced with plain text.
//
// A DelegatingLexer, which chroma uses where one language is embedded in
// another (ERB, Svelte, PHTML, YAML+Jinja, Go HTML templates), lexes the whole
// text before it yields its first token, so no deadline checked between
// tokens can stop it, and Svelte's and ERB's patterns are quadratic: 16 KiB of
// crafted Svelte takes seconds. A Markdown code block, an org-mode source
// block and the like name their language, so a file of any language can reach
// one. Each is therefore replaced, under the same name and file names, by the
// lexer of the markup the template is written in, HTML or YAML, which yields
// as it goes: the markup is highlighted, and the code inside the template is
// not. A template language with no markup named here is replaced by plain
// text. chroma's own lexer is kept for a caller that accepts what it costs
// (Options.Templates). bb uses chroma for nothing else.
func standInForTemplates(registry *chroma.LexerRegistry) (own map[string]chroma.Lexer, plain map[string]bool) {
	own, plain = map[string]chroma.Lexer{}, map[string]bool{}
	delegating := reflect.TypeOf(chroma.DelegatingLexer(nil, nil))
	for _, lexer := range registry.Lexers {
		if reflect.TypeOf(lexer) != delegating {
			continue
		}
		config := *lexer.Config()
		own[config.Name] = lexer
		if name := markupOfTemplates[config.Name]; name != "" {
			if markup := registry.Get(name); markup != nil && reflect.TypeOf(markup) != delegating {
				registry.Register(standIn{Lexer: markup, config: &config})
				continue
			}
		}
		plain[config.Name] = true
		registry.Register(chroma.MustNewLexer(&config, lexers.PlaintextRules))
	}
	return own, plain
}

// standIn is a lexer registered under another's name and file names: the
// markup lexer that highlights a template in place of chroma's own.
type standIn struct {
	chroma.Lexer
	config *chroma.Config
}

// Config is the template's, so the stand-in is found, and ranked, as it was.
func (s standIn) Config() *chroma.Config { return s.config }

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
