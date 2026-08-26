// Command docs-lint validates every documented `bb ...` invocation against the
// real Cobra command tree.
//
// Documentation that does not parse is worse than missing documentation: the
// README quickstart is the first thing a new user copy-pastes, and skills/bb
// is emitted verbatim by agents. Six such invocations were found by hand during
// an external review; this makes that check automatic. See ADR-048.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli"
	"github.com/vriesdemichael/bitbucket-server-cli/internal/mcp"
)

// shellLanguages are the fenced-block languages whose contents are shell.
//
// Blocks tagged text, json or yaml hold output and configuration rather than
// commands, and a `bb` line inside them is illustrative rather than runnable.
//
// This is what excludes the generated command reference, which is by far the
// largest source of `bb ...` lines in the tree: its blocks are tagged text
// because they are Cobra help output. Those lines are usage strings carrying
// placeholders like [flags], so parsing them would report failures for
// documentation that is correct by construction — it is generated from the same
// command tree this linter validates against.
var shellLanguages = map[string]bool{
	"bash":    true,
	"sh":      true,
	"shell":   true,
	"console": true,
	"zsh":     true,
}

var configLanguages = map[string]bool{
	"json": true,
	"yaml": true,
	"yml":  true,
}

type finding struct {
	File    string
	Line    int
	Command string
	Problem string
}

func main() {
	roots := flag.String("roots", "README.md,AGENTS.md,CONTRIBUTING.md,SECURITY.md,docs,skills", "Comma-separated files and directories to scan")
	flag.Parse()

	findings, checked, err := lintPaths(splitCSV(*roots))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if code := report(os.Stdout, findings, checked); code != 0 {
		os.Exit(code)
	}
}

func report(writer io.Writer, findings []finding, checked int) int {
	if len(findings) == 0 {
		fmt.Fprintf(writer, "docs-lint: %d documented bb invocations checked, all valid\n", checked)
		return 0
	}

	fmt.Fprintf(writer, "docs-lint: %d of %d documented bb invocations are invalid\n\n", len(findings), checked)
	for _, item := range findings {
		fmt.Fprintf(writer, "  %s:%d\n    %s\n    %s\n\n", item.File, item.Line, item.Command, item.Problem)
	}

	return 1
}

func lintPaths(roots []string) ([]finding, int, error) {
	files, err := collectMarkdownFiles(roots)
	if err != nil {
		return nil, 0, err
	}

	var (
		findings []finding
		checked  int
	)

	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			return nil, 0, fmt.Errorf("read %s: %w", file, err)
		}

		// Normalised before parsing: on a CRLF checkout every trailing token
		// would otherwise carry a \r, which pflag reports as an unknown flag or
		// an invalid integer — an invisible difference that would send someone
		// editing correct documentation.
		normalised := strings.ReplaceAll(string(contents), "\r\n", "\n")

		fileFindings, fileChecked := lintMarkdown(filepath.ToSlash(file), normalised)
		findings = append(findings, fileFindings...)
		checked += fileChecked
	}

	sort.Slice(findings, func(left, right int) bool {
		if findings[left].File == findings[right].File {
			return findings[left].Line < findings[right].Line
		}
		return findings[left].File < findings[right].File
	})

	return findings, checked, nil
}

func collectMarkdownFiles(roots []string) ([]string, error) {
	var files []string
	seen := map[string]bool{}

	for _, root := range roots {
		info, err := os.Stat(root)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("stat %s: %w", root, err)
		}

		if !info.IsDir() {
			if strings.HasSuffix(root, ".md") && !seen[root] {
				seen[root] = true
				files = append(files, root)
			}
			continue
		}

		walkErr := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".md") || seen[path] {
				return nil
			}

			seen[path] = true
			files = append(files, path)

			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("walk %s: %w", root, walkErr)
		}
	}

	sort.Strings(files)

	return files, nil
}

var unversionedArtifactRegex = regexp.MustCompile(`\bbb_(linux|darwin|windows)_(amd64|arm64|x86_64)\.(tar\.gz|zip|deb|rpm)\b`)

var (
	validMCPTools     map[string]bool
	validMCPToolsOnce sync.Once
)

func isValidMCPTool(name string) bool {
	validMCPToolsOnce.Do(func() {
		validMCPTools = make(map[string]bool)
		for _, spec := range mcp.AllSpecs() {
			validMCPTools[spec.Tool.Name] = true
		}
	})
	return validMCPTools[name]
}

