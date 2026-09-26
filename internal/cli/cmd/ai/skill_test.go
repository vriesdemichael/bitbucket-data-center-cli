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
	t.Parallel()

	skill, err := lookupSkill("bb")
	if err != nil {
		t.Fatalf("unexpected lookup error: %v", err)
	}
	result := buildSkill(skill, "1.2.3")
	if !strings.Contains(result, "1.2.3") {
		t.Fatal("buildSkill did not inject the version string")
	}
}

// TestBuildSkillFallsBackToDev ensures an empty version string yields "dev".
func TestBuildSkillFallsBackToDev(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
			relPath:  filepath.Join("skills", "bb", "SKILL.md"),
			expected: "# bb — Bitbucket Data Center CLI",
		},
		{
			name:     "named skill",
			args:     []string{"skill", "install", "bb"},
			relPath:  filepath.Join("skills", "bb", "SKILL.md"),
			expected: "# bb — Bitbucket Data Center CLI",
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

			// Where most agents read it, and where Claude Code, which reads no
			// other place, does.
			for _, agents := range []string{".agents", ".claude"} {
				dest := filepath.Join(dir, agents, tt.relPath)
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
			}
			if !strings.Contains(buf.String(), "Skill installed") {
				t.Fatalf("unexpected output: %q", buf.String())
			}
		})
	}
}

// TestBbDoctorSeesWhatInstallWrites holds the exports bb doctor reads to the
// install command: a skill doctor calls current must be byte for byte the file
// install wrote, in the place install wrote it.
func TestBbDoctorSeesWhatInstallWrites(t *testing.T) {
	// The working directory as the OS reports it, which on macOS is the
	// temporary directory with /var resolved to /private/var.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)

	for _, skill := range Skills {
		cmd := New(testSkillDeps("5.6.7"))
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		cmd.SetArgs([]string{"skill", "install", skill.Name})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("install %s: %v", skill.Name, err)
		}

		written, err := os.ReadFile(skill.Path(dir, agentsSkills))
		if err != nil {
			t.Fatalf("%s is not where Path says: %v", skill.Name, err)
		}
		if string(written) != skill.Rendered("5.6.7") {
			t.Errorf("%s: install wrote something other than Rendered", skill.Name)
		}
		if !strings.HasPrefix(skill.Rendered("5.6.7"), strings.TrimRight(skill.Repository(), "\n")) {
			t.Errorf("%s: Repository is not the skill Rendered stamps", skill.Name)
		}
	}

	if got, want := Skills[0].Repository(), string(bbskill.Content); got != want {
		t.Error("Repository is not the skill as the repository holds it")
	}

	want := map[string]string{
		"agents": filepath.Join(dir, ".agents", "skills", "bb", "SKILL.md"),
		"claude": filepath.Join(dir, ".claude", "skills", "bb", "SKILL.md"),
	}
	if len(SkillLocations) != len(want) {
		t.Fatalf("SkillLocations = %+v, want %d", SkillLocations, len(want))
	}
	for _, location := range SkillLocations {
		if got := Skills[0].Path(dir, location); got != want[location.Name] {
			t.Errorf("%s: Path = %q, want %q", location.Name, got, want[location.Name])
		}
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
			relPath: filepath.Join("skills", "bb", "SKILL.md"),
		},
		{
			name:    "named skill",
			args:    []string{"skill", "remove", "bb"},
			relPath: filepath.Join("skills", "bb", "SKILL.md"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// As the OS reports the working directory: on macOS the temporary
			// directory with /var resolved to /private/var, which is what the
			// output names.
			dir, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			origDir, err := os.Getwd()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chdir(origDir) })
			if err := os.Chdir(dir); err != nil {
				t.Fatal(err)
			}

			// Pre-create both copies install writes.
			copies := []string{filepath.Join(dir, ".agents", tt.relPath), filepath.Join(dir, ".claude", tt.relPath)}
			for _, dest := range copies {
				if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(dest, []byte("dummy"), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			cmd := New(testSkillDeps(""))
			buf := &bytes.Buffer{}
			cmd.SetOut(buf)
			cmd.SetErr(buf)
			cmd.SetArgs(tt.args)

			if err := cmd.Execute(); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, dest := range copies {
				if _, statErr := os.Stat(dest); !os.IsNotExist(statErr) {
					t.Fatalf("expected %s to be removed, but it still exists", dest)
				}
				if _, statErr := os.Stat(filepath.Dir(dest)); !os.IsNotExist(statErr) {
					t.Errorf("the skill's emptied directory %s is still there", filepath.Dir(dest))
				}
				if !strings.Contains(buf.String(), "Skill removed: "+dest) {
					t.Fatalf("the output does not name %s: %q", dest, buf.String())
				}
			}
		})
	}
}

