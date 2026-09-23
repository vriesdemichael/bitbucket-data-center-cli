package completion

import (
	"context"
	"errors"
	"testing"

	"github.com/spf13/cobra"
)

// TestAModelThatCouldNotBeReadDescribesNothing holds the other side of "not
// enabled" and "not configured": both are claims about a model that was read.
// When it could not be, every id is still offered, because the ids are fixed,
// and none is described, so no id claims something nobody checked.
func TestAModelThatCouldNotBeReadDescribesNothing(t *testing.T) {
	t.Parallel()

	environment := unresolvable(&cobra.Command{Use: "create"}, errors.New("no instance configured"))

	for name, candidates := range map[string][]Candidate{
		"model branches":   modelBranches(context.Background(), environment),
		"model categories": modelCategories(context.Background(), environment),
	} {
		if len(candidates) == 0 {
			t.Errorf("%s: expected the fixed ids to be offered anyway", name)
		}
		for _, candidate := range candidates {
			if candidate.Description != "" {
				t.Errorf("%s: %s was described as %q from a model nobody read", name, candidate.Value, candidate.Description)
			}
		}
	}
}
