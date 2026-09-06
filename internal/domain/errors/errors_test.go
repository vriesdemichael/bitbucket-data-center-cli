package errors

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"sync"
	"testing"
)

func TestExitCodeByKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected int
	}{
		{name: "validation", err: New(KindValidation, "bad input", nil), expected: 2},
		{name: "auth", err: New(KindAuthentication, "no token", nil), expected: 3},
		{name: "not found", err: New(KindNotFound, "missing", nil), expected: 4},
		{name: "conflict", err: New(KindConflict, "exists", nil), expected: 5},
		{name: "transient", err: New(KindTransient, "timeout", nil), expected: 10},
		{name: "not implemented", err: New(KindNotImplemented, "todo", nil), expected: 11},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if actual := ExitCode(test.err); actual != test.expected {
				t.Fatalf("expected %d, got %d", test.expected, actual)
			}
		})
	}
}

func TestErrorFormattingAndUnwrap(t *testing.T) {
	t.Parallel()

	withoutCause := New(KindValidation, "bad input", nil)
	if !strings.Contains(withoutCause.Error(), "validation: bad input") {
		t.Fatalf("unexpected error string without cause: %q", withoutCause.Error())
	}

	cause := errors.New("boom")
	withCause := New(KindTransient, "request failed", cause)
	if !strings.Contains(withCause.Error(), "transient: request failed") || !strings.Contains(withCause.Error(), "boom") {
		t.Fatalf("unexpected error string with cause: %q", withCause.Error())
	}
	if !errors.Is(withCause, cause) {
		t.Fatal("expected unwrap to expose cause")
	}
}

func TestExitCodeDefaults(t *testing.T) {
	t.Parallel()

	if ExitCode(nil) != 0 {
		t.Fatal("expected nil error exit code 0")
	}

	if ExitCode(errors.New("plain")) != 1 {
		t.Fatal("expected plain error exit code 1")
	}

	if ExitCode(New(KindInternal, "internal", nil)) != 1 {
		t.Fatal("expected internal app error exit code 1")
	}
	if ExitCode(New(KindPermanent, "permanent", nil)) != 1 {
		t.Fatal("expected permanent app error exit code 1")
	}
	if ExitCode(New(KindAuthorization, "forbidden", nil)) != 3 {
		t.Fatal("expected authorization exit code 3")
	}
}

func TestKindOf(t *testing.T) {
	t.Parallel()

	if got := KindOf(nil); got != "" {
		t.Fatalf("expected empty kind for nil error, got %q", got)
	}

	if got := KindOf(New(KindValidation, "bad", nil)); got != KindValidation {
		t.Fatalf("expected validation kind, got %q", got)
	}

	if got := KindOf(errors.New("plain")); got != KindInternal {
		t.Fatalf("expected internal kind for plain error, got %q", got)
	}
}

func TestKindsCoversEveryDeclaredKind(t *testing.T) {
	t.Parallel()

	kinds := Kinds()

	seen := map[Kind]bool{}
	for _, kind := range kinds {
		if kind == "" {
			t.Fatal("Kinds() contains an empty kind")
		}
		if seen[kind] {
			t.Fatalf("Kinds() lists %q twice", kind)
		}
		seen[kind] = true
	}

	// Every kind must map to an exit code the contract documents. A kind added
	// without a case in ExitCode silently becomes exit 1.
	for _, kind := range kinds {
		if code := ExitCode(New(kind, "boom", nil)); code < 1 {
			t.Fatalf("kind %q maps to invalid exit code %d", kind, code)
		}
	}

	// Read out of the source rather than restated here. A hand-written copy
	// of the list cannot detect a kind missing from Kinds(), because the same
	// omission is made twice: KindCancelled was added to the taxonomy, left
	// out of this list, and deleting it from Kinds() failed nothing -- while
	// the published errorKind enum, which derives from Kinds(), silently
	// narrowed. That is the drift this test exists to catch.
	for _, kind := range declaredKindsFromSource(t) {
		if !seen[kind] {
			t.Fatalf("declared kind %q is missing from Kinds()", kind)
		}
	}
}

// declaredKindsFromSource returns every Kind constant declared in errors.go.
func declaredKindsFromSource(t *testing.T) []Kind {
	t.Helper()

	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, "errors.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing errors.go failed: %v", err)
	}

	var declared []Kind

	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			// Only `X Kind = "y"`, which is how every kind is declared.
			if name, ok := value.Type.(*ast.Ident); !ok || name.Name != "Kind" {
				continue
			}
			for _, expression := range value.Values {
				literal, ok := expression.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				declared = append(declared, Kind(strings.Trim(literal.Value, `"`)))
			}
		}
	}

	if len(declared) == 0 {
		t.Fatal("no kind constants found; the scanner has stopped matching how they are declared")
	}

	return declared
}

// TestTheKindScannerSeesEveryKind is the sabotage, kept as a test (ADR-067).
//
// The scanner is only worth having if it would notice. It reads the real
// errors.go, so a change to how kinds are declared must fail here rather than
// quietly reducing the guard to nothing.
func TestTheKindScannerSeesEveryKind(t *testing.T) {
	t.Parallel()

	found := make(map[Kind]bool)
	for _, kind := range declaredKindsFromSource(t) {
		found[kind] = true
	}

	if len(found) != len(Kinds()) {
		t.Errorf("the scanner found %d kinds, Kinds() returns %d", len(found), len(Kinds()))
	}
	for _, kind := range Kinds() {
		if !found[kind] {
			t.Errorf("the scanner missed %q", kind)
		}
	}
	if found["not_a_kind"] {
		t.Error("the scanner invented a kind")
	}
}

