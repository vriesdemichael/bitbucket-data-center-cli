package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// restPath matches a Bitbucket REST route written into a mock. Serving one is
// the clearest statement that the mock is standing in for the server rather
// than for the network.
var restPath = regexp.MustCompile(`"(?:/rest)?/(?:api|git|branch-utils|build-status|ssh|keys|audit|jira|mirroring|default-reviewers|insights)/|"/(?:projects|repos|users|admin)/|/pull-requests`)

// bitbucketField matches a field name from a Bitbucket entity appearing in a
// literal the mock writes back. A body shaped like a Bitbucket object is a
// claim about the response, whatever route it was served on.
var bitbucketField = regexp.MustCompile(`"(values|isLastPage|errors|exceptionName|displayId|latestCommit|slug|projectKey|reviewers|fromRef|toRef|scmId|permission|links|clone|self)"\s*:`)

// scan walks a directory and classifies every mocked server it finds.
func scan(root string) ([]entry, error) {
	entries := []entry{}
	fileSet := token.NewFileSet()

	err := filepath.WalkDir(root, func(path string, item fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// The live suite is the destination, not the subject.
		if item.IsDir() {
			if item.Name() == "testdata" || strings.Contains(filepath.ToSlash(path), "tests/integration") {
				return filepath.SkipDir
			}

			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}

		source, err := readFile(path)
		if err != nil {
			return err
		}
		if !strings.Contains(source, "httptest.NewServer") && !strings.Contains(source, "httptest.NewTLSServer") {
			return nil
		}

		parsed, err := parser.ParseFile(fileSet, path, source, 0)
		if err != nil {
			return err
		}

		for _, declaration := range parsed.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}

			sites := mockServerCalls(function.Body)
			ownHandler := definesHTTPHandler(function.Body)
			suppliesHandler := len(sites) == 0 && ownHandler
			if len(sites) == 0 && !suppliesHandler {
				continue
			}

			// The whole enclosing function is the evidence: the handler, the
			// bodies it writes and the assertions that read them are what say
			// whether the mock stands in for Bitbucket.
			body := sourceRange(source, fileSet, function.Body.Pos(), function.Body.End())
			signals := signalsIn(body)
			if len(sites) > 0 && !ownHandler {
				signals = append(signals, "handler-supplied-by-the-caller")
			}
			if handlerFailsTheTest(function.Body) {
				signals = append(signals, "fails-if-the-server-is-reached")
			}
			if handlerInspectsPath(function.Body) {
				signals = append(signals, "inspects-the-request-path")
			}
			if answersAnyRequest(function.Body) {
				signals = append(signals, "answers-any-request")
				sort.Strings(signals)
			}
			class := classify(signals)
			if externalService(path) {
				class = ClassExternalService
			}

			// A function that hands a handler to a helper is one mock, recorded
			// where the handler is written rather than where the listener is
			// opened.
			if suppliesHandler {
				signals = append(signals, "handler-passed-to-a-helper")
				sort.Strings(signals)
				sites = []token.Pos{function.Pos()}
			}

			for _, site := range sites {
				entries = append(entries, entry{
					File:     filepath.ToSlash(path),
					Line:     fileSet.Position(site).Line,
					Function: function.Name.Name,
					Class:    class,
					Signals:  signals,
				})
			}
		}

		return nil
	})

	return entries, err
}

// mockServerCalls returns the position of every httptest server construction in
// a function body.
func mockServerCalls(body *ast.BlockStmt) []token.Pos {
	positions := []token.Pos{}

	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		receiver, ok := selector.X.(*ast.Ident)
		if !ok || receiver.Name != "httptest" {
			return true
		}
		if selector.Sel.Name == "NewServer" || selector.Sel.Name == "NewTLSServer" {
			positions = append(positions, call.Pos())
		}

		return true
	})

	return positions
}

// externalService reports whether a package talks to something other than
// Bitbucket.
//
// These mocks carry the same risk in principle -- GitHub and Sigstore can drift
// too -- but they are outside the Bitbucket policy and are counted apart rather
// than waved through, so the number is visible when someone decides what to do
// about them.
func externalService(path string) bool {
	normalised := filepath.ToSlash(path)
	for _, pkg := range []string{
		"internal/transport/githubrelease/",
		"internal/transport/sigstore/",
	} {
		if strings.Contains(normalised, pkg) {
			return true
		}
	}

	return false
}

