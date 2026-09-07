package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli"
)

// lintOutputExample checks a documented machine-output example against the
// contract the command actually declares.
//
// Documented output drifts silently in a way documented input does not. An
// invocation that names a removed flag fails the moment anyone runs it; a
// payload with a field the command stopped emitting looks right forever,
// because nothing executes it. Three examples were wrong when this check was
// written: a bb auth server use payload carrying a "status" field the command
// has never emitted, a bulk status artifact missing the apiVersion and kind its
// own published schema requires, and an envelope described as versioned two
// paragraphs above the text explaining that it is not.
//
// The schema comes from cli.DescribeCommand, which is the lookup --describe
// serves, so an example is checked against the same declaration the command
// answers with rather than against a second copy that could drift from it.
func lintOutputExample(file string, block codeBlock) []finding {
	if block.outputOf == "" && !block.envelopeShape {
		return nil
	}

	var document any
	if err := json.Unmarshal([]byte(strings.TrimSpace(block.body)), &document); err != nil {
		return []finding{{
			File:    file,
			Line:    block.startLine,
			Command: outputDirectiveText(block),
			Problem: fmt.Sprintf("block does not parse as JSON: %v", err),
		}}
	}

	envelope, ok := document.(map[string]any)
	if !ok {
		return []finding{{
			File:    file,
			Line:    block.startLine,
			Command: outputDirectiveText(block),
			Problem: "machine output is a JSON object; this block is not one",
		}}
	}

	findings := checkEnvelopeShape(file, block, envelope)
	if block.outputOf == "" {
		return findings
	}

	return append(findings, checkPayloadAgainstSchema(file, block, envelope)...)
}

// checkEnvelopeShape applies the rules ADR-064 and ADR-075 place on every
// document bb writes, whichever command produced it.
func checkEnvelopeShape(file string, block codeBlock, envelope map[string]any) []finding {
	var problems []string

	_, hasData := envelope["data"]
	_, hasError := envelope["error"]

	switch {
	case hasData && hasError:
		problems = append(problems, `carries both "data" and "error"; exactly one is present in a real document`)
	case !hasData && !hasError:
		problems = append(problems, `carries neither "data" nor "error"`)
	}

	meta, ok := envelope["meta"].(map[string]any)
	if !ok {
		problems = append(problems, `carries no "meta" object`)
	} else if version, ok := meta["bbVersion"].(string); !ok || strings.TrimSpace(version) == "" {
		// The field ADR-064 kept when it removed the contract version. An
		// example still showing meta.contract, or a bare meta.version, is
		// exactly the drift this catches.
		problems = append(problems, `"meta" carries no bbVersion string`)
	}

	if contract, ok := envelope["meta"].(map[string]any); ok {
		if _, present := contract["contract"]; present {
			problems = append(problems, `"meta.contract" was removed in v4; the binary version is meta.bbVersion`)
		}
	}

	return findingsFrom(file, block, problems)
}

// checkPayloadAgainstSchema validates the example's data payload against the
// command's declared schema.
//
// Only the success payload is checked. A failure envelope's shape is the error
// taxonomy's, not the command's, and checkEnvelopeShape already covers it.
func checkPayloadAgainstSchema(file string, block codeBlock, envelope map[string]any) []finding {
	described := cli.DescribeCommand(block.outputOf)

	if !described.Described {
		reason := described.Reason
		if reason == "" {
			reason = "no schema is published for it"
		}

		return findingsFrom(file, block, []string{
			fmt.Sprintf("%q declares no output schema to check this against: %s", "bb "+block.outputOf, reason),
		})
	}

	payload, ok := envelope["data"]
	if !ok {
		// A failure example named after a command is legitimate: the error
		// envelope is what that command emits when it fails.
		return nil
	}

	compiled, err := compileDescribedSchema(described.Schema)
	if err != nil {
		return findingsFrom(file, block, []string{
			fmt.Sprintf("could not compile the schema declared by %q: %v", "bb "+block.outputOf, err),
		})
	}

	if err := compiled.Validate(payload); err != nil {
		return findingsFrom(file, block, []string{
			fmt.Sprintf("data does not match the schema %q declares: %s", "bb "+block.outputOf, summariseSchemaError(err)),
		})
	}

	return nil
}

