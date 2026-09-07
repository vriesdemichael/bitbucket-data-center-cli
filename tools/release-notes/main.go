// Command release-notes writes the release body and the machine-readable
// changelog for a version.
//
// It reads commits through the conventionalcommits package, which is also what
// decided the version these notes belong to, so the notes and the number cannot
// disagree about which commits break things (ADR-065). Before that package
// existed they did: the bump asked whether a body contained "BREAKING CHANGE:"
// anywhere while the notes matched a footer anchored to a line, so a commit
// mentioning the phrase mid-sentence cut a major release and was not listed as
// breaking in the notes it cut.
//
// Two files come out: RELEASE_NOTES.md, which becomes the GitHub release body
// and is rendered into the versioned docs snapshot, and changelog.json, which
// is published as a release asset.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	cc "github.com/vriesdemichael/bitbucket-data-center-cli/tools/conventionalcommits"
)

// sectionTitles maps a commit type onto the heading it is listed under.
// Anything absent lands in Other.
var sectionTitles = map[string]string{
	"feat":     "Features",
	"fix":      "Fixes",
	"perf":     "Performance",
	"refactor": "Refactors",
	"docs":     "Docs",
	"test":     "Tests",
	"build":    "Build",
	"ci":       "CI",
	"chore":    "Chores",
}

var orderedSections = []string{
	"Features",
	"Fixes",
	"Performance",
	"Refactors",
	"Docs",
	"Tests",
	"Build",
	"CI",
	"Chores",
	"Other",
}

// collapseLedgerAbove is where the ledger folds into a <details>. Breaking
// changes stay in the open however many there are: they are the part a reader
// has to act on. The rest folds away once it is long enough to bury everything
// above it, which for a milestone release means several hundred bullets between
// the reader and anything that tells them what changed.
const collapseLedgerAbove = 40

// entry is one commit as the changelog reports it. The field order is the key
// order in changelog.json.
type entry struct {
	SHA         string  `json:"sha"`
	ShortSHA    string  `json:"shortSha"`
	Subject     string  `json:"subject"`
	Description string  `json:"description"`
	Scope       *string `json:"scope"`
	Type        string  `json:"type"`
	Section     string  `json:"section"`
	Breaking    bool    `json:"breaking"`
	URL         string  `json:"url"`
	// BreakingNote is set only on the breakingChanges entries, so it is absent
	// rather than empty on the ledger.
	BreakingNote *string `json:"breakingNote,omitempty"`
}

type payload struct {
	Version         string  `json:"version"`
	PreviousTag     string  `json:"previousTag"`
	CompareURL      string  `json:"compareUrl"`
	Commits         []entry `json:"commits"`
	BreakingChanges []entry `json:"breakingChanges"`
}

type settings struct {
	version       string
	previousTag   string
	repositoryURL string
	// preambleDir holds the hand-written introductions, one file per version.
	preambleDir string
}

// preamble is the written introduction for this release, if there is one.
//
// It has to be assembled here rather than added afterwards. The body is
// rendered into the versioned docs snapshot in the same run, and a mike
// snapshot is immutable in practice -- no later commit reaches the /vX.Y.Z/
// page (#542). Prose that is not in the file when the release is created never
// appears there at all.
//
// Absent for an ordinary release, which is why nothing changes for one.
func (s settings) preamble() (string, error) {
	raw, err := os.ReadFile(filepath.Join(s.preambleDir, s.version+".md"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}

		return "", err
	}

	return strings.TrimSpace(string(raw)), nil
}

