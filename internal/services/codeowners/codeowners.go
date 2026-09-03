package codeowners

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// SelectionStrategy defines the algorithm used to pick reviewers from a reviewer group.
type SelectionStrategy string

const (
	// StrategyAll selects all members of the group.
	StrategyAll SelectionStrategy = "all"
	// StrategyRandom randomly selects N members from the group.
	StrategyRandom SelectionStrategy = "random"
	// StrategyLeastBusy selects N members from the group with the fewest open pull requests.
	StrategyLeastBusy SelectionStrategy = "least_busy"
)

// OwnerRef represents an individual owner or group reference parsed from CODEOWNERS.
type OwnerRef struct {
	Raw     string // original token, e.g. "@backend\\ engineers:random(2)"
	Name    string // unescaped username or group name without '@' or strategy, e.g. "backend engineers"
	IsGroup bool   // true if prefixed with '@'
	// IsReviewerGroup is true for the "@reviewer-group/<name>" form, which the
	// Code Owners plugin resolves through the reviewer-group API rather than as
	// a Bitbucket group. The two prefixes look alike and resolve differently, so
	// recognising only the bare "@group" form left this one broken (#503).
	IsReviewerGroup bool
	Strategy        SelectionStrategy // all, random, least_busy
	Count           int               // N for random(N) or least_busy(N); always 0 for StrategyAll
}

// Rule represents a single pattern and its assigned owners.
type Rule struct {
	Pattern   string
	Owners    []string // formatted strings e.g. "@backend engineers" or "alice"
	OwnerRefs []OwnerRef
	regex     *regexp.Regexp
}

// CodeOwners holds parsed rules from a CODEOWNERS file.
type CodeOwners struct {
	Rules []Rule
}

// Parse parses the content of a CODEOWNERS file into a CodeOwners struct.
// Lines whose pattern cannot be compiled, or which carry no resolvable owners,
// are skipped so that a single malformed line never invalidates the whole file.
func Parse(content string) *CodeOwners {
	var rules []Rule
	scanner := bufio.NewScanner(strings.NewReader(content))

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		fields := splitFieldsPreservingEscapes(line)
		if len(fields) < 2 {
			continue
		}

		pattern := fields[0]
		var owners []string
		var ownerRefs []OwnerRef

		for _, field := range fields[1:] {
			cleaned := strings.TrimSpace(field)
			if cleaned == "" || strings.HasPrefix(cleaned, "#") {
				break
			}
			ref := parseOwnerRef(cleaned)
			if ref.Name != "" {
				ownerRefs = append(ownerRefs, ref)
				owners = append(owners, ref.Display())
			}
		}

		if len(ownerRefs) == 0 {
			continue
		}

		re, err := compilePattern(pattern)
		if err != nil {
			continue
		}

		rules = append(rules, Rule{
			Pattern:   pattern,
			Owners:    owners,
			OwnerRefs: ownerRefs,
			regex:     re,
		})
	}

	return &CodeOwners{Rules: rules}
}

// Display renders the owner reference the way it is written in a CODEOWNERS
// file: groups keep their leading at-sign, individual users do not.
func (o OwnerRef) Display() string {
	if o.IsGroup {
		return "@" + o.Name
	}
	return o.Name
}

// key returns the deduplication key for an owner reference. Groups and users
// occupy separate namespaces, so a user "alice" never collides with a reviewer
// group "@alice".
func (o OwnerRef) key() string {
	if o.IsGroup {
		return "@" + strings.ToLower(o.Name)
	}
	return strings.ToLower(o.Name)
}

// MatchFile returns the owner names matching a single file path.
// Rules are evaluated from top to bottom; the last matching rule wins.
func (c *CodeOwners) MatchFile(filePath string) []string {
	refs := c.MatchFileRefs(filePath)
	var result []string
	for _, ref := range refs {
		result = append(result, ref.Display())
	}
	return result
}

