package main

import (
	"strings"
	"testing"
)

// authStatusExample renders a documented bb auth status payload with the meta
// object supplied by the caller, so a test can vary one half at a time.
func authStatusExample(directive, meta string) string {
	return directive + "\n" +
		"```json\n" +
		"{\n" +
		"  \"data\": {\n" +
		"    \"ok\": true,\n" +
		"    \"bitbucketUrl\": \"https://bitbucket.example.com\",\n" +
		"    \"bitbucketVersionTarget\": \"\",\n" +
		"    \"authMode\": \"token\",\n" +
		"    \"authSource\": \"stored/default\",\n" +
		"    \"credentialStorage\": \"keyring\",\n" +
		"    \"checks\": []\n" +
		"  },\n" +
		"  \"meta\": " + meta + "\n" +
		"}\n" +
		"```\n"
}

const bbVersionMeta = "{ \"bbVersion\": \"v4.0.0\" }"

func TestOutputExampleAcceptsAPayloadThatMatchesTheSchema(t *testing.T) {
	t.Parallel()

	document := authStatusExample("<!-- docs-lint: output-of bb auth status -->", bbVersionMeta)

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %+v", findings)
	}
}

func TestOutputExampleCatchesAFieldTheCommandDoesNotEmit(t *testing.T) {
	t.Parallel()

	// The defect this check exists for. bb auth server use declares exactly one
	// property and forbids the rest, but its documented payload carried a
	// "status" field the command has never emitted -- invisible because nobody
	// runs a documented example.
	document := "<!-- docs-lint: output-of bb auth server use -->\n" +
		"```json\n" +
		"{\n" +
		"  \"data\": { \"status\": \"ok\", \"defaultHost\": \"https://bitbucket.example.com\" },\n" +
		"  \"meta\": " + bbVersionMeta + "\n" +
		"}\n" +
		"```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "status") {
		t.Fatalf("expected the extra field to be named, got %q", findings[0].Problem)
	}
}

func TestOutputExampleCatchesAMissingRequiredField(t *testing.T) {
	t.Parallel()

	document := "<!-- docs-lint: output-of bb auth server use -->\n" +
		"```json\n" +
		"{ \"data\": {}, \"meta\": " + bbVersionMeta + " }\n" +
		"```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "defaultHost") {
		t.Fatalf("expected the missing property to be named, got %q", findings[0].Problem)
	}
}

func TestOutputExampleCatchesTheRenamedVersionField(t *testing.T) {
	t.Parallel()

	// meta.version is what the v4 release notes told readers to parse. The
	// envelope emits meta.bbVersion, and an example is where that gets copied
	// from.
	document := authStatusExample("<!-- docs-lint: output-of bb auth status -->", "{ \"version\": \"v4.0.0\" }")

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "bbVersion") {
		t.Fatalf("expected bbVersion to be named, got %q", findings[0].Problem)
	}
}

func TestOutputExampleCatchesTheRemovedContractField(t *testing.T) {
	t.Parallel()

	document := authStatusExample(
		"<!-- docs-lint: output-of bb auth status -->",
		"{ \"bbVersion\": \"v4.0.0\", \"contract\": \"bb.machine/v1\" }")

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "meta.contract") {
		t.Fatalf("expected meta.contract to be named, got %q", findings[0].Problem)
	}
}

func TestOutputExampleRejectsBothDataAndError(t *testing.T) {
	t.Parallel()

	// ADR-075: a run emits one document, and which key is present is how a
	// caller tells the outcome. An example showing both teaches a parser that
	// cannot exist.
	document := "<!-- docs-lint: envelope-shape -->\n" +
		"```json\n" +
		"{ \"data\": {}, \"error\": { \"kind\": \"internal\" }, \"meta\": " + bbVersionMeta + " }\n" +
		"```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "both") {
		t.Fatalf("expected the conflict to be reported, got %q", findings[0].Problem)
	}
}

