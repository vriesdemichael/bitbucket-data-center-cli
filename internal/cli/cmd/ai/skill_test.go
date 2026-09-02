package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	bbskill "github.com/vriesdemichael/bitbucket-data-center-cli/skills/bb"
	bbbulkskill "github.com/vriesdemichael/bitbucket-data-center-cli/skills/bb-bulk"
)

// testDeps builds a minimal Dependencies for skill tests.
func testSkillDeps(version string) Dependencies {
	return Dependencies{
		Version: func() string { return version },
		LoadConfig: func(config.Overrides) (config.AppConfig, error) {
			return config.AppConfig{}, nil
		},
		WriteJSON: func(w io.Writer, v any) error {
			return jsonoutput.Write(w, v)
		},
	}
}

// TestBuildSkillStampsVersion ensures the rendered skill names the binary.
func TestBuildSkillStampsVersion(t *testing.T) {
	skill, err := lookupSkill("bb")
	if err != nil {
		t.Fatalf("unexpected lookup error: %v", err)
	}
	result := buildSkill(skill, "1.2.3")
	if !strings.Contains(result, "1.2.3") {
		t.Fatal("buildSkill did not inject the version string")
	}

	bulkSkill, err := lookupSkill("bulk")
	if err != nil {
		t.Fatalf("unexpected lookup error: %v", err)
	}
	bulkResult := buildSkill(bulkSkill, "1.2.3")
	if !strings.Contains(bulkResult, "1.2.3") {
		t.Fatal("buildSkill did not inject the version string into bulk skill")
	}
}

// TestBuildSkillFallsBackToDev ensures an empty version string yields "dev".
func TestBuildSkillFallsBackToDev(t *testing.T) {
	skill, err := lookupSkill("bb")
	if err != nil {
		t.Fatalf("unexpected lookup error: %v", err)
	}
	result := buildSkill(skill, "")
	if !strings.Contains(result, "dev") {
		t.Fatal("buildSkill did not substitute 'dev' for empty version")
	}
}

// TestSkillShowPrintsSkillContent tests that `bb ai skill show` writes skill content to stdout.
func TestSkillShowPrintsSkillContent(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		wantSnippet string
	}{
		{
			name:        "default skill",
			args:        []string{"skill", "show"},
			wantSnippet: "# bb — Bitbucket Data Center CLI",
		},
		{
			name:        "explicit bb skill",
			args:        []string{"skill", "show", "bb"},
			wantSnippet: "# bb — Bitbucket Data Center CLI",
		},
		{
			name:        "bulk alias",
			args:        []string{"skill", "show", "bulk"},
			wantSnippet: "# bb-bulk — Multi-Repository Bulk Governance Skill",
		},
		{
			name:        "bb-bulk alias",
			args:        []string{"skill", "show", "bb-bulk"},
			wantSnippet: "# bb-bulk — Multi-Repository Bulk Governance Skill",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := New(testSkillDeps("2.0.0"))
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tt.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			out := buf.String()
			if len(out) == 0 {
				t.Fatal("skill show produced no output")
			}
			if !strings.Contains(out, "2.0.0") {
				t.Fatalf("skill show output does not contain version '2.0.0': %q", out[:min(200, len(out))])
			}
			if !strings.Contains(out, tt.wantSnippet) {
				t.Fatalf("skill show output does not contain %q: %q", tt.wantSnippet, out[:min(200, len(out))])
			}
		})
	}
}

// TestSkillShowUnknownSkillRejects ensures invalid skill name reports validation error.
func TestSkillShowUnknownSkillRejects(t *testing.T) {
	cmd := New(testSkillDeps("1.0.0"))
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"skill", "show", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown skill, got nil")
	}
	if !strings.Contains(err.Error(), "unknown skill \"nonexistent\"") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestSkillInstallUnknownSkillRejects ensures invalid skill name reports validation error on install.
func TestSkillInstallUnknownSkillRejects(t *testing.T) {
	cmd := New(testSkillDeps("1.0.0"))
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"skill", "install", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown skill, got nil")
	}
	if !strings.Contains(err.Error(), "unknown skill \"nonexistent\"") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestSkillRemoveUnknownSkillRejects ensures invalid skill name reports validation error on remove.
