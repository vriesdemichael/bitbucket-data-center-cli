package main

import (
	"html"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

// reference renders the pages once for the tests that only read them.
var reference = sync.OnceValues(func() (map[string]string, error) {
	return renderReference(cli.NewRootCommand())
})

func pagesOf(t *testing.T) map[string]string {
	t.Helper()

	pages, err := reference()
	if err != nil {
		t.Fatalf("render the reference: %v", err)
	}

	return pages
}

// documented walks the command tree the way a reader sees it, without going
// through the exporter's own walk: a command the exporter dropped would
// otherwise be missing from the expectation as well as from the page.
func documented(root *cobra.Command) []*cobra.Command {
	var commands []*cobra.Command
	var walk func(parent *cobra.Command)
	walk = func(parent *cobra.Command) {
		for _, child := range parent.Commands() {
			if child.Hidden || child.Name() == "help" || child.Name() == "completion" {
				continue
			}
			commands = append(commands, child)
			walk(child)
		}
	}
	walk(root)

	return commands
}

func pathOf(command *cobra.Command) string {
	return strings.TrimPrefix(command.CommandPath(), "bb ")
}

func pageOf(command *cobra.Command) string {
	return strings.Fields(pathOf(command))[0] + ".md"
}

var (
	headingLine = regexp.MustCompile("(?m)^#{1,6} `?(.+?)`?$")
	explicitID  = regexp.MustCompile(`<a id="([^"]+)"></a>`)
	link        = regexp.MustCompile(`\]\(([^)\s]+)\)`)
	navEntry    = regexp.MustCompile(`reference/commands/([a-z0-9-]+\.md)`)
)

// idsOf returns every id a page gives a reader to link to, and how often:
// the slug Markdown derives from each heading, and each explicit anchor.
func idsOf(page string) map[string]int {
	ids := map[string]int{}
	for _, heading := range headingLine.FindAllStringSubmatch(withoutCodeBlocks(page), -1) {
		ids[strings.ReplaceAll(strings.ToLower(heading[1]), " ", "-")]++
	}
	for _, id := range explicitID.FindAllStringSubmatch(page, -1) {
		ids[id[1]]++
	}

	return ids
}

// withoutCodeBlocks drops the front matter and fenced blocks, where a line
// opening with # is a comment and not a heading.
func withoutCodeBlocks(page string) string {
	page = strings.TrimPrefix(page, frontMatter)

	var kept []string
	fenced := false
	for _, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			kept = append(kept, line)
		}
	}

	return strings.Join(kept, "\n")
}

// sectionOf returns a command's entry: from its heading to the next one.
func sectionOf(t *testing.T, pages map[string]string, command *cobra.Command) string {
	t.Helper()

	page := pages[pageOf(command)]
	heading := regexp.MustCompile("(?m)^#{1,6} `?" + regexp.QuoteMeta(command.CommandPath()) + "`?$")
	start := heading.FindStringIndex(page)
	if start == nil {
		t.Fatalf("%s has no heading on %s", command.CommandPath(), pageOf(command))
	}

	rest := page[start[1]:]
	if next := regexp.MustCompile("(?m)^#{1,6} ").FindStringIndex(withFencesBlanked(rest)); next != nil {
		rest = rest[:next[0]]
	}

	return rest
}

// withFencesBlanked replaces the lines of fenced blocks with empty ones, so a
// position in the result is the same position in the page.
func withFencesBlanked(page string) string {
	lines := strings.Split(page, "\n")
	fenced := false
	for index, line := range lines {
		opens := strings.HasPrefix(line, "```")
		if fenced || opens {
			lines[index] = strings.Repeat(" ", len(line))
		}
		if opens {
			fenced = !fenced
		}
	}

	return strings.Join(lines, "\n")
}

func TestEveryCommandHasAHeadingOnTheRightPage(t *testing.T) {
	t.Parallel()

	pages := pagesOf(t)
	for _, command := range documented(cli.NewRootCommand()) {
		page, written := pages[pageOf(command)]
		if !written {
			t.Errorf("%s: no page %s", command.CommandPath(), pageOf(command))
			continue
		}
		if count := idsOf(page)[anchor(pathOf(command))]; count != 1 {
			t.Errorf("%s: %s carries the id %q %d times, want once", command.CommandPath(), pageOf(command), anchor(pathOf(command)), count)
		}
	}
}

// A link to reference/commands/#bb-pr-merge is one anyone may have kept, and
// the index is the page it opens.
func TestTheIndexCarriesTheIDOfEveryCommand(t *testing.T) {
	t.Parallel()

	ids := idsOf(pagesOf(t)[indexPage])
	for _, command := range documented(cli.NewRootCommand()) {
		if count := ids[anchor(pathOf(command))]; count != 1 {
			t.Errorf("%s: the index carries the id %q %d times, want once", command.CommandPath(), anchor(pathOf(command)), count)
		}
	}
	if ids["bb"] != 1 || ids[globalFlagsAnchor] != 1 {
		t.Errorf("the index is missing the root command or the global flags: %v", ids)
	}
}

