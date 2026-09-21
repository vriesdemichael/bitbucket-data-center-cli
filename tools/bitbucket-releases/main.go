// Command bitbucket-releases keeps the window of Bitbucket Data Center
// releases bb serves honest.
//
// ADR-088 says bb serves every release Atlassian supports and never drops one,
// that every release in the window passes the live suite at least once, and
// that each difference between releases is catalogued. Three things had to
// agree for that to be true, and nothing checked that they did: the window
// itself, the differences declared in internal/compat, and the table in
// docs/site/reference/bitbucket-versions.md a reader decides on.
//
// docs/quality/bitbucket-releases.json is the window. It is a baseline in the
// sense ADR-045 means: an assertion about what must remain true, verified by
// reading sources rather than by running anything. Which releases passed on
// which day is a measurement, and stays out of the repository -- `task
// test:live:matrix` produces it into .tmp/ for whoever wants to see it again.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/compat"
)

// window is docs/quality/bitbucket-releases.json.
type window struct {
	Version int `json:"version"`
	// OldestServed is the release bb states it serves from, as the versions
	// page states it: major and minor, because a floor is not a patch.
	OldestServed string `json:"oldestServed"`
	// Releases are the exact releases the live suite is run against, one patch
	// per minor in the window. Nothing in the repository can derive this list:
	// which patch of 9.4 to run is a choice, and it is made here.
	Releases []string `json:"releases"`
}

// difference is one compat.Difference as the package declares it.
type difference struct {
	Name  string
	What  string
	Since compat.Release
}

const (
	windowPath        = "docs/quality/bitbucket-releases.json"
	compatDir         = "internal/compat"
	versionsPagePath  = "docs/site/reference/bitbucket-versions.md"
	harnessDockerfile = "docker/harness/Dockerfile"
)

// harnessImage is the release the stack provisions, which ADR-042 keeps in the
// Dockerfile and nowhere else.
var harnessImage = regexp.MustCompile(`(?m)^FROM\s+atlassian/bitbucket:(\S+)`)

func main() {
	path := flag.String("window", windowPath, "The committed window of releases")
	write := flag.Bool("write", false, "Rewrite the window, sorted and normalised")
	verify := flag.Bool("verify", false, "Fail when the window, internal/compat and the versions page disagree")
	list := flag.Bool("list", false, "Print one release per line, oldest first, for a script to loop over")
	flag.Parse()

	recorded, err := readWindow(*path)
	if err != nil {
		fail(err)
	}

	switch {
	case *list:
		sortReleases(recorded.Releases)
		for _, release := range recorded.Releases {
			fmt.Println(release)
		}
	case *write:
		sortReleases(recorded.Releases)
		if err := writeWindow(*path, recorded); err != nil {
			fail(err)
		}
		fmt.Printf("%s: %d releases, oldest served %s\n", *path, len(recorded.Releases), recorded.OldestServed)
	case *verify:
		problems, err := check(recorded)
		if err != nil {
			fail(err)
		}
		if len(problems) > 0 {
			for _, problem := range problems {
				fmt.Fprintln(os.Stderr, problem)
			}
			os.Exit(1)
		}
		fmt.Printf("%d releases served from %s, %d differences catalogued\n", len(recorded.Releases), recorded.OldestServed, len(mustDifferences()))
	default:
		fmt.Printf("bb serves Bitbucket Data Center %s and every release after it.\n", recorded.OldestServed)
		fmt.Printf("The live suite is run against: %s\n", strings.Join(recorded.Releases, ", "))
		for _, found := range mustDifferences() {
			fmt.Printf("  from %d.%d  %s\n", found.Since.Major, found.Since.Minor, found.What)
		}
	}
}

