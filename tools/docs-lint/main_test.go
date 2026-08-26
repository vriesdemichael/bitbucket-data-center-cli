package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestLintMarkdownAcceptsValidInvocations(t *testing.T) {
	document := "# Title\n\n" +
		"```bash\n" +
		"bb auth status\n" +
		"bb repo list --limit 20\n" +
		"bb project create DEMO --name \"Demo\"\n" +
		"printf '%s' \"$TOKEN\" | bb auth login https://example.com --token-stdin\n" +
		"```\n"

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 4 {
		t.Fatalf("expected 4 invocations checked, got %d", checked)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestLintMarkdownCatchesTheRealDefects(t *testing.T) {
	// The exact forms found by hand during the external review.
	testCases := []struct {
		name    string
		command string
		problem string
	}{
		{name: "flag that is really positional", command: `bb auth login --host https://example.com --token x`, problem: "unknown flag: --host"},
		{name: "command that does not exist", command: `bb repo view --repo TEST/repo`, problem: "not a subcommand"},
		{name: "search name as a flag", command: `bb search repos --name demo --limit 20`, problem: "unknown flag: --name"},
		{name: "project key as a flag", command: `bb project create --key DEMO --name Demo`, problem: "unknown flag: --key"},
		{name: "compare refs as flags", command: `bb commit compare --repo A/b --from x --to y`, problem: "unknown flag: --from"},
		{name: "too many positionals", command: `bb tag create --repo A/b v1.2.3 main`, problem: "accepts 1 arg"},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			findings, checked := lintMarkdown("doc.md", "```bash\n"+testCase.command+"\n```\n")

			if checked != 1 {
				t.Fatalf("expected 1 invocation checked, got %d", checked)
			}
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding, got %+v", findings)
			}
			if !strings.Contains(findings[0].Problem, testCase.problem) {
				t.Fatalf("expected problem containing %q, got %q", testCase.problem, findings[0].Problem)
			}
		})
	}
}

func TestLintMarkdownReportsUsableLineNumbers(t *testing.T) {
	document := "line one\nline two\n\n```bash\nbb auth status\nbb repo view --repo A/b\n```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	// The fence is line 4 (1-based), the body starts at 5, and the broken
	// command is the second body line: 6.
	if findings[0].Line != 6 {
		t.Fatalf("expected line 6, got %d", findings[0].Line)
	}
}

