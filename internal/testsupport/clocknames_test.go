package testsupport_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// nameSinks are the calls that turn a value into text a test would use as a
// name.
var nameSinks = map[string]bool{
	"fmt.Sprintf":        true,
	"fmt.Sprint":         true,
	"fmt.Sprintln":       true,
	"strconv.Itoa":       true,
	"strconv.FormatInt":  true,
	"strconv.FormatUint": true,
	// Assembling a name rather than formatting one. A review found all three
	// passing: a timestamp joined into a slice, substituted into a template,
	// or appended to a builder is a name the same way a Sprintf is.
	"strings.Join":       true,
	"strings.ReplaceAll": true,
	"strings.Replace":    true,
	"filepath.Join":      true,
	"path.Join":          true,
}

// methodSinks are sinks reached through a value rather than a package, so the
// receiver name is whatever the test called it.
var methodSinks = map[string]bool{"WriteString": true}

// clockReadings are the methods that read a number or a string off a time.
var clockReadings = map[string]bool{
	"Unix": true, "UnixMilli": true, "UnixMicro": true, "UnixNano": true,
	"Format": true, "String": true, "Nanosecond": true, "Second": true,
}

// clockTransforms keep a time a time.
var clockTransforms = map[string]bool{
	"Add": true, "UTC": true, "Local": true, "Truncate": true, "Round": true, "In": true,
}

// timings measure against the clock rather than read it, and their result is a
// duration, not a moment.
var timings = map[string]bool{"time.Since": true, "time.Until": true}

// notAName exempts a clock value that is not a name. It has to say why.
var notAName = regexp.MustCompile(`clock-value-not-a-name:\s*\S+`)

// TestNoFixtureIsNamedFromTheClock is ADR-085.
//
// Clock-derived names collided three ways, each found only after the failure
// was misread as a product bug. The first version of this test was two
// regular expressions, and a review found them passing a Sprintf whose earlier
// argument had a parenthesis in it, a concatenation, strconv, Format, and a
// timestamp stored in a variable first. So it reads the code: any string built
// from time.Now() -- through fmt.Sprint*, strconv, or +, directly or through a
// local variable -- fails, unless a comment says why it is not a name.
func TestNoFixtureIsNamedFromTheClock(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	var offenders []string
	var scanned int

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			// Dotted directories hold agent worktrees with whole other branches
			// of this repository in them (ADR-065).
			if path != root && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}

		contents, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++

		found, parseErr := clockNames(filepath.ToSlash(path), string(contents))
		if parseErr != nil {
			return parseErr
		}
		offenders = append(offenders, found...)

		return nil
	})
	if err != nil {
		t.Fatalf("walk the repository: %v", err)
	}

	// A walk that stopped finding test files would report perfect compliance,
	// which is the failure mode ADR-067 exists to catch. The detector itself is
	// held to the shapes it claims by TestTheClockNameDetectorCatchesWhatItClaims.
	if scanned < 50 {
		t.Fatalf("scanned only %d test files, expected dozens.\nThe walk is probably broken, not the suite.", scanned)
	}

	if len(offenders) > 0 {
		sort.Strings(offenders)
		t.Fatalf(
			"%d name(s) built from the clock:\n  %s\n\n"+
				"ADR-085: use testsupport.UniqueSuffix or testsupport.UniqueName. A timestamp\n"+
				"repeats when it is truncated, when the clock is coarse, and across runs when a\n"+
				"counter is what makes it unique. A clock value that is not a name says so with a\n"+
				"clock-value-not-a-name: comment giving the reason.",
			len(offenders), strings.Join(offenders, "\n  "),
		)
	}
}

// TestTheClockNameDetectorCatchesWhatItClaims holds the detector to the shapes
// ADR-085 says it fails, and to the clock use it must leave alone.
func TestTheClockNameDetectorCatchesWhatItClaims(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		body   string
		caught bool
	}{
		"Sprintf of a truncated clock":             {body: `_ = fmt.Sprintf("LT%d", time.Now().UnixNano()%100000)`, caught: true},
		"Sprintf with a parenthesis before":        {body: `_ = fmt.Sprintf("%s-%d", t.Name(), time.Now().UnixNano())`, caught: true},
		"strconv inside a concatenation":           {body: `_ = "LT" + strconv.FormatInt(time.Now().UnixNano(), 36)`, caught: true},
		"a formatted time concatenated":            {body: `_ = "x-" + time.Now().Format("20060102150405")`, caught: true},
		"a reading stored in a variable first":     {body: "stamp := time.Now().UnixNano()\n_ = fmt.Sprint(\"x-\", stamp)", caught: true},
		"a time stored and read later":             {body: "now := time.Now()\n_ = fmt.Sprintf(\"x-%d\", now.Unix())", caught: true},
		"Sprintln of a clock":                      {body: `_ = fmt.Sprintln("run", time.Now().UnixMilli())`, caught: true},
		"a clock joined into a name":               {body: `_ = strings.Join([]string{"LT", time.Now().Format("150405")}, "-")`, caught: true},
		"a clock substituted into a template":      {body: `_ = strings.ReplaceAll("LT-{stamp}", "{stamp}", time.Now().Format("150405"))`, caught: true},
		"a clock appended to a builder":            {body: "var builder strings.Builder\nbuilder.WriteString(time.Now().Format(\"150405\"))", caught: true},
		"a path built from the clock":              {body: `_ = filepath.Join(t.TempDir(), time.Now().Format("150405"))`, caught: true},
		"a duration measured against the clock":    {body: "start := time.Now()\n_ = fmt.Sprintf(\"elapsed %s\", time.Since(start))"},
		"a deadline that is never text":            {body: "deadline := time.Now().Add(time.Minute)\n_ = deadline"},
		"a random name":                            {body: `_ = testsupport.UniqueName("LT")`},
		"a clock value that says it is not a name": {body: "// clock-value-not-a-name: a query window, not a fixture\n_ = fmt.Sprintf(\"since=%d\", time.Now().Add(-time.Hour).UnixMilli())"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			source := "package example\n\nfunc probe(t *testing.T) {\n" + testCase.body + "\n}\n"
			found, err := clockNames("probe_test.go", source)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if caught := len(found) > 0; caught != testCase.caught {
				t.Fatalf("caught = %v, want %v; findings: %v", caught, testCase.caught, found)
			}
		})
	}
}