// MatchFileRefs returns the rich OwnerRef objects matching a single file path.
// The last matching rule in the file wins. The returned slice is a copy, so
// callers may mutate it without corrupting the parsed rule set.
func (c *CodeOwners) MatchFileRefs(filePath string) []OwnerRef {
	cleanPath := normalizePath(filePath)
	if cleanPath == "" {
		return nil
	}

	var matched []OwnerRef
	for _, rule := range c.Rules {
		if rule.regex != nil && rule.regex.MatchString(cleanPath) {
			matched = rule.OwnerRefs
		}
	}

	if matched == nil {
		return nil
	}

	out := make([]OwnerRef, len(matched))
	copy(out, matched)
	return out
}

// MatchFiles matches a slice of file paths and returns the deduplicated union
// of owner strings across all matched files.
func (c *CodeOwners) MatchFiles(filePaths []string) []string {
	var result []string
	for _, ref := range c.MatchFileRefsUnion(filePaths) {
		result = append(result, ref.Display())
	}
	return result
}

// MatchFileRefsUnion matches a slice of file paths and returns the deduplicated union
// of OwnerRef structures across all matched files, combining group strategies appropriately.
//
// When the same group is reached through several rules the widest selection wins:
// an ":all" reference beats any bounded selection, and between two bounded
// selections the larger count wins.
func (c *CodeOwners) MatchFileRefsUnion(filePaths []string) []OwnerRef {
	seen := make(map[string]int) // owner key -> index in result
	var result []OwnerRef

	for _, path := range filePaths {
		refs := c.MatchFileRefs(path)
		for _, ref := range refs {
			key := ref.key()
			idx, ok := seen[key]
			if !ok {
				seen[key] = len(result)
				result = append(result, ref)
				continue
			}
			if result[idx].Strategy == StrategyAll {
				continue
			}
			if ref.Strategy == StrategyAll {
				result[idx].Strategy = StrategyAll
				result[idx].Count = 0
				continue
			}
			if ref.Count > result[idx].Count {
				result[idx].Strategy = ref.Strategy
				result[idx].Count = ref.Count
			}
		}
	}

	return result
}

func splitFieldsPreservingEscapes(line string) []string {
	var fields []string
	var current strings.Builder
	n := len(line)

	for i := 0; i < n; i++ {
		ch := line[i]
		if ch == '\\' {
			if i+2 < n && line[i+1] == '\\' && (line[i+2] == ' ' || line[i+2] == '\t') {
				current.WriteString(`\\ `)
				i += 2
				continue
			}
			if i+1 < n && (line[i+1] == ' ' || line[i+1] == '\t') {
				current.WriteString(`\ `)
				i++
				continue
			}
		}
		if ch == ' ' || ch == '\t' {
			if current.Len() > 0 {
				fields = append(fields, current.String())
				current.Reset()
			}
			continue
		}
		current.WriteByte(ch)
	}
	if current.Len() > 0 {
		fields = append(fields, current.String())
	}
	return fields
}

