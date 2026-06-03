package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type coverageSegment struct {
	startLine int
	endLine   int
	numStmt   int
	covered   bool
}

type coverageProfile struct {
	totalStatements   int
	coveredStatements int
	byRelativePath    map[string][]coverageSegment
}

type patchCoverage struct {
	coverableLines int
	coveredLines   int
	percent        float64
}

type operationManifest struct {
	Version    int                 `json:"version"`
	Operations map[string][]string `json:"operations"`
}

type report struct {
	Version            int                      `json:"version"`
	Coverage           coverageSummary          `json:"coverage"`
	GeneratedContracts generatedContractSummary `json:"generated_contracts"`
}

type coverageSummary struct {
	UnitRawPercent           float64      `json:"unit_raw_percent"`
	UnitScopedPercent        float64      `json:"unit_scoped_percent"`
	LiveRawPercent           float64      `json:"live_raw_percent"`
	LiveScopedPercent        float64      `json:"live_scoped_percent"`
	CombinedRawPercent       float64      `json:"combined_raw_percent"`
	CombinedScopedPercent    float64      `json:"combined_scoped_percent"`
	PatchPercent             float64      `json:"patch_percent"`
	UnitStatements           countSummary `json:"unit_statements"`
	LiveStatements           countSummary `json:"live_statements"`
	CombinedStatements       countSummary `json:"combined_statements"`
	UnitScopedStatements     countSummary `json:"unit_scoped_statements"`
	LiveScopedStatements     countSummary `json:"live_scoped_statements"`
	CombinedScopedStatements countSummary `json:"combined_scoped_statements"`
	PatchLines               countSummary `json:"patch_lines"`
	Scope                    scopeSummary `json:"scope"`
}

type countSummary struct {
	Covered int `json:"covered"`
	Total   int `json:"total"`
}

type scopeSummary struct {
	IncludePrefixes []string `json:"include_prefixes"`
	ExcludePrefixes []string `json:"exclude_prefixes"`
}

type generatedContractSummary struct {
	CoveragePercent    float64  `json:"coverage_percent"`
	UsedOperations     []string `json:"used_operations"`
	MappedOperations   []string `json:"mapped_operations"`
	UnmappedOperations []string `json:"unmapped_operations"`
}

