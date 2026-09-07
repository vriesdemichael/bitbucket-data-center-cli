package main

import (
	"strings"
	"testing"
)

// mcpConfig renders an IDE MCP server block, the shape both documented
// configurations use.
func mcpConfig(args string) string {
	return "```json\n" +
		"{\n" +
		"  \"mcp\": {\n" +
		"    \"servers\": {\n" +
		"      \"bb\": {\n" +
		"        \"type\": \"stdio\",\n" +
		"        \"command\": \"bb\",\n" +
		"        \"args\": [" + args + "]\n" +
		"      }\n" +
		"    }\n" +
		"  }\n" +
		"}\n" +
		"```\n"
}

func TestConfigInvocationCatchesTheFlagThatSurvivedARelease(t *testing.T) {
	t.Parallel()

	// The defect this check exists for. Both documented IDE configurations went
	// on launching `bb ai mcp serve --token` for a release after the flag was
	// removed: valid JSON, correctly fenced, and unreachable by the shell
	// scanner, which reads `bb ...` lines and nothing else.
	document := mcpConfig(`"ai", "mcp", "serve", "--token", "READ_ONLY_PAT"`)

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 1 {
		t.Fatalf("expected the configured invocation to be counted, got %d checked", checked)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "unknown flag: --token") {
		t.Fatalf("expected the retired flag to be named, got %q", findings[0].Problem)
	}
	if !strings.Contains(findings[0].Command, "bb ai mcp serve") {
		t.Fatalf("expected the reconstructed command in the finding, got %q", findings[0].Command)
	}
}

func TestConfigInvocationAcceptsTheSupportedForm(t *testing.T) {
	t.Parallel()

	// The replacement the command's own help teaches: no credential flag, the
	// token supplied through the client's env block.
	document := mcpConfig(`"ai", "mcp", "serve", "--host", "https://bitbucket.example.com"`)

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 1 {
		t.Fatalf("expected 1 invocation checked, got %d", checked)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestConfigInvocationChecksToolNamesInArgsArrays(t *testing.T) {
	t.Parallel()

	document := mcpConfig(`"ai", "mcp", "serve", "--tools", "get_pull_request,no_such_tool"`)

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) == 0 {
		t.Fatal("expected an unknown tool in an args array to be reported")
	}
	var reported bool
	for _, found := range findings {
		if strings.Contains(found.Problem, "no_such_tool") {
			reported = true
		}
	}
	if !reported {
		t.Fatalf("expected the unknown tool to be named, got %+v", findings)
	}
}

func TestConfigInvocationDropsPlaceholderValuesButKeepsTheFlag(t *testing.T) {
	t.Parallel()

	// ${env:VAR} is a value the client substitutes. The flag it follows is
	// still worth checking, and giving the flag a made-up value would report
	// documentation for the linter's own stand-in.
	valid := mcpConfig(`"ai", "mcp", "serve", "--host", "${env:BITBUCKET_URL}"`)
	if findings, _ := lintMarkdown("doc.md", valid); len(findings) != 0 {
		t.Fatalf("expected a placeholder value to be accepted, got %+v", findings)
	}

	retired := mcpConfig(`"ai", "mcp", "serve", "--token", "${env:BITBUCKET_RO_TOKEN}"`)
	findings, _ := lintMarkdown("doc.md", retired)
	if len(findings) != 1 {
		t.Fatalf("expected the retired flag to be reported behind a placeholder, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "unknown flag: --token") {
		t.Fatalf("expected the flag to be named, got %q", findings[0].Problem)
	}
}

func TestConfigInvocationIgnoresOtherCommands(t *testing.T) {
	t.Parallel()

	// A configuration launching something else is not this linter's business,
	// and must not be reconstructed as though its arguments were bb's.
	document := "```json\n" +
		"{\n" +
		"  \"servers\": {\n" +
		"    \"other\": { \"command\": \"npx\", \"args\": [\"-y\", \"some-server\", \"--token\", \"x\"] }\n" +
		"  }\n" +
		"}\n" +
		"```\n"

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 0 {
		t.Fatalf("expected no bb invocations, got %d checked", checked)
	}
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestConfigInvocationReadsAbsoluteAndWindowsCommandPaths(t *testing.T) {
	t.Parallel()

	for _, command := range []string{"/usr/local/bin/bb", `C:\\tools\\bb.exe`, "bb.exe"} {
		document := "```json\n" +
			"{ \"command\": \"" + command + "\", \"args\": [\"ai\", \"mcp\", \"serve\", \"--token\", \"x\"] }\n" +
			"```\n"

		findings, _ := lintMarkdown("doc.md", document)
		if len(findings) != 1 {
			t.Fatalf("command %q: expected the invocation to be checked, got %+v", command, findings)
		}
	}
}

func TestConfigInvocationReadsYAMLConfigurations(t *testing.T) {
	t.Parallel()

	document := "```yaml\n" +
		"servers:\n" +
		"  bb:\n" +
		"    command: bb\n" +
		"    args: [\"ai\", \"mcp\", \"serve\", \"--token\", \"x\"]\n" +
		"```\n"

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 1 {
		t.Fatalf("expected the YAML invocation to be counted, got %d", checked)
	}
	if len(findings) != 1 || !strings.Contains(findings[0].Problem, "--token") {
		t.Fatalf("expected the retired flag to be reported, got %+v", findings)
	}
}

func TestConfigInvocationIgnoresBlocksThatDoNotParse(t *testing.T) {
	t.Parallel()

	// Configuration documentation carries deliberate fragments. Reporting them
	// as malformed would be reporting documentation for not being a whole file.
	document := "```json\n" +
		"  \"command\": \"bb\",\n" +
		"  \"args\": [\"ai\", \"mcp\", \"serve\", \"--token\", \"x\"]\n" +
		"```\n"

	findings, checked := lintMarkdown("doc.md", document)

	if checked != 0 || len(findings) != 0 {
		t.Fatalf("expected a fragment to be skipped, got %d checked and %+v", checked, findings)
	}
}

func TestArgsKeyLinesFallBackWhenTheyCannotBeMatched(t *testing.T) {
	t.Parallel()

	// Two servers, one of them not bb: the "args" keys outnumber the
	// invocations, so a line taken by position would name the wrong server.
	document := "```json\n" + // line 1
		"{\n" +
		"  \"a\": { \"command\": \"npx\", \"args\": [\"x\"] },\n" +
		"  \"b\": { \"command\": \"bb\", \"args\": [\"ai\", \"mcp\", \"serve\", \"--token\", \"x\"] }\n" +
		"}\n" +
		"```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if findings[0].Line != 1 {
		t.Fatalf("expected the fence line when keys cannot be matched, got %d", findings[0].Line)
	}
}