// handlerFailsTheTest reports whether a handler's own body fails the test.
//
// Checked inside the handler literal rather than by searching the function for
// a phrase: the test around it is full of t.Fatalf calls, and the wording of
// the message is nobody's contract.
func handlerFailsTheTest(body *ast.BlockStmt) bool {
	// Every handler must be a pure failure, not just one of them. A file whose
	// other mocks serve real Bitbucket payloads is not asserting that nothing
	// is sent, whatever one trivial handler beside them does.
	return everyHandler(body, func(handler *ast.FuncLit) bool {
		failed := false
		responds := false

		ast.Inspect(handler.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			// The failure has to come from the test, not from net/http:
			// http.Error writes a response and would otherwise read as one.
			receiver, isIdent := selector.X.(*ast.Ident)
			switch {
			case isIdent && receiver.Name == "t" &&
				(strings.HasPrefix(selector.Sel.Name, "Error") || strings.HasPrefix(selector.Sel.Name, "Fatal")):
				failed = true
			case selector.Sel.Name == "Write", selector.Sel.Name == "WriteHeader",
				selector.Sel.Name == "Encode", selector.Sel.Name == "Fprint",
				selector.Sel.Name == "Fprintf", selector.Sel.Name == "NotFound",
				selector.Sel.Name == "Error":
				responds = true
			}

			return true
		})

		// Only a handler that does nothing but fail is asserting that no
		// request happens. One that also serves a response is an ordinary mock
		// with a safety net on its default branch, and everything it serves is
		// still a claim about Bitbucket.
		return failed && !responds
	})
}

// handlerInspectsPath reports whether a handler reads the request path. Routes
// are often matched against a table rather than a literal, so the path may
// never appear in the handler as a string.
func handlerInspectsPath(body *ast.BlockStmt) bool {
	return anyHandler(body, func(handler *ast.FuncLit) bool {
		found := false
		ast.Inspect(handler.Body, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if selector.Sel.Name == "Path" || selector.Sel.Name == "RequestURI" {
				found = true
			}

			return !found
		})

		return found
	})
}

// everyHandler runs a predicate over all http handler literals in a function
// and reports whether they all satisfy it. A function with no handler of its
// own satisfies nothing.
func everyHandler(body *ast.BlockStmt, predicate func(*ast.FuncLit) bool) bool {
	handlers := 0
	matched := 0

	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.FuncLit)
		if !ok || !isHTTPHandler(literal.Type) {
			return true
		}
		handlers++
		if predicate(literal) {
			matched++
		}

		return true
	})

	return handlers > 0 && handlers == matched
}

// anyHandler runs a predicate over every http handler literal in a function.
func anyHandler(body *ast.BlockStmt, predicate func(*ast.FuncLit) bool) bool {
	matched := false

	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.FuncLit)
		if !ok || !isHTTPHandler(literal.Type) {
			return true
		}
		if predicate(literal) {
			matched = true
		}

		return !matched
	})

	return matched
}

// definesHTTPHandler reports whether a function writes an http handler without
// opening a listener itself, which is how a test supplies its mock to a shared
// constructor.
func definesHTTPHandler(body *ast.BlockStmt) bool {
	found := false

	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.FuncLit)
		if ok && isHTTPHandler(literal.Type) {
			found = true
		}

		return !found
	})

	return found
}

// answersAnyRequest reports whether every handler in a function replies the
// same way regardless of the request.
//
// A handler that never looks at the path or the method accepts anything and
// answers success. The test then exercises a command without any of it being
// checked: the request could be sent to the wrong route, with the wrong verb,
// carrying the wrong body, and the mock would still say yes. It produces
// coverage and verifies nothing, which is worse than no test, because the line
// count says the path is guarded.
func answersAnyRequest(body *ast.BlockStmt) bool {
	handlers := 0
	blind := 0

	ast.Inspect(body, func(node ast.Node) bool {
		literal, ok := node.(*ast.FuncLit)
		if !ok || !isHTTPHandler(literal.Type) {
			return true
		}
		handlers++

		routed := false
		ast.Inspect(literal.Body, func(inner ast.Node) bool {
			selector, ok := inner.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch selector.Sel.Name {
			case "Path", "Method", "RequestURI", "URL", "Query":
				routed = true
			}

			return !routed
		})
		if !routed {
			blind++
		}

		return true
	})

	return handlers > 0 && blind == handlers
}

// isHTTPHandler reports whether a function literal has the (ResponseWriter,
// *Request) shape.
func isHTTPHandler(signature *ast.FuncType) bool {
	if signature.Params == nil || len(signature.Params.List) != 2 {
		return false
	}
	rendered := func(expr ast.Expr) string {
		selector, ok := expr.(*ast.SelectorExpr)
		if ok {
			return selector.Sel.Name
		}
		if star, ok := expr.(*ast.StarExpr); ok {
			if inner, ok := star.X.(*ast.SelectorExpr); ok {
				return inner.Sel.Name
			}
		}

		return ""
	}

	return rendered(signature.Params.List[0].Type) == "ResponseWriter" &&
		rendered(signature.Params.List[1].Type) == "Request"
}

