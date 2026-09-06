package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type rejectedAlternative struct {
	Alternative string `yaml:"alternative"`
	Reason      string `yaml:"reason"`
}

type decisionRecord struct {
	Number               int                   `yaml:"number"`
	Title                string                `yaml:"title"`
	Category             string                `yaml:"category"`
	Status               string                `yaml:"status,omitempty"`
	SupersededBy         *int                  `yaml:"superseded_by,omitempty"`
	Supersedes           any                   `yaml:"supersedes,omitempty"`
	AmendedBy            *int                  `yaml:"amended_by,omitempty"`
	Amends               any                   `yaml:"amends,omitempty"`
	Decision             string                `yaml:"decision"`
	AgentInstructions    string                `yaml:"agent_instructions"`
	Rationale            string                `yaml:"rationale"`
	RejectedAlternatives []rejectedAlternative `yaml:"rejected_alternatives,omitempty"`
	Provenance           string                `yaml:"provenance"`

	SourceFile string
	Slug       string
}

func main() {
	inputDir := flag.String("in", "docs/decisions", "Directory containing ADR YAML files")
	outputDir := flag.String("out", "docs/site/adr", "Directory for generated ADR markdown files")
	flag.Parse()

	if err := exportADRMarkdown(*inputDir, *outputDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func exportADRMarkdown(inputDir string, outputDir string) error {
	records, err := loadDecisionRecords(inputDir)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create ADR output directory: %w", err)
	}

	for _, record := range records {
		path := filepath.Join(outputDir, fmt.Sprintf("%03d-%s.md", record.Number, record.Slug))
		if err := os.WriteFile(path, []byte(renderRecordMarkdown(record)), 0o644); err != nil {
			return fmt.Errorf("write ADR markdown %s: %w", path, err)
		}
		fmt.Printf("wrote %s\n", path)
	}

	indexPath := filepath.Join(outputDir, "index.md")
	if err := os.WriteFile(indexPath, []byte(renderIndexMarkdown(records)), 0o644); err != nil {
		return fmt.Errorf("write ADR index markdown %s: %w", indexPath, err)
	}
	fmt.Printf("wrote %s\n", indexPath)

	return nil
}

func loadDecisionRecords(inputDir string) ([]decisionRecord, error) {
	paths, err := filepath.Glob(filepath.Join(inputDir, "*.yaml"))
	if err != nil {
		return nil, fmt.Errorf("glob ADR files: %w", err)
	}

	sort.Strings(paths)
	records := make([]decisionRecord, 0, len(paths))

	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read ADR YAML %s: %w", path, err)
		}

		var record decisionRecord
		if err := yaml.Unmarshal(raw, &record); err != nil {
			return nil, fmt.Errorf("parse ADR YAML %s: %w", path, err)
		}

		if record.Number <= 0 {
			return nil, fmt.Errorf("ADR %s has invalid number", path)
		}
		if strings.TrimSpace(record.Title) == "" {
			return nil, fmt.Errorf("ADR %s is missing title", path)
		}

		record.SourceFile = filepath.Base(path)
		record.Slug = slugFromFileName(path)
		records = append(records, record)
	}

	sort.Slice(records, func(i, j int) bool {
		return records[i].Number < records[j].Number
	})

	return records, nil
}

func slugFromFileName(path string) string {
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	parts := strings.SplitN(name, "-", 2)
	if len(parts) == 2 {
		return parts[1]
	}
	return name
}

func renderRecordMarkdown(record decisionRecord) string {
	var out strings.Builder

	fmt.Fprintf(&out, "# ADR %03d: %s\n\n", record.Number, strings.TrimSpace(record.Title))
	out.WriteString("This page is generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`. Do not edit manually.\n\n")

	fmt.Fprintf(&out, "- Number: `%03d`\n", record.Number)
	fmt.Fprintf(&out, "- Title: `%s`\n", strings.TrimSpace(record.Title))
	fmt.Fprintf(&out, "- Category: `%s`\n", strings.TrimSpace(record.Category))
	if strings.TrimSpace(record.Status) != "" {
		fmt.Fprintf(&out, "- Status: `%s`\n", strings.TrimSpace(record.Status))
	}
	if record.SupersededBy != nil {
		fmt.Fprintf(&out, "- Superseded By: `%03d`\n", *record.SupersededBy)
	}
	if supersedes := formatSupersedes(record.Supersedes); supersedes != "" {
		fmt.Fprintf(&out, "- Supersedes: `%s`\n", supersedes)
	}
	if strings.TrimSpace(record.Provenance) != "" {
		fmt.Fprintf(&out, "- Provenance: `%s`\n", strings.TrimSpace(record.Provenance))
	}
	fmt.Fprintf(&out, "- Source: `docs/decisions/%s`\n\n", record.SourceFile)

	out.WriteString("## Decision\n\n")
	out.WriteString(strings.TrimSpace(record.Decision))
	out.WriteString("\n\n")

	out.WriteString("## Agent Instructions\n\n")
	out.WriteString(strings.TrimSpace(record.AgentInstructions))
	out.WriteString("\n\n")

	out.WriteString("## Rationale\n\n")
	out.WriteString(strings.TrimSpace(record.Rationale))
	out.WriteString("\n\n")

	if len(record.RejectedAlternatives) > 0 {
		out.WriteString("## Rejected Alternatives\n\n")
		for _, alternative := range record.RejectedAlternatives {
			fmt.Fprintf(&out, "- `%s`: %s\n", strings.TrimSpace(alternative.Alternative), strings.TrimSpace(alternative.Reason))
		}
		out.WriteString("\n")
	}

	return strings.TrimRight(out.String(), "\n") + "\n"
}

func renderIndexMarkdown(records []decisionRecord) string {
	var out strings.Builder

	out.WriteString("# ADR Records\n\n")
	out.WriteString("Published Architecture and Development Decision Records for this project.\n\n")
	out.WriteString("This page and linked ADR pages are generated from `docs/decisions/*.yaml` by `task docs:export-adr-markdown`.\n\n")

	total := len(records)
	accepted := 0
	for _, record := range records {
		if strings.EqualFold(strings.TrimSpace(record.Status), "accepted") {
			accepted++
		}
	}

	fmt.Fprintf(&out, "- Total ADRs: `%d`\n", total)
	fmt.Fprintf(&out, "- Accepted ADRs: `%d`\n\n", accepted)

	out.WriteString("## ADR List\n\n")
	for _, record := range records {
		path := fmt.Sprintf("%03d-%s.md", record.Number, record.Slug)
		status := strings.TrimSpace(record.Status)
		if status == "" {
			status = "unspecified"
		}
		fmt.Fprintf(&out, "- [ADR %03d: %s](%s) (`%s`, `%s`)\n", record.Number, strings.TrimSpace(record.Title), path, strings.TrimSpace(record.Category), status)
	}

	out.WriteString("\n")
	return out.String()
}

func formatSupersedes(value any) string {
	if value == nil {
		return ""
	}

	switch typed := value.(type) {
	case int:
		return fmt.Sprintf("%03d", typed)
	case int64:
		return fmt.Sprintf("%03d", typed)
	case float64:
		return fmt.Sprintf("%03d", int(typed))
	case string:
		return strings.TrimSpace(typed)
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, stringifyAny(item))
		}
		return strings.Join(parts, ", ")
	default:
		return stringifyAny(typed)
	}
}

func stringifyAny(value any) string {
	switch typed := value.(type) {
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.Itoa(int(typed))
	case string:
		return strings.TrimSpace(typed)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	}
}
