package errors

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestExitCodeByKind(t *testing.T) {
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

	for _, kind := range []Kind{
		KindAuthentication, KindAuthorization, KindValidation, KindNotFound,
		KindConflict, KindTransient, KindPermanent, KindNotImplemented, KindInternal,
	} {
		if !seen[kind] {
			t.Fatalf("declared kind %q is missing from Kinds()", kind)
		}
	}
}

func TestMessageOf(t *testing.T) {
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