func TestNoPageGivesTwoThingsTheSameID(t *testing.T) {
	t.Parallel()

	for name, page := range pagesOf(t) {
		for id, count := range idsOf(page) {
			if count > 1 {
				t.Errorf("%s: id %q is given %d times", name, id, count)
			}
		}
	}
}

func TestEveryLinkLeadsSomewhere(t *testing.T) {
	t.Parallel()

	pages := pagesOf(t)
	for name, page := range pages {
		for _, match := range link.FindAllStringSubmatch(withoutCodeBlocks(page), -1) {
			target, fragment, _ := strings.Cut(match[1], "#")
			if target == "" {
				target = name
			}

			destination, written := pages[target]
			if !written {
				t.Errorf("%s: link to %q, which is not a page of the reference", name, match[1])
				continue
			}
			if fragment != "" && idsOf(destination)[fragment] != 1 {
				t.Errorf("%s: link to %q, but %s has no such id", name, match[1], target)
			}
		}
	}
}

var (
	inheritedLabel = regexp.MustCompile("(?m)^Inherited from \\[`(bb [^`]+)`\\]\\(#[a-z0-9-]+\\):$")
	flagTerm       = regexp.MustCompile("(?m)^`(?:-[a-zA-Z], )?--([a-z0-9-]+)[ `\\[]")
)

// flagsBySource splits an entry where it says a group's flags begin, and
// returns the flags listed in each part: the command's own under "", and a
// group's under its path.
func flagsBySource(t *testing.T, command *cobra.Command, entry string) map[string][]string {
	t.Helper()

	const globalLine = "Also takes the [global flags](" + globalFlagsPage + ").\n"
	flags, _, found := strings.Cut(entry, globalLine)
	if !found || strings.Count(entry, globalLine) != 1 {
		t.Fatalf("%s: its entry does not say once that it takes the global flags", command.CommandPath())
	}

	bySource := map[string][]string{}
	source := ""
	start := 0
	add := func(text string) {
		for _, term := range flagTerm.FindAllStringSubmatch(text, -1) {
			bySource[source] = append(bySource[source], term[1])
		}
	}
	for _, label := range inheritedLabel.FindAllStringSubmatchIndex(flags, -1) {
		add(flags[start:label[0]])
		source, start = flags[label[2]:label[3]], label[1]
	}
	add(flags[start:])

	return bySource
}

// Cobra's help prints a flag inherited from a group under "Global Flags", where
// an entry built from the command's own flags alone would lose it, and where
// nothing says which group it came from. The entry lists each flag once, under
// the command or the group that declares it.
func TestAnEntryListsEachFlagUnderWhereItComesFrom(t *testing.T) {
	t.Parallel()

	pages := pagesOf(t)
	root := cli.NewRootCommand()
	global := func(flag *pflag.Flag) bool {
		return root.PersistentFlags().Lookup(flag.Name) == flag
	}

	for _, command := range documented(root) {
		if !command.Runnable() {
			continue
		}

		want := map[string][]string{}
		command.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			if !flag.Hidden && flag.Name != "help" && !global(flag) {
				want[""] = append(want[""], flag.Name)
			}
		})
		command.InheritedFlags().VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden || global(flag) {
				return
			}
			for group := command.Parent(); group != nil; group = group.Parent() {
				if group.PersistentFlags().Lookup(flag.Name) == flag {
					want[group.CommandPath()] = append(want[group.CommandPath()], flag.Name)
					return
				}
			}
			t.Errorf("%s: no group above it declares --%s", command.CommandPath(), flag.Name)
		})

		got := flagsBySource(t, command, sectionOf(t, pages, command))
		for source := range got {
			if _, expected := want[source]; !expected {
				t.Errorf("%s: its entry lists flags from %q, which gives it none", command.CommandPath(), source)
			}
		}
		for source, names := range want {
			sort.Strings(names)
			listed := append([]string(nil), got[source]...)
			sort.Strings(listed)
			if strings.Join(listed, " ") != strings.Join(names, " ") {
				from := "its own flags"
				if source != "" {
					from = "the flags from " + source
				}
				t.Errorf("%s: its entry lists %s as %v, want %v", command.CommandPath(), from, listed, names)
			}
		}
	}
}

