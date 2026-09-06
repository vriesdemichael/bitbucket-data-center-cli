package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCalculatePatchCoverageRequiresAllOverlappingSegmentsCovered(t *testing.T) {
	t.Parallel()

	changed := map[string]map[int]struct{}{
		"internal/example.go": {
			10: {},
		},
	}

	profile := coverageProfile{
		byRelativePath: map[string][]coverageSegment{
			"internal/example.go": {
				{startLine: 10, endLine: 10, covered: true},
				{startLine: 10, endLine: 10, covered: false},
			},
		},
	}

	result := calculatePatchCoverage(changed, profile, []string{"internal/"}, nil)
	if result.coverableLines != 1 {
		t.Fatalf("expected 1 coverable line, got %d", result.coverableLines)
	}
	if result.coveredLines != 0 {
		t.Fatalf("expected 0 covered lines for partial overlap coverage, got %d", result.coveredLines)
	}
}

func TestCalculatePatchCoverageCountsFullyCoveredOverlaps(t *testing.T) {
	t.Parallel()

	changed := map[string]map[int]struct{}{
		"internal/example.go": {
			10: {},
		},
	}

	profile := coverageProfile{
		byRelativePath: map[string][]coverageSegment{
			"internal/example.go": {
				{startLine: 10, endLine: 10, covered: true},
				{startLine: 10, endLine: 10, covered: true},
			},
		},
	}

	result := calculatePatchCoverage(changed, profile, []string{"internal/"}, nil)
	if result.coverableLines != 1 {
		t.Fatalf("expected 1 coverable line, got %d", result.coverableLines)
	}
	if result.coveredLines != 1 {
		t.Fatalf("expected 1 covered line, got %d", result.coveredLines)
	}
}

func TestCalculatePatchCoverageRecordsUncoveredLines(t *testing.T) {
	t.Parallel()

	changed := map[string]map[int]struct{}{
		"internal/b.go": {12: {}, 13: {}, 14: {}, 20: {}},
		"internal/a.go": {5: {}},
	}

	profile := coverageProfile{
		byRelativePath: map[string][]coverageSegment{
			"internal/b.go": {
				{startLine: 12, endLine: 14, covered: false},
				{startLine: 20, endLine: 20, covered: true},
			},
			"internal/a.go": {
				{startLine: 5, endLine: 5, covered: false},
			},
		},
	}

	result := calculatePatchCoverage(changed, profile, []string{"internal/"}, nil)

	want := []uncoveredLine{
		{file: "internal/a.go", line: 5},
		{file: "internal/b.go", line: 12},
		{file: "internal/b.go", line: 13},
		{file: "internal/b.go", line: 14},
	}
	if len(result.uncovered) != len(want) {
		t.Fatalf("expected %d uncovered lines, got %d (%v)", len(want), len(result.uncovered), result.uncovered)
	}
	for index, expected := range want {
		if result.uncovered[index] != expected {
			t.Fatalf("uncovered[%d] = %v, want %v", index, result.uncovered[index], expected)
		}
	}
}

func TestReportUncoveredPatchLinesCollapsesRanges(t *testing.T) {
	t.Parallel()

	patch := patchCoverage{
		uncovered: []uncoveredLine{
			{file: "internal/a.go", line: 5},
			{file: "internal/b.go", line: 12},
			{file: "internal/b.go", line: 13},
			{file: "internal/b.go", line: 14},
			{file: "internal/b.go", line: 40},
		},
	}

	var buffer bytes.Buffer
	reportUncoveredPatchLines(&buffer, patch)

	got := buffer.String()
	for _, want := range []string{
		"Uncovered changed lines (5):",
		"  internal/a.go:5\n",
		"  internal/b.go:12-14,40\n",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("expected output to contain %q, got:\n%s", want, got)
		}
	}
}

func TestReportUncoveredPatchLinesWithNothingUncovered(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	reportUncoveredPatchLines(&buffer, patchCoverage{})

	if got := buffer.String(); got != "No uncovered changed lines.\n" {
		t.Fatalf("unexpected output %q", got)
	}
}