func main() {
	coverProfilePath := flag.String("coverprofile", ".tmp/coverage.unit.out", "Path to unit go coverage profile")
	liveCoverProfilePath := flag.String("live-coverprofile", ".tmp/coverage.live.out", "Path to live go coverage profile")
	baseRef := flag.String("base-ref", "main", "Base git ref used for patch coverage diff")
	includePrefixes := flag.String("scope-include", "internal/,cmd/", "Comma-separated include path prefixes for scoped coverage")
	excludePrefixes := flag.String("scope-exclude", "internal/openapi/generated/,internal/models/generated/", "Comma-separated exclude path prefixes for scoped coverage")
	manifestPath := flag.String("manifest", "docs/quality/generated-operation-contracts.json", "Path to generated-operation contract manifest")
	reportPath := flag.String("report-file", "docs/quality/coverage-report.json", "Path to coverage report file")
	rawCoverProfilePath := flag.String("raw-coverprofile-file", "", "Path to committed combined raw coverprofile artifact")
	scopedCoverProfilePath := flag.String("scoped-coverprofile-file", "", "Path to committed combined scoped coverprofile artifact")
	writeReport := flag.Bool("write-report", false, "Write report file")
	verifyReport := flag.Bool("verify-report", false, "Verify report file matches generated output (recomputed from coverage profiles)")
	verifyCommitted := flag.Bool("verify-committed", false, "Verify committed report file against thresholds without recomputing coverage")
	writeCoverProfiles := flag.Bool("write-coverprofiles", false, "Write committed raw/scoped coverprofile artifacts when paths are provided")
	verifyCoverProfiles := flag.Bool("verify-coverprofiles", false, "Verify committed raw/scoped coverprofile artifacts match recomputed output when paths are provided")
	minGlobalCombined := flag.Float64("min-global-combined", 85.0, "Minimum required global combined coverage percentage")
	minScoped := flag.Float64("min-scoped", -1.0, "Deprecated alias for --min-global-combined")
	minPatch := flag.Float64("min-patch", 85.0, "Minimum required patch coverage percentage")
	minPatchLines := flag.Int("min-patch-lines", 30, "Minimum coverable patch lines required before applying percentage-based patch gate")
	maxUncoveredSmallPatch := flag.Int("max-uncovered-small-patch", 2, "Maximum uncovered patch lines allowed when coverable patch lines are below --min-patch-lines")
	minContract := flag.Float64("min-contract", 0.0, "Minimum required used generated operation contract coverage percentage")
	specCoverageMode := flag.Bool("spec-coverage", false, "Compute OpenAPI spec path coverage (both transports) and exit")
	specCoverageFile := flag.String("spec-coverage-file", "docs/quality/spec-coverage.json", "Path to spec coverage artifact")
	openapiSpecPath := flag.String("openapi-spec", "docs/reference/atlassian/bitbucket-9.4-openapi.json", "Path to the Bitbucket OpenAPI spec")
	generatedClientPath := flag.String("generated-client", "internal/openapi/generated/bitbucket_client.gen.go", "Path to the generated OpenAPI client")
	servicesRoot := flag.String("services-root", "internal/services", "Root directory scanned for API usage")
	flag.Parse()

	if *specCoverageMode {
		runSpecCoverage(*openapiSpecPath, *generatedClientPath, *servicesRoot, *specCoverageFile, *writeReport, *verifyReport)
		return
	}

	resolvedMinGlobalCombined := *minGlobalCombined
	if *minScoped >= 0 {
		resolvedMinGlobalCombined = *minScoped
	}

	if *verifyCommitted {
		reportData, err := readCommittedReport(*reportPath)
		if err != nil {
			fail("failed to read committed report: %v", err)
		}

		if *rawCoverProfilePath != "" || *scopedCoverProfilePath != "" {
			modulePath, err := readModulePath("go.mod")
			if err != nil {
				fail("failed to read module path: %v", err)
			}
			if *rawCoverProfilePath != "" {
				if _, err := parseCoverageProfile(*rawCoverProfilePath, modulePath); err != nil {
					fail("failed to parse committed raw coverprofile: %v", err)
				}
			}
			if *scopedCoverProfilePath != "" {
				if _, err := parseCoverageProfile(*scopedCoverProfilePath, modulePath); err != nil {
					fail("failed to parse committed scoped coverprofile: %v", err)
				}
			}
		}

		printCoverageSummary(reportData)
		enforceThresholds(reportData, resolvedMinGlobalCombined, *minPatch, *minPatchLines, *maxUncoveredSmallPatch, *minContract)
		fmt.Printf("Verified committed report: %s\n", *reportPath)
		if *rawCoverProfilePath != "" {
			fmt.Printf("Verified committed raw coverprofile: %s\n", *rawCoverProfilePath)
		}
		if *scopedCoverProfilePath != "" {
			fmt.Printf("Verified committed scoped coverprofile: %s\n", *scopedCoverProfilePath)
		}
		return
	}

	modulePath, err := readModulePath("go.mod")
	if err != nil {
		fail("failed to read module path: %v", err)
	}

	unitProfile, err := parseCoverageProfile(*coverProfilePath, modulePath)
	if err != nil {
		fail("failed to parse unit coverage profile: %v", err)
	}

	liveProfile, err := parseCoverageProfile(*liveCoverProfilePath, modulePath)
	if err != nil {
		fail("failed to parse live coverage profile: %v", err)
	}

	combinedProfile := mergeCoverageProfiles(unitProfile, liveProfile)

	changedLines, err := collectChangedLines(*baseRef)
	if err != nil {
		fail("failed to collect changed lines: %v", err)
	}

	includes := splitCSV(*includePrefixes)
	excludes := splitCSV(*excludePrefixes)
	unitScopedCovered, unitScopedTotal := calculateScopedCoverage(unitProfile, includes, excludes)
	liveScopedCovered, liveScopedTotal := calculateScopedCoverage(liveProfile, includes, excludes)
	combinedScopedCovered, combinedScopedTotal := calculateScopedCoverage(combinedProfile, includes, excludes)
	patch := calculatePatchCoverage(changedLines, combinedProfile, includes, excludes)

	usedOperations, err := discoverUsedGeneratedOperations("internal/services")
	if err != nil {
		fail("failed to discover used generated operations: %v", err)
	}
	manifest, err := loadOperationManifest(*manifestPath)
	if err != nil {
		fail("failed to load operation manifest: %v", err)
	}
	mapped, unmapped := calculateContractMapping(usedOperations, manifest)

	reportData := report{
		Version: 2,
		Coverage: coverageSummary{
			UnitRawPercent:           percent(unitProfile.coveredStatements, unitProfile.totalStatements),
			UnitScopedPercent:        percent(unitScopedCovered, unitScopedTotal),
			LiveRawPercent:           percent(liveProfile.coveredStatements, liveProfile.totalStatements),
			LiveScopedPercent:        percent(liveScopedCovered, liveScopedTotal),
			CombinedRawPercent:       percent(combinedProfile.coveredStatements, combinedProfile.totalStatements),
			CombinedScopedPercent:    percent(combinedScopedCovered, combinedScopedTotal),
			PatchPercent:             patch.percent,
			UnitStatements:           countSummary{Covered: unitProfile.coveredStatements, Total: unitProfile.totalStatements},
			LiveStatements:           countSummary{Covered: liveProfile.coveredStatements, Total: liveProfile.totalStatements},
			CombinedStatements:       countSummary{Covered: combinedProfile.coveredStatements, Total: combinedProfile.totalStatements},
			UnitScopedStatements:     countSummary{Covered: unitScopedCovered, Total: unitScopedTotal},
			LiveScopedStatements:     countSummary{Covered: liveScopedCovered, Total: liveScopedTotal},
			CombinedScopedStatements: countSummary{Covered: combinedScopedCovered, Total: combinedScopedTotal},
			PatchLines:               countSummary{Covered: patch.coveredLines, Total: patch.coverableLines},
			Scope:                    scopeSummary{IncludePrefixes: includes, ExcludePrefixes: excludes},
		},
		GeneratedContracts: generatedContractSummary{
			CoveragePercent:    percent(len(mapped), len(usedOperations)),
			UsedOperations:     usedOperations,
			MappedOperations:   mapped,
			UnmappedOperations: unmapped,
		},
	}

	encoded, err := json.MarshalIndent(reportData, "", "  ")
	if err != nil {
		fail("failed to encode report: %v", err)
	}
	encoded = append(encoded, '\n')

	rawCoverProfileEncoded := encodeCoverageProfile(combinedProfile, modulePath)
	scopedProfile := filterCoverageProfile(combinedProfile, includes, excludes)
	scopedCoverProfileEncoded := encodeCoverageProfile(scopedProfile, modulePath)

	printCoverageSummary(reportData)
	if patch.coverableLines == 0 {
		fmt.Println("Patch coverage: 100.00% (no coverable changed lines)")
	} else {
		fmt.Printf("Patch coverage: %.2f%% (%d/%d changed lines)\n", reportData.Coverage.PatchPercent, reportData.Coverage.PatchLines.Covered, reportData.Coverage.PatchLines.Total)
	}
	fmt.Printf("Generated used-operation contract coverage: %.2f%% (%d/%d operations mapped)\n", reportData.GeneratedContracts.CoveragePercent, len(mapped), len(usedOperations))

	if *writeReport {
		if err := os.MkdirAll(filepath.Dir(*reportPath), 0o755); err != nil {
			fail("failed to create report directory: %v", err)
		}
		if err := os.WriteFile(*reportPath, encoded, 0o644); err != nil {
			fail("failed to write report file: %v", err)
		}
		fmt.Printf("Wrote report: %s\n", *reportPath)
	}

	if *verifyReport {
		existing, err := os.ReadFile(*reportPath)
		if err != nil {
			fail("failed to read report file for verification: %v", err)
		}
		if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(encoded)) {
			fail("coverage report is out of date: run quality:coverage:report:update and commit %s", *reportPath)
		}
		fmt.Printf("Verified report: %s\n", *reportPath)
	}

	if *rawCoverProfilePath != "" {
		if *writeCoverProfiles {
			if err := os.MkdirAll(filepath.Dir(*rawCoverProfilePath), 0o755); err != nil {
				fail("failed to create raw coverprofile directory: %v", err)
			}
			if err := os.WriteFile(*rawCoverProfilePath, rawCoverProfileEncoded, 0o644); err != nil {
				fail("failed to write raw coverprofile: %v", err)
			}
			fmt.Printf("Wrote raw coverprofile: %s\n", *rawCoverProfilePath)
		}
		if *verifyCoverProfiles {
			existing, err := os.ReadFile(*rawCoverProfilePath)
			if err != nil {
				fail("failed to read raw coverprofile for verification: %v", err)
			}
			if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(rawCoverProfileEncoded)) {
				fail("raw coverprofile is out of date: run quality:coverage:report:update and commit %s", *rawCoverProfilePath)
			}
			fmt.Printf("Verified raw coverprofile: %s\n", *rawCoverProfilePath)
		}
	}

	if *scopedCoverProfilePath != "" {
		if *writeCoverProfiles {
			if err := os.MkdirAll(filepath.Dir(*scopedCoverProfilePath), 0o755); err != nil {
				fail("failed to create scoped coverprofile directory: %v", err)
			}
			if err := os.WriteFile(*scopedCoverProfilePath, scopedCoverProfileEncoded, 0o644); err != nil {
				fail("failed to write scoped coverprofile: %v", err)
			}
			fmt.Printf("Wrote scoped coverprofile: %s\n", *scopedCoverProfilePath)
		}
		if *verifyCoverProfiles {
			existing, err := os.ReadFile(*scopedCoverProfilePath)
			if err != nil {
				fail("failed to read scoped coverprofile for verification: %v", err)
			}
			if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(scopedCoverProfileEncoded)) {
				fail("scoped coverprofile is out of date: run quality:coverage:report:update and commit %s", *scopedCoverProfilePath)
			}
			fmt.Printf("Verified scoped coverprofile: %s\n", *scopedCoverProfilePath)
		}
	}

	enforceThresholds(reportData, resolvedMinGlobalCombined, *minPatch, *minPatchLines, *maxUncoveredSmallPatch, *minContract)
}

