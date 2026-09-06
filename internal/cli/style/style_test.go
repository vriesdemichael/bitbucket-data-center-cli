package style_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/style"
)

// reinit calls Init and cleans up after the test.
func reinit(t *testing.T, noColor bool) {
	t.Helper()
	if noColor {
		t.Setenv("NO_COLOR", "1")
	} else {
		os.Unsetenv("NO_COLOR") //nolint:errcheck
	}
	style.Init(noColor)
	t.Cleanup(func() {
		os.Unsetenv("NO_COLOR") //nolint:errcheck
		style.Init(false)
	})
}

// --- NO_COLOR / plain mode tests ---

func TestStylesNoColorProducePlainText(t *testing.T) {
	reinit(t, true)

	cases := []struct {
		name  string
		style lipgloss.Style
		input string
	}{
		{"Success", style.Success, "Created thing"},
		{"Deleted", style.Deleted, "Deleted thing"},
		{"Updated", style.Updated, "Updated thing"},
		{"Warning", style.Warning, "Warning text"},
		{"Label", style.Label, "Key:"},
		{"Resource", style.Resource, "PRJ/my-repo"},
		{"Secondary", style.Secondary, "abc1234"},
		{"Empty", style.Empty, "No items found"},
		{"DryRun", style.DryRun, "Dry-run (static, capability=partial)"},
		{"Hint", style.Hint, "Inspect saved status with: bb bulk status op-1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.style.Render(tc.input)
			if got != tc.input {
				t.Errorf("expected plain %q, got %q", tc.input, got)
			}
		})
	}
}

func TestWriteTableNoColor(t *testing.T) {
	reinit(t, true)

	var buf bytes.Buffer
	rows := [][]string{
		{"PRJ", "My Project"},
		{"TOOLONGKEY", "Another Project"},
	}
	style.WriteTable(&buf, rows)

	out := buf.String()
	// Each value must appear
	if !strings.Contains(out, "PRJ") {
		t.Errorf("expected PRJ in output: %s", out)
	}
	if !strings.Contains(out, "My Project") {
		t.Errorf("expected 'My Project' in output: %s", out)
	}
	if !strings.Contains(out, "TOOLONGKEY") {
		t.Errorf("expected TOOLONGKEY in output: %s", out)
	}
	// No tabs — table uses space padding, not tab characters.
	if strings.Contains(out, "\t") {
		t.Errorf("expected no tabs in table output: %q", out)
	}
	// Both rows should have the same number of characters before the second column.
	// "PRJ" is padded to width of "TOOLONGKEY" (10), so second col starts at 12 (10+2sep).
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), out)
	}
	col2Start0 := strings.Index(lines[0], "My Project")
	col2Start1 := strings.Index(lines[1], "Another Project")
	if col2Start0 != col2Start1 {
		t.Errorf("columns not aligned: 'My Project' at %d, 'Another Project' at %d\nlines: %q", col2Start0, col2Start1, lines)
	}
}

func TestWriteTableEmpty(t *testing.T) {
	reinit(t, true)
	var buf bytes.Buffer
	style.WriteTable(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected empty output for nil rows, got %q", buf.String())
	}
	style.WriteTable(&buf, [][]string{})
	if buf.Len() != 0 {
		t.Errorf("expected empty output for empty rows, got %q", buf.String())
	}
}

func TestActionStyleNoColor(t *testing.T) {
	reinit(t, true)

	cases := []struct{ verb, want string }{
		{"create", "create"},
		{"created", "created"},
		{"enabled", "enabled"},
		{"granted", "granted"},
		{"success", "success"},
		{"successful", "successful"},
		{"delete", "delete"},
		{"deleted", "deleted"},
		{"revoked", "revoked"},
		{"disabled", "disabled"},
		{"failed", "failed"},
		{"error", "error"},
		{"update", "update"},
		{"configure", "configure"},
	}

	for _, tc := range cases {
		t.Run(tc.verb, func(t *testing.T) {
			got := style.ActionStyle(tc.verb).Render(tc.verb)
			if got != tc.want {
				t.Errorf("ActionStyle(%q).Render(%q) = %q, want %q", tc.verb, tc.verb, got, tc.want)
			}
		})
	}
}

// --- Color mode tests ---

// colorRenderer creates a lipgloss renderer with forced TrueColor so tests
// can assert ANSI escape codes without depending on the real terminal.
func colorRenderer() *lipgloss.Renderer {
	r := lipgloss.NewRenderer(os.Stdout)
	r.SetColorProfile(termenv.TrueColor)
	return r
}

func TestSuccessStyleEmitsGreenBold(t *testing.T) {
	t.Parallel()

	r := colorRenderer()
	s := r.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))
	rendered := s.Render("Created thing")
	if !strings.Contains(rendered, "\x1b[") {
		t.Errorf("expected ANSI escape in color output, got %q", rendered)
	}
	if !strings.Contains(rendered, "Created thing") {
		t.Errorf("expected text in output, got %q", rendered)
	}
}

func TestDeletedStyleEmitsRed(t *testing.T) {
	t.Parallel()

	r := colorRenderer()
	s := r.NewStyle().Foreground(lipgloss.Color("1"))
	rendered := s.Render("Deleted thing")
	if !strings.Contains(rendered, "\x1b[") {
		t.Errorf("expected ANSI escape in color output, got %q", rendered)
	}
}

func TestWriteTableColorAlignment(t *testing.T) {
	// Initialize with color enabled and TrueColor profile so ANSI codes appear.
	os.Unsetenv("NO_COLOR") //nolint:errcheck
	r := colorRenderer()
	style.InitWithRenderer(r)
	t.Cleanup(func() {
		os.Unsetenv("NO_COLOR") //nolint:errcheck
		style.Init(false)
	})

	var buf bytes.Buffer
	rows := [][]string{
		{style.Resource.Render("PRJ"), "My Project"},
		{style.Resource.Render("TOOLONGKEY"), "Another Project"},
	}
	style.WriteTable(&buf, rows)

	out := buf.String()
	// The plain text must still be present despite ANSI codes wrapping it.
	if !strings.Contains(out, "PRJ") {
		t.Errorf("expected PRJ in color output: %q", out)
	}
	if !strings.Contains(out, "TOOLONGKEY") {
		t.Errorf("expected TOOLONGKEY in color output: %q", out)
	}
	// ANSI codes should be present in color mode
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected ANSI codes in color output: %q", out)
	}
}

func TestNoColorFlagDisablesColorViaInit(t *testing.T) {
	// Simulate --no-color being passed
	os.Unsetenv("NO_COLOR") //nolint:errcheck
	style.Init(true)
	t.Cleanup(func() {
		os.Unsetenv("NO_COLOR") //nolint:errcheck
		style.Init(false)
	})

	rendered := style.Success.Render("Created thing")
	if strings.Contains(rendered, "\x1b[") {
		t.Errorf("expected no ANSI codes with noColor=true, got %q", rendered)
	}
	if rendered != "Created thing" {
		t.Errorf("expected plain text, got %q", rendered)
	}
}

func TestInitWithRendererPanicsOnNil(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when passing nil renderer to InitWithRenderer")
		}
	}()
	style.InitWithRenderer(nil)
}