// check reports every way the window, the code and the page disagree, rather
// than the first: a reader fixing one wants to see the others.
func check(recorded window) ([]string, error) {
	problems := []string{}

	if len(recorded.Releases) == 0 {
		return append(problems, windowPath+" names no releases"), nil
	}

	releases, err := parseAll(recorded.Releases)
	if err != nil {
		return nil, err
	}
	sorted := append([]compat.Release(nil), releases...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Before(sorted[j]) })
	for index := range releases {
		if releases[index] != sorted[index] {
			problems = append(problems, fmt.Sprintf("%s lists releases out of order; run `task quality:bitbucket-releases:update`", windowPath))
			break
		}
	}
	for index := 1; index < len(sorted); index++ {
		if sorted[index] == sorted[index-1] {
			problems = append(problems, fmt.Sprintf("%s lists %s twice", windowPath, sorted[index]))
		}
	}

	oldest, newest := sorted[0], sorted[len(sorted)-1]
	if floor := fmt.Sprintf("%d.%d", oldest.Major, oldest.Minor); floor != recorded.OldestServed {
		problems = append(problems, fmt.Sprintf("%s serves from %s but its oldest release is %s", windowPath, recorded.OldestServed, oldest))
	}

	provisioned, err := harnessRelease()
	if err != nil {
		return nil, err
	}
	if provisioned != newest {
		problems = append(problems, fmt.Sprintf(
			"the stack provisions %s and the newest release in %s is %s. Add it once the live suite has passed against it -- CI runs that on every pull request",
			provisioned, windowPath, newest))
	}

	differences, err := differencesIn(compatDir)
	if err != nil {
		return nil, err
	}
	page, err := os.ReadFile(versionsPagePath)
	if err != nil {
		return nil, err
	}
	problems = append(problems, compareToPage(differences, string(page), recorded, oldest, newest)...)

	return problems, nil
}

// compareToPage checks the catalogue a reader sees against the differences the
// code acts on. They are compared by the release each names, which is the part
// that decides what bb does; the wording is for the reader and is not checked.
func compareToPage(differences []difference, page string, recorded window, oldest, newest compat.Release) []string {
	problems := []string{}

	if !strings.Contains(page, "Data Center "+recorded.OldestServed+" and every release after it") {
		problems = append(problems, fmt.Sprintf("%s does not state that bb serves %s and every release after it", versionsPagePath, recorded.OldestServed))
	}

	documented := map[string]int{}
	for _, from := range tableReleases(page) {
		documented[from]++
	}

	declared := map[string]int{}
	for _, found := range differences {
		key := fmt.Sprintf("%d.%d", found.Since.Major, found.Since.Minor)
		declared[key]++

		if found.Since.Before(oldest) {
			problems = append(problems, fmt.Sprintf(
				"%s arrived in %s, before the oldest release served (%s), so every release in the window has it", found.Name, key, oldest))
		}
		if newest.Before(found.Since) {
			problems = append(problems, fmt.Sprintf(
				"%s arrives in %s, after the newest release tested (%s)", found.Name, key, newest))
		}
	}

	for key, count := range declared {
		if documented[key] != count {
			problems = append(problems, fmt.Sprintf(
				"internal/compat declares %d difference(s) from %s and %s catalogues %d", count, key, versionsPagePath, documented[key]))
		}
	}
	for key, count := range documented {
		if declared[key] == 0 {
			problems = append(problems, fmt.Sprintf(
				"%s catalogues %d difference(s) from %s that internal/compat does not declare", versionsPagePath, count, key))
		}
	}

	return problems
}

// tableRow matches a row of the catalogue table: the capability, the release it
// arrived in, and what an earlier release does.
var tableRow = regexp.MustCompile(`(?m)^\|[^|\n]+\|\s*([0-9]+\.[0-9]+)\s*\|`)

// tableReleases is the From column of every row in the catalogue.
func tableReleases(page string) []string {
	found := []string{}
	for _, match := range tableRow.FindAllStringSubmatch(page, -1) {
		found = append(found, match[1])
	}

	return found
}

