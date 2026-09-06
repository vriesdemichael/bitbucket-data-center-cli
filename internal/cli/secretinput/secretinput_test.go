package secretinput

import (
	"strings"
	"testing"
)

// The stdin reading itself is covered by the auth package, which has driven it
// since #464. What is new here is Resolve: the precedence between a pipe and a
// variable, and the rule that neither being present is not an error.

func TestResolvePrefersThePipeOverTheVariable(t *testing.T) {
	t.Setenv("BB_TEST_SECRET", "from-the-environment")

	resolved, err := Resolve(true, strings.NewReader("from-stdin\n"), "--secret-stdin", "BB_TEST_SECRET", "example")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Piping is the explicit act; a variable can come from a shell profile the
	// caller has forgotten about.
	if resolved.Value != "from-stdin" {
		t.Errorf("value = %q, want the piped one", resolved.Value)
	}
	if resolved.Origin != "--secret-stdin" || !resolved.Given {
		t.Errorf("origin = %q given = %v", resolved.Origin, resolved.Given)
	}
}

func TestResolveReadsTheVariableWhenNothingIsPiped(t *testing.T) {
	t.Setenv("BB_TEST_SECRET", "from-the-environment")

	resolved, err := Resolve(false, nil, "--secret-stdin", "BB_TEST_SECRET", "example")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if resolved.Value != "from-the-environment" || !resolved.Given {
		t.Errorf("resolved = %#v", resolved)
	}
	// The origin is what a dry run prints in place of the value, so it has to
	// name the variable.
	if resolved.Origin != "$BB_TEST_SECRET" {
		t.Errorf("origin = %q", resolved.Origin)
	}
}

func TestResolveTreatsAnEmptyVariableAsUnset(t *testing.T) {
	t.Setenv("BB_TEST_SECRET", "   ")

	resolved, err := Resolve(false, nil, "--secret-stdin", "BB_TEST_SECRET", "example")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	// Otherwise a typo in a variable name two files away quietly configures an
	// empty credential.
	if resolved.Given {
		t.Errorf("a blank variable was taken as a secret: %#v", resolved)
	}
}

func TestResolveIsSilentWhenNobodySaidAnything(t *testing.T) {
	t.Setenv("BB_TEST_SECRET", "")

	resolved, err := Resolve(false, nil, "--secret-stdin", "BB_TEST_SECRET", "example")
	if err != nil {
		t.Fatalf("neither present should not be an error: %v", err)
	}
	if resolved.Given || resolved.Value != "" {
		t.Errorf("resolved = %#v", resolved)
	}
}

func TestResolveReportsAPipeThatCarriedNothing(t *testing.T) {
	t.Setenv("BB_TEST_SECRET", "from-the-environment")

	// A flag that asked for a pipe and got an empty one is a mistake worth
	// reporting, not a reason to quietly fall back to the variable.
	if _, err := Resolve(true, strings.NewReader(""), "--secret-stdin", "BB_TEST_SECRET", "example"); err == nil {
		t.Error("an empty pipe was accepted")
	}
	if _, err := Resolve(true, nil, "--secret-stdin", "BB_TEST_SECRET", "example"); err == nil {
		t.Error("a missing stdin was accepted")
	}
}

func TestFromStdinRefusesWhatIsNotOneCredential(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: "stdin was empty"},
		{name: "blank", input: "  \n", want: "stdin was empty"},
		{name: "two words", input: "two words\n", want: "contains whitespace"},
		{name: "two lines", input: "first\nsecond\n", want: "contains whitespace"},
		{name: "too long", input: strings.Repeat("a", MaxLength+1), want: "not a credential"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := FromStdin(strings.NewReader(testCase.input), "--secret-stdin", "example")
			if err == nil {
				t.Fatalf("expected a refusal for %q", testCase.input)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Errorf("error = %v, want it to mention %q", err, testCase.want)
			}
		})
	}
}

func TestFromStdinKeepsTheTrailingNewlineOut(t *testing.T) {
	t.Parallel()

	// So that the common `echo "$SECRET" | bb ...` form works rather than
	// configuring a credential with a newline on the end, which fails much
	// later and somewhere else.
	secret, err := FromStdin(strings.NewReader("s3cr3t\r\n"), "--secret-stdin", "example")
	if err != nil {
		t.Fatalf("FromStdin: %v", err)
	}
	if secret != "s3cr3t" {
		t.Errorf("secret = %q", secret)
	}
}
