package config

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The reference pages that describe configuration are written by hand, so
// these tests hold each of their tables to the code that reads what the table
// describes. A key, a policy or a variable added or dropped in one place and
// not the other fails here, rather than being found by a reader.

// repositoryRoot is the directory that holds go.mod.
func repositoryRoot(t *testing.T) string {
	t.Helper()

	directory, err := os.Getwd()
	if err != nil {
		t.Fatalf("working directory: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(directory, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			t.Fatal("no go.mod above the test's directory")
		}
		directory = parent
	}

	return directory
}

// referencePage reads a page of the documentation site.
func referencePage(t *testing.T, name string) string {
	t.Helper()

	content, err := os.ReadFile(filepath.Join(repositoryRoot(t), "docs", "site", "reference", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}

	return strings.ReplaceAll(string(content), "\r\n", "\n")
}

// section returns the lines under a heading, up to the next heading of the
// same or a higher level.
func section(t *testing.T, page, heading string) []string {
	t.Helper()

	level := strings.Index(heading, " ")
	var lines []string
	inside := false
	for _, line := range strings.Split(page, "\n") {
		if inside {
			if hashes := strings.Index(line, " "); strings.HasPrefix(line, "#") && hashes > 0 && hashes <= level && strings.Trim(line[:hashes], "#") == "" {
				break
			}
			lines = append(lines, line)
			continue
		}
		inside = line == heading
	}
	if !inside {
		t.Fatalf("no heading %q", heading)
	}

	return lines
}

// tableRows returns the cells of each row of the Markdown tables in lines,
// header and separator left out.
func tableRows(lines []string) [][]string {
	var rows [][]string
	for index, line := range lines {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		if strings.HasPrefix(strings.ReplaceAll(line, " ", ""), "|---") {
			continue
		}
		// A header is the row a separator follows.
		if index+1 < len(lines) && strings.HasPrefix(strings.ReplaceAll(lines[index+1], " ", ""), "|---") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for cell := range cells {
			cells[cell] = strings.TrimSpace(cells[cell])
		}
		rows = append(rows, cells)
	}

	return rows
}

var quoted = regexp.MustCompile("`([^`]+)`")

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	return keys
}

func assertSameKeys(t *testing.T, subject string, documented, actual map[string]bool) {
	t.Helper()

	for _, key := range sortedKeys(actual) {
		if !documented[key] {
			t.Errorf("%s: %s is read by bb and missing from the page", subject, key)
		}
	}
	for _, key := range sortedKeys(documented) {
		if !actual[key] {
			t.Errorf("%s: %s is on the page and not read by bb", subject, key)
		}
	}
}

// policyKeys are the keys of the policy block, as the system file reads them
// at its top level too.
func policyKeys() map[string]bool {
	keys := yamlKeysOf(PolicyConfig{})
	delete(keys, "$schema")

	return keys
}

// TestConfigurationPageSaysWhichFileReadsWhichKey holds the table of
// configuration.md to the keys each file's loader decodes.
func TestConfigurationPageSaysWhichFileReadsWhichKey(t *testing.T) {
	t.Parallel()

	columns := map[string]int{TierStored: 1, TierWorkspace: 2, TierSystem: 3}
	documented := map[string]map[string]bool{TierStored: {}, TierWorkspace: {}, TierSystem: {}}

	rows := tableRows(section(t, referencePage(t, "configuration.md"), "## What each file holds"))
	if len(rows) == 0 {
		t.Fatal("configuration.md has no table under What each file holds")
	}
	for _, row := range rows {
		if len(row) != 5 {
			t.Fatalf("a row with %d cells, want 5: %v", len(row), row)
		}

		keys := map[string]bool{}
		if strings.Contains(row[0], "policy keys") {
			keys = policyKeys()
			keys["policy"], keys["policies"] = true, true
		} else if match := quoted.FindStringSubmatch(row[0]); match != nil {
			keys[match[1]] = true
		} else {
			t.Fatalf("a row that names no key: %v", row)
		}

		for tier, column := range columns {
			switch row[column] {
			case "read":
				for key := range keys {
					documented[tier][key] = true
				}
			case "":
			default:
				t.Errorf("%s: the %s column says %q, want read or nothing", row[0], tier, row[column])
			}
		}
	}

	for tier := range columns {
		actual := map[string]bool{}
		for key := range tierReadKeys()[tier] {
			actual[key] = true
		}
		// Every file takes $schema, and the page says so below the table.
		delete(actual, "$schema")

		assertSameKeys(t, tier+" file", documented[tier], actual)
	}
}

