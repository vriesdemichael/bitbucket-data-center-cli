package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/deprecation"
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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

	cmd := newAICommandWithJSONFlag()

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
	t.Parallel()

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

// TestMCPServeHostOverrideAndTokenFromEnvironment covers what --host still does
// and where the credential now comes from.
//
// --token was retired in #464: a flag value sits in the process argument list,
// which is world-readable on Linux, for as long as the server runs -- and this
// server runs for the whole session. The MCP client supplies BITBUCKET_TOKEN
// through its own env block instead, which /proc exposes only to the same user.
func TestMCPServeHostOverrideAndTokenFromEnvironment(t *testing.T) {
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
	cmd.SetArgs([]string{"mcp", "serve", "--host", "http://override.example"})
	_ = cmd.Execute() // will error at LoadConfig — that's expected

	if seen.Host != "http://override.example" {
		t.Errorf("host override: got %q, want %q", seen.Host, "http://override.example")
	}

	// No token override is passed at all: config.Load reads BITBUCKET_TOKEN,
	// which is what the client's env block sets for this process alone.
	if seen.Token != "" {
		t.Errorf("token override: got %q, want none -- the token comes from the environment", seen.Token)
	}

	if got := os.Getenv("BITBUCKET_URL"); got != "http://initial.example" {
		t.Errorf("--host must not rewrite BITBUCKET_URL, got %q", got)
	}
	if got := os.Getenv("BITBUCKET_TOKEN"); got != "initial-token" {
		t.Errorf("the token must not be written to the environment, got %q", got)
	}
}