func render(config settings, commits []cc.Commit) (markdown string, data payload, err error) {
	grouped := map[string][]entry{}
	all := []entry{}
	breaking := []entry{}

	for _, commit := range commits {
		section, known := sectionTitles[commit.Type]
		if !known {
			section = "Other"
		}

		item := entry{
			SHA:         commit.SHA,
			ShortSHA:    commit.ShortSHA(),
			Subject:     commit.Subject,
			Description: commit.Description,
			Scope:       commit.Scope,
			Type:        commit.Type,
			Section:     section,
			Breaking:    commit.Breaking,
			URL:         fmt.Sprintf("%s/commit/%s", config.repositoryURL, commit.SHA),
		}

		grouped[section] = append(grouped[section], item)
		all = append(all, item)

		if commit.Breaking {
			note := commit.BreakingNote
			withNote := item
			withNote.BreakingNote = &note
			breaking = append(breaking, withNote)
		}
	}

	compareURL := ""
	if config.previousTag != "" {
		compareURL = fmt.Sprintf("%s/compare/%s...%s", config.repositoryURL, config.previousTag, config.version)
	}

	lines := []string{"## " + config.version, ""}

	introduction, err := config.preamble()
	if err != nil {
		return "", payload{}, err
	}
	if introduction != "" {
		lines = append(lines, introduction, "")
	}

	if config.previousTag != "" {
		lines = append(lines,
			fmt.Sprintf("Changes since %s.", config.previousTag),
			"",
			fmt.Sprintf("Compare: [%s...%s](%s)", config.previousTag, config.version, compareURL),
			"",
		)
	} else {
		lines = append(lines, "Initial release changes.", "")
	}

	if len(breaking) > 0 {
		lines = append(lines, "### ⚠ Breaking Changes")
		for _, item := range breaking {
			note := ""
			if item.BreakingNote != nil && *item.BreakingNote != "" {
				note = " — " + *item.BreakingNote
			}
			lines = append(lines, fmt.Sprintf("- %s ([%s](%s))%s", item.Description, item.ShortSHA, item.URL, note))
		}
		lines = append(lines, "")
	}

	collapse := len(all) > collapseLedgerAbove
	if collapse {
		lines = append(lines, "<details>", fmt.Sprintf("<summary>All %d changes</summary>", len(all)), "")
	}

	for _, section := range orderedSections {
		entries := grouped[section]
		if len(entries) == 0 {
			continue
		}

		lines = append(lines, "### "+section)
		for _, item := range entries {
			described := item.Description
			if item.Scope != nil && *item.Scope != "" {
				described = *item.Scope + ": " + item.Description
			}
			lines = append(lines, fmt.Sprintf("- %s ([%s](%s))", described, item.ShortSHA, item.URL))
		}
		lines = append(lines, "")
	}

	if collapse {
		lines = append(lines, "</details>", "")
	}

	markdown = strings.TrimRight(strings.Join(lines, "\n"), " \t\n\r\v\f") + "\n"

	return markdown, payload{
		Version:         config.version,
		PreviousTag:     config.previousTag,
		CompareURL:      compareURL,
		Commits:         all,
		BreakingChanges: breaking,
	}, nil
}

// encode writes the changelog the way the previous generator did: two-space
// indent, and no HTML escaping, so a subject containing < or & stays readable.
func encode(data payload) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(data); err != nil {
		return nil, err
	}

	return buffer.Bytes(), nil
}

func main() {
	config := settings{
		version:       os.Getenv("VERSION"),
		previousTag:   strings.TrimSpace(os.Getenv("PREVIOUS_TAG")),
		repositoryURL: strings.TrimRight(os.Getenv("REPOSITORY_URL"), "/"),
		preambleDir:   filepath.Join("docs", "release-notes"),
	}

	if config.version == "" || config.repositoryURL == "" {
		fmt.Fprintln(os.Stderr, "VERSION and REPOSITORY_URL are required.")
		os.Exit(1)
	}

	rangeSpec := "HEAD"
	if config.previousTag != "" {
		rangeSpec = config.previousTag + "..HEAD"
	}

	raw, err := exec.Command("git", cc.LogArgs(rangeSpec)...).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "read the commits in %s: %v\n", rangeSpec, err)
		os.Exit(1)
	}

	markdown, data, err := render(config, cc.ParseLog(string(raw)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "render the notes: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile("RELEASE_NOTES.md", []byte(markdown), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write RELEASE_NOTES.md: %v\n", err)
		os.Exit(1)
	}

	encoded, err := encode(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode the changelog: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile("changelog.json", encoded, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write changelog.json: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("%s: %d changes, %d breaking.\n", config.version, len(data.Commits), len(data.BreakingChanges))
}