// TestSkillRemoveLeavesWhatElseIsInTheSkillsDirectory: remove takes out the
// file install wrote, and the directory only when nothing else is in it.
func TestSkillRemoveLeavesWhatElseIsInTheSkillsDirectory(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	skill := filepath.Join(dir, ".agents", "skills", "bb", "SKILL.md")
	notes := filepath.Join(filepath.Dir(skill), "notes.md")
	for _, path := range []string{skill, notes} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("dummy"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cmd := New(testSkillDeps(""))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"skill", "remove"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(skill); !os.IsNotExist(err) {
		t.Errorf("the skill file is still there: %v", err)
	}
	if _, err := os.Stat(notes); err != nil {
		t.Errorf("a file beside the skill went with it: %v", err)
	}
}

// TestSkillRemoveTakesWhicheverCopyIsThere covers a skill installed before bb
// wrote Claude Code's copy, or one whose other copy was deleted by hand.
func TestSkillRemoveTakesWhicheverCopyIsThere(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	only := filepath.Join(dir, ".agents", "skills", "bb", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(only), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(only, []byte("dummy"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps := testSkillDeps("")
	var written any
	deps.JSONEnabled = func() bool { return true }
	deps.WriteJSON = func(_ io.Writer, payload any) error {
		written = payload
		return nil
	}

	cmd := New(deps)
	cmd.SetArgs([]string{"skill", "remove"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report, ok := written.(SkillFile)
	if !ok || report.Status != "removed" || len(report.Paths) != 1 || report.Paths[0] != only || report.Path != only {
		t.Fatalf("report = %+v", written)
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
	// The working directory as the OS reports it, which on macOS is the
	// temporary directory with /var resolved to /private/var.
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	bbSkill, _ := lookupSkill("bb")
	got, err := resolveInstallPaths(bbSkill, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{
		filepath.Join(dir, ".agents", "skills", "bb", "SKILL.md"),
		filepath.Join(dir, ".claude", "skills", "bb", "SKILL.md"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("project paths for bb: got %q, want %q", got, want)
	}
}

// TestResolveInstallPathGlobal tests global (home directory) path resolution.
func TestResolveInstallPathGlobal(t *testing.T) {
	t.Parallel()

	bbSkill, _ := lookupSkill("bb")
	got, err := resolveInstallPaths(bbSkill, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	home, _ := os.UserHomeDir()
	want := []string{
		filepath.Join(home, ".agents", "skills", "bb", "SKILL.md"),
		filepath.Join(home, ".claude", "skills", "bb", "SKILL.md"),
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("global paths for bb: got %q, want %q", got, want)
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

	for _, agents := range []string{".agents", ".claude"} {
		dest := filepath.Join(home, agents, "skills", "bb", "SKILL.md")
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			t.Fatalf("expected the global skill to be written to %s", dest)
		}
	}
}

// TestSkillShowJSONNotUsedBySkillShow ensures skill show always writes raw text, not JSON envelope.
func TestSkillShowIsPlainText(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	for _, skill := range Skills {
		committed := skill.Repository()
		for _, marker := range []string{"{{", "}}"} {
			if strings.Contains(committed, marker) {
				t.Errorf("committed %s/SKILL.md contains the template marker %q; it is distributed verbatim by npx", skill.Name, marker)
			}
		}
	}
}

// TestCommittedSkillDoesNotClaimToBeGenerated guards the second half of #325:
// the file used to promise that `bb ai skill show` reflected "the exact
// capabilities of your installed binary", which was never true — it is a static
// embed with one string substitution.
func TestCommittedSkillDoesNotClaimToBeGenerated(t *testing.T) {
	t.Parallel()

	for _, skill := range Skills {
		committed := strings.ToLower(skill.Repository())
		for _, claim := range []string{
			"exact capabilities of your installed binary",
			"version-specific skill",
		} {
			if strings.Contains(committed, claim) {
				t.Errorf("%s/SKILL.md still claims %q, which the static embed does not deliver", skill.Name, claim)
			}
		}
	}
}

// TestSkillDoesNotListMCPToolsInline guards the drift that produced #325: a
// hand-maintained copy of the tool catalogue that fell out of step with the
// server and gave no hint how its tools behave.
func TestSkillDoesNotListMCPToolsInline(t *testing.T) {
	t.Parallel()

	committed := string(bbskill.Content)

	// Naming a tool that asks is the specific failure: which tools ask is the
	// server's to say, and a copy here is the one that goes stale.
	for name := range askingTools {
		if strings.Contains(committed, "`"+name+"`") {
			t.Errorf("SKILL.md names %q, which asks before it runs; point at `bb ai mcp tools` instead", name)
		}
	}

	if !strings.Contains(committed, "bb ai mcp tools") {
		t.Error("SKILL.md should direct the reader to `bb ai mcp tools` for the catalogue")
	}
	if !strings.Contains(committed, "ASKS") {
		t.Error("SKILL.md should explain the ASKS column, so an agent knows a call may wait on the person")
	}
}