func printCoverageSummary(reportData report) {
	fmt.Printf("Unit raw coverage: %.2f%% (%d/%d statements)\n", reportData.Coverage.UnitRawPercent, reportData.Coverage.UnitStatements.Covered, reportData.Coverage.UnitStatements.Total)
	fmt.Printf("Live raw coverage: %.2f%% (%d/%d statements)\n", reportData.Coverage.LiveRawPercent, reportData.Coverage.LiveStatements.Covered, reportData.Coverage.LiveStatements.Total)
	fmt.Printf("Combined raw coverage: %.2f%% (%d/%d statements)\n", reportData.Coverage.CombinedRawPercent, reportData.Coverage.CombinedStatements.Covered, reportData.Coverage.CombinedStatements.Total)
	fmt.Printf("Combined scoped coverage: %.2f%% (%d/%d statements)\n", reportData.Coverage.CombinedScopedPercent, reportData.Coverage.CombinedScopedStatements.Covered, reportData.Coverage.CombinedScopedStatements.Total)
}

func enforceThresholds(reportData report, minGlobalCombined, minPatch float64, minPatchLines int, maxUncoveredSmallPatch int, minContract float64) {
	var failed bool
	globalCombinedPercent := reportData.Coverage.CombinedScopedPercent
	if globalCombinedPercent < minGlobalCombined {
		fmt.Printf("FAIL: global combined coverage %.2f%% is below required %.2f%%\n", globalCombinedPercent, minGlobalCombined)
		failed = true
	}
	patchTotal := reportData.Coverage.PatchLines.Total
	patchCovered := reportData.Coverage.PatchLines.Covered
	patchUncovered := patchTotal - patchCovered
	if patchTotal < minPatchLines {
		if patchUncovered > maxUncoveredSmallPatch {
			fmt.Printf("FAIL: uncovered patch lines %d exceed allowed %d for small patch (%d coverable lines < %d)\n", patchUncovered, maxUncoveredSmallPatch, patchTotal, minPatchLines)
			failed = true
		}
	} else if reportData.Coverage.PatchPercent < minPatch {
		fmt.Printf("FAIL: patch coverage %.2f%% is below required %.2f%% (%d coverable lines >= %d)\n", reportData.Coverage.PatchPercent, minPatch, patchTotal, minPatchLines)
		failed = true
	}
	if reportData.GeneratedContracts.CoveragePercent < minContract {
		fmt.Printf("FAIL: used generated operation contract coverage %.2f%% is below required %.2f%%\n", reportData.GeneratedContracts.CoveragePercent, minContract)
		failed = true
	}
	if failed {
		os.Exit(1)
	}
}