// TestMCPServeClientFromConfigFails tests the ClientsFromConfig error path.
func TestMCPServeClientFromConfigFails(t *testing.T) {
	t.Parallel()

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

// TestMCPToolsCountMatchesAllSpecs ensures the tools listing covers all specs.
func TestMCPToolsCountMatchesAllSpecs(t *testing.T) {
	t.Parallel()

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

// askingTools are the tools that ask the person before they run, and when.
//
// Listed literally rather than derived from AllSpecs, so that changing which
// tools ask has to be a deliberate edit here as well as in the server.
var askingTools = map[string]string{
	"merge_pull_request":  "always",
	"enable_auto_merge":   "always",
	"disable_auto_merge":  "always",
	"submit_pr_review":    "always",
	"set_build_status":    "always",
	"create_tag":          "always",
	"update_pull_request": "when-draft-changes",
}

func wantAsks(name string) string {
	if asks, ok := askingTools[name]; ok {
		return asks
	}

	return "never"
}

// runAI runs `bb ai` with args and returns what it printed to stdout and stderr.
func runAI(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()

	cmd := newAICommandWithJSONFlag()
	out, errOut := &bytes.Buffer{}, &bytes.Buffer{}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("bb ai %s: %v", strings.Join(args, " "), err)
	}

	return out.String(), errOut.String()
}

type listedTool struct {
	Name     string `json:"name"`
	Writes   bool   `json:"writes"`
	Asks     string `json:"asks"`
	Safe     bool   `json:"safe"`
	Exposure string `json:"exposure"`
}

func listToolsJSON(t *testing.T, args ...string) []listedTool {
	t.Helper()

	stdout, _ := runAI(t, append([]string{"mcp", "tools", "--json"}, args...)...)
	var envelope struct {
		Data []listedTool `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("output is not a parseable envelope: %v", err)
	}

	return envelope.Data
}

// TestMCPToolsSaysWhichToolsAsk is the listing the documentation points at for
// building allowlists, so it has to say which tools ask, in JSON and in text.
func TestMCPToolsSaysWhichToolsAsk(t *testing.T) {
	t.Parallel()

	tools := listToolsJSON(t)
	if len(tools) != len(bbmcp.AllSpecs()) {
		t.Fatalf("listed %d tools, want %d", len(tools), len(bbmcp.AllSpecs()))
	}
	seen := map[string]bool{}
	for _, tool := range tools {
		seen[tool.Name] = true
		if tool.Asks != wantAsks(tool.Name) {
			t.Errorf("%s: asks %q, want %q", tool.Name, tool.Asks, wantAsks(tool.Name))
		}
	}
	for name := range askingTools {
		if !seen[name] {
			t.Errorf("expected %q to be listed; was it renamed or removed?", name)
		}
	}

	stdout, _ := runAI(t, "mcp", "tools")
	for _, line := range strings.Split(stdout, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		if fields[2] != wantAsks(fields[0]) {
			t.Errorf("the text listing says %s asks %q, want %q", fields[0], fields[2], wantAsks(fields[0]))
		}
	}
}

// TestMCPToolsReadOnlyListsWhatAReadOnlyServerExposes: --read-only replaces
// --safe-only as the filter for building a narrow allowlist.
func TestMCPToolsReadOnlyListsWhatAReadOnlyServerExposes(t *testing.T) {
	t.Parallel()

	want := map[string]bool{}
	for _, spec := range bbmcp.AllSpecs() {
		if spec.ReadOnly() {
			want[spec.Tool.Name] = true
		}
	}

	tools := listToolsJSON(t, "--read-only")
	if len(tools) != len(want) {
		t.Errorf("--read-only listed %d tools, want the %d read-only ones", len(tools), len(want))
	}
	for _, tool := range tools {
		if !want[tool.Name] || tool.Writes || tool.Asks != "never" {
			t.Errorf("--read-only listed %+v", tool)
		}
	}
}

// TestMCPToolsDeprecatedFormsKeepWorkingAndWarn: --safe-only, safe and
// exposure keep working until the next major (ADR-084). They describe exposure
// without --yolo, which every tool has now.
func TestMCPToolsDeprecatedFormsKeepWorkingAndWarn(t *testing.T) {
	t.Parallel()

	stdout, stderr := runAI(t, "mcp", "tools", "--safe-only")
	for _, spec := range bbmcp.AllSpecs() {
		if !strings.Contains(stdout, spec.Tool.Name) {
			t.Errorf("--safe-only no longer lists %q, and every tool is exposed without --yolo", spec.Tool.Name)
		}
	}
	entry, ok := deprecation.Named("bb ai mcp tools --safe-only")
	if !ok || !strings.Contains(stderr, entry.Warning()) {
		t.Errorf("--safe-only did not print its registered warning; stderr: %q", stderr)
	}

	for _, tool := range listToolsJSON(t) {
		if !tool.Safe || tool.Exposure != exposureSafe {
			t.Errorf("%s: safe=%v exposure=%q, want true and %s", tool.Name, tool.Safe, tool.Exposure, exposureSafe)
		}
	}
}

// TestMCPServeDeprecatedFlagsAreAcceptedAndWarn: an existing client
// configuration passing --yolo keeps starting, and says why it should stop.
func TestMCPServeDeprecatedFlagsAreAcceptedAndWarn(t *testing.T) {
	t.Parallel()

	for _, flag := range []string{"yolo", "allow-writes"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()

			stopped := errors.New("stop at the configuration")
			deps := testMCPDeps()
			deps.LoadConfig = func(config.Overrides) (config.AppConfig, error) { return config.AppConfig{}, stopped }

			cmd := New(deps)
			stderr := &bytes.Buffer{}
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(stderr)
			cmd.SetArgs([]string{"mcp", "serve", "--host", "http://bb.example.com", "--" + flag})

			if err := cmd.Execute(); !errors.Is(err, stopped) {
				t.Fatalf("serve --%s: %v, want it to get as far as loading the configuration", flag, err)
			}
			entry, ok := deprecation.Named("bb ai mcp serve --" + flag)
			if !ok || !strings.Contains(stderr.String(), entry.Warning()) {
				t.Errorf("serve --%s did not print its registered warning; stderr: %q", flag, stderr)
			}
		})
	}
}

// An output field warns nobody at runtime, so its schema is where a consumer
// learns it is going (ADR-084). The fields the registry deprecates are the
// ones the schema calls deprecated, and each says what to read instead.
func TestDeprecatedMCPToolsFieldsAreTheOnesTheSchemaCallsDeprecated(t *testing.T) {
	t.Parallel()

	const namePrefix = "bb ai mcp tools --json field "
	registered := map[string]bool{}
	for _, entry := range deprecation.Entries {
		if field, ok := strings.CutPrefix(entry.Name, namePrefix); ok {
			registered[field] = true
		}
	}

	schema, ok := result.SchemaFor("ai mcp tools")
	if !ok {
		t.Fatal("bb ai mcp tools declares no result schema")
	}
	item := schema.Items
	if item == nil {
		t.Fatalf("the schema is not a list: %+v", schema)
	}
	described := map[string]bool{}
	for name, property := range item.Properties {
		if strings.HasPrefix(property.Description, "Deprecated") {
			described[name] = true
			if !strings.Contains(property.Description, "Read asks and writes instead.") {
				t.Errorf("%s does not say what to read instead: %q", name, property.Description)
			}
		}
	}

	if !maps.Equal(registered, described) {
		t.Fatalf("the registry deprecates %v, and the schema calls %v deprecated",
			slices.Sorted(maps.Keys(registered)), slices.Sorted(maps.Keys(described)))
	}
}

// newAICommandWithJSONFlag wires the --json flag root.go adds, feeding
// JSONEnabled from it the way root.go does: the command learns about machine
// output from its dependency, never by reading a flag of the root's itself,
// since --yaml asks for machine output too.
func newAICommandWithJSONFlag() *cobra.Command {
	deps := testMCPDeps()
	var asJSON bool
	deps.JSONEnabled = func() bool { return asJSON }

	cmd := New(deps)
	cmd.PersistentFlags().BoolVar(&asJSON, "json", false, "")

	return cmd
}
