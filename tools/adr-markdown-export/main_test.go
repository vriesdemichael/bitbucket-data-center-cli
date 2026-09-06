package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportADRMarkdownGeneratesIndexAndRecords(t *testing.T) {
	t.Parallel()

	inputDir := filepath.Join(t.TempDir(), "decisions")
	outputDir := filepath.Join(t.TempDir(), "site", "adr")

	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatalf("mkdir input: %v", err)
	}

	recordOne := `number: 1
title: First Decision
category: architecture
status: accepted
decision: Use thing A.
agent_instructions: Follow thing A.
rationale: Thing A is best.
provenance: human
`
	recordTwo := `number: 2
title: Second Decision
category: development
status: proposed
decision: Use thing B.
agent_instructions: Follow thing B.
rationale: Thing B may work.
rejected_alternatives:
  - alternative: Thing C
    reason: Not enough signal.
provenance: guided-ai
`

	if err := os.WriteFile(filepath.Join(inputDir, "001-first-decision.yaml"), []byte(recordOne), 0o644); err != nil {
		t.Fatalf("write record one: %v", err)
	}
	if err := os.WriteFile(filepath.Join(inputDir, "002-second-decision.yaml"), []byte(recordTwo), 0o644); err != nil {
		t.Fatalf("write record two: %v", err)
	}

	if err := exportADRMarkdown(inputDir, outputDir); err != nil {
		t.Fatalf("exportADRMarkdown failed: %v", err)
	}

	indexPath := filepath.Join(outputDir, "index.md")
	indexRaw, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	index := string(indexRaw)
	if !strings.Contains(index, "# ADR Records") {
		t.Fatalf("missing index title")
	}
	if !strings.Contains(index, "[ADR 001: First Decision](001-first-decision.md)") {
		t.Fatalf("missing first ADR link")
	}

	recordPath := filepath.Join(outputDir, "002-second-decision.md")
	recordRaw, err := os.ReadFile(recordPath)
	if err != nil {
		t.Fatalf("read record: %v", err)
	}
	record := string(recordRaw)
	if !strings.Contains(record, "# ADR 002: Second Decision") {
		t.Fatalf("missing record title")
	}
	if !strings.Contains(record, "## Rejected Alternatives") {
		t.Fatalf("missing rejected alternatives section")
	}
	if !strings.HasSuffix(record, "\n") {
		t.Fatalf("expected trailing newline in record")
	}
}

func TestExportADRMarkdownErrorsForInvalidRecord(t *testing.T) {
	t.Parallel()

	inputDir := filepath.Join(t.TempDir(), "decisions")
	outputDir := filepath.Join(t.TempDir(), "site", "adr")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatalf("mkdir input: %v", err)
	}

	invalid := `number: 0
title: Invalid
`
	if err := os.WriteFile(filepath.Join(inputDir, "000-invalid.yaml"), []byte(invalid), 0o644); err != nil {
		t.Fatalf("write invalid record: %v", err)
	}

	err := exportADRMarkdown(inputDir, outputDir)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid number") {
		t.Fatalf("unexpected error: %v", err)
	}
}
