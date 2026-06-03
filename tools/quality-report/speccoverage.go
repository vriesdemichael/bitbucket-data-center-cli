package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// runSpecCoverage computes spec path coverage and writes or verifies the
// committed artifact, then prints a summary.
func runSpecCoverage(specPath, generatedPath, servicesRoot, outputPath string, writeReport, verifyReport bool) {
	report, err := computeSpecCoverage(specPath, generatedPath, servicesRoot)
	if err != nil {
		fail("failed to compute spec coverage: %v", err)
	}

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fail("failed to encode spec coverage report: %v", err)
	}
	encoded = append(encoded, '\n')

	fmt.Printf("OpenAPI spec coverage: %.2f%% (%d/%d operations)\n", report.CoveragePercent, report.Covered, report.Total)
	for _, tag := range report.ByTag {
		label := tag.Tag
		if label == "" {
			label = "(untagged)"
		}
		fmt.Printf("  %-26s %3d/%-3d (%.0f%%)\n", label, tag.Covered, tag.Total, percent(tag.Covered, tag.Total))
	}

	if writeReport {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			fail("failed to create spec coverage directory: %v", err)
		}
		if err := os.WriteFile(outputPath, encoded, 0o644); err != nil {
			fail("failed to write spec coverage report: %v", err)
		}
		fmt.Printf("Wrote spec coverage report: %s\n", outputPath)
	}

	if verifyReport {
		existing, err := os.ReadFile(outputPath)
		if err != nil {
			fail("failed to read spec coverage report for verification: %v", err)
		}
		if !bytes.Equal(bytes.TrimSpace(existing), bytes.TrimSpace(encoded)) {
			fail("spec coverage report is out of date: run quality:spec-coverage:update and commit %s", outputPath)
		}
		fmt.Printf("Verified spec coverage report: %s\n", outputPath)
	}
}

// methodPath is the canonical unit of API coverage: an HTTP verb paired with a
// normalized request path (path/format parameters collapsed to "{}").
type methodPath struct {
	Method string
	Path   string
}

type specOperation struct {
	Method   string
	Path     string // original spec path, kept for human-readable gap reporting
	NormPath string
	Tag      string
	Summary  string
}

type specTagCoverage struct {
	Tag     string `json:"tag"`
	Covered int    `json:"covered"`
	Total   int    `json:"total"`
}

type specGap struct {
	Method  string `json:"method"`
	Path    string `json:"path"`
	Tag     string `json:"tag"`
	Summary string `json:"summary,omitempty"`
}

type specCoverageReport struct {
	Version         int               `json:"version"`
	Spec            string            `json:"spec"`
	Covered         int               `json:"covered"`
	Total           int               `json:"total"`
	CoveragePercent float64           `json:"coverage_percent"`
	ByTag           []specTagCoverage `json:"by_tag"`
	Gaps            []specGap         `json:"gaps"`
}

var (
	formatVerbRe = regexp.MustCompile(`%[+#0-9.]*[sdvqtxX]`)
	pathParamRe  = regexp.MustCompile(`\{[^}]*\}`)
	httpMethods  = map[string]struct{}{
		"get": {}, "post": {}, "put": {}, "delete": {}, "patch": {},
	}
	jsonHelperMethod = map[string]string{
		"GetJSON":    "GET",
		"PostJSON":   "POST",
		"PutJSON":    "PUT",
		"DeleteJSON": "DELETE",
		"PatchJSON":  "PATCH",
	}
)

// normalizeAPIPath collapses a spec or constructed path into the canonical form
// used for joining: query strings dropped, a leading "/rest" stripped, format
// verbs and "{param}" segments replaced with "{}", and the API version folded to
// "latest" so 1.0 and latest paths compare equal.
func normalizeAPIPath(p string) string {
	if i := strings.IndexByte(p, '?'); i >= 0 {
		p = p[:i]
	}
	p = strings.TrimPrefix(p, "/rest")
	p = formatVerbRe.ReplaceAllString(p, "{}")
	p = pathParamRe.ReplaceAllString(p, "{}")
	p = strings.ReplaceAll(p, "/1.0/", "/latest/")
	return p
}

