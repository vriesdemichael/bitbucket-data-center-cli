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
	"os/exec"
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
	updateVersion := flag.Bool("update-version", false, "Update stale version strings across documentation in-place")
	versionFlag := flag.String("version", "", "Target release version to enforce or update (defaults to latest git tag)")
	retiredHostRoot := flag.String("retired-host-root", ".", "Tree to scan for references to the retired documentation host")
	flag.Parse()

	targetVer := resolveTargetVersion(*versionFlag)

	if *updateVersion {
		if targetVer == "" {
			fmt.Fprintln(os.Stderr, "docs-lint: error: unable to determine target release version for -update-version")
			os.Exit(1)
		}
		updatedCount, err := updateDocumentationVersions(splitCSV(*roots), targetVer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "docs-lint: error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("docs-lint: updated release version to %s in %d file(s)\n", targetVer, updatedCount)
		return
	}

	if targetVer == "" {
		fmt.Fprintln(os.Stderr, "docs-lint: error: unable to resolve target release version from git tags (ensure tags are fetched, or specify -version or BB_RELEASE_VERSION)")
		os.Exit(1)
	}

	findings, checked, err := lintPathsWithVersion(splitCSV(*roots), targetVer)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Scanned over the whole tree rather than the documentation roots: the
	// retired host reached Go source and generated schemas as well as prose.
	hostFindings, err := lintRetiredHost(*retiredHostRoot)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	findings = append(findings, hostFindings...)
	sortFindings(findings)

	if code := report(os.Stdout, findings, checked); code != 0 {
		os.Exit(code)
	}
}

func resolveTargetVersion(explicit string) string {
	if explicit != "" {
		return strings.TrimPrefix(strings.TrimSpace(explicit), "v")
	}
	if envVer := os.Getenv("BB_RELEASE_VERSION"); envVer != "" {
		return strings.TrimPrefix(strings.TrimSpace(envVer), "v")
	}

	if ver := findLocalGitTag(); ver != "" {
		return ver
	}

	// In shallow clones (e.g. CI environments with fetch-depth: 1), tags may not be present locally.
	// Attempt fetching tags from origin if available.
	fetchCmd := exec.Command("git", "fetch", "--tags", "origin")
	_ = fetchCmd.Run()

	return findLocalGitTag()
}

func findLocalGitTag() string {
	cmd := exec.Command("git", "describe", "--tags", "--abbrev=0", "--match", "v[0-9]*.[0-9]*.[0-9]*")
	out, err := cmd.Output()
	if err == nil {
		tag := strings.TrimSpace(string(out))
		if tag != "" {
			return strings.TrimPrefix(tag, "v")
		}
	}
	cmd = exec.Command("git", "tag", "-l", "--sort=-v:refname", "v[0-9]*.[0-9]*.[0-9]*")
	out, err = cmd.Output()
	if err == nil {
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
			return strings.TrimPrefix(strings.TrimSpace(lines[0]), "v")
		}
	}
	return ""
}

func report(writer io.Writer, findings []finding, checked int) int {
	if len(findings) == 0 {
		fmt.Fprintf(writer, "docs-lint: %d documented bb invocations checked, all valid\n", checked)
		return 0
	}

	fmt.Fprintf(writer, "docs-lint: %d problem(s) found (%d documented bb invocations checked)\n\n", len(findings), checked)
	for _, item := range findings {
		fmt.Fprintf(writer, "  %s:%d\n    %s\n    %s\n\n", item.File, item.Line, item.Command, item.Problem)
	}

	return 1
}

func lintPaths(roots []string) ([]finding, int, error) {
	return lintPathsWithVersion(roots, resolveTargetVersion(""))
}

func lintPathsWithVersion(roots []string, targetVer string) ([]finding, int, error) {
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

		fileFindings, fileChecked := lintMarkdownWithVersion(filepath.ToSlash(file), normalised, targetVer)
		findings = append(findings, fileFindings...)
		checked += fileChecked
	}

	sortFindings(findings)

	return findings, checked, nil
}

