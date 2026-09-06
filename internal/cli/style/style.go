// Package style provides shared terminal output styles and a table renderer.
// Call Init once from the root command before any human-readable output is produced.
package style

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

var (
	// Success is used for creation, enablement and grant confirmations (bold green).
	Success lipgloss.Style
	// Deleted is used for deletion, revocation and disablement confirmations (red).
	Deleted lipgloss.Style
	// Updated is used for update, configure and set confirmations (cyan).
	Updated lipgloss.Style
	// Warning is used for non-fatal warnings such as insecure-TLS notices (bold yellow).
	Warning lipgloss.Style
	// Label is used for key-value labels such as "Key:", "Name:", "Description:" (bold).
	Label lipgloss.Style
	// Resource is used for primary identifiers in lists and confirmations (bold).
	Resource lipgloss.Style
	// Secondary is used for supplementary identifiers like commit SHAs or plan hashes (dim).
	Secondary lipgloss.Style
	// Empty is used for "No X found" empty-state messages (dim italic).
	Empty lipgloss.Style
	// DryRun is used for dry-run preview headers (magenta).
	DryRun lipgloss.Style
	// Hint is used for follow-up command suggestions (dim).
	Hint lipgloss.Style

	renderer *lipgloss.Renderer

	// initMu guards the rebuild, and builtPlain records what the styles were
	// last built for.
	//
	// Every command calls Init from the root's PersistentPreRunE, so a process
	// running two commands at once had two goroutines writing these ten
	// package variables while a third read them to render output. The race
	// detector reported it the first time the tests ran in parallel; it had
	// been unreachable before only because nothing ran at the same time as
	// anything else.
	//
	// This narrows the window rather than closing it. The styles are still
	// package-level and still read directly at every use site, so a rebuild
	// that genuinely changes them still races with output being written. What
	// it removes is the case that actually happens: repeated Init calls
	// asking for the setting the styles already have, which now write nothing
	// at all. Closing it properly means the renderer travelling with the
	// command rather than living here.
	initMu     sync.Mutex
	built      bool
	builtPlain bool
)

func init() {
	Init(false)
}

// Init configures all package-level styles. noColor disables all ANSI formatting
// regardless of terminal detection. It is called automatically on package init with
// noColor=false, and should be called again in the root command's PersistentPreRunE
// to apply the --no-color flag and NO_COLOR environment variable.
//
// Calling it again with the setting already in force does nothing.
func Init(noColor bool) {
	_, noColorEnv := os.LookupEnv("NO_COLOR")
	plain := noColor || noColorEnv

	initMu.Lock()
	defer initMu.Unlock()

	if built && builtPlain == plain {
		return
	}

	if plain {
		renderer = lipgloss.NewRenderer(os.Stdout)
		renderer.SetColorProfile(termenv.Ascii)
	} else {
		renderer = lipgloss.DefaultRenderer()
	}
	built, builtPlain = true, plain
	rebuild()
}

// InitWithRenderer configures all package-level styles using the provided renderer.
// This is intended for testing scenarios that require a specific color profile.
// It panics if r is nil.
func InitWithRenderer(r *lipgloss.Renderer) {
	if r == nil {
		panic("style: InitWithRenderer called with nil renderer")
	}

	initMu.Lock()
	defer initMu.Unlock()

	renderer = r
	// A renderer chosen by the caller is never the one Init would have built,
	// so the next Init rebuilds rather than deciding it has nothing to do.
	built = false
	rebuild()
}

func rebuild() {
	Success = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	Deleted = renderer.NewStyle().Foreground(lipgloss.Color("1"))
	Updated = renderer.NewStyle().Foreground(lipgloss.Color("6"))
	Warning = renderer.NewStyle().Bold(true).Foreground(lipgloss.Color("3"))
	Label = renderer.NewStyle().Bold(true)
	Resource = renderer.NewStyle().Bold(true)
	Secondary = renderer.NewStyle().Faint(true)
	Empty = renderer.NewStyle().Faint(true).Italic(true)
	DryRun = renderer.NewStyle().Foreground(lipgloss.Color("5"))
	Hint = renderer.NewStyle().Faint(true)
}

// WriteTable writes rows to out as a space-aligned table. Each cell may contain
// ANSI escape sequences; widths are measured with lipgloss.Width to ensure correct
// alignment. Columns are separated by two spaces.
func WriteTable(out io.Writer, rows [][]string) {
	if len(rows) == 0 {
		return
	}

	// determine column count
	cols := 0
	for _, row := range rows {
		if len(row) > cols {
			cols = len(row)
		}
	}

	// compute max visual width per column (last column is not padded)
	widths := make([]int, cols)
	for _, row := range rows {
		for j, cell := range row {
			cw := lipgloss.Width(cell)
			if cw > widths[j] {
				widths[j] = cw
			}
		}
	}

	// write rows
	for _, row := range rows {
		parts := make([]string, len(row))
		for j, cell := range row {
			if j < len(row)-1 {
				pad := widths[j] - lipgloss.Width(cell)
				if pad < 0 {
					pad = 0
				}
				parts[j] = cell + strings.Repeat(" ", pad)
			} else {
				parts[j] = cell
			}
		}
		fmt.Fprintln(out, strings.Join(parts, "  "))
	}
}

// ActionStyle returns the style appropriate for a mutation verb or status value.
// "create"/"created"/"enabled"/"granted"/"success"/"successful" → Success,
// "delete"/"deleted"/"revoked"/"disabled"/"failed"/"error" → Deleted,
// everything else → Updated.
func ActionStyle(verb string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(verb)) {
	case "create", "created", "enabled", "granted", "success", "successful":
		return Success
	case "delete", "deleted", "revoked", "disabled", "failed", "error":
		return Deleted
	default:
		return Updated
	}
}