// loadSpecOperations reads the OpenAPI document and returns every (method, path)
// operation it declares.
func loadSpecOperations(specPath string) ([]specOperation, error) {
	content, err := os.ReadFile(specPath)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}
	if err := json.Unmarshal(content, &doc); err != nil {
		return nil, fmt.Errorf("parse openapi spec: %w", err)
	}

	operations := make([]specOperation, 0, len(doc.Paths))
	for path, item := range doc.Paths {
		for method, raw := range item {
			if _, ok := httpMethods[strings.ToLower(method)]; !ok {
				continue
			}
			var op struct {
				Tags    []string `json:"tags"`
				Summary string   `json:"summary"`
			}
			if err := json.Unmarshal(raw, &op); err != nil {
				return nil, fmt.Errorf("parse operation %s %s: %w", method, path, err)
			}
			tag := ""
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}
			operations = append(operations, specOperation{
				Method:   strings.ToUpper(method),
				Path:     path,
				NormPath: normalizeAPIPath(path),
				Tag:      tag,
				Summary:  op.Summary,
			})
		}
	}
	sort.Slice(operations, func(i, j int) bool {
		if operations[i].Path != operations[j].Path {
			return operations[i].Path < operations[j].Path
		}
		return operations[i].Method < operations[j].Method
	})
	return operations, nil
}

// parseGeneratedOperationPaths maps each generated operation name (e.g.
// "GetCommit") to the (method, normalized path) its request builder targets, by
// reading the New<Operation>Request constructors in the generated client.
func parseGeneratedOperationPaths(generatedPath string) (map[string]methodPath, error) {
	fileSet := token.NewFileSet()
	node, err := parser.ParseFile(fileSet, generatedPath, nil, 0)
	if err != nil {
		return nil, err
	}

	result := map[string]methodPath{}
	for _, decl := range node.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil {
			continue
		}
		name := fn.Name.Name
		if !strings.HasPrefix(name, "New") {
			continue
		}
		// Operations with a request body delegate from New<Op>Request to
		// New<Op>RequestWithBody, and only the latter constructs the path.
		var op string
		switch {
		case strings.HasSuffix(name, "RequestWithBody"):
			op = name[len("New") : len(name)-len("RequestWithBody")]
		case strings.HasSuffix(name, "Request"):
			op = name[len("New") : len(name)-len("Request")]
		default:
			continue
		}

		var method, path string
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			switch stmt := n.(type) {
			case *ast.AssignStmt:
				// operationPath := fmt.Sprintf("...") | "..."
				if path == "" && len(stmt.Lhs) == 1 && len(stmt.Rhs) == 1 {
					if id, ok := stmt.Lhs[0].(*ast.Ident); ok && id.Name == "operationPath" {
						if lit, ok := stringFromExpr(stmt.Rhs[0]); ok {
							path = lit
						}
					}
				}
			case *ast.CallExpr:
				// http.NewRequest("GET", ...)
				if method == "" && isSelector(stmt.Fun, "http", "NewRequest") && len(stmt.Args) >= 1 {
					if lit, ok := stringLiteral(stmt.Args[0]); ok {
						method = strings.ToUpper(lit)
					}
				}
			}
			return method == "" || path == ""
		})

		if method != "" && path != "" {
			result[op] = methodPath{Method: method, Path: normalizeAPIPath(path)}
		}
	}
	return result, nil
}

// usedOperationToName strips the generated WithResponse / WithBody suffixes so a
// discovered call name maps back to its base operation.
func usedOperationToName(name string) string {
	for _, suffix := range []string{"WithBodyWithResponse", "WithResponse", "WithBody"} {
		if strings.HasSuffix(name, suffix) {
			return strings.TrimSuffix(name, suffix)
		}
	}
	return name
}

// collectRawHTTPPaths statically resolves the request paths reached through the
// hand-rolled httpclient (GetJSON/PostJSON/...) across the services tree. It
// resolves string literals, fmt.Sprintf templates, single-return path helpers,
// local path variables, and substitutes function parameters whose call sites
// pass string literals (e.g. the shared PR transition("merge"|"decline") helper).
func collectRawHTTPPaths(root string) (map[methodPath]struct{}, error) {
	fileSet := token.NewFileSet()
	var funcs []*ast.FuncDecl

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		node, err := parser.ParseFile(fileSet, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range node.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				funcs = append(funcs, fn)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Pass 1: literal string arguments passed to each named function, by position.
	paramLiterals := map[string]map[int][]string{}
	for _, fn := range funcs {
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := callName(call.Fun)
			if name == "" {
				return true
			}
			for i, arg := range call.Args {
				if lit, ok := stringLiteral(arg); ok {
					if paramLiterals[name] == nil {
						paramLiterals[name] = map[int][]string{}
					}
					if !contains(paramLiterals[name][i], lit) {
						paramLiterals[name][i] = append(paramLiterals[name][i], lit)
					}
				}
			}
			return true
		})
	}

	// Pass 2: single-return path helpers (functions whose body returns a /rest path).
	helpers := map[string]string{}
	for _, fn := range funcs {
		if fn.Recv != nil {
			continue
		}
		tmpl, ok := helperReturnTemplate(fn)
		if ok {
			helpers[fn.Name.Name] = tmpl
		}
	}

	resolver := &pathResolver{helpers: helpers, paramLiterals: paramLiterals}
	covered := map[methodPath]struct{}{}

	// Pass 3: resolve each *JSON call within each function.
	for _, fn := range funcs {
		params := paramIndex(fn)
		locals := resolver.collectLocalPathVars(fn)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			method, ok := jsonHelperMethod[sel.Sel.Name]
			if !ok || len(call.Args) < 2 {
				return true
			}
			for _, raw := range resolver.resolvePathRaw(call.Args[1], fn.Name.Name, params, locals) {
				norm := normalizeAPIPath(raw)
				if strings.HasPrefix(norm, "/") {
					covered[methodPath{Method: method, Path: norm}] = struct{}{}
				}
			}
			return true
		})
	}

	return covered, nil
}

