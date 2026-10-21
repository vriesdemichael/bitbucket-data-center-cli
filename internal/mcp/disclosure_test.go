package mcp

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The line names the person as a mention Bitbucket links: bare when the name
// is plain, quoted when it is not, since a bare email-style name renders as a
// mail link (#797).
func TestTheDisclosureLineNamesThePersonAsBitbucketLinksThem(t *testing.T) {
	t.Parallel()

	for name, want := range map[string]string{
		"alice":                 "_Written by an AI agent for @alice, via bb._",
		"svc_build-2":           "_Written by an AI agent for @svc_build-2, via bb._",
		"LT.Agent+7@x.local":    `_Written by an AI agent for @"LT.Agent+7@x.local", via bb._`,
		"access-token-user/2/9": `_Written by an AI agent for @"access-token-user/2/9", via bb._`,
		"":                      "_Written by an AI agent via bb._",
	} {
		if got := disclosureLine(name); got != want {
			t.Errorf("disclosureLine(%q) = %q, want %q", name, got, want)
		}
	}
}

// The line ends the text once: text sent back with it, as a description read
// and edited is, keeps the one it has.
func TestTheDisclosureLineIsAddedOnce(t *testing.T) {
	t.Parallel()

	line := disclosureLine("alice")
	for text, want := range map[string]string{
		"Adds retries.":                        "Adds retries.\n\n" + line,
		"Adds retries.\n\n":                    "Adds retries.\n\n" + line,
		"":                                     line,
		"Adds retries.\n\n" + line:             "Adds retries.\n\n" + line,
		"Adds retries.\n\n  " + line + "  \n":  "Adds retries.\n\n  " + line + "  \n",
		line + "\n\nAnd logs each one.":        line + "\n\nAnd logs each one.",
		"Mentions " + line + " in a sentence.": "Mentions " + line + " in a sentence.\n\n" + line,
	} {
		if got := withDisclosure(text, line); got != want {
			t.Errorf("withDisclosure(%q) = %q, want %q", text, got, want)
		}
	}
}

// With no client to ask who the person is, the line names nobody rather than
// leaving the text unmarked.
func TestTextWrittenWithNoClientStillSaysAnAgentWroteIt(t *testing.T) {
	t.Parallel()

	if got, want := disclosed(context.Background(), Clients{}, "Adds retries."), "Adds retries.\n\n_Written by an AI agent via bb._"; got != want {
		t.Errorf("disclosed = %q, want %q", got, want)
	}
}

// Without the line, nothing the agent writes is published under the person's
// name without them (ADR-104): the tools that publish a pull request's title
// and description at once are not offered, a tag message is refused before
// any request, comments still wait in the review, and the model is told why.
func TestWithoutTheLineNothingIsPublishedUnderThePersonsName(t *testing.T) {
	t.Parallel()

	clients := testClients(t)
	clients.WithoutAIDisclosure = true
	opts := ServerOptions{Name: "bb", Version: "test", Clients: clients}

	exposed := map[string]bool{}
	for _, spec := range exposedSpecs(opts) {
		exposed[spec.Tool.Name] = true
	}
	for _, withheld := range []string{"create_pull_request", "update_pull_request"} {
		if exposed[withheld] {
			t.Errorf("%s is offered on a server that adds no line", withheld)
		}
	}
	for _, kept := range []string{"add_pr_comment", "create_tag", "prepare_pr_review"} {
		if !exposed[kept] {
			t.Errorf("%s is withheld, though it publishes nothing unattributed at once", kept)
		}
	}
	if text := instructions(opts); !strings.Contains(text, "no tool creates or edits a pull request") {
		t.Errorf("the instructions do not say why there is no pull request tool:\n%s", text)
	}
	if text := instructions(ServerOptions{Clients: testClients(t)}); strings.Contains(text, "adds no line") {
		t.Errorf("a server with the line says it has none:\n%s", text)
	}

	if got := disclosed(context.Background(), clients, "Adds retries."); got != "Adds retries." {
		t.Errorf("disclosed = %q on a server that adds no line, want the text as written", got)
	}

	session := connectWith(t, opts)
	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "create_tag", Arguments: map[string]any{
		"project": "PAY", "repo": "ledger", "name": "v1.0.0", "start_point": "main", "message": "First release.",
	}})
	if err != nil {
		t.Fatalf("create_tag: %v", err)
	}
	if text := resultText(result); !result.IsError || !strings.Contains(text, "leave message out") {
		t.Errorf("a tag message on a server that adds no line was not refused before any request: %q", text)
	}
}
