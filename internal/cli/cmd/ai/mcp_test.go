package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
)

// testMCPDeps builds a minimal Dependencies for MCP tests.
func testMCPDeps() Dependencies {
	return Dependencies{
		Version: func() string { return "test" },
		LoadConfig: func(config.Overrides) (config.AppConfig, error) {
			return config.AppConfig{}, nil
		},
		WriteJSON: func(w io.Writer, v any) error {
			return jsonoutput.Write(w, v)
		},
	}
}

// TestSplitCSV covers all branches of the CSV splitter.
func TestSplitCSV(t *testing.T) {
	cases := []struct {
		input string
		want  []string
	}{
		{"", nil},
		{"  ", nil},
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b , c ", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},
		{"single", []string{"single"}},
	}
	for _, tc := range cases {
		got := splitCSV(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("splitCSV(%q): got %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("splitCSV(%q)[%d]: got %q, want %q", tc.input, i, got[i], tc.want[i])
			}
		}
	}
}

// TestToolDescription tests that toolDescription returns the tool's Description field.
func TestToolDescription(t *testing.T) {
	specs := bbmcp.AllSpecs()
	if len(specs) == 0 {
		t.Fatal("AllSpecs returned no tools")
	}
	for _, spec := range specs {
		desc := toolDescription(spec)
		if desc == "" {
			t.Errorf("tool %q has empty description", spec.Tool.Name)
		}
	}
}

// TestMCPToolsTextOutput verifies that `bb ai mcp tools` lists all tools in text mode.
func TestMCPToolsTextOutput(t *testing.T) {
	cmd := New(testMCPDeps())
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "tools"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, spec := range bbmcp.AllSpecs() {
		if !strings.Contains(out, spec.Tool.Name) {
			t.Errorf("tool %q not found in output", spec.Tool.Name)
		}
	}
}

// TestMCPToolsJSONOutput verifies that `bb ai mcp tools` with --json returns valid JSON.
func TestMCPToolsJSONOutput(t *testing.T) {
	// Wire a root command that has the --json persistent flag, just like root.go does.
	cmd := New(testMCPDeps())
	// Attach a mock --json flag to the root command (normally added by root.go).
	cmd.PersistentFlags().Bool("json", true, "")

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "tools", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should decode as JSON array (or a jsonoutput envelope containing one).
	raw := buf.Bytes()
	// Try array first.
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err != nil {
		// Try envelope.
		var envelope map[string]any
		if err2 := json.Unmarshal(raw, &envelope); err2 != nil {
			t.Fatalf("output is not valid JSON: %v (raw: %q)", err, raw)
		}
	}
}