// differencesIn reads the differences a package declares.
//
// The package-level vars are read rather than a list the package exports,
// because a list can be forgotten: a difference that is declared and used but
// never registered would pass a check built on one.
func differencesIn(dir string) ([]difference, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	found := []difference{}
	fileSet := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fileSet, filepath.Join(dir, name), nil, 0)
		if err != nil {
			return nil, err
		}
		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.VAR {
				continue
			}
			for _, spec := range general.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, expression := range value.Values {
					declared, ok := differenceFrom(expression)
					if !ok {
						continue
					}
					if index < len(value.Names) {
						declared.Name = value.Names[index].Name
					}
					found = append(found, declared)
				}
			}
		}
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })

	return found, nil
}

// differenceFrom reads a Difference{What: ..., Since: Release{...}} literal.
func differenceFrom(expression ast.Expr) (difference, bool) {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return difference{}, false
	}
	if name, ok := literal.Type.(*ast.Ident); !ok || name.Name != "Difference" {
		return difference{}, false
	}

	declared := difference{}
	for _, element := range literal.Elts {
		pair, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, ok := pair.Key.(*ast.Ident)
		if !ok {
			continue
		}
		switch key.Name {
		case "What":
			if text, ok := pair.Value.(*ast.BasicLit); ok && text.Kind == token.STRING {
				if unquoted, err := strconv.Unquote(text.Value); err == nil {
					declared.What = unquoted
				}
			}
		case "Since":
			declared.Since = releaseFrom(pair.Value)
		}
	}

	return declared, true
}

// releaseFrom reads a Release{Major: a, Minor: b} literal, keyed or positional.
func releaseFrom(expression ast.Expr) compat.Release {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return compat.Release{}
	}

	release := compat.Release{}
	for index, element := range literal.Elts {
		if pair, ok := element.(*ast.KeyValueExpr); ok {
			key, ok := pair.Key.(*ast.Ident)
			if !ok {
				continue
			}
			number := numberFrom(pair.Value)
			switch key.Name {
			case "Major":
				release.Major = number
			case "Minor":
				release.Minor = number
			case "Patch":
				release.Patch = number
			}

			continue
		}

		switch index {
		case 0:
			release.Major = numberFrom(element)
		case 1:
			release.Minor = numberFrom(element)
		case 2:
			release.Patch = numberFrom(element)
		}
	}

	return release
}

func numberFrom(expression ast.Expr) int {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.INT {
		return 0
	}
	number, err := strconv.Atoi(literal.Value)
	if err != nil {
		return 0
	}

	return number
}

// harnessRelease is the release the container stack provisions.
func harnessRelease() (compat.Release, error) {
	dockerfile, err := os.ReadFile(harnessDockerfile)
	if err != nil {
		return compat.Release{}, err
	}
	match := harnessImage.FindSubmatch(dockerfile)
	if match == nil {
		return compat.Release{}, fmt.Errorf("%s names no atlassian/bitbucket image", harnessDockerfile)
	}

	return compat.ParseRelease(string(match[1]))
}

func parseAll(releases []string) ([]compat.Release, error) {
	parsed := make([]compat.Release, 0, len(releases))
	for _, release := range releases {
		one, err := compat.ParseRelease(release)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", windowPath, err)
		}
		parsed = append(parsed, one)
	}

	return parsed, nil
}

func sortReleases(releases []string) {
	sort.Slice(releases, func(i, j int) bool {
		left, leftErr := compat.ParseRelease(releases[i])
		right, rightErr := compat.ParseRelease(releases[j])
		if leftErr != nil || rightErr != nil {
			return releases[i] < releases[j]
		}

		return left.Before(right)
	})
}

func readWindow(path string) (window, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return window{}, err
	}
	recorded := window{}
	if err := json.Unmarshal(raw, &recorded); err != nil {
		return window{}, fmt.Errorf("%s: %w", path, err)
	}

	return recorded, nil
}

func writeWindow(path string, recorded window) error {
	encoded, err := json.MarshalIndent(recorded, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, append(encoded, '\n'), 0o644)
}

func mustDifferences() []difference {
	found, err := differencesIn(compatDir)
	if err != nil {
		fail(err)
	}

	return found
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