// TestSystemPolicyPageListsEveryPolicyKey holds both tables of
// system-policy.md to the policy block: the keys, and the registry values that
// stand for them on Windows.
//
// The registry value names themselves are not checked: they are read in
// policy_windows.go, which builds on Windows only.
func TestSystemPolicyPageListsEveryPolicyKey(t *testing.T) {
	t.Parallel()

	page := referencePage(t, "system-policy.md")

	documented := map[string]bool{}
	for _, row := range tableRows(section(t, page, "## Keys")) {
		if match := quoted.FindStringSubmatch(row[0]); match != nil {
			documented[match[1]] = true
		}
	}
	assertSameKeys(t, "Keys", documented, policyKeys())

	// mcp_audit_file has no registry value.
	withRegistryValue := policyKeys()
	delete(withRegistryValue, "mcp_audit_file")

	inRegistry := map[string]bool{}
	for _, row := range tableRows(section(t, page, "### Windows registry values")) {
		if len(row) != 3 {
			t.Fatalf("a registry row with %d cells, want 3: %v", len(row), row)
		}
		if match := quoted.FindStringSubmatch(row[2]); match != nil {
			inRegistry[match[1]] = true
		}
	}
	assertSameKeys(t, "Windows registry values", inRegistry, withRegistryValue)
}

// variableNotForUsers are names bb's code holds that are not settings a user
// has: each is honoured under go test only.
var variableNotForUsers = map[string]string{
	"BB_SYSTEM_CONFIG_PATH": "redirects the system file under go test only (ADR-058)",
}

// variableReadByALibrary are documented variables whose name no Go file of
// bb's spells out, because a library derives it.
var variableReadByALibrary = map[string]string{
	"BB_ACTIVE_HELP": "Cobra reads <PROGRAM>_ACTIVE_HELP, from the root command's name",
}

// variablesInSource are the BB_ and BITBUCKET_ names bb's own Go files spell
// out, tests left aside: the variables bb reads, but the ones a library reads
// for it.
func variablesInSource(t *testing.T) map[string]bool {
	t.Helper()

	literal := regexp.MustCompile(`"((?:BB|BITBUCKET)_[A-Z0-9_]+)"`)
	inCode := map[string]bool{}
	root := repositoryRoot(t)
	for _, directory := range []string{"internal", "cmd"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, match := range literal.FindAllStringSubmatch(string(content), -1) {
				inCode[match[1]] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", directory, err)
		}
	}
	if len(inCode) < 20 {
		t.Fatalf("found only %d variable names in the source; the walk has stopped reaching it", len(inCode))
	}

	return inCode
}

// TestEnvironmentPageNamesEveryVariableBBReads holds environment.md to the
// BB_ and BITBUCKET_ names in bb's source: a variable in a table has to be one
// the code names, and every name the code holds has to be on the page.
func TestEnvironmentPageNamesEveryVariableBBReads(t *testing.T) {
	t.Parallel()

	page := referencePage(t, "environment.md")

	variable := regexp.MustCompile(`^(BB|BITBUCKET)_[A-Z0-9_]+$`)
	inTables, onPage := map[string]bool{}, map[string]bool{}
	for _, row := range tableRows(strings.Split(page, "\n")) {
		for _, match := range quoted.FindAllStringSubmatch(row[0], -1) {
			if variable.MatchString(match[1]) {
				inTables[match[1]] = true
			}
		}
	}
	// Line by line: a code fence is three backticks, and read across lines it
	// would pair each backtick after it with the wrong one.
	for _, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			continue
		}
		for _, match := range quoted.FindAllStringSubmatch(line, -1) {
			onPage[match[1]] = true
		}
	}

	inCode := variablesInSource(t)

	for _, name := range sortedKeys(inTables) {
		if !inCode[name] && variableReadByALibrary[name] == "" {
			t.Errorf("environment.md documents %s, and no Go file of bb's names it", name)
		}
	}
	for _, name := range sortedKeys(inCode) {
		if !onPage[name] && variableNotForUsers[name] == "" {
			t.Errorf("bb's source names %s, and environment.md does not", name)
		}
	}
	for name := range variableNotForUsers {
		if !inCode[name] {
			t.Errorf("%s is excused as not for users, and the source no longer names it", name)
		}
	}
	for name := range variableReadByALibrary {
		if !inTables[name] {
			t.Errorf("%s is excused as read by a library, and environment.md no longer lists it", name)
		}
	}
}
