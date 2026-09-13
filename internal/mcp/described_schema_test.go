package mcp

import (
	"strings"
	"testing"
)

// describedInputSchema panics on a wiring mistake, as enumInputSchema does, so
// the mistake fails the first test that builds the catalogue rather than
// shipping a description attached to nothing.

func TestDescribedInputSchemaRefusesAPropertyTheInputDoesNotHave(t *testing.T) {
	t.Parallel()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("a description for a property the input does not have was accepted")
		}
		if message, _ := recovered.(string); !strings.Contains(message, "no_such_property") {
			t.Fatalf("the panic does not name the property: %v", recovered)
		}
	}()

	describedInputSchema[ListPullRequestsInput](map[string]string{"no_such_property": "ignored"})
}

func TestDescribedInputSchemaRefusesATypeItCannotDerive(t *testing.T) {
	t.Parallel()

	type unrepresentable struct {
		Callback func() `json:"callback"`
	}

	defer func() {
		if recover() == nil {
			t.Fatal("an input type with no JSON Schema was accepted")
		}
	}()

	describedInputSchema[unrepresentable](nil)
}