func TestEnvelopeShapeDirectiveSkipsTheSchemaButNotTheEnvelope(t *testing.T) {
	t.Parallel()

	// The generic illustration that opens the machine-mode page. There is no
	// command to check it against, so it is exempt from the schema check and
	// still held to the rules every document obeys.
	valid := "<!-- docs-lint: envelope-shape -->\n" +
		"```json\n" +
		"{ \"data\": {}, \"meta\": " + bbVersionMeta + " }\n" +
		"```\n"
	if findings, _ := lintMarkdown("doc.md", valid); len(findings) != 0 {
		t.Fatalf("expected the illustration to be accepted, got %+v", findings)
	}

	broken := "<!-- docs-lint: envelope-shape -->\n" +
		"```json\n" +
		"{ \"data\": {}, \"meta\": { \"contract\": \"bb.machine/v1\" } }\n" +
		"```\n"
	if findings, _ := lintMarkdown("doc.md", broken); len(findings) == 0 {
		t.Fatal("expected an exempt block to still be checked for envelope rules")
	}
}

func TestUnannotatedEnvelopeIsReported(t *testing.T) {
	t.Parallel()

	// Without this the two directives would only check the blocks somebody
	// remembered to annotate, and the next example added would be unchecked by
	// default -- the state the whole check exists to leave.
	document := "```json\n" +
		"{ \"data\": { \"ok\": true }, \"meta\": " + bbVersionMeta + " }\n" +
		"```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected the unannotated example to be reported, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "carries no directive") {
		t.Fatalf("expected the directive to be requested, got %q", findings[0].Problem)
	}
}

func TestUnannotatedCheckIgnoresConfigurationAndArtifacts(t *testing.T) {
	t.Parallel()

	// Only a meta.bbVersion marks a bb machine document. IDE settings and the
	// persistent bulk artifacts are neither, and are checked elsewhere against
	// different schemas.
	documents := []string{
		mcpConfig(`"ai", "mcp", "serve"`),
		"```json\n{ \"apiVersion\": \"bb.io/v1alpha1\", \"kind\": \"BulkApplyStatus\", \"status\": \"ok\" }\n```\n",
		"```json\n{ \"data\": { \"ok\": true } }\n```\n",
	}

	for _, document := range documents {
		if findings, _ := lintMarkdown("doc.md", document); len(findings) != 0 {
			t.Fatalf("expected no findings for a non-envelope block, got %+v", findings)
		}
	}
}

func TestOutputOfDirectiveNamingAnUndescribedCommand(t *testing.T) {
	t.Parallel()

	// bb api forwards whatever Bitbucket sent, so it declares no schema. Saying
	// so is more useful than silently passing: the author has named a command
	// whose output cannot be checked, and should know that.
	document := "<!-- docs-lint: output-of bb api -->\n" +
		"```json\n" +
		"{ \"data\": {}, \"meta\": " + bbVersionMeta + " }\n" +
		"```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "no output schema") {
		t.Fatalf("expected the absence to be explained, got %q", findings[0].Problem)
	}
}

func TestOutputDirectiveDoesNotCarryAcrossProse(t *testing.T) {
	t.Parallel()

	// A directive binds the next fence, not the next envelope it can find.
	document := "<!-- docs-lint: output-of bb auth status -->\n" +
		"Some prose in between.\n\n" +
		"```json\n" +
		"{ \"data\": { \"ok\": true }, \"meta\": " + bbVersionMeta + " }\n" +
		"```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %+v", findings)
	}
	if !strings.Contains(findings[0].Problem, "carries no directive") {
		t.Fatalf("expected the binding to have lapsed, got %q", findings[0].Problem)
	}
}

func TestOutputExampleReportsAFailureEnvelopeWithoutASchemaCheck(t *testing.T) {
	t.Parallel()

	// A command's failure document is the error taxonomy's shape, not the
	// command's, so naming the command must not demand its data schema.
	document := "<!-- docs-lint: output-of bb repo list -->\n" +
		"```json\n" +
		"{ \"error\": { \"kind\": \"validation\", \"message\": \"unknown flag\", \"exitCode\": 2 }, \"meta\": " + bbVersionMeta + " }\n" +
		"```\n"

	findings, _ := lintMarkdown("doc.md", document)

	if len(findings) != 0 {
		t.Fatalf("expected a failure envelope to pass, got %+v", findings)
	}
}
