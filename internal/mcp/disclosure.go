package mcp

import (
	"context"
	"strings"
	"sync"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/transport/httpclient"
)

// Through MCP bb writes with the person's token, under their name, and
// Bitbucket has no mark for text an AI wrote. So bb ends every comment and
// pull request description written through MCP with a line saying so (#797),
// in code, where the model cannot leave it out. The person removes it when
// they adopt the text as their own.

// disclosureLine is that line, naming the person by the mention Bitbucket
// links to their profile, or naming nobody when Bitbucket did not say who
// they are.
func disclosureLine(name string) string {
	if name == "" {
		return "_Written by an AI agent via bb._"
	}
	return "_Written by an AI agent for @" + mention(name) + ", via bb._"
}

// mention is a login name as Bitbucket's Markdown links it: bare when it is
// plain, quoted otherwise. Bare, an email-style name renders as a mail link.
func mention(name string) string {
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '_' && r != '-' {
			return `"` + name + `"`
		}
	}
	return name
}

// withDisclosure ends text with line, once: text that already carries it, as
// a description read back and sent again does, is left as it is.
func withDisclosure(text, line string) string {
	for _, existing := range strings.Split(text, "\n") {
		if strings.TrimSpace(existing) == line {
			return text
		}
	}
	trimmed := strings.TrimRight(text, " \t\r\n")
	if trimmed == "" {
		return line
	}
	return trimmed + "\n\n" + line
}

// disclosed is text with the disclosure line for the person bb acts for, or
// the text as it is on a server that adds no line.
func disclosed(ctx context.Context, c Clients, text string) string {
	if c.WithoutAIDisclosure {
		return text
	}
	return withDisclosure(text, disclosureLine(personName(ctx, c.HTTP)))
}

// publishesAtOnce are the tools that publish what the agent wrote, under the
// person's name, as soon as they run: a pull request's title and description.
// A comment waits in the person's review, and a tag message is refused, but
// these cannot wait. Without the line, only the person could okay that text
// before others read it, and nothing asks them reliably, so a server without
// the line does not offer them (ADR-104).
var publishesAtOnce = map[string]bool{"create_pull_request": true, "update_pull_request": true}

// WithheldWithoutAIDisclosure reports whether a server that adds no line keeps
// the tool from the agent.
func WithheldWithoutAIDisclosure(spec Spec) bool {
	return spec.Tool != nil && publishesAtOnce[spec.Tool.Name]
}

// personNames caches who each server's credentials belong to, keyed by its
// client: they do not change while it runs, and two servers in one process
// may act for different people.
var personNames sync.Map

// personName is the login name of the person bb acts for, or empty when
// Bitbucket does not say. A failed lookup is not kept, so the next write asks
// again.
func personName(ctx context.Context, client *httpclient.Client) string {
	if client == nil {
		return ""
	}
	if cached, ok := personNames.Load(client); ok {
		name, _ := cached.(string)
		return name
	}
	name, err := client.CurrentUserName(ctx)
	if err != nil {
		return ""
	}
	personNames.Store(client, name)
	return name
}