// clockNames reports each place in a Go source file that builds a string from
// the clock.
func clockNames(path, source string) ([]string, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, path, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}

	exempt := map[int]bool{}
	for _, group := range file.Comments {
		if notAName.MatchString(group.Text()) {
			line := files.Position(group.End()).Line
			exempt[line] = true
			exempt[line+1] = true
		}
	}

	lines := strings.Split(source, "\n")
	seen := map[token.Pos]bool{}
	var found []string

	record := func(position token.Pos) {
		if seen[position] {
			return
		}
		seen[position] = true

		line := files.Position(position).Line
		if exempt[line] {
			return
		}
		text := ""
		if line-1 < len(lines) {
			text = strings.TrimSpace(lines[line-1])
		}
		found = append(found, path+":"+strconv.Itoa(line)+": "+text)
	}

	ast.Inspect(file, func(node ast.Node) bool {
		var body *ast.BlockStmt
		switch function := node.(type) {
		case *ast.FuncDecl:
			body = function.Body
		case *ast.FuncLit:
			body = function.Body
		}
		if body == nil {
			return true
		}

		clock := clockVariables(body)

		ast.Inspect(body, func(inner ast.Node) bool {
			switch expression := inner.(type) {
			case *ast.CallExpr:
				if nameSinks[callName(expression)] || isMethodSink(expression) {
					for _, argument := range expression.Args {
						if readsClock(argument, clock) {
							record(expression.Pos())
							break
						}
					}
				}
			case *ast.BinaryExpr:
				if expression.Op == token.ADD && (isString(expression.X) || isString(expression.Y)) &&
					(readsClock(expression.X, clock) || readsClock(expression.Y, clock)) {
					record(expression.Pos())
				}
			}

			return true
		})

		// The body's nested function literals were inspected with it.
		return false
	})

	return found, nil
}

// clockVariables are the local names a function assigns from the clock.
func clockVariables(body *ast.BlockStmt) map[string]bool {
	clock := map[string]bool{}

	// Twice, so a variable assigned from another clock variable is caught
	// whichever order the two assignments are visited in.
	for pass := 0; pass < 2; pass++ {
		ast.Inspect(body, func(node ast.Node) bool {
			switch statement := node.(type) {
			case *ast.AssignStmt:
				for index, value := range statement.Rhs {
					if index < len(statement.Lhs) && isClock(value, clock) {
						if name, ok := statement.Lhs[index].(*ast.Ident); ok {
							clock[name.Name] = true
						}
					}
				}
			case *ast.ValueSpec:
				for index, value := range statement.Values {
					if index < len(statement.Names) && isClock(value, clock) {
						clock[statement.Names[index].Name] = true
					}
				}
			}

			return true
		})
	}

	return clock
}

// isClock reports whether an expression is the clock: time.Now(), a time
// derived from it, a reading taken from one, or a variable holding any of
// those.
func isClock(expression ast.Expr, clock map[string]bool) bool {
	switch typed := expression.(type) {
	case *ast.Ident:
		return clock[typed.Name]
	case *ast.ParenExpr:
		return isClock(typed.X, clock)
	case *ast.BinaryExpr:
		return isClock(typed.X, clock) || isClock(typed.Y, clock)
	case *ast.CallExpr:
		if callName(typed) == "time.Now" {
			return true
		}
		selector, ok := typed.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		if clockReadings[selector.Sel.Name] || clockTransforms[selector.Sel.Name] {
			return isClock(selector.X, clock)
		}
	}

	return false
}

// readsClock reports whether an expression reads the clock anywhere inside it,
// other than through a timing, whose result is a duration.
func readsClock(expression ast.Expr, clock map[string]bool) bool {
	reads := false
	ast.Inspect(expression, func(node ast.Node) bool {
		if reads {
			return false
		}
		if call, ok := node.(*ast.CallExpr); ok && timings[callName(call)] {
			return false
		}
		if value, ok := node.(ast.Expr); ok && isClock(value, clock) {
			reads = true
			return false
		}

		return true
	})

	return reads
}

func callName(call *ast.CallExpr) string {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok {
		return ""
	}

	return pkg.Name + "." + selector.Sel.Name
}

func isString(expression ast.Expr) bool {
	literal, ok := expression.(*ast.BasicLit)

	return ok && literal.Kind == token.STRING
}

// isMethodSink reports a sink called on a value: builder.WriteString, whose
// receiver is named by the test rather than by a package.
func isMethodSink(call *ast.CallExpr) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)

	return ok && methodSinks[selector.Sel.Name]
}