// sortFindings orders findings by location so the report reads like a file
// listing regardless of which rule produced each one.
func sortFindings(findings []finding) {
	sort.Slice(findings, func(left, right int) bool {
		if findings[left].File == findings[right].File {
			return findings[left].Line < findings[right].Line
		}
		return findings[left].File < findings[right].File
	})
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

var (
	// A rule once rejected release filenames with no version segment, because
	// nothing was published under such a name and the URL would 404. Releases
	// now publish version-less aliases on purpose (ADR-057), so the shape it
	// enforced is valid and the rule is gone.
	unrenderedLaTeXRegex = regexp.MustCompile(`\$[^\$\n]+?\$`)
	mcpToolListRe        = regexp.MustCompile(`--(?:tools|exclude)\b["']?[\s:=,]*["']?([a-zA-Z0-9_.-]+(?:\s*,\s*[a-zA-Z0-9_.-]+)*)`)
	mcpToolValueOnlyRe   = regexp.MustCompile(`^["']?([a-zA-Z0-9_.-]+(?:\s*,\s*[a-zA-Z0-9_.-]+)*)`)

	shellVersionRe  = regexp.MustCompile(`^(?:export\s+)?VERSION=["']?(v?[0-9]+\.[0-9]+\.[0-9]+)["']?$`)
	pwshVersionRe   = regexp.MustCompile(`^\$Version\s*=\s*["']?(v?[0-9]+\.[0-9]+\.[0-9]+)["']?$`)
	dockerVersionRe = regexp.MustCompile(`^ARG\s+BB_VERSION=(v?[0-9]+\.[0-9]+\.[0-9]+)$`)
	yamlVersionRe   = regexp.MustCompile(`^\s*bb_version:\s*["']?(v?[0-9]+\.[0-9]+\.[0-9]+)["']?$`)
	proseVersionRe  = regexp.MustCompile(`(?i)\b(?:target|release)\s+version\s*\((?:e\.g\.|example:)\s*[\x60"]?(v?[0-9]+\.[0-9]+\.[0-9]+)[\x60"]?\)`)

	updateShellVersionRe  = regexp.MustCompile(`(?m)^((?:export\s+)?VERSION=["']?)(v?)[0-9]+\.[0-9]+\.[0-9]+(["']?)$`)
	updatePwshVersionRe   = regexp.MustCompile(`(?m)^(\$Version\s*=\s*["']?)(v?)[0-9]+\.[0-9]+\.[0-9]+(["']?)$`)
	updateDockerVersionRe = regexp.MustCompile(`(?m)^(ARG\s+BB_VERSION=)(v?)[0-9]+\.[0-9]+\.[0-9]+$`)
	updateYamlVersionRe   = regexp.MustCompile(`(?m)^(\s*bb_version:\s*["']?)(v?)[0-9]+\.[0-9]+\.[0-9]+(["']?)$`)
	updateProseVersionRe  = regexp.MustCompile(`(?i)\b((?:target|release)\s+version\s*\((?:e\.g\.|example:)\s*[\x60"]?)(v?)[0-9]+\.[0-9]+\.[0-9]+([\x60"]?\))`)
)

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

var (
	mkdocsMermaidOK   bool
	mkdocsMermaidOnce sync.Once
)

func checkMkdocsMermaidConfig() bool {
	mkdocsMermaidOnce.Do(func() {
		data, err := findMkDocsConfig()
		if err != nil {
			return
		}
		content := string(data)
		if strings.Contains(content, "custom_fences") && strings.Contains(content, "name: mermaid") {
			mkdocsMermaidOK = true
		}
	})
	return mkdocsMermaidOK
}

func findMkDocsConfig() ([]byte, error) {
	for _, candidate := range []string{"mkdocs.yml", "../../mkdocs.yml"} {
		if data, err := os.ReadFile(candidate); err == nil {
			return data, nil
		}
	}
	return nil, errors.New("mkdocs.yml not found")
}

func removeInlineCode(line string) string {
	var b strings.Builder
	inCode := false
	runes := []rune(line)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '`' {
			inCode = !inCode
			continue
		}
		if !inCode {
			b.WriteRune(runes[i])
		}
	}
	return b.String()
}

// lintMarkdown checks every bb invocation in the shell blocks of one document,
// as well as markdown dialect rules, release artifact naming, and configuration values.
func lintMarkdown(file, contents string) ([]finding, int) {
	return lintMarkdownWithVersion(file, contents, resolveTargetVersion(""))
}