func readCommittedReport(path string) (report, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return report{}, err
	}
	parsed := report{}
	if err := json.Unmarshal(content, &parsed); err != nil {
		return report{}, err
	}
	return parsed, nil
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			result = append(result, filepath.ToSlash(trimmed))
		}
	}
	return result
}

func parseCoverageProfile(path string, modulePath string) (coverageProfile, error) {
	file, err := os.Open(path)
	if err != nil {
		return coverageProfile{}, err
	}
	defer file.Close()

	profile := coverageProfile{byRelativePath: map[string][]coverageSegment{}}
	scanner := bufio.NewScanner(file)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}

		pathPart, rangePart, numStmt, count, err := parseCoverageLine(line)
		if err != nil {
			return coverageProfile{}, err
		}

		relPath := pathPart
		modulePrefix := modulePath + "/"
		if strings.HasPrefix(relPath, modulePrefix) {
			relPath = strings.TrimPrefix(relPath, modulePrefix)
		}
		relPath = filepath.ToSlash(relPath)

		segment := coverageSegment{startLine: rangePart.startLine, endLine: rangePart.endLine, numStmt: numStmt, covered: count > 0}
		profile.byRelativePath[relPath] = append(profile.byRelativePath[relPath], segment)
		profile.totalStatements += numStmt
		if count > 0 {
			profile.coveredStatements += numStmt
		}
	}

	if err := scanner.Err(); err != nil {
		return coverageProfile{}, err
	}
	if profile.totalStatements == 0 {
		return coverageProfile{}, errors.New("no statements found in coverage profile")
	}
	return profile, nil
}

