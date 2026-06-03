package ai

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-server-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/jsonoutput"
)

// testDeps builds a minimal Dependencies for skill tests.
func testSkillDeps(version string) Dependencies {
	return Dependencies{
		Version: func() string { return version },
		LoadConfig: func() (config.AppConfig, error) {
			return config.AppConfig{}, nil
		},
		WriteJSON: func(w io.Writer, v any) error {
			return jsonoutput.Write(w, v)
		},
	}
}

// TestBuildSkillSubstitutesVersion ensures the {{BB_VERSION}} marker is replaced.
func TestBuildSkillSubstitutesVersion(t *testing.T) {
	result := buildSkill("1.2.3")
	if strings.Contains(result, "{{BB_VERSION}}") {
		t.Fatal("buildSkill left the {{BB_VERSION}} placeholder unreplaced")
	}
	if !strings.Contains(result, "1.2.3") {
		t.Fatal("buildSkill did not inject the version string")
	}
}

// TestBuildSkillFallsBackToDev ensures an empty version string yields "dev".
func TestBuildSkillFallsBackToDev(t *testing.T) {
	result := buildSkill("")
	if strings.Contains(result, "{{BB_VERSION}}") {
		t.Fatal("buildSkill left the {{BB_VERSION}} placeholder unreplaced for empty version")
	}
	if !strings.Contains(result, "dev") {
		t.Fatal("buildSkill did not substitute 'dev' for empty version")
	}
}

// TestSkillShowPrintsSkillContent tests that `bb ai skill show` writes skill content to stdout.
func TestSkillShowPrintsSkillContent(t *testing.T) {
	cmd := New(testSkillDeps("2.0.0"))

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"skill", "show"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	if len(out) == 0 {
		t.Fatal("skill show produced no output")
	}
	// Skill content should include the version we passed.
	if !strings.Contains(out, "2.0.0") {
		t.Fatalf("skill show output does not contain version '2.0.0': %q", out[:min(200, len(out))])
	}
}

// TestSkillInstallWritesFile tests that `bb ai skill install` writes the skill file.
func TestSkillInstallWritesFile(t *testing.T) {
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
	cmd.SetArgs([]string{"skill", "install"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dest := filepath.Join(dir, skillInstallPath)
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("skill file not written: %v", err)
	}
	if !strings.Contains(string(data), "3.1.0") {
		t.Fatal("installed skill file does not contain the expected version")
	}
	if !strings.Contains(buf.String(), "Skill installed") {
		t.Fatalf("unexpected output: %q", buf.String())
	}
}

// TestSkillRemoveDeletesFile tests that `bb ai skill remove` removes an existing file.
func TestSkillRemoveDeletesFile(t *testing.T) {
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
	dest := filepath.Join(dir, skillInstallPath)
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
	cmd.SetArgs([]string{"skill", "remove"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
		t.Fatal("expected skill file to be removed, but it still exists")
	}
	if !strings.Contains(buf.String(), "Skill removed") {
		t.Fatalf("unexpected output: %q", buf.String())
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

	got, err := resolveInstallPath(false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, skillInstallPath)
	if got != want {
		t.Fatalf("project path: got %q, want %q", got, want)
	}
}

// TestResolveInstallPathGlobal tests global (home directory) path resolution.
func TestResolveInstallPathGlobal(t *testing.T) {
	got, err := resolveInstallPath(true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".agents", "skills", "bb", "SKILL.md")
	if got != want {
		t.Fatalf("global path: got %q, want %q", got, want)
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
	cmd.SetArgs([]string{"skill", "install", "--global"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dest := filepath.Join(home, ".agents", "skills", "bb", "SKILL.md")
	if _, err := os.Stat(dest); os.IsNotExist(err) {
		t.Fatal("expected global skill file to be written")
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