// The global flags are listed once, on a page of their own that every entry
// links to.
func TestTheGlobalFlagsAreListedOnOnePage(t *testing.T) {
	t.Parallel()

	root := cli.NewRootCommand()
	var want []string
	root.PersistentFlags().VisitAll(func(flag *pflag.Flag) {
		if !flag.Hidden {
			want = append(want, flag.Name)
		}
	})

	pages := pagesOf(t)
	var got []string
	for _, term := range flagTerm.FindAllStringSubmatch(pages[globalFlagsPage], -1) {
		got = append(got, term[1])
	}
	sort.Strings(got)
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("%s lists %v, want the root's persistent flags %v", globalFlagsPage, got, want)
	}

	for name, page := range pages {
		if name != globalFlagsPage && strings.Contains(page, "# Global flags\n") {
			t.Errorf("%s lists the global flags as well", name)
		}
	}
}

// A top-level command called index or global-flags would be written to the
// file of a page that is not a command's.
func TestACommandCannotTakeTheFileOfAnotherPage(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"index", "global-flags"} {
		root := &cobra.Command{Use: "bb"}
		root.AddCommand(&cobra.Command{Use: name, Short: "Shadows a page", Run: func(*cobra.Command, []string) {}})

		if _, err := renderReference(root); err == nil || !strings.Contains(err.Error(), "would overwrite "+name+".md") {
			t.Errorf("a command named %s: got error %v, want it refused", name, err)
		}
	}
}

func TestAFlagIsNamedTheWayHelpNamesIt(t *testing.T) {
	t.Parallel()

	root := cli.NewRootCommand()
	for _, command := range append(documented(root), root) {
		command.LocalFlags().VisitAll(func(flag *pflag.Flag) {
			if flag.Hidden || flag.Name == "help" {
				return
			}

			alone := pflag.NewFlagSet(flag.Name, pflag.ContinueOnError)
			alone.AddFlag(flag)
			lines := flagsOf(alone, nil)
			if len(lines) != 1 {
				t.Fatalf("%s --%s: got %d lines, want 1", command.CommandPath(), flag.Name, len(lines))
			}

			want := "--" + flag.Name
			if flag.Shorthand != "" {
				want = "-" + flag.Shorthand + ", " + want
			}
			if value, _ := pflag.UnquoteUsage(flag); value != "" {
				want += " " + value
			}
			if !strings.HasPrefix(lines[0].name, want) || strings.Contains(lines[0].name, flagSeparator) {
				t.Errorf("%s --%s: named %q, want it to open with %q", command.CommandPath(), flag.Name, lines[0].name, want)
			}

			_, usage := pflag.UnquoteUsage(flag)
			if !strings.HasPrefix(lines[0].usage, usage) {
				t.Errorf("%s --%s: usage %q does not open with the flag's own %q", command.CommandPath(), flag.Name, lines[0].usage, usage)
			}
		})
	}
}

// plain undoes what prose does to running text, leaving what a reader sees.
func plain(markdown string) string {
	unescape := regexp.MustCompile(`\\(.)`)

	var lines []string
	fenced := false
	for _, line := range strings.Split(markdown, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
			continue
		}
		if !fenced {
			line = html.UnescapeString(unescape.ReplaceAllString(line, "$1"))
		}
		lines = append(lines, line)
	}

	return strings.Join(lines, "\n")
}

// A description is rewrapped and escaped on its way to the page. Nothing in it
// may be dropped or changed on the way.
func TestADescriptionReachesThePageWordForWord(t *testing.T) {
	t.Parallel()

	root := cli.NewRootCommand()
	for _, command := range append(documented(root), root) {
		description := descriptionOf(command)
		want := strings.Fields(description)
		got := strings.Fields(plain(prose(description)))
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("%s: the description reads\n%s\nand the page reads\n%s", command.CommandPath(), strings.Join(want, " "), strings.Join(got, " "))
		}

		if short := plain(inline(command.Short)); short != strings.TrimSpace(command.Short) {
			t.Errorf("%s: the short description %q reaches the page as %q", command.CommandPath(), command.Short, short)
		}
	}
}

func TestEveryEntryHoldsWhatTheCommandDeclares(t *testing.T) {
	t.Parallel()

	pages := pagesOf(t)
	for _, command := range documented(cli.NewRootCommand()) {
		entry := sectionOf(t, pages, command)

		if !strings.Contains(entry, inline(command.Short)) {
			t.Errorf("%s: its entry lacks its short description", command.CommandPath())
		}
		if example := strings.TrimSpace(command.Example); example != "" && !strings.Contains(entry, "```bash\n") {
			t.Errorf("%s: its examples are not in a shell block, so docs-lint does not check them", command.CommandPath())
		}
		if !command.Runnable() {
			continue
		}

		if !strings.Contains(entry, "```text\n"+command.UseLine()+"\n```") {
			t.Errorf("%s: its entry lacks its usage line %q", command.CommandPath(), command.UseLine())
		}
		if line := cli.DryRunHelp(command); line != "" && !strings.Contains(entry, "**Dry run:** "+inline(line)) {
			t.Errorf("%s: its entry lacks what --dry-run does", command.CommandPath())
		}

		outlined := strings.Count(entry, "<details ")
		explained := strings.Count(entry, "**Output with `--json`:** ")
		if outlined+explained != 1 {
			t.Errorf("%s: its entry says what it prints %d times, want once", command.CommandPath(), outlined+explained)
		}
	}
}

