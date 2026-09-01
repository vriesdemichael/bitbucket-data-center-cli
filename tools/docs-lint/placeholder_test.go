package main

import "testing"

// TestResolvePlaceholdersHandlesConsecutivePlaceholders covers two shapes that
// the first version got wrong, one of them by crashing.
//
// The decision "is this placeholder a flag's value?" was made against the
// original arguments, so a placeholder could treat a preceding placeholder as
// its flag -- and then pop a token that was never one.
func TestResolvePlaceholdersHandlesConsecutivePlaceholders(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		args         []string
		wantUsable   bool
		wantCleaned  []string
		wantToVerify []string
	}{
		{
			name:       "two placeholders in a row popped past the end and panicked",
			args:       []string{"-f", "-<x>", "<y>"},
			wantUsable: false,
		},
		{
			name:       "a placeholder after a flag value popped the subcommand",
			args:       []string{"branch", "list", "--filter", "-*", "<repo>"},
			wantUsable: false,
		},
		{
			name:         "an ordinary flag value is dropped and the flag remembered",
			args:         []string{"auth", "login", "--host", "..."},
			wantUsable:   true,
			wantCleaned:  []string{"auth", "login"},
			wantToVerify: []string{"--host"},
		},
		{
			name:        "no placeholders passes through untouched",
			args:        []string{"branch", "create", "x", "--start-point", "main"},
			wantUsable:  true,
			wantCleaned: []string{"branch", "create", "x", "--start-point", "main"},
		},
		{
			name:       "a placeholder in subcommand position is a shape, not an invocation",
			args:       []string{"project", "permissions", "users", "…"},
			wantUsable: false,
		},
		{
			name:       "a leading placeholder has nothing before it",
			args:       []string{"<command>"},
			wantUsable: false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cleaned, toVerify, usable := resolvePlaceholders(testCase.args)

			if usable != testCase.wantUsable {
				t.Fatalf("usable = %v, want %v", usable, testCase.wantUsable)
			}
			if !usable {
				return
			}
			if len(cleaned) != len(testCase.wantCleaned) {
				t.Fatalf("cleaned = %v, want %v", cleaned, testCase.wantCleaned)
			}
			for index, want := range testCase.wantCleaned {
				if cleaned[index] != want {
					t.Errorf("cleaned[%d] = %q, want %q", index, cleaned[index], want)
				}
			}
			if len(toVerify) != len(testCase.wantToVerify) {
				t.Fatalf("flags to verify = %v, want %v", toVerify, testCase.wantToVerify)
			}
			for index, want := range testCase.wantToVerify {
				if toVerify[index] != want {
					t.Errorf("flag[%d] = %q, want %q", index, toVerify[index], want)
				}
			}
		})
	}
}