// compileDescribedSchema turns the value --describe reports into a validator.
//
// DescribeResult.Schema is deliberately `any`, because a derived schema and a
// published one are different Go values that encode to the same JSON. Marshal
// and reparse rather than type-switching, so this keeps working when the mix
// of sources changes.
func compileDescribedSchema(schema any) (*jsonschema.Schema, error) {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}

	var document any
	if err := json.Unmarshal(encoded, &document); err != nil {
		return nil, err
	}

	compiler := jsonschema.NewCompiler()
	const resource = "describe.schema.json"
	if err := compiler.AddResource(resource, document); err != nil {
		return nil, err
	}

	return compiler.Compile(resource)
}

// summariseSchemaError renders a validation failure as the lines that name a
// location and what was wrong there.
//
// The library's default rendering is a tree whose root repeats the whole
// instance, which buries the one line an author needs.
func summariseSchemaError(err error) string {
	var validation *jsonschema.ValidationError
	if !errors.As(err, &validation) {
		return err.Error()
	}

	causes := leafCauses(validation)
	sort.Strings(causes)
	if len(causes) == 0 {
		return err.Error()
	}
	if len(causes) > 4 {
		causes = append(causes[:4], fmt.Sprintf("(and %d more)", len(causes)-4))
	}

	return strings.Join(causes, "; ")
}

// leafCauses collects the deepest explanations in a validation error tree.
//
// A leaf renders itself as `at '/field': <what was wrong>`, which is the line
// worth reporting. The root additionally prefixes the schema's file URL, so the
// rendering is reduced to its last line: that URL names a temporary resource
// this linter compiled in memory and would only mislead.
func leafCauses(err *jsonschema.ValidationError) []string {
	if len(err.Causes) == 0 {
		return []string{lastMeaningfulLine(err.Error())}
	}

	var causes []string
	for _, cause := range err.Causes {
		causes = append(causes, leafCauses(cause)...)
	}

	return causes
}

func lastMeaningfulLine(rendered string) string {
	lines := strings.Split(rendered, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		line = strings.TrimPrefix(line, "- ")
		if line != "" {
			return line
		}
	}

	return strings.TrimSpace(rendered)
}

func findingsFrom(file string, block codeBlock, problems []string) []finding {
	var findings []finding
	for _, problem := range problems {
		findings = append(findings, finding{
			File:    file,
			Line:    block.startLine,
			Command: outputDirectiveText(block),
			Problem: problem,
		})
	}

	return findings
}

func outputDirectiveText(block codeBlock) string {
	if block.outputOf != "" {
		return outputOfDirectivePrefix + "bb " + block.outputOf
	}

	return envelopeShapeDirective
}

// lintUnannotatedEnvelope reports a machine-output example that carries no
// directive saying what it is.
//
// Without this the two directives would only ever check the blocks somebody
// remembered to annotate, and the next example added would be unchecked by
// default -- which is the state this whole check exists to leave. An author has
// two ways to say what a block is, and both of them get verified.
func lintUnannotatedEnvelope(file string, block codeBlock) []finding {
	if block.outputOf != "" || block.envelopeShape || block.expectInvalid {
		return nil
	}
	if block.language != "json" || !looksLikeEnvelope(block.body) {
		return nil
	}

	return []finding{{
		File:    file,
		Line:    block.startLine,
		Command: strings.TrimSpace(firstLine(block.body)),
		Problem: fmt.Sprintf(
			"machine-output example carries no directive: add <!-- %sbb <command> --> to check it against that command's schema, or <!-- %s --> if it illustrates the envelope rather than one command's payload",
			outputOfDirectivePrefix, envelopeShapeDirective),
	}}
}

// looksLikeEnvelope reports whether a block is a bb machine document.
//
// Deliberately narrow: a meta object carrying bbVersion is the one thing every
// envelope has and nothing else in the documentation does. Matching on "data"
// alone would sweep in bulk artifacts and IDE settings, which are checked
// elsewhere and against different schemas.
func looksLikeEnvelope(body string) bool {
	var document any
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &document); err != nil {
		return false
	}

	envelope, ok := document.(map[string]any)
	if !ok {
		return false
	}

	meta, ok := envelope["meta"].(map[string]any)
	if !ok {
		return false
	}

	_, versioned := meta["bbVersion"]

	return versioned
}

func firstLine(body string) string {
	if index := strings.Index(body, "\n"); index >= 0 {
		return body[:index]
	}

	return body
}
