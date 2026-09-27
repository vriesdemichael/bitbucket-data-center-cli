package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// PromptSpec is one prompt: a request the person picks in the client, often
// as a slash command, that brings the Bitbucket content it is about with it.
//
// The content is embedded rather than linked, so the model has it whichever
// client it runs in, read through the same code as the resources and scoped
// and audited like them.
type PromptSpec struct {
	Prompt *mcp.Prompt

	// Tools are the tools whose answers the prompt embeds. The prompt is
	// served only while all of them are exposed, as a resource template is.
	Tools []string

	get func(ctx context.Context, c Clients, arguments map[string]string) (*mcp.GetPromptResult, error)
}

// AllPromptSpecs returns every prompt bb has. A server serves those whose
// tools it exposes (servedPromptSpecs).
func AllPromptSpecs() []PromptSpec {
	return []PromptSpec{
		{
			Prompt: &mcp.Prompt{
				Name:  "review_pull_request",
				Title: "Review a pull request",
				Description: "Review a pull request for defects, risky changes and missing tests, with its details, " +
					"its diff and its open review threads attached.",
				Arguments: pullRequestPromptArguments(),
			},
			Tools: []string{"get_pull_request", "get_pr_diff", "list_pr_comments"},
			get:   reviewPullRequestPrompt,
		},
		{
			Prompt: &mcp.Prompt{
				Name:  "explain_pull_request",
				Title: "Explain a pull request",
				Description: "Explain what a pull request changes and why, for someone who has not seen it, with its " +
					"details and its diff attached.",
				Arguments: pullRequestPromptArguments(),
			},
			Tools: []string{"get_pull_request", "get_pr_diff"},
			get:   explainPullRequestPrompt,
		},
	}
}

// servedPromptSpecs are the prompts a server serves: those whose tools it all
// exposes.
func servedPromptSpecs(exposed map[string]bool) []PromptSpec {
	var served []PromptSpec
	for _, spec := range AllPromptSpecs() {
		all := true
		for _, tool := range spec.Tools {
			all = all && exposed[tool]
		}
		if all {
			served = append(served, spec)
		}
	}

	return served
}

func pullRequestPromptArguments() []*mcp.PromptArgument {
	return []*mcp.PromptArgument{
		{Name: "project", Title: "Project", Description: "Bitbucket project key", Required: true},
		{Name: "repo", Title: "Repository", Description: "Repository slug", Required: true},
		{Name: "id", Title: "Pull request", Description: "Pull request ID", Required: true},
	}
}

// promptHandler answers prompts/get for one prompt.
func promptHandler(spec PromptSpec, clients Clients) mcp.PromptHandler {
	return func(ctx context.Context, req *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
		arguments := map[string]string{}
		for name, value := range req.Params.Arguments {
			arguments[name] = strings.TrimSpace(value)
		}
		for _, argument := range spec.Prompt.Arguments {
			if argument.Required && arguments[argument.Name] == "" {
				return nil, &jsonrpc.Error{
					Code:    jsonrpc.CodeInvalidParams,
					Message: fmt.Sprintf("%s needs the argument %s", spec.Prompt.Name, argument.Name),
				}
			}
		}

		return spec.get(ctx, clients, arguments)
	}
}

func reviewPullRequestPrompt(ctx context.Context, c Clients, arguments map[string]string) (*mcp.GetPromptResult, error) {
	project, repo, id := arguments["project"], arguments["repo"], arguments["id"]
	ref := func(kind string) string {
		return resourceURI(resourceRef{kind: kind, project: project, repo: repo, id: id})
	}

	return pullRequestPrompt(ctx, c,
		fmt.Sprintf("Review of pull request #%s in %s/%s", id, project, repo),
		fmt.Sprintf("Review pull request #%s in %s/%s. Its details, its diff and its open review threads follow.\n\n"+
			"Look for defects, risky changes and missing tests, and say how sure you are of each. Cite the file and the "+
			"line in the diff for every point. Leave out what an open thread already raises.", id, project, repo),
		ref(pullRequestKind),
		ref(pullRequestDiffKind),
		ref(pullRequestThreadsKind),
	)
}

func explainPullRequestPrompt(ctx context.Context, c Clients, arguments map[string]string) (*mcp.GetPromptResult, error) {
	project, repo, id := arguments["project"], arguments["repo"], arguments["id"]
	ref := func(kind string) string {
		return resourceURI(resourceRef{kind: kind, project: project, repo: repo, id: id})
	}

	return pullRequestPrompt(ctx, c,
		fmt.Sprintf("Explanation of pull request #%s in %s/%s", id, project, repo),
		fmt.Sprintf("Explain pull request #%s in %s/%s to someone who has not seen it. Its details and its diff follow.\n\n"+
			"Say what problem it addresses, how it goes about it, and what a reviewer should look at first.", id, project, repo),
		ref(pullRequestKind),
		ref(pullRequestDiffKind),
	)
}

// pullRequestPrompt is the instruction, then each resource embedded as its
// own message.
func pullRequestPrompt(ctx context.Context, c Clients, description, instruction string, uris ...string) (*mcp.GetPromptResult, error) {
	messages := []*mcp.PromptMessage{{Role: "user", Content: &mcp.TextContent{Text: instruction}}}

	for _, uri := range uris {
		spec, ref, ok := matchResource(uri)
		if !ok {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		if err := spec.validate(ref); err != nil {
			return nil, invalidResource(uri, err)
		}
		contents, err := spec.read(ctx, c, ref)
		if err != nil {
			return nil, resourceError(uri, err)
		}
		for _, content := range contents {
			messages = append(messages, &mcp.PromptMessage{Role: "user", Content: &mcp.EmbeddedResource{Resource: content}})
		}
	}

	return &mcp.GetPromptResult{Description: description, Messages: messages}, nil
}