func TestSkillRemoveUnknownSkillRejects(t *testing.T) {
	cmd := New(testSkillDeps("1.0.0"))
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"skill", "remove", "nonexistent"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for unknown skill, got nil")
	}
	if !strings.Contains(err.Error(), "unknown skill \"nonexistent\"") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// TestSkillInstallWritesFile tests that `bb ai skill install` writes the skill file.
func TestSkillInstallWritesFile(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		relPath  string
		expected string
	}{
		{
			name:     "default skill",
			args:     []string{"skill", "install"},
			relPath:  filepath.Join(".agents", "skills", "bb", "SKILL.md"),
			expected: "# bb — Bitbucket Data Center CLI",
		},
		{
			name:     "bulk skill",
			args:     []string{"skill", "install", "bulk"},
			relPath:  filepath.Join(".agents", "skills", "bb-bulk", "SKILL.md"),
			expected: "# bb-bulk — Multi-Repository Bulk Governance Skill",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			origDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}

			cmd := New(testSkillDeps("3.1.0"))
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tt.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			dest := filepath.Join(dir, tt.relPath)
			data, err := os.ReadFile(dest)
			if err != nil {
				t.Fatalf("skill file not written: %v", err)
			}
			if !strings.Contains(string(data), "3.1.0") {
				t.Fatal("installed skill file does not contain the expected version")
			}
			if !strings.Contains(string(data), tt.expected) {
				t.Fatalf("installed skill file does not contain expected snippet %q", tt.expected)
			}
			if !strings.Contains(buf.String(), "Skill installed") {
				t.Fatalf("unexpected output: %q", buf.String())
			}
		})
	}
}

// TestSkillRemoveDeletesFile tests that `bb ai skill remove` removes an existing file.
func TestSkillRemoveDeletesFile(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		relPath string
	}{
		{
			name:    "default skill",
			args:    []string{"skill", "remove"},
			relPath: filepath.Join(".agents", "skills", "bb", "SKILL.md"),
		},
		{
			name:    "bulk skill",
			args:    []string{"skill", "remove", "bulk"},
			relPath: filepath.Join(".agents", "skills", "bb-bulk", "SKILL.md"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			origDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}

			// Pre-create the file.
			dest := filepath.Join(dir, tt.relPath)
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dest, []byte("dummy"), 0o644); err != nil {
				t.Fatal(err)
			}

			cmd := New(testSkillDeps(""))
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tt.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
				t.Fatal("expected skill file to be removed, but it still exists")
			}
			if !strings.Contains(buf.String(), "Skill removed") {
				t.Fatalf("unexpected output: %q", buf.String())
			}
		})
	}
}

// TestSkillRemoveReportsNotFound tests that remove is a no-op when the file is absent.
func TestSkillRemoveReportsNotFound(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	cmd := New(testSkillDeps(""))
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"skill", "remove"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "not found") {
		t.Fatalf("expected 'not found' message, got: %q", buf.String())
	}
}