type pathResolver struct {
	helpers       map[string]string
	paramLiterals map[string]map[int][]string
}

// collectLocalPathVars maps local identifiers assigned a path-like value within a
// function to their raw template(s).
func (r *pathResolver) collectLocalPathVars(fn *ast.FuncDecl) map[string][]string {
	locals := map[string][]string{}
	params := paramIndex(fn)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
			return true
		}
		id, ok := assign.Lhs[0].(*ast.Ident)
		if !ok {
			return true
		}
		for _, tmpl := range r.resolvePathRaw(assign.Rhs[0], fn.Name.Name, params, nil) {
			if strings.Contains(tmpl, "/") && !contains(locals[id.Name], tmpl) {
				locals[id.Name] = append(locals[id.Name], tmpl)
			}
		}
		return true
	})
	return locals
}

// resolvePathRaw returns the un-normalized path template(s) an expression can
// evaluate to. Unresolvable dynamic segments are left as "{}".
func (r *pathResolver) resolvePathRaw(expr ast.Expr, funcName string, params map[string]int, locals map[string][]string) []string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if lit, ok := unquote(e); ok && strings.Contains(lit, "/") {
			return []string{lit}
		}
	case *ast.Ident:
		if locals != nil {
			if tmpls, ok := locals[e.Name]; ok {
				return tmpls
			}
		}
	case *ast.CallExpr:
		name := callName(e.Fun)
		if tmpl, ok := r.helpers[name]; ok {
			return []string{tmpl}
		}
		if isSelector(e.Fun, "fmt", "Sprintf") && len(e.Args) >= 1 {
			format, ok := stringLiteral(e.Args[0])
			if !ok {
				return nil
			}
			options := make([][]string, 0, len(e.Args)-1)
			for _, arg := range e.Args[1:] {
				options = append(options, r.resolveArgRaw(arg, funcName, params, locals))
			}
			results := []string{}
			for _, combo := range cartesian(options) {
				results = append(results, substituteVerbs(format, combo))
			}
			return results
		}
	}
	return nil
}

// resolveArgRaw resolves a single fmt.Sprintf argument into its candidate
// replacement string(s). Non-path dynamic values collapse to "{}".
func (r *pathResolver) resolveArgRaw(expr ast.Expr, funcName string, params map[string]int, locals map[string][]string) []string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		if lit, ok := unquote(e); ok {
			return []string{lit}
		}
	case *ast.Ident:
		if idx, ok := params[e.Name]; ok {
			if lits := r.paramLiterals[funcName][idx]; len(lits) > 0 {
				return lits
			}
		}
		if locals != nil {
			if tmpls, ok := locals[e.Name]; ok {
				return tmpls
			}
		}
	case *ast.CallExpr:
		if tmpl, ok := r.helpers[callName(e.Fun)]; ok {
			return []string{tmpl}
		}
	}
	return []string{"{}"}
}

// helperReturnTemplate detects functions of the form `return "<path>"` or
// `return fmt.Sprintf("<path>", ...)` whose path targets the REST API.
func helperReturnTemplate(fn *ast.FuncDecl) (string, bool) {
	if fn.Body == nil || len(fn.Body.List) == 0 {
		return "", false
	}
	ret, ok := fn.Body.List[len(fn.Body.List)-1].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return "", false
	}
	resolver := &pathResolver{}
	for _, tmpl := range resolver.resolvePathRaw(ret.Results[0], fn.Name.Name, nil, nil) {
		if strings.Contains(tmpl, "/rest") {
			return tmpl, true
		}
	}
	return "", false
}