type segmentKey struct {
	filePath  string
	startLine int
	endLine   int
	numStmt   int
}

func mergeCoverageProfiles(unitProfile coverageProfile, liveProfile coverageProfile) coverageProfile {
	segments := map[segmentKey]coverageSegment{}

	addSegments := func(profile coverageProfile) {
		for relPath, values := range profile.byRelativePath {
			for _, segment := range values {
				key := segmentKey{filePath: relPath, startLine: segment.startLine, endLine: segment.endLine, numStmt: segment.numStmt}
				existing, ok := segments[key]
				if !ok {
					existing = coverageSegment{startLine: segment.startLine, endLine: segment.endLine, numStmt: segment.numStmt, covered: false}
				}
				existing.covered = existing.covered || segment.covered
				segments[key] = existing
			}
		}
	}

	addSegments(unitProfile)
	addSegments(liveProfile)

	combined := coverageProfile{byRelativePath: map[string][]coverageSegment{}}
	for key, segment := range segments {
		combined.byRelativePath[key.filePath] = append(combined.byRelativePath[key.filePath], segment)
		combined.totalStatements += segment.numStmt
		if segment.covered {
			combined.coveredStatements += segment.numStmt
		}
	}

	for filePath := range combined.byRelativePath {
		sort.Slice(combined.byRelativePath[filePath], func(i, j int) bool {
			left := combined.byRelativePath[filePath][i]
			right := combined.byRelativePath[filePath][j]
			if left.startLine != right.startLine {
				return left.startLine < right.startLine
			}
			if left.endLine != right.endLine {
				return left.endLine < right.endLine
			}
			return left.numStmt < right.numStmt
		})
	}

	return combined
}

type segmentRange struct {
	startLine int
	endLine   int
}

func parseCoverageLine(line string) (string, segmentRange, int, int, error) {
	fields := strings.Fields(line)
	if len(fields) != 3 {
		return "", segmentRange{}, 0, 0, fmt.Errorf("invalid coverage line format: %q", line)
	}

	location := fields[0]
	numStmt, err := strconv.Atoi(fields[1])
	if err != nil {
		return "", segmentRange{}, 0, 0, fmt.Errorf("invalid statement count: %w", err)
	}
	count, err := strconv.Atoi(fields[2])
	if err != nil {
		return "", segmentRange{}, 0, 0, fmt.Errorf("invalid execution count: %w", err)
	}

	colonIndex := strings.LastIndex(location, ":")
	if colonIndex == -1 {
		return "", segmentRange{}, 0, 0, fmt.Errorf("missing location separator in %q", location)
	}
	pathPart := location[:colonIndex]
	rangePart := location[colonIndex+1:]
	parts := strings.Split(rangePart, ",")
	if len(parts) != 2 {
		return "", segmentRange{}, 0, 0, fmt.Errorf("invalid range in %q", rangePart)
	}

	startParts := strings.Split(parts[0], ".")
	endParts := strings.Split(parts[1], ".")
	if len(startParts) != 2 || len(endParts) != 2 {
		return "", segmentRange{}, 0, 0, fmt.Errorf("invalid position in %q", rangePart)
	}

	startLine, err := strconv.Atoi(startParts[0])
	if err != nil {
		return "", segmentRange{}, 0, 0, fmt.Errorf("invalid start line: %w", err)
	}
	endLine, err := strconv.Atoi(endParts[0])
	if err != nil {
		return "", segmentRange{}, 0, 0, fmt.Errorf("invalid end line: %w", err)
	}

	return pathPart, segmentRange{startLine: startLine, endLine: endLine}, numStmt, count, nil
}