// lintMarkdown checks every bb invocation in the shell blocks of one document,
// as well as markdown dialect rules, release artifact naming, and configuration values.
func lintMarkdown(file, contents string) ([]finding, int) {
	var (
		findings []finding
		checked  int
	)

	lines := strings.Split(contents, "\n")
	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		// 1. Prohibit file:// links in documentation
		if strings.Contains(line, "file://") {
			findings = append(findings, finding{
				File:    file,
				Line:    lineNum,
				Command: line,
				Problem: "prohibited file:// link; use relative markdown links instead",
			})
		}

		// 2. Prohibit GitHub-style callouts
		if strings.HasPrefix(trimmed, "> [!") {
			findings = append(findings, finding{
				File:    file,
				Line:    lineNum,
				Command: line,
				Problem: "prohibited GitHub callout syntax '> [!'; use Material for MkDocs '!!! note' syntax",
			})
		}

		// 3. Prohibit unversioned release artifact filenames (e.g. bb_linux_amd64.tar.gz)
		if match := unversionedArtifactRegex.FindString(line); match != "" {
			findings = append(findings, finding{
				File:    file,
				Line:    lineNum,
				Command: line,
				Problem: fmt.Sprintf("release artifact %q is missing a version segment (must match bb_${VERSION}_<os>_<arch>.<ext>)", match),
			})
		}
	}

	// Invocations in shell code blocks & config checks in config blocks
	shellBlocks, configBlocks := parseCodeBlocks(contents)

	for _, block := range configBlocks {
		findings = append(findings, lintConfigMCPTools(file, block)...)
	}

	for _, block := range shellBlocks {
		for _, invocation := range extractBBInvocations(block.body) {
			checked++

			problem := validateInvocation(invocation.Args)

			if block.expectInvalid {
				// Inverted: the block documents what a malformed invocation
				// looks like, so a command that now parses means the example no
				// longer demonstrates what the prose says it does.
				if problem == "" {
					findings = append(findings, finding{
						File:    file,
						Line:    block.startLine + invocation.Line + 1,
						Command: invocation.Raw,
						Problem: "block is marked " + expectInvalidDirective + " but this command is valid",
					})
				}

				continue
			}

			if problem != "" {
				findings = append(findings, finding{
					File:    file,
					Line:    block.startLine + invocation.Line + 1,
					Command: invocation.Raw,
					Problem: problem,
				})
			}
		}
	}

	return findings, checked
}

func lintConfigMCPTools(file string, block codeBlock) []finding {
	var findings []finding
	lines := strings.Split(block.body, "\n")

	for i := 0; i < len(lines); i++ {
		line := lines[i]
		flagName := ""
		if strings.Contains(line, "--tools") {
			flagName = "--tools"
		} else if strings.Contains(line, "--exclude") {
			flagName = "--exclude"
		}
		if flagName == "" {
			continue
		}

		raw := extractToolListString(line, flagName)
		targetLine := block.startLine + i + 1
		if raw == "" && i+1 < len(lines) {
			raw = strings.TrimSpace(lines[i+1])
			targetLine = block.startLine + i + 2
		}

		raw = strings.Trim(raw, `"'[],`)
		if raw == "" {
			continue
		}

		for _, tool := range strings.Split(raw, ",") {
			tool = strings.Trim(strings.TrimSpace(tool), `"'`)
			if tool == "" || strings.HasPrefix(tool, "$") || strings.HasPrefix(tool, "--") {
				continue
			}
			if !isValidMCPTool(tool) {
				findings = append(findings, finding{
					File:    file,
					Line:    targetLine,
					Command: strings.TrimSpace(line),
					Problem: fmt.Sprintf("unknown MCP tool %q in %s", tool, flagName),
				})
			}
		}
	}

	return findings
}

func extractToolListString(line, flagName string) string {
	idx := strings.Index(line, flagName)
	if idx < 0 {
		return ""
	}
	rest := strings.TrimSpace(line[idx+len(flagName):])
	rest = strings.TrimPrefix(rest, "=")
	rest = strings.TrimPrefix(rest, ":")
	rest = strings.TrimSpace(rest)
	rest = strings.TrimPrefix(rest, ",")
	rest = strings.TrimSpace(rest)
	return rest
}

type codeBlock struct {
	// startLine is the 1-based line of the fence, so a body line at index n sits
	// at startLine+n+1 in the file.
	startLine int
	body      string
	// expectInvalid inverts the check for this block, for documentation that
	// deliberately shows a malformed invocation.
	expectInvalid bool
}

// expectInvalidDirective marks the next fenced block as deliberately showing
// invalid commands.
//
// An HTML comment rather than a shell comment, so it does not appear in the
// rendered page or in what a reader copies. The check is inverted rather than
// merely skipped: a block claiming to demonstrate a broken invocation that has
// quietly become valid is itself documentation drift, and the point of this
// linter is that no exemption can silently rot.
const expectInvalidDirective = "docs-lint: expect-invalid"

func isDirectiveComment(trimmed, directive string) bool {
	if !strings.HasPrefix(trimmed, "<!--") || !strings.HasSuffix(trimmed, "-->") {
		return false
	}

	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "<!--"), "-->"))

	return inner == directive
}