func TestLintMarkdownIgnoresNonShellBlocks(t *testing.T) {
	// The generated command reference is text-fenced Cobra help; parsing it
	// would report failures for documentation that is correct by construction.
	document := "```text\nUsage:\n  bb repo view [flags]\n```\n\n```json\n{\"cmd\": \"bb repo view\"}\n```\n"

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 0 {
		t.Fatalf("expected no invocations checked, got %d", checked)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestLintMarkdownHonoursTheExpectInvalidDirective(t *testing.T) {
	document := "<!-- docs-lint: expect-invalid -->\n```bash\nbb --json repo list --nonexistent-flag\n```\n"

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 1 {
		t.Fatalf("expected 1 invocation checked, got %d", checked)
	}
	if len(findings) != 0 {
		t.Fatalf("expected the deliberately invalid command to be accepted, got %+v", findings)
	}
}

// TestExpectInvalidFailsWhenTheCommandBecomesValid is the reason the directive
// asserts rather than merely skips: an exemption that cannot rot.
func TestExpectInvalidFailsWhenTheCommandBecomesValid(t *testing.T) {
	document := "<!-- docs-lint: expect-invalid -->\n```bash\nbb auth status\n```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected a finding for a valid command in an expect-invalid block, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "but this command is valid") {
		t.Fatalf("unexpected problem %q", findings[0].Problem)
	}
}

func TestExpectInvalidDirectiveDoesNotCarryAcrossProse(t *testing.T) {
	document := "<!-- docs-lint: expect-invalid -->\n```bash\nbb repo view --repo A/b\n```\n\nSome prose.\n\n```bash\nbb auth status\n```\n"

	findings, _ := lintMarkdown("doc.md", document)

	// The second block is a normal block: a valid command there is fine, and the
	// directive must not have leaked into it.
	if len(findings) != 0 {
		t.Fatalf("expected the directive to apply only to the next block, got %+v", findings)
	}
}

func TestLintMarkdownToleratesCRLF(t *testing.T) {
	// Guards the trap that made the first run of this tool report a dozen false
	// positives on a Windows checkout: a trailing \r reaches pflag as part of
	// the token and is reported as an unknown flag.
	document := strings.ReplaceAll("```bash\nbb repo list --limit 20\n```\n", "\n", "\r\n")
	normalised := strings.ReplaceAll(document, "\r\n", "\n")

	findings, checked := lintMarkdown("doc.md", normalised)

	if checked != 1 {
		t.Fatalf("expected 1 invocation checked, got %d", checked)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings after normalisation, got %+v", findings)
	}
}

func TestLintMarkdownAcceptsHelpAndVersionFlags(t *testing.T) {
	// Cobra registers these lazily during Execute, which this tool never calls.
	document := "```bash\nbb --help\nbb repo settings security --help\nbb --version\n```\n"

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 3 {
		t.Fatalf("expected 3 invocations checked, got %d", checked)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestLintMarkdownHandlesContinuationsAndEnvironmentPrefixes(t *testing.T) {
	document := "```bash\n" +
		"BITBUCKET_URL=https://example.com bb auth status\n" +
		"bb pr create --repo A/b \\\n  --from-ref feature \\\n  --to-ref main --title \"x\"\n" +
		"bb repo list --limit 5 | jq .data\n" +
		"# bb repo view --repo A/b\n" +
		"```\n"

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 3 {
		t.Fatalf("expected 3 invocations checked (the comment is not one), got %d", checked)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestSplitShellSegmentsIgnoresOperatorsInsideQuotes(t *testing.T) {
	segments := splitShellSegments(`bb pr comment add 42 --text "a | b && c"`)

	if len(segments) != 1 {
		t.Fatalf("expected one segment, got %d: %q", len(segments), segments)
	}
}

func TestSplitShellSegmentsTrailingComments(t *testing.T) {
	segments := splitShellSegments(`bb ai skill install bulk # comment here`)

	if len(segments) != 1 || strings.TrimSpace(segments[0]) != "bb ai skill install bulk" {
		t.Fatalf("unexpected segments: %+v", segments)
	}

	prHashRef := splitShellSegments(`bb pr checkout #42`)
	if len(prHashRef) != 1 || strings.TrimSpace(prHashRef[0]) != "bb pr checkout #42" {
		t.Fatalf("unexpected segments for PR hash argument: %+v", prHashRef)
	}

	hashInRef := splitShellSegments(`bb commit compare --repo A/b HEAD#1`)
	if len(hashInRef) != 1 || strings.TrimSpace(hashInRef[0]) != "bb commit compare --repo A/b HEAD#1" {
		t.Fatalf("unexpected segments for unspaced hash: %+v", hashInRef)
	}
}

func TestSplitFieldsRejectsUnterminatedQuotes(t *testing.T) {
	if _, ok := splitFields(`bb pr create --title "unterminated`); ok {
		t.Fatal("expected an unterminated quote to be reported rather than guessed at")
	}
}

func TestParseBBSegmentSkipsNonBBCommands(t *testing.T) {
	for _, segment := range []string{"jq .data", "git status", "echo bb", "curl https://example.com"} {
		if _, ok := parseBBSegment(segment); ok {
			t.Fatalf("expected %q not to be treated as a bb invocation", segment)
		}
	}
}

func TestReportExitCodes(t *testing.T) {
	buffer := &bytes.Buffer{}
	if code := report(buffer, nil, 12); code != 0 {
		t.Fatalf("expected exit 0 with no findings, got %d", code)
	}
	if !strings.Contains(buffer.String(), "12 documented bb invocations checked") {
		t.Fatalf("unexpected output %q", buffer.String())
	}

	buffer.Reset()
	findings := []finding{{File: "a.md", Line: 3, Command: "bb nope", Problem: "boom"}}
	if code := report(buffer, findings, 12); code != 1 {
		t.Fatalf("expected exit 1 with findings, got %d", code)
	}
	for _, want := range []string{"a.md:3", "bb nope", "boom"} {
		if !strings.Contains(buffer.String(), want) {
			t.Fatalf("expected %q in output %q", want, buffer.String())
		}
	}
}

func TestLintMarkdownDialectChecks(t *testing.T) {
	testCases := []struct {
		name    string
		content string
		problem string
	}{
		{
			name:    "flags file:// uri",
			content: "See [docs](file:///workspace/repo/README.md) for details.",
			problem: "prohibited file:// link",
		},
		{
			name:    "flags GitHub callouts",
			content: "> [!NOTE]\n> Some note here.",
			problem: "prohibited GitHub callout",
		},
		{
			name:    "flags unversioned release artifact",
			content: "Download `bb_linux_amd64.tar.gz` from releases.",
			problem: "missing a version segment",
		},
		{
			name:    "flags unrendered LaTeX math",
			content: "Threat $\\leftrightarrow$ Mitigation $\\leftrightarrow$ Audit",
			problem: "unrendered LaTeX math",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			findings, _ := lintMarkdown("test.md", tc.content)
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
			}
			if !strings.Contains(findings[0].Problem, tc.problem) {
				t.Fatalf("expected problem containing %q, got %q", tc.problem, findings[0].Problem)
			}
		})
	}

	t.Run("accepts escaped dollar and inline code", func(t *testing.T) {
		content := "Cost is \\$100, schema is `$schema`, and subshell is `eval $(dbus-launch)`.\n"
		findings, _ := lintMarkdown("test.md", content)
		if len(findings) != 0 {
			t.Fatalf("expected 0 findings, got %+v", findings)
		}
	})

	t.Run("honours expect-invalid directive for dialect check", func(t *testing.T) {
		content := "<!-- docs-lint: expect-invalid -->\nThreat $\\leftrightarrow$ Mitigation\n"
		findings, _ := lintMarkdown("test.md", content)
		if len(findings) != 0 {
			t.Fatalf("expected expect-invalid dialect check to pass, got %+v", findings)
		}
	})
}

func TestLintMarkdownMCPToolsValidation(t *testing.T) {
	docInvalid := "```bash\nbb ai mcp serve --tools non_existent_tool\n```\n"
	findings, _ := lintMarkdown("test.md", docInvalid)
	if len(findings) != 1 || !strings.Contains(findings[0].Problem, `unknown MCP tool "non_existent_tool" in --tools`) {
		t.Fatalf("expected invalid MCP tool finding, got: %+v", findings)
	}

	docValid := "```bash\nbb ai mcp serve --tools get_pr_diff,list_pull_requests\n```\n"
	findings, _ = lintMarkdown("test.md", docValid)
	if len(findings) != 0 {
		t.Fatalf("expected valid MCP tools to pass, got: %+v", findings)
	}
}

func TestLintConfigMCPToolsValidation(t *testing.T) {
	docInvalidJSON := "```json\n{\n  \"args\": [\"ai\", \"mcp\", \"serve\", \"--tools\", \"invalid_tool_mcp\"]\n}\n```\n"
	findings, _ := lintMarkdown("test.md", docInvalidJSON)
	if len(findings) != 1 || !strings.Contains(findings[0].Problem, `unknown MCP tool "invalid_tool_mcp" in --tools`) {
		t.Fatalf("expected finding for invalid tool in json block, got: %+v", findings)
	}

	docValidJSON := "```json\n{\n  \"args\": [\"ai\", \"mcp\", \"serve\", \"--tools\", \"get_pr_diff,list_pull_requests\"]\n}\n```\n"
	findings, _ = lintMarkdown("test.md", docValidJSON)
	if len(findings) != 0 {
		t.Fatalf("expected valid tools in json block to pass, got: %+v", findings)
	}

	docCompactValid := "```json\n{\"args\":[\"ai\",\"mcp\",\"serve\",\"--tools\",\"get_pull_request,add_pr_comment\"]}\n```\n"
	findings, _ = lintMarkdown("test.md", docCompactValid)
	if len(findings) != 0 {
		t.Fatalf("expected compact JSON with valid tools to pass, got: %+v", findings)
	}

	docCompactInvalid := "```json\n{\"args\":[\"ai\",\"mcp\",\"serve\",\"--tools\",\"get_pull_request,bad_tool_name\"]}\n```\n"
	findings, _ = lintMarkdown("test.md", docCompactInvalid)
	if len(findings) != 1 || !strings.Contains(findings[0].Problem, `unknown MCP tool "bad_tool_name" in --tools`) {
		t.Fatalf("expected compact JSON with invalid tool to fail, got: %+v", findings)
	}
}

func TestLintMermaidValidation(t *testing.T) {
	docMermaid := "```mermaid\nflowchart TD\n  A --> B\n```\n"
	findings, _ := lintMarkdown("test.md", docMermaid)
	if len(findings) != 0 {
		t.Fatalf("expected valid mermaid block with mkdocs.yml configured to pass, got: %+v", findings)
	}
}

func TestLintMarkdownPositionalArityOnZeroArgCommands(t *testing.T) {
	doc := "```bash\nbb auth logout https://wrong-host.example.com\n```\n"
	findings, _ := lintMarkdown("test.md", doc)
	if len(findings) != 1 || (!strings.Contains(findings[0].Problem, "unknown command") && !strings.Contains(findings[0].Problem, "accepts 0 arg(s)")) {
		t.Fatalf("expected positional arity error on bb auth logout, got: %+v", findings)
	}
}

func TestShellRedirectionAndPlaceholders(t *testing.T) {
	args, ok := parseBBSegment("bb repo archive --repo MYPROJ/payments --format tar.gz -o - > archive.tar.gz")
	if !ok {
		t.Fatal("expected successful parse")
	}
	expected := []string{"repo", "archive", "--repo", "MYPROJ/payments", "--format", "tar.gz", "-o", "-"}
	if len(args) != len(expected) {
		t.Fatalf("expected args %v, got %v", expected, args)
	}

	diagArgs, ok := parseBBSegment("bb --json --log-level warn auth status 2> diagnostics.jsonl")
	if !ok {
		t.Fatal("expected successful parse")
	}
	expectedDiag := []string{"--json", "--log-level", "warn", "auth", "status"}
	if len(diagArgs) != len(expectedDiag) {
		t.Fatalf("expected args %v, got %v", expectedDiag, diagArgs)
	}

	placeholderArgs, ok := parseBBSegment("bb bulk status <operation-id>")
	if !ok {
		t.Fatal("expected successful parse")
	}
	expectedPlaceholder := []string{"bulk", "status", "<operation-id>"}
	if len(placeholderArgs) != len(expectedPlaceholder) || placeholderArgs[2] != "<operation-id>" {
		t.Fatalf("expected placeholder preserved, got %v", placeholderArgs)
	}
}

// TestRepositoryDocumentationIsValid runs the linter over the real tree, so the
// check is enforced by `go test` as well as by the task and CI.
func TestRepositoryDocumentationIsValid(t *testing.T) {
	findings, checked, err := lintPaths([]string{
		"../../README.md",
		"../../AGENTS.md",
		"../../CONTRIBUTING.md",
		"../../docs",
		"../../skills",
	})
	if err != nil {
		t.Fatalf("lint failed: %v", err)
	}

	if checked == 0 {
		t.Fatal("expected to check some invocations; the paths are probably wrong")
	}

	if len(findings) > 0 {
		buffer := &bytes.Buffer{}
		report(buffer, findings, checked)
		t.Fatalf("documentation contains invalid bb invocations:\n%s", buffer.String())
	}
}

func TestLintMarkdownCatchesStaleReleaseVersions(t *testing.T) {
	targetVersion := "2.11.0"

	testCases := []struct {
		name    string
		content string
		problem string
		valid   bool
	}{
		{
			name:    "flags stale shell version assignment",
			content: "```bash\nVERSION=\"2.10.0\"\ncurl -LO \"https://example.com/bb_${VERSION}_linux_amd64.tar.gz\"\n```\n",
			problem: `stale release version "2.10.0" in code block; must match current release version "2.11.0"`,
		},
		{
			name:    "flags stale shell version assignment with v prefix",
			content: "```bash\nVERSION=v1.0.0\ncurl -LO \"https://example.com/bb_${VERSION#v}_linux_amd64.deb\"\n```\n",
			problem: `stale release version "v1.0.0" in code block; must match current release version "2.11.0"`,
		},
		{
			name:    "flags stale powershell version assignment",
			content: "```powershell\n$Version = \"2.10.0\"\nwinget install vriesdemichael.bb\n```\n",
			problem: `stale release version "2.10.0" in code block; must match current release version "2.11.0"`,
		},
		{
			name:    "flags stale dockerfile ARG assignment",
			content: "```dockerfile\nFROM alpine:3.21\nARG BB_VERSION=2.10.0\nRUN curl -fsSL ...\n```\n",
			problem: `stale release version "2.10.0" in code block; must match current release version "2.11.0"`,
		},
		{
			name:    "flags stale yaml config version assignment",
			content: "```yaml\nvars:\n  bb_version: \"2.10.0\"\n```\n",
			problem: `stale release version "2.10.0" in code block; must match current release version "2.11.0"`,
		},
		{
			name:    "flags stale prose parenthetical example",
			content: "Set the target version (e.g. `2.10.0`):\n",
			problem: `stale release version "2.10.0"; must match current release version "2.11.0"`,
		},
		{
			name:    "accepts current shell version",
			content: "```bash\nVERSION=\"2.11.0\"\n```\n",
			valid:   true,
		},
		{
			name:    "accepts current shell version with v prefix",
			content: "```bash\nVERSION=v2.11.0\n```\n",
			valid:   true,
		},
		{
			name:    "accepts dynamic shell version assignments",
			content: "```bash\nVERSION=\"${BB_VERSION}\"\nLATEST=$(curl -s https://example.com)\n```\n",
			valid:   true,
		},
		{
			name:    "ignores unrelated version numbers",
			content: "FROM alpine:3.21\nPython 3.10 and Bitbucket 10.2 OpenAPI\n",
			valid:   true,
		},
		{
			name:    "honours expect-invalid for stale versions",
			content: "<!-- docs-lint: expect-invalid -->\n```bash\nVERSION=\"2.0.0\"\n```\n",
			valid:   true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			findings, _ := lintMarkdownWithVersion("test.md", tc.content, targetVersion)
			if tc.valid {
				if len(findings) != 0 {
					t.Fatalf("expected valid content to have 0 findings, got: %+v", findings)
				}
				return
			}

			if len(findings) != 1 {
				t.Fatalf("expected 1 finding, got %d: %+v", len(findings), findings)
			}
			if !strings.Contains(findings[0].Problem, tc.problem) {
				t.Fatalf("expected problem %q, got %q", tc.problem, findings[0].Problem)
			}
		})
	}
}

func TestUpdateContentVersions(t *testing.T) {
	input := `# Install Guide

Set the target version (e.g. ` + "`" + `2.10.0` + "`" + `):

` + "```bash" + `
VERSION="2.10.0"
curl -LO "https://example.com/releases/download/v${VERSION}/bb_${VERSION}_linux_amd64.tar.gz"
` + "```" + `

` + "```bash" + `
VERSION=v1.0.0
curl -LO "https://example.com/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.deb"
` + "```" + `

` + "```powershell" + `
$Version = "2.10.0"
` + "```" + `

` + "```dockerfile" + `
FROM alpine:3.21
ARG BB_VERSION=2.10.0
` + "```" + `

` + "```yaml" + `
vars:
  bb_version: "2.10.0"
` + "```" + `
`

	expected := `# Install Guide

Set the target version (e.g. ` + "`" + `2.12.0` + "`" + `):

` + "```bash" + `
VERSION="2.12.0"
curl -LO "https://example.com/releases/download/v${VERSION}/bb_${VERSION}_linux_amd64.tar.gz"
` + "```" + `

` + "```bash" + `
VERSION=v2.12.0
curl -LO "https://example.com/releases/download/${VERSION}/bb_${VERSION#v}_linux_amd64.deb"
` + "```" + `

` + "```powershell" + `
$Version = "2.12.0"
` + "```" + `

` + "```dockerfile" + `
FROM alpine:3.21
ARG BB_VERSION=2.12.0
` + "```" + `

` + "```yaml" + `
vars:
  bb_version: "2.12.0"
` + "```" + `
`

	actual := updateContentVersions(input, "2.12.0")
	if actual != expected {
		t.Fatalf("updateContentVersions mismatch:\nExpected:\n%s\nActual:\n%s", expected, actual)
	}
}