func lintMarkdownWithVersion(file, contents, targetVer string) ([]finding, int) {
	var (
		findings []finding
		checked  int
	)

	lines := strings.Split(contents, "\n")
	var (
		inFence        bool
		fenceMarker    string
		pendingInvalid bool
	)

	for i, line := range lines {
		lineNum := i + 1
		trimmed := strings.TrimSpace(line)

		if !inFence {
			if isDirectiveComment(trimmed, expectInvalidDirective) {
				pendingInvalid = true
				continue
			}

			if marker, _, ok := openingFence(trimmed); ok {
				inFence = true
				fenceMarker = marker
				pendingInvalid = false
				continue
			}
		} else {
			if strings.HasPrefix(trimmed, fenceMarker) && strings.TrimSpace(strings.TrimPrefix(trimmed, fenceMarker)) == "" {
				inFence = false
			}
			continue
		}

		expectFail := pendingInvalid
		if trimmed != "" {
			pendingInvalid = false
		}

		var lineFindings []finding

		// 1. Prohibit file:// links in documentation
		if strings.Contains(line, "file://") {
			lineFindings = append(lineFindings, finding{
				File:    file,
				Line:    lineNum,
				Command: line,
				Problem: "prohibited file:// link; use relative markdown links instead",
			})
		}

		// 2. Prohibit GitHub-style callouts
		if strings.HasPrefix(trimmed, "> [!") {
			lineFindings = append(lineFindings, finding{
				File:    file,
				Line:    lineNum,
				Command: line,
				Problem: "prohibited GitHub callout syntax '> [!'; use Material for MkDocs '!!! note' syntax",
			})
		}

		// 3. Prohibit unrendered LaTeX math syntax outside code blocks
		proseLine := removeInlineCode(line)
		proseLine = strings.ReplaceAll(proseLine, `\$`, "")
		if unrenderedLaTeXRegex.MatchString(proseLine) || strings.Contains(proseLine, `$\`) {
			lineFindings = append(lineFindings, finding{
				File:    file,
				Line:    lineNum,
				Command: strings.TrimSpace(line),
				Problem: "unrendered LaTeX math found outside code block; write in words or configure arithmatex",
			})
		}

		// 5. Check prose release version examples (e.g. "Select a release version (example: `v0.1.0`).")
		if targetVer != "" {
			if m := proseVersionRe.FindStringSubmatch(line); len(m) > 1 {
				extracted := strings.TrimPrefix(m[1], "v")
				if extracted != targetVer {
					lineFindings = append(lineFindings, finding{
						File:    file,
						Line:    lineNum,
						Command: strings.TrimSpace(line),
						Problem: fmt.Sprintf("stale release version %q; must match current release version %q", m[1], targetVer),
					})
				}
			}
		}

		if expectFail {
			if len(lineFindings) == 0 {
				findings = append(findings, finding{
					File:    file,
					Line:    lineNum,
					Command: line,
					Problem: "line is marked " + expectInvalidDirective + " but this line is valid",
				})
			}
		} else {
			findings = append(findings, lineFindings...)
		}
	}

	shellBlocks, configBlocks, otherBlocks := parseCodeBlocks(contents)

	allBlocks := append(append(shellBlocks, configBlocks...), otherBlocks...)
	for _, block := range allBlocks {
		findings = append(findings, lintCodeBlockVersions(file, block, targetVer)...)
	}

	// Verify mermaid custom_fences configuration
	for _, block := range otherBlocks {
		if block.language == "mermaid" {
			if !checkMkdocsMermaidConfig() {
				findings = append(findings, finding{
					File:    file,
					Line:    block.startLine,
					Command: "```mermaid",
					Problem: "mermaid diagram requires pymdownx.superfences custom_fences configuration for mermaid in mkdocs.yml",
				})
			}
		}
	}

	for _, block := range configBlocks {
		findings = append(findings, lintConfigMCPTools(file, block)...)
	}

	for _, block := range shellBlocks {
		for _, invocation := range extractBBInvocations(block.body) {
			checked++

			problem := validateInvocation(invocation.Args)

			if block.expectInvalid {
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

func lintCodeBlockVersions(file string, block codeBlock, targetVer string) []finding {
	if targetVer == "" {
		return nil
	}

	var findings []finding
	hasVersionDeclaration := false
	lines := strings.Split(block.body, "\n")

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		var ver string

		if m := shellVersionRe.FindStringSubmatch(trimmed); len(m) > 1 {
			ver = m[1]
		} else if m := pwshVersionRe.FindStringSubmatch(trimmed); len(m) > 1 {
			ver = m[1]
		} else if m := dockerVersionRe.FindStringSubmatch(trimmed); len(m) > 1 {
			ver = m[1]
		} else if m := yamlVersionRe.FindStringSubmatch(trimmed); len(m) > 1 {
			ver = m[1]
		}

		if ver == "" {
			continue
		}

		hasVersionDeclaration = true
		normVer := strings.TrimPrefix(ver, "v")
		if normVer != targetVer {
			findings = append(findings, finding{
				File:    file,
				Line:    block.startLine + i + 1,
				Command: trimmed,
				Problem: fmt.Sprintf("stale release version %q in code block; must match current release version %q", ver, targetVer),
			})
		}
	}

	if !hasVersionDeclaration {
		return nil
	}

	if block.expectInvalid {
		if len(findings) == 0 {
			return []finding{
				{
					File:    file,
					Line:    block.startLine,
					Command: block.body,
					Problem: "block is marked " + expectInvalidDirective + " but this block has current release version",
				},
			}
		}
		return nil
	}

	return findings
}

func updateDocumentationVersions(roots []string, targetVer string) (int, error) {
	files, err := collectMarkdownFiles(roots)
	if err != nil {
		return 0, err
	}

	updatedCount := 0
	for _, file := range files {
		contents, err := os.ReadFile(file)
		if err != nil {
			return 0, fmt.Errorf("read %s: %w", file, err)
		}

		original := string(contents)
		updated := updateContentVersions(original, targetVer)
		if updated != original {
			normalised := strings.ReplaceAll(updated, "\r\n", "\n")
			if err := os.WriteFile(file, []byte(normalised), 0644); err != nil {
				return 0, fmt.Errorf("write %s: %w", file, err)
			}
			updatedCount++
		}
	}

	return updatedCount, nil
}

func updateContentVersions(content, targetVersion string) string {
	content = updateShellVersionRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := updateShellVersionRe.FindStringSubmatch(m)
		prefix := sub[1]
		hasV := sub[2] == "v"
		suffix := sub[3]
		if hasV {
			return prefix + "v" + targetVersion + suffix
		}
		return prefix + targetVersion + suffix
	})

	content = updatePwshVersionRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := updatePwshVersionRe.FindStringSubmatch(m)
		prefix := sub[1]
		hasV := sub[2] == "v"
		suffix := sub[3]
		if hasV {
			return prefix + "v" + targetVersion + suffix
		}
		return prefix + targetVersion + suffix
	})

	content = updateDockerVersionRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := updateDockerVersionRe.FindStringSubmatch(m)
		prefix := sub[1]
		hasV := sub[2] == "v"
		if hasV {
			return prefix + "v" + targetVersion
		}
		return prefix + targetVersion
	})

	content = updateYamlVersionRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := updateYamlVersionRe.FindStringSubmatch(m)
		prefix := sub[1]
		hasV := sub[2] == "v"
		suffix := sub[3]
		if hasV {
			return prefix + "v" + targetVersion + suffix
		}
		return prefix + targetVersion + suffix
	})

	content = updateProseVersionRe.ReplaceAllStringFunc(content, func(m string) string {
		sub := updateProseVersionRe.FindStringSubmatch(m)
		prefix := sub[1]
		hasV := sub[2] == "v"
		suffix := sub[3]
		if hasV {
			return prefix + "v" + targetVersion + suffix
		}
		return prefix + targetVersion + suffix
	})

	return content
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

		var toolsStr string
		targetLine := block.startLine + i + 1

		if m := mcpToolListRe.FindStringSubmatch(line); len(m) > 1 {
			toolsStr = m[1]
		} else if i+1 < len(lines) {
			if mNext := mcpToolValueOnlyRe.FindStringSubmatch(strings.TrimSpace(lines[i+1])); len(mNext) > 1 {
				toolsStr = mNext[1]
				targetLine = block.startLine + i + 2
			}
		}

		if toolsStr == "" {
			continue
		}

		for _, tool := range strings.Split(toolsStr, ",") {
			tool = strings.TrimSpace(tool)
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

	if block.expectInvalid {
		if len(findings) == 0 {
			return []finding{
				{
					File:    file,
					Line:    block.startLine,
					Command: block.body,
					Problem: "block is marked " + expectInvalidDirective + " but this configuration is valid",
				},
			}
		}
		return nil
	}

	return findings
}

type codeBlock struct {
	startLine     int
	language      string
	body          string
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

func parseCodeBlocks(contents string) (shellBlocks []codeBlock, configBlocks []codeBlock, otherBlocks []codeBlock) {
	var (
		body            []string
		fence           string
		inBlock         bool
		start           int
		currentLanguage string
		shell           bool
		isConfig        bool
		expectInvalid   bool
		pending         bool
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
			currentLanguage = language
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
				language:      currentLanguage,
				body:          strings.Join(body, "\n"),
				expectInvalid: expectInvalid,
			}
			if shell {
				shellBlocks = append(shellBlocks, block)
			} else if isConfig {
				configBlocks = append(configBlocks, block)
			} else {
				otherBlocks = append(otherBlocks, block)
			}
			inBlock = false

			continue
		}

		body = append(body, line)
	}

	return shellBlocks, configBlocks, otherBlocks
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
