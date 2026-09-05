package cli

import (
	"strings"
	"testing"
)

func TestReviewerConditionCreateMutualExclusionCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost")
	command := NewRootCommand()
	command.SetArgs([]string{"reviewer", "condition", "create", "{}", "--config-file", "some.json", "--project", "PRJ"})
	err := command.Execute()
	if err == nil {
		t.Fatal("expected error for mutual exclusion")
	}
	if !strings.Contains(err.Error(), "cannot provide condition config as both an argument and via --config-file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestReviewerConditionUpdateMutualExclusionCLI(t *testing.T) {
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")
	t.Setenv("BITBUCKET_URL", "http://localhost")
	command := NewRootCommand()
	command.SetArgs([]string{"reviewer", "condition", "update", "1", "{}", "--config-file", "some.json", "--project", "PRJ"})
	err := command.Execute()
	if err == nil {
		t.Fatal("expected error for mutual exclusion")
	}
	if !strings.Contains(err.Error(), "cannot provide condition config as both an argument and via --config-file") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// The two input-route suites are live now, in
// TestLiveReviewerConditionInputRoutes.
//
// They drove --config-file and stdin against a handler that answered 201 to
// any POST whose path contained "condition", so a body that was not a
// condition at all would have passed. The live version creates one from a file
// and finds it in the listing, then updates it from stdin with a different
// approval count and reads that count back.