// computeSpecCoverage joins the spec against operations reachable through the
// generated client (restricted to the used set) and the raw httpclient.
func computeSpecCoverage(specPath, generatedPath, servicesRoot string) (specCoverageReport, error) {
	specOps, err := loadSpecOperations(specPath)
	if err != nil {
		return specCoverageReport{}, fmt.Errorf("load spec operations: %w", err)
	}
	generatedPaths, err := parseGeneratedOperationPaths(generatedPath)
	if err != nil {
		return specCoverageReport{}, fmt.Errorf("parse generated client: %w", err)
	}
	usedOps, err := discoverUsedGeneratedOperations(servicesRoot)
	if err != nil {
		return specCoverageReport{}, fmt.Errorf("discover used operations: %w", err)
	}

	covered := map[methodPath]struct{}{}
	for _, op := range usedOps {
		if mp, ok := generatedPaths[usedOperationToName(op)]; ok {
			covered[mp] = struct{}{}
		}
	}
	rawPaths, err := collectRawHTTPPaths(servicesRoot)
	if err != nil {
		return specCoverageReport{}, fmt.Errorf("collect raw http paths: %w", err)
	}
	for mp := range rawPaths {
		covered[mp] = struct{}{}
	}

	tagOrder := []string{}
	tagStats := map[string]*specTagCoverage{}
	gaps := []specGap{}
	coveredCount := 0
	for _, op := range specOps {
		stat, ok := tagStats[op.Tag]
		if !ok {
			stat = &specTagCoverage{Tag: op.Tag}
			tagStats[op.Tag] = stat
			tagOrder = append(tagOrder, op.Tag)
		}
		stat.Total++
		if _, isCovered := covered[methodPath{Method: op.Method, Path: op.NormPath}]; isCovered {
			stat.Covered++
			coveredCount++
			continue
		}
		gaps = append(gaps, specGap{Method: op.Method, Path: op.Path, Tag: op.Tag, Summary: op.Summary})
	}

	sort.Strings(tagOrder)
	byTag := make([]specTagCoverage, 0, len(tagOrder))
	for _, tag := range tagOrder {
		byTag = append(byTag, *tagStats[tag])
	}

	return specCoverageReport{
		Version:         1,
		Spec:            filepath.ToSlash(specPath),
		Covered:         coveredCount,
		Total:           len(specOps),
		CoveragePercent: percent(coveredCount, len(specOps)),
		ByTag:           byTag,
		Gaps:            gaps,
	}, nil
}

// --- small AST helpers ---

func isSelector(expr ast.Expr, pkg, sel string) bool {
	s, ok := expr.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != sel {
		return false
	}
	id, ok := s.X.(*ast.Ident)
	return ok && id.Name == pkg
}

func callName(fun ast.Expr) string {
	switch f := fun.(type) {
	case *ast.Ident:
		return f.Name
	case *ast.SelectorExpr:
		return f.Sel.Name
	}
	return ""
}

func stringLiteral(expr ast.Expr) (string, bool) {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	return unquote(lit)
}

func unquote(lit *ast.BasicLit) (string, bool) {
	if lit.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

// stringFromExpr resolves a string literal or fmt.Sprintf format literal.
func stringFromExpr(expr ast.Expr) (string, bool) {
	if lit, ok := stringLiteral(expr); ok {
		return lit, true
	}
	if call, ok := expr.(*ast.CallExpr); ok && isSelector(call.Fun, "fmt", "Sprintf") && len(call.Args) >= 1 {
		return stringLiteral(call.Args[0])
	}
	return "", false
}

func paramIndex(fn *ast.FuncDecl) map[string]int {
	index := map[string]int{}
	if fn.Type == nil || fn.Type.Params == nil {
		return index
	}
	pos := 0
	for _, field := range fn.Type.Params.List {
		if len(field.Names) == 0 {
			pos++
			continue
		}
		for _, name := range field.Names {
			index[name.Name] = pos
			pos++
		}
	}
	return index
}

// substituteVerbs replaces the i-th format verb in format with reps[i]. Surplus
// verbs (no replacement supplied) are left intact for later normalization.
func substituteVerbs(format string, reps []string) string {
	locs := formatVerbRe.FindAllStringIndex(format, -1)
	var b strings.Builder
	last := 0
	for i, loc := range locs {
		b.WriteString(format[last:loc[0]])
		if i < len(reps) {
			b.WriteString(reps[i])
		} else {
			b.WriteString(format[loc[0]:loc[1]])
		}
		last = loc[1]
	}
	b.WriteString(format[last:])
	return b.String()
}

func cartesian(options [][]string) [][]string {
	result := [][]string{{}}
	for _, opts := range options {
		if len(opts) == 0 {
			opts = []string{"{}"}
		}
		next := make([][]string, 0, len(result)*len(opts))
		for _, prefix := range result {
			for _, opt := range opts {
				combo := append(append([]string{}, prefix...), opt)
				next = append(next, combo)
			}
		}
		result = next
	}
	return result
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