func calculateScopedCoverage(profile coverageProfile, includePrefixes, excludePrefixes []string) (int, int) {
	covered := 0
	total := 0
	for relPath, segments := range profile.byRelativePath {
		if !pathIncluded(relPath, includePrefixes, excludePrefixes) {
			continue
		}
		for _, segment := range segments {
			total += segment.numStmt
			if segment.covered {
				covered += segment.numStmt
			}
		}
	}
	return covered, total
}

func filterCoverageProfile(profile coverageProfile, includePrefixes, excludePrefixes []string) coverageProfile {
	filtered := coverageProfile{byRelativePath: map[string][]coverageSegment{}}
	for relPath, segments := range profile.byRelativePath {
		if !pathIncluded(relPath, includePrefixes, excludePrefixes) {
			continue
		}
		copied := make([]coverageSegment, len(segments))
		copy(copied, segments)
		filtered.byRelativePath[relPath] = copied
		for _, segment := range copied {
			filtered.totalStatements += segment.numStmt
			if segment.covered {
				filtered.coveredStatements += segment.numStmt
			}
		}
	}
	return filtered
}

func encodeCoverageProfile(profile coverageProfile, modulePath string) []byte {
	var builder strings.Builder
	builder.WriteString("mode: count\n")

	paths := make([]string, 0, len(profile.byRelativePath))
	for relPath := range profile.byRelativePath {
		paths = append(paths, relPath)
	}
	sort.Strings(paths)

	for _, relPath := range paths {
		segments := profile.byRelativePath[relPath]
		qualifiedPath := relPath
		if modulePath != "" && !strings.HasPrefix(relPath, modulePath+"/") {
			qualifiedPath = modulePath + "/" + relPath
		}
		for _, segment := range segments {
			count := 0
			if segment.covered {
				count = 1
			}
			startCol := 1
			endCol := 1
			if segment.startLine == segment.endLine {
				endCol = startCol + 1
			}
			fmt.Fprintf(&builder, "%s:%d.%d,%d.%d %d %d\n", qualifiedPath, segment.startLine, startCol, segment.endLine, endCol, segment.numStmt, count)
		}
	}

	return []byte(builder.String())
}

func pathIncluded(path string, includes, excludes []string) bool {
	for _, excluded := range excludes {
		if strings.HasPrefix(path, excluded) {
			return false
		}
	}
	if len(includes) == 0 {
		return true
	}
	for _, included := range includes {
		if strings.HasPrefix(path, included) {
			return true
		}
	}
	return false
}

func collectChangedLines(baseRef string) (map[string]map[int]struct{}, error) {
	mergeBaseCmd := exec.Command("git", "merge-base", baseRef, "HEAD")
	mergeBaseOutput, err := mergeBaseCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git merge-base failed: %w", err)
	}
	mergeBase := strings.TrimSpace(string(mergeBaseOutput))
	if mergeBase == "" {
		return nil, errors.New("empty merge-base result")
	}

	diffCmd := exec.Command("git", "diff", "--unified=0", "--no-color", mergeBase, "--", ".")
	diffOutput, err := diffCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff failed: %w", err)
	}

	return parseUnifiedDiffChangedLines(string(diffOutput)), nil
}

