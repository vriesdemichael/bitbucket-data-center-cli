package pullrequest

import (
	"reflect"
	"testing"
)

// When the read after a review change fails, the change has landed and the
// pull request read before it stands in with the change applied. These are
// that application, which needs no server to check.
func TestAConfirmedParticipantReplacesTheReviewerItNames(t *testing.T) {
	t.Parallel()

	before := []Reviewer{
		{Name: "alice", Role: "REVIEWER", Status: "UNAPPROVED"},
		{Name: "bob", Role: "REVIEWER", Status: "UNAPPROVED"},
	}
	approved := pullRequestParticipant{User: &pullRequestUserIdentity{Name: "Bob"}, Role: "REVIEWER", Status: "APPROVED", Approved: true}

	got := withParticipant(before, approved)
	want := []Reviewer{
		{Name: "alice", Role: "REVIEWER", Status: "UNAPPROVED"},
		{Name: "Bob", Role: "REVIEWER", Status: "APPROVED", Approved: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}

	// A participant who was not a reviewer is added.
	if got := withParticipant(before, pullRequestParticipant{User: &pullRequestUserIdentity{Name: "carol"}, Role: "PARTICIPANT", Status: "NEEDS_WORK"}); len(got) != 3 {
		t.Fatalf("a new participant was not added: %+v", got)
	}

	// A reply that names nobody changes nothing.
	if got := withParticipant(before, pullRequestParticipant{}); !reflect.DeepEqual(got, before) {
		t.Fatalf("an empty reply changed the reviewers: %+v", got)
	}
}

func TestARemovedReviewerIsLeftOut(t *testing.T) {
	t.Parallel()

	before := []Reviewer{{Name: "alice"}, {Name: "bob"}}
	if got := withoutReviewer(before, "ALICE"); !reflect.DeepEqual(got, []Reviewer{{Name: "bob"}}) {
		t.Fatalf("got %+v", got)
	}
}