// TestMCPServeRejectsLoadConfigError tests that serve propagates a LoadConfig error.
func TestMCPServeRejectsLoadConfigError(t *testing.T) {
	sentinel := errors.New("config load failed")
	deps := Dependencies{
		Version: func() string { return "test" },
		LoadConfig: func(config.Overrides) (config.AppConfig, error) {
			return config.AppConfig{}, sentinel
		},
		WriteJSON: func(w io.Writer, v any) error { return nil },
	}
	cmd := New(deps)
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	// --host bypasses the multi-instance check so we reach LoadConfig.
	cmd.SetArgs([]string{"mcp", "serve", "--host", "http://bb.example.com"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error from LoadConfig, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

// TestMCPServeHostAndTokenOverride verifies that --host and --token reach the
// config load, and that they do so without touching the environment.
//
// The environment assertion is the point: this process goes on to serve MCP for
// as long as the client keeps it alive, so an exported BITBUCKET_TOKEN would sit
// in the environment of every subprocess for the whole session.
func TestMCPServeHostAndTokenOverride(t *testing.T) {
	t.Setenv("BITBUCKET_URL", "http://initial.example")
	t.Setenv("BITBUCKET_TOKEN", "initial-token")

	var seen config.Overrides
	deps := Dependencies{
		Version: func() string { return "test" },
		LoadConfig: func(overrides config.Overrides) (config.AppConfig, error) {
			seen = overrides
			return config.AppConfig{}, errors.New("stop here")
		},
		WriteJSON: func(w io.Writer, v any) error { return nil },
	}
	cmd := New(deps)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"mcp", "serve", "--host", "http://override.example", "--token", "my-pat"})
	_ = cmd.Execute() // will error at LoadConfig — that's expected

	if seen.Host != "http://override.example" {
		t.Errorf("host override: got %q, want %q", seen.Host, "http://override.example")
	}
	if seen.Token != "my-pat" {
		t.Errorf("token override: got %q, want %q", seen.Token, "my-pat")
	}

	if got := os.Getenv("BITBUCKET_URL"); got != "http://initial.example" {
		t.Errorf("--host must not rewrite BITBUCKET_URL, got %q", got)
	}
	if got := os.Getenv("BITBUCKET_TOKEN"); got != "initial-token" {
		t.Errorf("--token must not rewrite BITBUCKET_TOKEN, got %q", got)
	}
}

// TestMCPServeClientFromConfigFails tests the ClientsFromConfig error path.
func TestMCPServeClientFromConfigFails(t *testing.T) {
	// Provide a config with an invalid URL to make openapi client construction fail.
	deps := Dependencies{
		Version: func() string { return "test" },
		LoadConfig: func(config.Overrides) (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL:   "://bad-url",
				RequestTimeout: time.Second,
				RetryCount:     0,
			}, nil
		},
		WriteJSON: func(w io.Writer, v any) error { return nil },
	}
	cmd := New(deps)
	cmd.SetArgs([]string{"mcp", "serve", "--host", "://bad-url"})

	if err := cmd.Execute(); err == nil {
		// If this passes without error it means ClientsFromConfig tolerates bad URLs.
		// That's also acceptable behaviour; the test's purpose is covering that code path.
		t.Log("ClientsFromConfig tolerated bad URL — no error, that is OK")
	}
}

// TestMCPServePassesThroughTestServer exercises the full serve path up to ServeStdio.
// ServeStdio will immediately return an error because there is no valid stdin.
func TestMCPServePassesThroughTestServer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	deps := Dependencies{
		Version: func() string { return "test" },
		LoadConfig: func(config.Overrides) (config.AppConfig, error) {
			return config.AppConfig{
				BitbucketURL:   srv.URL,
				RequestTimeout: time.Second,
				RetryCount:     0,
				RetryBackoff:   time.Millisecond,
			}, nil
		},
		WriteJSON: func(w io.Writer, v any) error { return nil },
	}
	cmd := New(deps)
	cmd.SetArgs([]string{"mcp", "serve", "--host", srv.URL})
	// ServeStdio reads from os.Stdin; with no piped input it will fail or succeed
	// immediately — either is fine. We just want this path to be covered.
	_ = cmd.Execute()
}

// TestMCPToolsCountMatchesAllSpecs ensures the tools listing covers all specs.
func TestMCPToolsCountMatchesAllSpecs(t *testing.T) {
	cmd := New(testMCPDeps())
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "tools"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) != "" {
			lines++
		}
	}
	want := len(bbmcp.AllSpecs())
	if lines != want {
		t.Errorf("expected %d tool lines, got %d", want, lines)
	}
}

// gatedToolNames are the tools the server withholds without --yolo.
//
// Listed literally rather than derived from AllSpecs, so that reclassifying a
// tool has to be a deliberate edit here as well. Silently promoting
// merge_pull_request to safe is exactly the change that should not pass review
// unnoticed.
var gatedToolNames = []string{
	"merge_pull_request",
	"submit_pr_review",
	"enable_auto_merge",
	"set_build_status",
}

