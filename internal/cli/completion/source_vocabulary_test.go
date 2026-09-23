package completion

import (
	"context"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	aicmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/ai"
	projectcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/project"
	repocmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/repo"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

// TestATokenPermissionFollowsTheScopeOnTheLine is the one place in this file
// where a completion depends on something already typed.
//
// Bitbucket refuses PROJECT_READ on a repository-scoped token -- "Permissions
// for REPOSITORY scope must be one of PROJECT_READ" -- so offering all six
// after `--repo PROJ/app` offers three values the create will reject. The
// scope flags are exclusive, and a line that names two of them is refused by
// the command before any permission is read, so the narrowing applies only
// when --repo is the only one given.
//
// Sabotage that proved it guards: returning the six values unconditionally
// from tokenPermissionSource fails the first subtest with the three project
// permissions named.
func TestATokenPermissionFollowsTheScopeOnTheLine(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		line   []string
		want   []string
		absent []string
	}{
		{
			name:   "a repository-scoped token takes only the repository permissions",
			line:   []string{"--repo", "PROJ/app"},
			want:   TokenRepositoryPermissions,
			absent: TokenProjectPermissions,
		},
		{
			name: "a user-scoped token takes all six",
			line: nil,
			want: append(append([]string{}, TokenRepositoryPermissions...), TokenProjectPermissions...),
		},
		{
			name: "a project-scoped token takes all six",
			line: []string{"--project", "PROJ"},
			want: append(append([]string{}, TokenRepositoryPermissions...), TokenProjectPermissions...),
		},
		{
			// Two scopes is a line the command refuses outright, so there is
			// no scope to narrow to and the full set is the honest answer.
			name: "a line naming two scopes is not narrowed",
			line: []string{"--repo", "PROJ/app", "--project", "PROJ"},
			want: append(append([]string{}, TokenRepositoryPermissions...), TokenProjectPermissions...),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			command := tokenCreateCommand(t, test.line)

			result, err := tokenPermissionSource(context.Background(), nil, Request{Command: command, Flag: "permission", Position: -1})
			if err != nil {
				t.Fatalf("tokenPermissionSource: %v", err)
			}

			offered := valuesOf(result.Candidates)
			for _, want := range test.want {
				if !contains(offered, want) {
					t.Errorf("%s is accepted at this scope but was not offered; got %v", want, offered)
				}
			}
			for _, absent := range test.absent {
				if contains(offered, absent) {
					t.Errorf("%s is refused at this scope but was offered; got %v", absent, offered)
				}
			}
		})
	}
}

// TestThePermissionsOfferedFollowTheCommandBeingCompleted covers the one
// placeholder in the vocabulary that means two different sets.
//
// <permission> is PROJECT_* under bb project and REPO_* under bb repo, and
// each command refuses the other's values. The declaration cannot say which,
// so the source reads the command -- and what it reads has to be the slice the
// command itself enforces, not a copy.
//
// Sabotage that proved it guards: dropping the topLevelCommand check, so every
// <permission> completes the project set, fails the repo subtest with
// PROJECT_READ offered where REPO_READ belongs.
func TestThePermissionsOfferedFollowTheCommandBeingCompleted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path []string
		want []string
	}{
		{"project", []string{"project", "permissions", "grant"}, projectcmd.PermissionNames},
		{"project, one level deeper", []string{"project", "permissions", "users", "grant"}, projectcmd.PermissionNames},
		{"repo", []string{"repo", "permissions", "grant"}, repocmd.PermissionNames},
		{"repo, one level deeper", []string{"repo", "permissions", "groups", "grant"}, repocmd.PermissionNames},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			result, err := permissionSource(context.Background(), nil, Request{Command: commandAt(test.path...), Position: 1})
			if err != nil {
				t.Fatalf("permissionSource: %v", err)
			}

			if got := valuesOf(result.Candidates); !sameOrder(got, test.want) {
				t.Errorf("bb %s <permission> completes %v; the command accepts %v",
					strings.Join(test.path, " "), got, test.want)
			}
		})
	}
}