func TestMessageOf(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		err      error
		expected string
	}{
		{name: "nil", err: nil, expected: ""},
		{
			name:     "strips the kind prefix",
			err:      New(KindValidation, "bad input", nil),
			expected: "bad input",
		},
		{
			name:     "keeps the cause",
			err:      New(KindConflict, "already exists", errors.New("409")),
			expected: "already exists (409)",
		},
		{
			name:     "plain errors pass through",
			err:      errors.New("something broke"),
			expected: "something broke",
		},
		{
			name:     "wrapping context is preserved",
			err:      fmt.Errorf("loading config: %w", New(KindNotFound, "missing file", nil)),
			expected: "loading config: not_found: missing file",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := MessageOf(testCase.err); got != testCase.expected {
				t.Fatalf("MessageOf() = %q, want %q", got, testCase.expected)
			}
		})
	}
}

// TestWithDetailDoesNotMutateTheErrorItWasGiven covers the shared-error case.
//
// The helper reads as functional -- it returns an error -- so a caller may
// reasonably hand it something held elsewhere: a sentinel, a cached failure, an
// error another goroutine already has. The first version wrote into that
// error's own map and returned the same pointer, so every holder silently
// acquired the detail.
func TestWithDetailDoesNotMutateTheErrorItWasGiven(t *testing.T) {
	t.Parallel()

	shared := New(KindCancelled, "bulk apply was cancelled", nil)

	first := WithDetail(shared, "operation_id", "op-one")
	second := WithDetail(shared, "operation_id", "op-two")

	if details := DetailsOf(shared); details != nil {
		t.Errorf("the shared error acquired details: %v", details)
	}
	if got := DetailsOf(first)["operation_id"]; got != "op-one" {
		t.Errorf("first copy carries %q, want op-one", got)
	}
	if got := DetailsOf(second)["operation_id"]; got != "op-two" {
		t.Errorf("second copy carries %q, want op-two", got)
	}

	// The copy is a copy in both directions: mutating what DetailsOf returns
	// must not reach back into the error either.
	DetailsOf(first)["operation_id"] = "tampered"
	if got := DetailsOf(first)["operation_id"]; got != "op-one" {
		t.Errorf("DetailsOf handed out the live map: %q", got)
	}
}

// TestWithDetailIsSafeUnderConcurrentUse is the same property stated as the
// failure it would actually produce: concurrent callers writing into one map.
//
// The assertion does not depend on the race detector, which this project does
// not currently run -- the shared error visibly acquiring a detail is enough to
// fail. Under -race the mutating version would additionally be reported as the
// data race it is.
func TestWithDetailIsSafeUnderConcurrentUse(t *testing.T) {
	t.Parallel()

	shared := New(KindConflict, "conflict", nil)

	var waiter sync.WaitGroup
	for index := 0; index < 8; index++ {
		waiter.Add(1)
		go func(index int) {
			defer waiter.Done()
			_ = WithDetail(shared, "operation_id", fmt.Sprintf("op-%d", index))
		}(index)
	}
	waiter.Wait()

	if details := DetailsOf(shared); details != nil {
		t.Errorf("concurrent callers wrote into the shared error: %v", details)
	}
}

// TestWithDetailPreservesEverythingElse guards the copy against dropping a
// field: a copy that loses the cause or the kind would reclassify the failure.
func TestWithDetailPreservesEverythingElse(t *testing.T) {
	t.Parallel()

	cause := errors.New("underlying")
	original := WithDetail(New(KindCancelled, "cancelled", cause), "first", "1")
	extended := WithDetail(original, "second", "2")

	if KindOf(extended) != KindCancelled {
		t.Errorf("kind = %q, want cancelled", KindOf(extended))
	}
	if !errors.Is(extended, cause) {
		t.Error("the copy lost the cause, so errors.Is stopped working")
	}
	if MessageOf(extended) != MessageOf(original) {
		t.Errorf("message = %q, want %q", MessageOf(extended), MessageOf(original))
	}

	details := DetailsOf(extended)
	if details["first"] != "1" || details["second"] != "2" {
		t.Errorf("details = %v, want both keys", details)
	}
	if got := DetailsOf(original); len(got) != 1 {
		t.Errorf("extending the copy changed the original: %v", got)
	}
}

// TestWithDetailLeavesWrappedErrorsAlone pins the documented limit.
//
// Reaching through a wrapper would mean returning the inner error and losing
// the context the wrapping added, so the helper does nothing. This is a test
// rather than a comment because "does nothing" is the kind of behaviour that
// gets silently changed.
func TestWithDetailLeavesWrappedErrorsAlone(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("loading config: %w", New(KindNotFound, "missing", nil))

	got := WithDetail(wrapped, "operation_id", "op-1")
	if got.Error() != wrapped.Error() {
		t.Errorf("a wrapped error came back changed: %q", got.Error())
	}
	if details := DetailsOf(got); details != nil {
		t.Errorf("a wrapped error reported details: %v", details)
	}
}