// TestResolveInstallPathProject tests project-scoped path resolution.
func TestResolveInstallPathProject(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	bbSkill, _ := lookupSkill("bb")
	got, err := resolveInstallPath(bbSkill, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, ".agents", "skills", "bb", "SKILL.md")
	if got != want {
		t.Fatalf("project path for bb: got %q, want %q", got, want)
	}

	bulkSkill, _ := lookupSkill("bulk")
	gotBulk, err := resolveInstallPath(bulkSkill, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantBulk := filepath.Join(dir, ".agents", "skills", "bb-bulk", "SKILL.md")
	if gotBulk != wantBulk {
		t.Fatalf("project path for bulk: got %q, want %q", gotBulk, wantBulk)
	}
}

// TestResolveInstallPathGlobal tests global (home directory) path resolution.
func TestResolveInstallPathGlobal(t *testing.T) {
	bbSkill, _ := lookupSkill("bb")
	got, err := resolveInstallPath(bbSkill, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".agents", "skills", "bb", "SKILL.md")
	if got != want {
		t.Fatalf("global path for bb: got %q, want %q", got, want)
	}

	bulkSkill, _ := lookupSkill("bulk")
	gotBulk, err := resolveInstallPath(bulkSkill, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantBulk := filepath.Join(home, ".agents", "skills", "bb-bulk", "SKILL.md")
	if gotBulk != wantBulk {
		t.Fatalf("global path for bulk: got %q, want %q", gotBulk, wantBulk)
	}
}

// TestSkillInstallGlobalWritesFile tests --global flag writes to home dir.
func TestSkillInstallGlobalWritesFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir() reads USERPROFILE on Windows, HOME elsewhere

	cmd := New(testSkillDeps("4.0.0"))
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"skill", "install", "bulk", "--global"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dest := filepath.Join(home, ".agents", "skills", "bb-bulk", "SKILL.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Fatal("expected global bulk skill file to be written")
	}
}

// TestSkillShowJSONNotUsedBySkillShow ensures skill show always writes raw text, not JSON envelope.
func TestSkillShowIsPlainText(t *testing.T) {
	cmd := New(testSkillDeps("1.0.0"))
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	// Even with --json flag the skill show should output plain text (it's a template file).
	cmd.SetArgs([]string{"skill", "show"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Verify output is not a JSON envelope.
	var envelope map[string]any
	if err := json.NewDecoder(buf).Decode(&envelope); err == nil {
		if _, hasData := envelope["data"]; hasData {
			t.Fatal("skill show should not produce a JSON envelope")
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// TestCommittedSkillHasNoUnrenderedPlaceholders is the defect from #325.
//
// The committed SKILL.md is what `npx skills add` distributes, and the skill
// advertises that install path itself — so anyone following the documented
// instructions reads the file exactly as committed. A template marker left in
// it ships raw to them.
func TestCommittedSkillHasNoUnrenderedPlaceholders(t *testing.T) {
	skills := map[string][]byte{
		"bb":      bbskill.Content,
		"bb-bulk": bbbulkskill.Content,
	}

	for name, content := range skills {
		committed := string(content)
		for _, marker := range []string{"{{", "}}"} {
			if strings.Contains(committed, marker) {
				t.Errorf("committed %s/SKILL.md contains the template marker %q; it is distributed verbatim by npx", name, marker)
			}
		}
	}
}

// TestCommittedSkillDoesNotClaimToBeGenerated guards the second half of #325:
// the file used to promise that `bb ai skill show` reflected "the exact
// capabilities of your installed binary", which was never true — it is a static
// embed with one string substitution.
func TestCommittedSkillDoesNotClaimToBeGenerated(t *testing.T) {
	skills := map[string][]byte{
		"bb":      bbskill.Content,
		"bb-bulk": bbbulkskill.Content,
	}

	for name, content := range skills {
		committed := strings.ToLower(string(content))
		for _, claim := range []string{
			"exact capabilities of your installed binary",
			"version-specific skill",
		} {
			if strings.Contains(committed, claim) {
				t.Errorf("%s/SKILL.md still claims %q, which the static embed does not deliver", name, claim)
			}
		}
	}
}

// TestSkillDoesNotListMCPToolsInline guards the drift that produced #325: a
// hand-maintained copy of the tool catalogue that fell out of step with the
// server and gave no hint which tools are withheld by default.
func TestSkillDoesNotListMCPToolsInline(t *testing.T) {
	committed := string(bbskill.Content)

	// Naming a gated tool is the specific failure: an agent plans around it and
	// discovers at call time that the default server does not expose it.
	for _, gated := range gatedToolNames {
		if strings.Contains(committed, "`"+gated+"`") {
			t.Errorf("SKILL.md names the gated tool %q; point at `bb ai mcp tools` instead", gated)
		}
	}

	if !strings.Contains(committed, "bb ai mcp tools") {
		t.Error("SKILL.md should direct the reader to `bb ai mcp tools` for the catalogue")
	}
	if !strings.Contains(committed, "YOLO") {
		t.Error("SKILL.md should explain the YOLO exposure class so an agent can plan around it")
	}
}
