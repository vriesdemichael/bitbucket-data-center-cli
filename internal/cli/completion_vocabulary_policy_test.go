package cli

import (
	"bytes"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/completion"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/usage"
	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

// The tests here are the second half of the rule
// TestThePositionalStatusMatchesTheFlagThatTakesTheSameValues states: a
// vocabulary that exists in two places needs something holding the copies
// together, and a vocabulary that is Bitbucket's rather than bb's needs
// something holding it to Bitbucket. The live half lives in
// tests/integration/live/completion_vocabulary_live_test.go, which creates a
// token with each permission and a webhook with each event; these are the
// halves that need no server.

// TestTheStrategyArgumentMatchesTheFlagThatTakesTheSameValues ties the merge
// strategy ids to the enum that owns them.
//
// `bb repo settings pull-requests set-strategy <strategy-id>` takes as an
// argument what `bb pr auto-merge enable --strategy` takes as a flag. The flag
// is an enumflag and is therefore the authority; a positional cannot be one,
// so completion has to reach the same slice rather than carry a copy. Four
// disagreeing copies of this set is what #577 found, two of them wrong in
// opposite directions.
//
// Sabotage that proved it guards: registering --strategy with a list missing
// "ff" fails here, naming the value completion offers that the flag would
// refuse.
func TestTheStrategyArgumentMatchesTheFlagThatTakesTheSameValues(t *testing.T) {
	command, _, err := NewRootCommand().Find([]string{"pr", "auto-merge", "enable"})
	if err != nil {
		t.Fatalf("finding bb pr auto-merge enable failed: %v", err)
	}

	flag := command.Flags().Lookup("strategy")
	if flag == nil {
		t.Fatal("bb pr auto-merge enable no longer has a --strategy flag; the positional <strategy-id> now has no authority to match")
	}

	allowed, isEnum := enumflag.Allowed(flag)
	if !isEnum {
		t.Fatal("bb pr auto-merge enable --strategy is no longer an enum flag")
	}

	if !reflect.DeepEqual(allowed, openapi.MergeStrategies) {
		t.Fatalf("--strategy accepts %v; openapi.MergeStrategies, which the argument is validated against, holds %v",
			allowed, openapi.MergeStrategies)
	}

	offered := completeWith(t, "repo", "settings", "pull-requests", "set-strategy", "")
	if !reflect.DeepEqual(offered, openapi.MergeStrategies) {
		t.Errorf("the values completed for <strategy-id> are %v; --strategy accepts %v", offered, allowed)
	}
}

// TestEveryWebhookEventFlagDefaultsToAnEventCompletionOffers holds the
// webhook vocabulary to the one place it is also written down.
//
// The five --event flags each carry repo:refs_changed as their default, which
// is a copy of an element of completion.WebhookEvents with nothing joining
// them. If either moved -- a default corrected to a key Bitbucket renamed, a
// list rewritten from the documentation -- one of the two would be wrong and
// nothing would say which.
//
// The count is asserted too, because a walk that finds no --event flag would
// pass while checking nothing.
//
// Sabotage that proved it guards: changing one create's default to
// "repo:refs-changed" fails here naming that command.
func TestEveryWebhookEventFlagDefaultsToAnEventCompletionOffers(t *testing.T) {
	t.Parallel()

	offered := map[string]bool{}
	for _, event := range completion.WebhookEvents {
		offered[event] = true
	}

	checked := 0
	walkCommands(NewRootCommand(), func(command *cobra.Command) {
		command.Flags().VisitAll(func(flag *pflag.Flag) {
			if flag.Name != "event" {
				return
			}
			checked++

			for _, value := range defaultsOf(flag) {
				if !offered[value] {
					t.Errorf("bb %s --event defaults to %q, which completion does not offer -- one of the two is wrong",
						completionPath(command), value)
				}
			}
		})
	})

	if checked == 0 {
		t.Fatal("no --event flag was found; has the webhook surface moved?")
	}
	t.Logf("%d --event flags checked", checked)
}

// TestEveryPermissionArgumentCompletesTheSetItsCommandEnforces covers the one
// placeholder in the vocabulary that means two different sets.
//
// <permission> is PROJECT_* under bb project and REPO_* under bb repo. Both
// are enforced by enumflag.Value against a slice the command package owns, and
// completion offers those same slices -- so what this checks is that every
// slot in the tree lands on the right one of the two, including the shallow
// aliases and the per-subject commands, which are separate cobra commands
// built from the same constructor.
//
// Sabotage that proved it guards: making permissionSource answer with the
// project set unconditionally fails here, once per repo grant command.
func TestEveryPermissionArgumentCompletesTheSetItsCommandEnforces(t *testing.T) {
	slots := 0

	walkCommands(NewRootCommand(), func(command *cobra.Command) {
		if completionIsCobrasOwn(command) {
			return
		}

		placeholders := usage.Placeholders(command.Use)
		for index, placeholder := range placeholders {
			if usage.Name(placeholder) != "permission" {
				continue
			}
			kind, _ := completion.DeclaredPositional(completionPath(command), placeholder)
			if kind != completion.KindPermission {
				continue
			}
			slots++

			path := strings.Fields(completionPath(command))
			want := []string{"PROJECT_READ", "PROJECT_WRITE", "PROJECT_ADMIN"}
			if path[0] == "repo" {
				want = []string{"REPO_READ", "REPO_WRITE", "REPO_ADMIN"}
			}

			// Placeholders before the one being completed are filled with
			// something arbitrary: they are the names of a project and a
			// person, and nothing reads them to answer this slot.
			line := append([]string{}, path...)
			for range placeholders[:index] {
				line = append(line, "x")
			}

			if got := completeWith(t, append(line, "")...); !reflect.DeepEqual(got, want) {
				t.Errorf("bb %s <permission> completes %v; the command accepts %v", completionPath(command), got, want)
			}
		}
	})

	if slots == 0 {
		t.Fatal("no <permission> argument was found; has the permissions surface moved?")
	}
	t.Logf("%d <permission> arguments checked", slots)
}

// TestEveryMCPToolOfferedIsOneTheServerCanExpose holds --tools and --exclude
// to the listing their help text sends the caller to.
//
// `bb ai mcp tools` says "Use this output to build --tools and --exclude
// allowlists/denylists", so a name completed for either flag that the listing
// does not print is a name the server has no tool for -- and the server drops
// an unknown name from an allowlist silently, leaving a caller with fewer
// tools than they asked for and nothing saying why.
//
// Sabotage that proved it guards: offering SafeSpecs rather than AllSpecs
// fails here with the four tools --yolo exposes.
func TestEveryMCPToolOfferedIsOneTheServerCanExpose(t *testing.T) {
	listed := map[string]bool{}
	for _, spec := range bbmcp.AllSpecs() {
		listed[spec.Tool.Name] = true
	}

	output := runCommand(t, "ai", "mcp", "tools")
	printed := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 {
			printed[fields[0]] = true
		}
	}

	for _, flag := range []string{"--tools", "--exclude"} {
		offered := completeWith(t, "ai", "mcp", "serve", flag, "")
		if len(offered) == 0 {
			t.Fatalf("bb ai mcp serve %s completes nothing", flag)
		}

		for _, name := range offered {
			if !listed[name] {
				t.Errorf("%s offers %q, which is not a tool the server has", flag, name)
			}
			if !printed[name] {
				t.Errorf("%s offers %q, which `bb ai mcp tools` does not print", flag, name)
			}
		}

		if len(offered) != len(listed) {
			t.Errorf("%s offers %d tools; the server has %d", flag, len(offered), len(listed))
		}
	}
}

