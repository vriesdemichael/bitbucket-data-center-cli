package mcp

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

// The reviewers a form starts with are each list's in order, each person
// once however their username is cased, as Bitbucket's usernames do not
// differ by case alone.
func TestFormReviewersJoinEachPersonOnce(t *testing.T) {
	t.Parallel()

	joined := joinPeople([]string{"bob", " Carol "}, []string{"carol", "", "erin"}, []string{"BOB", "dave"})
	if want := []string{"bob", "Carol", "erin", "dave"}; !slices.Equal(joined, want) {
		t.Errorf("joined %v, want %v", joined, want)
	}
	if kept := withoutPerson([]string{"erin", "Admin", "dave"}, "admin"); !slices.Equal(kept, []string{"erin", "dave"}) {
		t.Errorf("without the author %v, want erin and dave", kept)
	}
	if kept := withoutPerson([]string{"erin"}, ""); !slices.Equal(kept, []string{"erin"}) {
		t.Errorf("without nobody %v, want erin", kept)
	}
}

// The model is told who the form starts with, since it did not choose all of
// them.
func TestFormSummaryNamesTheReviewers(t *testing.T) {
	t.Parallel()

	for reviewers, want := range map[string]string{
		"":                 "",
		"bob":              ", with reviewer bob",
		"bob,carol,erin":   ", with reviewers bob, carol and erin",
		"bob,carol":        ", with reviewers bob and carol",
		"bob,carol,erin,x": ", with reviewers bob, carol, erin and x",
	} {
		if got := reviewersNote(parseCommaList(reviewers)); got != want {
			t.Errorf("reviewers %q read %q, want %q", reviewers, got, want)
		}
	}
}

// Two branches that are the same have no pull request between them, and
// nobody is named for one: nothing is asked of Bitbucket.
func TestNobodyIsNamedForOneBranchIntoItself(t *testing.T) {
	t.Parallel()

	found := reviewersForBranches(context.Background(), Clients{}, "PAY", "ledger", "master", "refs/heads/master")
	if len(found.defaults) != 0 || len(found.owners) != 0 || found.defaultsErr != nil || found.ownersErr != nil {
		t.Errorf("one branch into itself named %+v", found)
	}
}

// Whom Bitbucket names is asked for a pair of branches, and the tool says so
// rather than ask about half a pair; the fields it answers are in its schema.
func TestSuggestFormValuesNeedsBothBranches(t *testing.T) {
	t.Parallel()

	for _, field := range []string{formFieldDefaultReviewers, formFieldCodeOwners} {
		_, err := suggestFormValues(context.Background(), Clients{}, SuggestFormValuesInput{Project: "PAY", Repo: "ledger", Field: field, FromRef: "feature/x"})
		if err == nil || !strings.Contains(err.Error(), "from_ref and to_ref") {
			t.Errorf("%s with one branch answered %v, want it to need both", field, err)
		}
	}
	if _, err := suggestFormValues(context.Background(), Clients{}, SuggestFormValuesInput{Field: "group"}); err == nil || !strings.Contains(err.Error(), "code_owners") {
		t.Errorf("an unknown field answered %v, want the fields it takes", err)
	}

	raw, err := json.Marshal(specSuggestFormValues().Tool.InputSchema)
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Properties map[string]struct {
			Enum []string `json:"enum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	fields := []string{formFieldBranch, formFieldReviewer, formFieldDefaultReviewers, formFieldCodeOwners}
	if got := schema.Properties["field"].Enum; !slices.Equal(got, fields) {
		t.Errorf("the input schema offers the fields %v, want %v", got, fields)
	}
	for _, branch := range []string{"from_ref", "to_ref"} {
		if _, ok := schema.Properties[branch]; !ok {
			t.Errorf("the input schema has no %s", branch)
		}
	}
}
