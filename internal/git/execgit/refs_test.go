package execgit

import (
	"context"
	"testing"
)

// TestListRefsCarriesTheSubjectOfWhatEachReferenceNames holds the field shell
// completion describes a branch with.
//
// The subject is the last field of a record because it is the only one that
// can hold a tab, so a subject with one in it is the case that would split it.
func TestListRefsCarriesTheSubjectOfWhatEachReferenceNames(t *testing.T) {
	t.Parallel()

	backend := New()
	repositoryDirectory := newCommittedRepository(t, backend)

	for _, args := range [][]string{
		{"commit", "--allow-empty", "-m", "fix\tthe thing\n\nand say why in the body"},
		{"tag", "-a", "v1.0.0", "-m", "release one"},
		{"branch", "spike"},
	} {
		if _, err := backend.run(context.Background(), runOptions{cwd: repositoryDirectory, args: args}); err != nil {
			t.Fatalf("git %v failed: %v", args, err)
		}
	}

	refs, err := backend.ListRefs(context.Background(), repositoryDirectory, 0)
	if err != nil {
		t.Fatalf("ListRefs failed: %v", err)
	}

	subjects := make(map[string]string, len(refs))
	for _, ref := range refs {
		subjects[ref.Name] = ref.Subject
	}

	want := map[string]string{
		"refs/heads/master": "fix\tthe thing",
		"refs/heads/spike":  "fix\tthe thing",
		// An annotated tag's subject is its own message, not the commit's.
		"refs/tags/v1.0.0": "release one",
	}
	for name, subject := range want {
		if got, ok := subjects[name]; !ok || got != subject {
			t.Errorf("subject of %s = %q (listed: %v), want %q", name, got, ok, subject)
		}
	}
}