// TestEverySkillOfferedCanBeShown holds the skill completion to the command it
// completes for.
//
// `bb ai skill install|remove|show [skill]` refuses a name lookupSkill does
// not know, so a completed name that does not resolve is a suggestion that
// turns straight into a validation error. Running show for each is the
// cheapest way to ask the real resolver.
//
// Sabotage that proved it guards: having skillSource offer skill.Name+"-v2",
// which is the shape of a completion built from a list of its own rather than
// from the registry, fails here on "unknown skill \"bb-v2\"".
func TestEverySkillOfferedCanBeShown(t *testing.T) {
	offered := completeWith(t, "ai", "skill", "show", "")
	if len(offered) == 0 {
		t.Fatal("bb ai skill show completes no skill")
	}

	for _, name := range offered {
		output := runCommand(t, "ai", "skill", "show", name)
		if strings.TrimSpace(output) == "" {
			t.Errorf("bb ai skill show %s printed nothing", name)
		}
		if strings.Contains(output, "unknown skill") {
			t.Errorf("completion offers %q, which bb ai skill show refuses: %s", name, output)
		}
	}
}

// completeWith runs a real tab press through the finished command tree and
// returns the values offered, in the order the shell will see them.
//
// Through __complete rather than by calling a source, because the question
// these tests ask is what a shell gets -- which goes through the declaration
// tables, the installer and the prefix filter, any one of which can drop a
// value the source returned.
func completeWith(t *testing.T, words ...string) []string {
	t.Helper()

	output := runCommand(t, append([]string{cobra.ShellCompRequestCmd}, words...)...)

	values := []string{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.TrimSpace(trimmed) == "" || strings.HasPrefix(trimmed, ":") ||
			strings.HasPrefix(trimmed, "Completion ended with directive:") {
			continue
		}

		value, _, _ := strings.Cut(trimmed, "\t")
		values = append(values, strings.TrimSpace(value))
	}

	return values
}

func runCommand(t *testing.T, args ...string) string {
	t.Helper()

	// No stored configuration and no instance: everything these tests complete
	// is a fixed vocabulary, and a source that reached for a host would be
	// answering a different question.
	t.Setenv("BB_DISABLE_STORED_CONFIG", "1")

	command := NewRootCommand()
	output := &bytes.Buffer{}
	command.SetOut(output)
	command.SetErr(output)
	command.SetArgs(args)

	if err := command.Execute(); err != nil {
		t.Fatalf("bb %s failed: %v\noutput: %s", strings.Join(args, " "), err, output)
	}

	return output.String()
}

// defaultsOf reads a slice flag's default back out of the string pflag prints
// for it, which is the only form pflag exposes: "[repo:refs_changed]".
func defaultsOf(flag *pflag.Flag) []string {
	trimmed := strings.TrimSpace(flag.DefValue)
	if unquoted, err := strconv.Unquote(trimmed); err == nil {
		trimmed = unquoted
	}
	trimmed = strings.TrimSuffix(strings.TrimPrefix(trimmed, "["), "]")

	values := []string{}
	for _, value := range strings.Split(trimmed, ",") {
		if cleaned := strings.TrimSpace(value); cleaned != "" {
			values = append(values, cleaned)
		}
	}

	sort.Strings(values)

	return values
}