// An outline left in the search index matches every word in the document a
// command returns, and buries the command that does what was searched for.
func TestEveryOutputOutlineIsKeptOutOfTheSearchIndex(t *testing.T) {
	t.Parallel()

	for name, page := range pagesOf(t) {
		if all, excluded := strings.Count(page, "<details "), strings.Count(page, "<details class=\"note\" data-search-exclude>"); all != excluded {
			t.Errorf("%s: %d of %d outlines are kept out of the search index", name, excluded, all)
		}
	}
}

// The docs build runs every page through the macros plugin, which reads [[ ]]
// as a variable and {% %} or {# #} as a statement or a comment.
func TestNoPageHoldsWhatTheDocsBuildReadsAsAMacro(t *testing.T) {
	t.Parallel()

	for name, page := range pagesOf(t) {
		for _, opening := range []string{"[[", "{%", "{#"} {
			if strings.Contains(page, opening) {
				t.Errorf("%s contains %q, which the docs build would read as a macro", name, opening)
			}
		}
	}
}

func TestTheNavigationListsEveryPage(t *testing.T) {
	t.Parallel()

	config, err := os.ReadFile(filepath.Join("..", "..", "mkdocs.yml"))
	if err != nil {
		t.Fatalf("read mkdocs.yml: %v", err)
	}

	listed := map[string]int{}
	for _, entry := range navEntry.FindAllStringSubmatch(string(config), -1) {
		listed[entry[1]]++
	}

	pages := pagesOf(t)
	for name := range pages {
		if listed[name] != 1 {
			t.Errorf("mkdocs.yml lists reference/commands/%s %d times, want once", name, listed[name])
		}
	}
	for name := range listed {
		if _, written := pages[name]; !written {
			t.Errorf("mkdocs.yml lists reference/commands/%s, which the reference does not have", name)
		}
	}
}

func TestInlineLeavesNothingForMarkdownToReadAsMarkup(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"Merge <pr-id> into its target":    "Merge &lt;pr-id&gt; into its target",
		"BITBUCKET_URL or *any* [host]":    `BITBUCKET\_URL or \*any\* \[host\]`,
		"run `bb pr list <id>` first":      "run `bb pr list <id>` first",
		"an unclosed ` stays a character":  "an unclosed \\` stays a character",
		"- not a list":                     `\- not a list`,
		": not a definition":               `\: not a definition`,
		"a & b":                            "a &amp; b",
		`a back\slash`:                     `a back\\slash`,
		"  surrounding space is dropped  ": "surrounding space is dropped",
	}
	for text, want := range cases {
		if got := inline(text); got != want {
			t.Errorf("inline(%q) = %q, want %q", text, got, want)
		}
	}
}

func TestProseSetsIndentedLinesAsCode(t *testing.T) {
	t.Parallel()

	got := prose("Reads <host> from\nthe environment.\n\nField arguments:\n  -f k=v    a string\n  -F k=v    a typed value\n\n    bb api /rest --paginate")
	want := "Reads &lt;host&gt; from the environment.\n\n" +
		"Field arguments:\n\n" +
		"```text\n-f k=v    a string\n-F k=v    a typed value\n```\n\n" +
		"```text\nbb api /rest --paginate\n```\n\n"
	if got != want {
		t.Errorf("prose wrote\n%s\nwant\n%s", got, want)
	}
}

func TestExportWritesEveryPageAndRemovesAnyOther(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	stale := filepath.Join(directory, "removed-command.md")
	if err := os.WriteFile(stale, []byte("# bb removed-command\n"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	kept := filepath.Join(directory, "notes.txt")
	if err := os.WriteFile(kept, []byte("not a page"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	if err := exportCommandReference(directory); err != nil {
		t.Fatalf("exportCommandReference failed: %v", err)
	}

	for name, want := range pagesOf(t) {
		got, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("%s on disk is not the page that was rendered", name)
		}
		if !strings.HasSuffix(want, "\n") || strings.HasSuffix(want, "\n\n") {
			t.Errorf("%s does not end with exactly one newline", name)
		}
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("the page of a command that no longer exists was left behind")
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("a file that is not a page was removed: %v", err)
	}
}

func TestExportReturnsErrorForInvalidPath(t *testing.T) {
	t.Parallel()

	filePath := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	err := exportCommandReference(filepath.Join(filePath, "commands"))
	if err == nil {
		t.Fatal("expected error for invalid path")
	}
	if !strings.Contains(err.Error(), "create command docs directory") {
		t.Fatalf("unexpected error: %v", err)
	}
}