// unescapeToken resolves the backslash escapes CODEOWNERS uses to embed spaces
// in a path pattern or a reviewer group name. It is shared by the pattern and
// the owner parsers so that "docs\ site/" and "@backend\ engineers" unescape
// identically.
func unescapeToken(token string) string {
	out := strings.ReplaceAll(token, `\\ `, " ")
	out = strings.ReplaceAll(out, `\ `, " ")
	out = strings.ReplaceAll(out, `\\`, "")
	out = strings.ReplaceAll(out, `\`, "")
	return out
}

// reviewerGroupPrefix marks the Code Owners plugin's named-reviewer-group
// form, as distinct from a plain Bitbucket group.
const reviewerGroupPrefix = "reviewer-group/"

func parseOwnerRef(token string) OwnerRef {
	raw := strings.TrimSpace(token)
	strategy := StrategyAll
	count := 0
	base := raw

	if idx := strings.LastIndex(raw, ":"); idx != -1 {
		modifier := raw[idx+1:]
		switch {
		case modifier == "all":
			strategy = StrategyAll
			base = raw[:idx]
		case strings.HasPrefix(modifier, "random(") && strings.HasSuffix(modifier, ")"):
			strategy = StrategyRandom
			nStr := strings.TrimSuffix(strings.TrimPrefix(modifier, "random("), ")")
			if n, err := strconv.Atoi(nStr); err == nil && n > 0 {
				count = n
			}
			base = raw[:idx]
		case strings.HasPrefix(modifier, "least_busy(") && strings.HasSuffix(modifier, ")"):
			strategy = StrategyLeastBusy
			nStr := strings.TrimSuffix(strings.TrimPrefix(modifier, "least_busy("), ")")
			if n, err := strconv.Atoi(nStr); err == nil && n > 0 {
				count = n
			}
			base = raw[:idx]
		}
	}

	// A bounded strategy without a usable positive count selects everyone, so
	// normalize it to StrategyAll rather than carrying an ambiguous count of 0.
	if strategy != StrategyAll && count <= 0 {
		strategy = StrategyAll
		count = 0
	}

	isGroup := strings.HasPrefix(base, "@")
	name := unescapeToken(strings.TrimPrefix(base, "@"))

	// "@reviewer-group/cog_product" names a reviewer group called cog_product,
	// not a group called "reviewer-group/cog_product". Carrying the prefix into
	// the lookup made it miss, and the fallback then sent the whole string as a
	// username, which Bitbucket rejected with a 409 (#503).
	isReviewerGroup := false
	if isGroup {
		if bare, found := strings.CutPrefix(name, reviewerGroupPrefix); found {
			isReviewerGroup = true
			name = bare
		}
	}

	return OwnerRef{
		Raw:             raw,
		Name:            strings.TrimSpace(name),
		IsGroup:         isGroup,
		IsReviewerGroup: isReviewerGroup,
		Strategy:        strategy,
		Count:           count,
	}
}

func normalizePath(p string) string {
	clean := strings.ReplaceAll(p, "\\", "/")
	clean = strings.TrimPrefix(clean, "/")
	clean = strings.TrimSuffix(clean, "/")
	return clean
}

func compilePattern(pattern string) (*regexp.Regexp, error) {
	// Resolve escaped spaces before anything else: a pattern such as
	// "docs\ site/*.md" targets the directory "docs site", and the backslash
	// must never reach the regular expression.
	unescaped := unescapeToken(pattern)

	p := strings.TrimPrefix(unescaped, "/")
	p = strings.TrimSuffix(p, "/")
	if p == "" {
		return nil, fmt.Errorf("codeowners: pattern %q is empty", pattern)
	}

	anchored := false
	if strings.HasPrefix(unescaped, "/") {
		anchored = true
	} else if strings.Contains(strings.TrimSuffix(unescaped, "/"), "/") {
		anchored = true
	}

	matchesDir := strings.HasSuffix(unescaped, "/")

	var sb strings.Builder
	sb.WriteString("^")
	if !anchored {
		sb.WriteString("(?:.*/)?")
	}

	i := 0
	runes := []rune(p)
	n := len(runes)

	for i < n {
		if i+2 <= n && string(runes[i:i+2]) == "**" {
			if i > 0 && runes[i-1] == '/' && i+2 < n && runes[i+2] == '/' {
				sb.WriteString("(?:.+/)?")
				i += 3
				continue
			} else if i == 0 && i+2 < n && runes[i+2] == '/' {
				sb.WriteString("(?:.+/)?")
				i += 3
				continue
			} else {
				sb.WriteString(".*")
				i += 2
				continue
			}
		} else if runes[i] == '*' {
			sb.WriteString("[^/]*")
			i++
		} else if runes[i] == '?' {
			sb.WriteString("[^/]")
			i++
		} else {
			ch := runes[i]
			if strings.ContainsRune(`.+()|[]{}\^$`, ch) {
				sb.WriteRune('\\')
			}
			sb.WriteRune(ch)
			i++
		}
	}

	if matchesDir {
		sb.WriteString("/.*$")
	} else {
		sb.WriteString("(?:/.*)?$")
	}

	return regexp.Compile(sb.String())
}