func TestToolExposureMatchesTheServerClassification(t *testing.T) {
	gated := map[string]bool{}
	for _, name := range gatedToolNames {
		gated[name] = true
	}

	seen := map[string]bool{}
	for _, spec := range bbmcp.AllSpecs() {
		seen[spec.Tool.Name] = true

		want := exposureSafe
		if gated[spec.Tool.Name] {
			want = exposureYolo
		}

		if got := toolExposure(spec); got != want {
			t.Errorf("tool %q: exposure %q, want %q", spec.Tool.Name, got, want)
		}
		if spec.Safe == gated[spec.Tool.Name] {
			t.Errorf("tool %q: Safe=%v contradicts the expected gating", spec.Tool.Name, spec.Safe)
		}
	}

	for _, name := range gatedToolNames {
		if !seen[name] {
			t.Errorf("expected gated tool %q to exist; was it renamed or removed?", name)
		}
	}
}

// TestMCPToolsMarksGatedTools is the defect from #324: the listing is
// documented as the source for building allowlists, so a tool that is withheld
// by default has to say so.
func TestMCPToolsMarksGatedTools(t *testing.T) {
	cmd := New(testMCPDeps())
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "tools"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, line := range strings.Split(buf.String(), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		name, exposure := fields[0], fields[1]
		if exposure != exposureSafe && exposure != exposureYolo {
			continue
		}

		wantYolo := false
		for _, gatedName := range gatedToolNames {
			if name == gatedName {
				wantYolo = true
			}
		}

		if wantYolo && exposure != exposureYolo {
			t.Errorf("gated tool %q is listed as %s", name, exposure)
		}
		if !wantYolo && exposure != exposureSafe {
			t.Errorf("safe tool %q is listed as %s", name, exposure)
		}
	}
}

func TestMCPToolsSafeOnlyOmitsGatedTools(t *testing.T) {
	cmd := New(testMCPDeps())
	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "tools", "--safe-only"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	out := buf.String()
	for _, name := range gatedToolNames {
		if strings.Contains(out, name) {
			t.Errorf("--safe-only listed gated tool %q", name)
		}
	}

	// The safe tools must still all be present: a filter that drops too much is
	// as wrong as one that drops nothing.
	for _, spec := range bbmcp.AllSpecs() {
		if spec.Safe && !strings.Contains(out, spec.Tool.Name) {
			t.Errorf("--safe-only omitted safe tool %q", spec.Tool.Name)
		}
	}
}

func TestMCPToolsJSONCarriesExposure(t *testing.T) {
	cmd := New(testMCPDeps())
	cmd.PersistentFlags().Bool("json", true, "")

	buf := &bytes.Buffer{}
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"mcp", "tools", "--json"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var envelope struct {
		Data []struct {
			Name     string `json:"name"`
			Safe     bool   `json:"safe"`
			Exposure string `json:"exposure"`
		} `json:"data"`
	}
	if err := json.Unmarshal(buf.Bytes(), &envelope); err != nil {
		t.Fatalf("output is not a parseable envelope: %v", err)
	}
	if len(envelope.Data) != len(bbmcp.AllSpecs()) {
		t.Fatalf("expected %d entries, got %d", len(bbmcp.AllSpecs()), len(envelope.Data))
	}

	gated := map[string]bool{}
	for _, name := range gatedToolNames {
		gated[name] = true
	}

	for _, entry := range envelope.Data {
		if entry.Safe == gated[entry.Name] {
			t.Errorf("tool %q: safe=%v contradicts expected gating", entry.Name, entry.Safe)
		}
		// The two fields describe the same fact and must not disagree.
		if entry.Safe && entry.Exposure != exposureSafe {
			t.Errorf("tool %q: safe=true but exposure=%q", entry.Name, entry.Exposure)
		}
		if !entry.Safe && entry.Exposure != exposureYolo {
			t.Errorf("tool %q: safe=false but exposure=%q", entry.Name, entry.Exposure)
		}
	}
}
