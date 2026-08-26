package codeowners

import (
	"bufio"
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
	Raw      string            // original token, e.g. "@backend\\ engineers:random(2)"
	Name     string            // unescaped username or group name without '@' or strategy, e.g. "backend engineers"
	IsGroup  bool              // true if prefixed with '@'
	Strategy SelectionStrategy // all, random, least_busy
	Count    int               // N for random(N) or least_busy(N); 0 if not specified (means all)
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
				if ref.IsGroup {
					owners = append(owners, "@"+ref.Name)
				} else {
					owners = append(owners, ref.Name)
				}
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

// MatchFile returns the owner names matching a single file path.
// Rules are evaluated from top to bottom; the last matching rule wins.
func (c *CodeOwners) MatchFile(filePath string) []string {
	refs := c.MatchFileRefs(filePath)
	var result []string
	for _, ref := range refs {
		if ref.IsGroup {
			result = append(result, "@"+ref.Name)
		} else {
			result = append(result, ref.Name)
		}
	}
	return result
}

// MatchFileRefs returns the rich OwnerRef objects matching a single file path.
// The last matching rule in the file wins.
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

	return matched
}

// MatchFiles matches a slice of file paths and returns the deduplicated union
// of owner strings across all matched files.
func (c *CodeOwners) MatchFiles(filePaths []string) []string {
	seen := make(map[string]bool)
	var result []string

	for _, ref := range c.MatchFileRefsUnion(filePaths) {
		key := strings.ToLower(ref.Name)
		if !seen[key] {
			seen[key] = true
			if ref.IsGroup {
				result = append(result, "@"+ref.Name)
			} else {
				result = append(result, ref.Name)
			}
		}
	}

	return result
}

// MatchFileRefsUnion matches a slice of file paths and returns the deduplicated union
// of OwnerRef structures across all matched files, combining group strategies appropriately.
func (c *CodeOwners) MatchFileRefsUnion(filePaths []string) []OwnerRef {
	seen := make(map[string]int) // lowercase name -> index in result
	var result []OwnerRef

	for _, path := range filePaths {
		refs := c.MatchFileRefs(path)
		for _, ref := range refs {
			key := strings.ToLower(ref.Name)
			if idx, ok := seen[key]; ok {
				// If a group was previously specified with random(N) or least_busy(N)
				// but another matching rule specifies :all (or higher count), upgrade it.
				if ref.Strategy == StrategyAll {
					result[idx].Strategy = StrategyAll
					result[idx].Count = 0
				} else if result[idx].Strategy != StrategyAll && ref.Count > result[idx].Count {
					result[idx].Count = ref.Count
				}
			} else {
				seen[key] = len(result)
				result = append(result, ref)
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

func parseOwnerRef(token string) OwnerRef {
	raw := strings.TrimSpace(token)
	strategy := StrategyAll
	count := 0
	base := raw

	if idx := strings.LastIndex(raw, ":"); idx != -1 {
		modifier := raw[idx+1:]
		if modifier == "all" {
			strategy = StrategyAll
			base = raw[:idx]
		} else if strings.HasPrefix(modifier, "random(") && strings.HasSuffix(modifier, ")") {
			strategy = StrategyRandom
			nStr := strings.TrimSuffix(strings.TrimPrefix(modifier, "random("), ")")
			if n, err := strconv.Atoi(nStr); err == nil && n > 0 {
				count = n
			}
			base = raw[:idx]
		} else if strings.HasPrefix(modifier, "least_busy(") && strings.HasSuffix(modifier, ")") {
			strategy = StrategyLeastBusy
			nStr := strings.TrimSuffix(strings.TrimPrefix(modifier, "least_busy("), ")")
			if n, err := strconv.Atoi(nStr); err == nil && n > 0 {
				count = n
			}
			base = raw[:idx]
		}
	}

	isGroup := strings.HasPrefix(base, "@")
	name := strings.TrimPrefix(base, "@")

	// Unescape backslash-escaped spaces (e.g. "backend\\ engineers" or "backend\ engineers")
	name = strings.ReplaceAll(name, `\\ `, " ")
	name = strings.ReplaceAll(name, `\ `, " ")
	name = strings.ReplaceAll(name, `\\`, "")
	name = strings.ReplaceAll(name, `\`, "")

	return OwnerRef{
		Raw:      raw,
		Name:     strings.TrimSpace(name),
		IsGroup:  isGroup,
		Strategy: strategy,
		Count:    count,
	}
}

func normalizePath(p string) string {
	clean := strings.ReplaceAll(p, "\\", "/")
	clean = strings.TrimPrefix(clean, "/")
	clean = strings.TrimSuffix(clean, "/")
	return clean
}

func compilePattern(pattern string) (*regexp.Regexp, error) {
	p := normalizePath(pattern)
	if p == "" {
		return nil, nil
	}

	anchored := false
	if strings.HasPrefix(pattern, "/") {
		anchored = true
	} else if strings.Contains(strings.TrimSuffix(pattern, "/"), "/") {
		anchored = true
	}

	matchesDir := strings.HasSuffix(pattern, "/")

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