// signalsIn reports what a function's mock does. The names are the vocabulary
// the inventory is read in, so they say what is being assumed rather than which
// Go call was spotted.
func signalsIn(body string) []string {
	found := map[string]bool{}

	if restPath.MatchString(body) {
		found["serves-bitbucket-routes"] = true
	}
	if bitbucketField.MatchString(body) {
		found["returns-bitbucket-entity"] = true
	}
	if strings.Contains(body, "r.Body") || strings.Contains(body, "request.Body") ||
		strings.Contains(body, "req.Body") {
		found["asserts-request-body"] = true
	}
	if strings.Contains(body, "URL.Query()") {
		found["asserts-query-parameters"] = true
	}
	if strings.Contains(body, "r.Method") || strings.Contains(body, "request.Method") {
		found["asserts-http-method"] = true
	}
	if strings.Contains(body, "WriteHeader(") {
		found["returns-a-status"] = true
	}
	if strings.Contains(body, "server.Close()") && strings.Contains(body, "defer") {
		// Ordinary cleanup, not fault injection; recorded only so a closed
		// listener used deliberately can be told apart below.
		found["closes-on-cleanup"] = true
	}
	if strings.Contains(body, "invalid-json") || strings.Contains(body, "{invalid") ||
		strings.Contains(body, "not json") || strings.Contains(body, "unexpected end") {
		found["returns-malformed-body"] = true
	}
	if strings.Contains(body, "hijack") || strings.Contains(body, "Hijack") ||
		strings.Contains(body, "time.Sleep") || strings.Contains(body, "context.WithTimeout") {
		found["simulates-a-stalled-connection"] = true
	}

	names := make([]string, 0, len(found))
	for name := range found {
		names = append(names, name)
	}
	sort.Strings(names)

	return names
}

// classify turns the signals into where the test belongs.
//
// The order matters. Anything claiming to be Bitbucket is behaviour whatever
// else it also does, because that claim is the one that cannot be checked from
// inside a unit test.
func classify(signals []string) Class {
	has := func(name string) bool {
		for _, signal := range signals {
			if signal == name {
				return true
			}
		}

		return false
	}

	switch {
	// The handler is there to prove the code never calls out: it fails the test
	// if it is reached. Nothing about Bitbucket is assumed, because nothing is
	// expected to be sent. This is what a unit test of input handling should
	// look like.
	case has("fails-if-the-server-is-reached"):
		return ClassUnreachedGuard

	// Checking something about the request is what makes a mock a stand-in for
	// Bitbucket: the assertion rests on the route, the verb or the payload
	// being what the real server wants, and a unit test cannot tell whether it
	// is -- the mock and the code agree by construction.
	case has("serves-bitbucket-routes"), has("asserts-request-body"),
		has("asserts-query-parameters"), has("asserts-http-method"),
		has("inspects-the-request-path"):
		return ClassBehaviour

	// Injected below the API. A real server will not truncate a body or stall a
	// connection on request, and the subject is our client either way.
	case has("returns-malformed-body"), has("simulates-a-stalled-connection"):
		return ClassTransportFault

	// A bare status with no Bitbucket payload. Not routing is expected here --
	// the point is the code, not the route -- but "Bitbucket answers this code
	// in this situation" is still a claim about the server, so it needs a live
	// test proving the server really does before it can stay.
	case has("returns-a-status") && !has("returns-bitbucket-entity"):
		return ClassStatusTaxonomy

	// Opens a listener around a handler it was given. It states nothing
	// itself; whatever is assumed is assumed by the caller, which is indexed
	// separately.
	case has("handler-supplied-by-the-caller"):
		return ClassHarnessConstructor

	// Answers everything identically. The request could be sent to the wrong
	// route, with the wrong verb, carrying the wrong body, and this would still
	// say yes.
	case has("answers-any-request"):
		return ClassCannedResponse

	// A Bitbucket-shaped body is a claim about the response even where nothing
	// about the request was checked.
	case has("returns-bitbucket-entity"):
		return ClassBehaviour

	default:
		return ClassUnclear
	}

}

func sourceRange(source string, fileSet *token.FileSet, from, to token.Pos) string {
	start := fileSet.Position(from).Offset
	end := fileSet.Position(to).Offset
	if start < 0 || end > len(source) || start >= end {
		return ""
	}

	return source[start:end]
}