// TestAnMCPToolListIsCompletedOneElementAtATime covers the comma handling the
// installer does not do here.
//
// --tools and --exclude take a comma-separated list as a plain string, which
// pflag types as "string", so the installer's multi-value path never runs for
// them: nothing splits the word at the last comma and nothing carries the
// elements already typed back onto the candidate. A shell replaces the whole
// word, so a candidate that is only the tool name would delete everything
// before it.
//
// Sabotage that proved it guards: returning spec.Tool.Name rather than
// prefix+name fails the second subtest, showing "list_tags" where
// "get_commit,list_tags" belongs.
func TestAnMCPToolListIsCompletedOneElementAtATime(t *testing.T) {
	t.Parallel()

	t.Run("the first element is completed bare", func(t *testing.T) {
		t.Parallel()

		result, err := mcpToolSource(context.Background(), nil, Request{ToComplete: "list_b"})
		if err != nil {
			t.Fatalf("mcpToolSource: %v", err)
		}

		offered := valuesOf(result.Candidates)
		if !contains(offered, "list_branches") {
			t.Fatalf("expected list_branches among the candidates, got %v", offered)
		}
		for _, value := range offered {
			if strings.Contains(value, ",") {
				t.Errorf("a first element came back with a comma in it: %q", value)
			}
		}
	})

	t.Run("a later element carries the ones already typed", func(t *testing.T) {
		t.Parallel()

		result, err := mcpToolSource(context.Background(), nil, Request{ToComplete: "get_commit,list_t"})
		if err != nil {
			t.Fatalf("mcpToolSource: %v", err)
		}

		offered := valuesOf(result.Candidates)
		if !contains(offered, "get_commit,list_tags") {
			t.Fatalf("expected the finished element carried back on the candidate, got %v", offered)
		}
		if !result.NoSpace {
			t.Error("a list element completed without NoSpace puts a space where the next comma goes")
		}
	})

	t.Run("a tool already named is not offered again", func(t *testing.T) {
		t.Parallel()

		result, err := mcpToolSource(context.Background(), nil, Request{ToComplete: "list_tags,list_t"})
		if err != nil {
			t.Fatalf("mcpToolSource: %v", err)
		}

		for _, value := range valuesOf(result.Candidates) {
			if value == "list_tags,list_tags" {
				t.Fatalf("list_tags was offered twice: %v", valuesOf(result.Candidates))
			}
		}
	})
}

// TestEveryVocabularyValueCarriesADescription guards the maps beside the
// lists.
//
// A description is keyed by value, so a value added to the list and forgotten
// in the map completes silently with nothing beside it -- which for
// rebase-ff-only or pr:from_ref_updated is a candidate nobody can choose
// between. The lists are the authority; this only checks that the annotation
// kept up.
//
// Sabotage that proved it guards: removing pr:deleted from
// webhookEventDescriptions fails here naming it.
func TestEveryVocabularyValueCarriesADescription(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		values       []string
		descriptions map[string]string
	}{
		{"webhook events", WebhookEvents, webhookEventDescriptions},
		{"merge strategies", openapi.MergeStrategies, mergeStrategyDescriptions},
		{"token permissions", append(append([]string{}, TokenRepositoryPermissions...), TokenProjectPermissions...), tokenPermissionDescriptions},
		{"project permissions", projectcmd.PermissionNames, permissionDescriptions},
		{"repository permissions", repocmd.PermissionNames, permissionDescriptions},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			for _, value := range test.values {
				if strings.TrimSpace(test.descriptions[value]) == "" {
					t.Errorf("%s is offered with no description beside it", value)
				}
			}
		})
	}
}

// TestEveryShippedSkillIsOfferedWithWhatItIsFor holds the completion to the
// registry `bb ai skill` resolves against: each skill by the name the argument
// takes, which is also the directory its file lands in, with its summary beside
// it.
func TestEveryShippedSkillIsOfferedWithWhatItIsFor(t *testing.T) {
	t.Parallel()

	result, err := skillSource(context.Background(), nil, Request{})
	if err != nil {
		t.Fatalf("skillSource: %v", err)
	}

	if len(result.Candidates) != len(aicmd.Skills) {
		t.Fatalf("offered %v, want one candidate for each of the %d shipped skills", valuesOf(result.Candidates), len(aicmd.Skills))
	}
	for index, skill := range aicmd.Skills {
		if candidate := result.Candidates[index]; candidate.Value != skill.Name || candidate.Description != skill.Summary {
			t.Errorf("candidate %d = %+v, want %s with %q beside it", index, candidate, skill.Name, skill.Summary)
		}
	}
}

// tokenCreateCommand builds the shape of `bb auth token create`: the scope
// flags are persistent on bb auth token, which is why the source can read them
// off the command being completed.
//
// The line is handed to ParseFlags rather than each flag being Set, because
// that is what Cobra does before it asks for a completion -- and it is also
// what merges a parent's persistent flags into the command's own set, which is
// the step that makes them readable at all.
func tokenCreateCommand(t *testing.T, line []string) *cobra.Command {
	t.Helper()

	root := &cobra.Command{Use: "bb"}
	auth := &cobra.Command{Use: "auth"}
	token := &cobra.Command{Use: "token"}
	create := &cobra.Command{Use: "create [name]"}

	for _, name := range []string{"user", "project", "repo"} {
		token.PersistentFlags().String(name, "", "")
	}
	create.Flags().StringSlice("permission", nil, "")

	root.AddCommand(auth)
	auth.AddCommand(token)
	token.AddCommand(create)

	if err := create.ParseFlags(line); err != nil {
		t.Fatalf("parsing %v: %v", line, err)
	}

	return create
}

// commandAt builds a command at the given path under a root called bb, which
// is all topLevelCommand reads.
func commandAt(path ...string) *cobra.Command {
	command := &cobra.Command{Use: "bb"}
	for _, name := range path {
		child := &cobra.Command{Use: name}
		command.AddCommand(child)
		command = child
	}

	return command
}

func sameOrder(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}

	return true
}