func parseUnifiedDiffChangedLines(diff string) map[string]map[int]struct{} {
	changed := map[string]map[int]struct{}{}
	lines := strings.Split(diff, "\n")
	currentFile := ""
	currentNewLine := 0
	inHunk := false
	hunkPattern := regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

	for _, line := range lines {
		if strings.HasPrefix(line, "+++ ") {
			inHunk = false
			fileToken := strings.TrimSpace(strings.TrimPrefix(line, "+++ "))
			if fileToken == "/dev/null" {
				currentFile = ""
				continue
			}
			currentFile = filepath.ToSlash(strings.TrimPrefix(fileToken, "b/"))
			if !strings.HasSuffix(currentFile, ".go") {
				currentFile = ""
			}
			continue
		}
		if strings.HasPrefix(line, "@@") {
			matches := hunkPattern.FindStringSubmatch(line)
			if len(matches) == 0 {
				inHunk = false
				continue
			}
			startLine, err := strconv.Atoi(matches[1])
			if err != nil {
				inHunk = false
				continue
			}
			currentNewLine = startLine
			inHunk = true
			continue
		}
		if !inHunk || currentFile == "" {
			continue
		}

		switch {
		case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
			if !strings.HasSuffix(currentFile, "_test.go") {
				if _, ok := changed[currentFile]; !ok {
					changed[currentFile] = map[int]struct{}{}
				}
				changed[currentFile][currentNewLine] = struct{}{}
			}
			currentNewLine++
		case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
		case strings.HasPrefix(line, " "):
			currentNewLine++
		default:
			inHunk = false
		}
	}

	return changed
}

func calculatePatchCoverage(changed map[string]map[int]struct{}, profile coverageProfile, includePrefixes, excludePrefixes []string) patchCoverage {
	result := patchCoverage{}
	for filePath, lineSet := range changed {
		if !pathIncluded(filePath, includePrefixes, excludePrefixes) {
			continue
		}
		segments := profile.byRelativePath[filePath]
		if len(segments) == 0 {
			continue
		}
		for line := range lineSet {
			coverable := false
			covered := true
			for _, segment := range segments {
				if line < segment.startLine || line > segment.endLine {
					continue
				}
				coverable = true
				if !segment.covered {
					covered = false
				}
			}
			if !coverable {
				continue
			}
			result.coverableLines++
			if covered {
				result.coveredLines++
			}
		}
	}
	if result.coverableLines == 0 {
		result.percent = 100.0
		return result
	}
	result.percent = percent(result.coveredLines, result.coverableLines)
	return result
}

func discoverUsedGeneratedOperations(root string) ([]string, error) {
	set := map[string]struct{}{}
	fileSet := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}

		node, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}

		hasGeneratedImport := false
		for _, imported := range node.Imports {
			importPath := strings.Trim(imported.Path.Value, "\"")
			if strings.HasSuffix(importPath, "/internal/openapi/generated") {
				hasGeneratedImport = true
				break
			}
		}
		if !hasGeneratedImport {
			return nil
		}

		ast.Inspect(node, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel == nil {
				return true
			}
			inner, ok := sel.X.(*ast.SelectorExpr)
			if !ok || inner.Sel == nil || inner.Sel.Name != "client" {
				return true
			}
			set[sel.Sel.Name] = struct{}{}
			return true
		})

		return nil
	})
	if err != nil {
		return nil, err
	}

	operations := make([]string, 0, len(set))
	for operation := range set {
		operations = append(operations, operation)
	}
	sort.Strings(operations)
	return operations, nil
}

func loadOperationManifest(path string) (operationManifest, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return operationManifest{}, err
	}
	manifest := operationManifest{}
	if err := json.Unmarshal(content, &manifest); err != nil {
		return operationManifest{}, err
	}
	if manifest.Operations == nil {
		manifest.Operations = map[string][]string{}
	}
	return manifest, nil
}

func calculateContractMapping(used []string, manifest operationManifest) ([]string, []string) {
	mapped := make([]string, 0)
	unmapped := make([]string, 0)
	for _, operation := range used {
		tests := manifest.Operations[operation]
		if len(tests) == 0 {
			unmapped = append(unmapped, operation)
			continue
		}
		mapped = append(mapped, operation)
	}
	sort.Strings(mapped)
	sort.Strings(unmapped)
	return mapped, unmapped
}

func readModulePath(goModPath string) (string, error) {
	content, err := os.ReadFile(goModPath)
	if err != nil {
		return "", err
	}
	for _, rawLine := range strings.Split(string(content), "\n") {
		line := strings.TrimSpace(rawLine)
		if strings.HasPrefix(line, "module ") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			if value != "" {
				return value, nil
			}
		}
	}
	return "", errors.New("module declaration not found")
}

func percent(covered int, total int) float64 {
	if total <= 0 {
		return 100.0
	}
	return (float64(covered) / float64(total)) * 100.0
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
