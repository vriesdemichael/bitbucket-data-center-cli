package prcmd

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/testsupport"
)

// everyPRCommand is one invocation of each runnable pr command: its arguments
// and required flags, and nothing more.
var everyPRCommand = [][]string{
	{"activity", "list", "42"},
	{"auto-merge", "disable", "42"},
	{"auto-merge", "enable", "42"},
	{"auto-merge", "get", "42"},
	{"build", "status", "42"},
	{"checkout", "42"},
	{"checks", "42"},
	{"comment", "add", "42", "--text", "A comment"},
	{"comment", "apply-suggestion", "42", "7"},
	{"comment", "delete", "42", "7"},
	{"comment", "get", "42", "7"},
	{"comment", "list", "42"},
	{"comment", "react", "42", "7", "thumbsup"},
	{"comment", "reopen", "42", "7"},
	{"comment", "resolve", "42", "7"},
	{"commits", "42"},
	{"create", "--from-ref", "feature", "--to-ref", "main", "--title", "A change"},
	{"decline", "42"},
	{"default-reviewers"},
	{"diff", "42"},
	{"files", "42"},
	{"get", "42"},
	{"jira", "42"},
	{"list"},
	{"merge", "42"},
	{"merge-base", "42"},
	{"participants", "--search", "alice"},
	{"ready", "42"},
	{"rebase", "42"},
	{"reopen", "42"},
	{"review", "approve", "42"},
	{"review", "complete", "42"},
	{"review", "discard", "42"},
	{"review", "get", "42"},
	{"review", "reviewer", "add", "42", "--user", "alice"},
	{"review", "reviewer", "remove", "42", "--user", "alice"},
	{"review", "set", "42", "approved"},
	{"review", "unapprove", "42"},
	{"status"},
	{"unwatch", "42"},
	{"update", "42", "--title", "A change"},
	{"watch", "42"},
}

// TestEveryPRCommandIsInvokedHere keeps everyPRCommand to the command tree, so
// a command added to pr is held to the tests below as well.
func TestEveryPRCommandIsInvokedHere(t *testing.T) {
	t.Parallel()

	root := New(Dependencies{})
	invoked := []string{}
	for _, invocation := range everyPRCommand {
		command, _, err := root.Find(invocation)
		if err != nil {
			t.Fatalf("%v names no pr command: %v", invocation, err)
		}
		invoked = append(invoked, command.CommandPath())
	}

	runnable := []string{}
	var walk func(*cobra.Command)
	walk = func(command *cobra.Command) {
		if command.Runnable() && !command.Hidden && command != root {
			runnable = append(runnable, command.CommandPath())
		}
		for _, sub := range command.Commands() {
			walk(sub)
		}
	}
	walk(root)

	slices.Sort(invoked)
	slices.Sort(runnable)
	if !slices.Equal(invoked, runnable) {
		t.Errorf("everyPRCommand invokes\n  %s\nbut the runnable pr commands are\n  %s", strings.Join(invoked, "\n  "), strings.Join(runnable, "\n  "))
	}
}

// TestEveryPRCommandStopsAtAConfigurationItCannotLoad: a command whose
// configuration cannot be loaded says why, rather than going on without one.
func TestEveryPRCommandStopsAtAConfigurationItCannotLoad(t *testing.T) {
	t.Parallel()

	unloadable := errors.New("the configuration cannot be loaded")
	deps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return config.AppConfig{}, unloadable },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			return config.AppConfig{}, nil, unloadable
		},
	}

	for _, invocation := range everyPRCommand {
		if err := executeInvocation(deps, invocation...); !errors.Is(err, unloadable) {
			t.Errorf("bb pr %s gave %v, want the configuration failure", strings.Join(invocation, " "), err)
		}
	}
}

// TestEveryPRCommandRefusesAMalformedRepository: a --repo that is not
// PROJECT/slug is refused as invalid before any request is sent. pr status is
// left out: --repo only places its current-branch section, which then says it
// has no repository context, and its other sections need none.
func TestEveryPRCommandRefusesAMalformedRepository(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(testsupport.UnreachedHandler(t))
	t.Cleanup(server.Close)
	deps := Dependencies{
		LoadConfig: func() (config.AppConfig, error) { return loadTestConfig(server.URL) },
		LoadConfigAndClient: func() (config.AppConfig, *openapigenerated.ClientWithResponses, error) {
			return loadTestConfigAndClient(server.URL)
		},
	}

	for _, invocation := range everyPRCommand {
		if invocation[0] == "status" {
			continue
		}
		args := append(slices.Clone(invocation), "--repo", "not-a-repository")
		err := executeInvocation(deps, args...)
		if apperrors.KindOf(err) != apperrors.KindValidation || !strings.Contains(err.Error(), "PROJECT/slug") {
			t.Errorf("bb pr %s gave %v, want the repository refused as invalid", strings.Join(args, " "), err)
		}
	}
}

func executeInvocation(deps Dependencies, args ...string) error {
	root := New(deps)
	root.SilenceUsage = true
	root.SilenceErrors = true
	output := &bytes.Buffer{}
	root.SetOut(output)
	root.SetErr(output)
	root.SetArgs(args)

	return root.Execute()
}