func parseCodeBlocks(contents string) (shellBlocks []codeBlock, configBlocks []codeBlock) {
	var (
		body          []string
		fence         string
		inBlock       bool
		start         int
		shell         bool
		isConfig      bool
		expectInvalid bool
		pending       bool
	)

	for index, line := range strings.Split(contents, "\n") {
		trimmed := strings.TrimSpace(line)

		if !inBlock {
			if isDirectiveComment(trimmed, expectInvalidDirective) {
				pending = true
				continue
			}

			marker, language, ok := openingFence(trimmed)
			if !ok {
				// A directive applies to the next fence, not across prose.
				if trimmed != "" {
					pending = false
				}
				continue
			}

			inBlock = true
			fence = marker
			start = index + 1
			shell = shellLanguages[language]
			isConfig = configLanguages[language]
			expectInvalid = pending
			pending = false
			body = nil

			continue
		}

		if strings.HasPrefix(trimmed, fence) && strings.TrimSpace(strings.TrimPrefix(trimmed, fence)) == "" {
			block := codeBlock{
				startLine:     start,
				body:          strings.Join(body, "\n"),
				expectInvalid: expectInvalid,
			}
			if shell {
				shellBlocks = append(shellBlocks, block)
			} else if isConfig {
				configBlocks = append(configBlocks, block)
			}
			inBlock = false

			continue
		}

		body = append(body, line)
	}

	return shellBlocks, configBlocks
}

func openingFence(trimmed string) (marker, language string, ok bool) {
	for _, candidate := range []string{"```", "~~~"} {
		if !strings.HasPrefix(trimmed, candidate) {
			continue
		}

		// An info string may be absent, or carry attributes as in ```bash title="x".
		info := strings.Fields(strings.TrimPrefix(trimmed, candidate))
		if len(info) > 0 {
			language = strings.ToLower(info[0])
		}

		return candidate, language, true
	}

	return "", "", false
}

// validateInvocation resolves args against a fresh command tree and reports the
// first problem, or "" when the invocation is valid.
//
// A fresh tree per invocation keeps parsed flag values from leaking between
// checks, which would otherwise make results depend on document order.
func validateInvocation(args []string) string {
	root := cli.NewRootCommand()
	root.SetOut(io.Discard)
	root.SetErr(io.Discard)
	// cmd/bb sets this at startup, and Cobra only registers --version when it is
	// non-empty. Without it, a documented `bb --version` reads as an unknown flag.
	root.Version = "docs-lint"

	target, remaining, err := root.Find(args)
	if err != nil {
		return err.Error()
	}

	// Cobra registers --help and --version lazily during Execute, which this
	// deliberately never calls. Without these, every documented `bb --help`
	// would be reported as an unknown flag.
	target.InitDefaultHelpFlag()
	target.InitDefaultVersionFlag()

	// Placeholders stand in for values a reader substitutes, so an invocation
	// that is only a prefix of a real command path is still wrong: bb would
	// print help rather than doing what the surrounding prose claims.
	if target.HasAvailableSubCommands() && len(remaining) > 0 {
		if first := remaining[0]; !strings.HasPrefix(first, "-") {
			return fmt.Sprintf("%q is not a subcommand of %q", first, target.CommandPath())
		}
	}

	if err := target.ParseFlags(remaining); err != nil {
		return err.Error()
	}

	if target.CommandPath() == "bb ai mcp serve" {
		if flag := target.Flags().Lookup("tools"); flag != nil && flag.Changed {
			for _, tool := range strings.Split(flag.Value.String(), ",") {
				tool = strings.TrimSpace(tool)
				if tool != "" && !strings.HasPrefix(tool, "$") && !isValidMCPTool(tool) {
					return fmt.Sprintf("unknown MCP tool %q in --tools", tool)
				}
			}
		}
		if flag := target.Flags().Lookup("exclude"); flag != nil && flag.Changed {
			for _, tool := range strings.Split(flag.Value.String(), ",") {
				tool = strings.TrimSpace(tool)
				if tool != "" && !strings.HasPrefix(tool, "$") && !isValidMCPTool(tool) {
					return fmt.Sprintf("unknown MCP tool %q in --exclude", tool)
				}
			}
		}
	}

	if err := validatePositionals(target, target.Flags().Args()); err != nil {
		return err.Error()
	}

	return ""
}

// validatePositionals applies the command's own argument rules.
//
// Skipped when help was requested, since --help legitimately ignores them.
func validatePositionals(target *cobra.Command, positionals []string) error {
	if helpRequested(target) {
		return nil
	}
	if target.Args == nil {
		if !hasPositionalPlaceholder(target.Use) && len(positionals) > 0 {
			return fmt.Errorf("accepts 0 arg(s), received %d", len(positionals))
		}
		return nil
	}

	return target.Args(target, positionals)
}

func hasPositionalPlaceholder(use string) bool {
	parts := strings.Fields(use)
	if len(parts) <= 1 {
		return false
	}
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, "<") || strings.HasPrefix(p, "[") {
			return true
		}
	}
	return false
}

func helpRequested(target *cobra.Command) bool {
	for _, name := range []string{"help", "version"} {
		flag := target.Flags().Lookup(name)
		if flag != nil && flag.Changed {
			return true
		}
	}

	return false
}

func splitCSV(raw string) []string {
	var values []string

	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			values = append(values, trimmed)
		}
	}

	return values
}
