package main

import (
	"encoding/json"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// configInvocation is a bb invocation reconstructed from a configuration
// document rather than from a shell line.
//
// An MCP client launches bb through an object -- "command": "bb" beside an
// "args" array -- and none of that reaches the shell scanner, which reads
// `bb ...` lines. Two IDE configuration blocks went on passing --token for a
// release after the flag was removed: both were valid JSON, correctly fenced,
// and invisible here. A configuration block is copied wholesale rather than
// adapted, so it earns more scrutiny than a shell line, not less.
type configInvocation struct {
	// line is the "args" key this invocation was read from when the block's
	// keys can be matched one-for-one, and the opening fence otherwise. The
	// reconstructed command is reported alongside it either way, so a finding
	// names which server it came from even when the line is approximate.
	line int
	args []string
}

// extractConfigInvocations reads every bb invocation out of one configuration
// block.
func extractConfigInvocations(block codeBlock) []configInvocation {
	document, ok := decodeConfigDocument(block.language, block.body)
	if !ok {
		// A block that does not parse is not this check's business. Config
		// blocks carry deliberate fragments -- a snippet of a larger file, a
		// commented example -- and reporting them as malformed would be
		// reporting documentation for not being a whole document.
		return nil
	}

	var found []configInvocation
	collectConfigInvocations(document, &found)
	if len(found) == 0 {
		return nil
	}

	lines := argsKeyLines(block)
	for i := range found {
		if len(lines) == len(found) {
			found[i].line = lines[i]
			continue
		}
		found[i].line = block.startLine
	}

	return found
}

// decodeConfigDocument parses a block body as the language its fence declares.
//
// YAML is decoded through the JSON round trip the rest of the project uses, so
// both languages arrive as the same Go shapes and the walk below has one case
// to handle rather than two.
func decodeConfigDocument(language, body string) (any, bool) {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return nil, false
	}

	var document any
	switch language {
	case "json":
		if err := json.Unmarshal([]byte(trimmed), &document); err != nil {
			return nil, false
		}
	case "yaml", "yml":
		if err := yaml.Unmarshal([]byte(trimmed), &document); err != nil {
			return nil, false
		}
		normalised, err := json.Marshal(document)
		if err != nil {
			return nil, false
		}
		document = nil
		if err := json.Unmarshal(normalised, &document); err != nil {
			return nil, false
		}
	default:
		return nil, false
	}

	return document, document != nil
}

// collectConfigInvocations walks a decoded document for objects that launch bb.
//
// Keys are visited in sorted order so that a block with several servers reports
// the same way on every run; map iteration order would otherwise make findings
// depend on the hash seed.
func collectConfigInvocations(node any, out *[]configInvocation) {
	switch value := node.(type) {
	case map[string]any:
		if args, ok := bbInvocationFrom(value); ok {
			*out = append(*out, configInvocation{args: args})
		}

		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			collectConfigInvocations(value[key], out)
		}
	case []any:
		for _, item := range value {
			collectConfigInvocations(item, out)
		}
	}
}

// bbInvocationFrom reports the args of an object that launches bb, if it does.
func bbInvocationFrom(object map[string]any) ([]string, bool) {
	command, ok := object["command"].(string)
	if !ok || !isBBCommandName(command) {
		return nil, false
	}

	raw, ok := object["args"].([]any)
	if !ok || len(raw) == 0 {
		// "command": "bb" with nothing after it is a complete invocation and a
		// valid one -- bb prints help. There is nothing here to get wrong.
		return nil, false
	}

	args := make([]string, 0, len(raw))
	for _, item := range raw {
		text, ok := item.(string)
		if !ok {
			// A non-string argument is a configuration error rather than a bb
			// one, and the client will reject it long before bb sees it.
			return nil, false
		}
		args = append(args, text)
	}

	return args, true
}

// isBBCommandName reports whether a launcher command runs this CLI.
//
// The command may be a bare name or an absolute path, and on Windows it carries
// an extension. Splitting on both separators avoids depending on the separator
// of the machine the linter runs on rather than the one the config describes.
func isBBCommandName(command string) bool {
	name := strings.TrimSpace(command)
	if cut := strings.LastIndexAny(name, `/\`); cut >= 0 {
		name = name[cut+1:]
	}
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")

	return name == "bb"
}

// argsKeyLines returns the line of every "args" key in a block, in order.
//
// Used only to point a finding at the right server in a multi-server config.
// The count is required to match the number of invocations found, because a
// mismatch means the two orders cannot be trusted to correspond -- an "args"
// belonging to some other command, say -- and a confidently wrong line number
// is worse than the fence.
func argsKeyLines(block codeBlock) []int {
	var lines []int
	for offset, line := range strings.Split(block.body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, `"args"`) || strings.HasPrefix(trimmed, "args:") {
			lines = append(lines, block.startLine+offset+1)
		}
	}

	return lines
}

// lintConfigInvocations validates every bb invocation a configuration block
// launches, by the same rules a shell line gets.
func lintConfigInvocations(file string, block codeBlock) ([]finding, int) {
	invocations := extractConfigInvocations(block)
	if len(invocations) == 0 {
		return nil, 0
	}

	var findings []finding
	checked := 0

	for _, invocation := range invocations {
		rendered := "bb " + strings.Join(invocation.args, " ")

		cleaned, flagsToVerify, usable := resolvePlaceholders(invocation.args)
		if !usable {
			continue
		}
		checked++

		// A configured invocation is complete by construction -- the client
		// runs exactly these arguments -- so its positionals are checked, the
		// way a shell line's are and an inline prose reference's are not.
		problem := validate(cleaned, true)
		if problem == "" {
			problem = verifyFlagsExist(cleaned, flagsToVerify)
		}
		if problem == "" {
			continue
		}

		findings = append(findings, finding{
			File:    file,
			Line:    invocation.line,
			Command: rendered,
			Problem: problem,
		})
	}

	return findings, checked
}
