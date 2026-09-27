package mcp

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
	branchservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/branch"
	commitservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/commit"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/listing"
	pullrequestservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/pullrequest"
	tagservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/tag"
)

// Completion suggests values for the arguments of a resource template or a
// prompt while the person fills them in: project keys, repository slugs, pull
// requests, paths and refs, listed the way the shell completes them.
//
// A picker asks on every keystroke, so each answer is bounded: at most
// completionValues values, as the specification allows, and at most
// completionConcurrency answers reaching Bitbucket at once for a server. That
// is the rate limit the specification requires of a server that completes: a
// request waits at most completionTimeout for its turn, and its listing takes
// at most completionTimeout once it has one.
const (
	completionValues      = 100
	completionTimeout     = 3 * time.Second
	completionConcurrency = 2
	completionPageSize    = 25
)

// completionHandler answers completion/complete.
//
// A listing that fails answers with no values rather than an error, as the
// shell does: a picker that raised an error on every keystroke while Bitbucket
// is slow would be worse than one that stays quiet. A request that names no
// template, prompt or argument of this server is an error, since no retry
// fixes it.
func completionHandler(clients Clients, resources []ResourceSpec, prompts []PromptSpec) func(context.Context, *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	slots := make(chan struct{}, completionConcurrency)

	return func(ctx context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
		variables, err := completableArguments(req.Params.Ref, resources, prompts)
		if err != nil {
			return nil, err
		}
		name := req.Params.Argument.Name
		if !slices.Contains(variables, name) {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("%q is not an argument it takes", name)}
		}

		given := map[string]string{}
		if req.Params.Context != nil {
			for argument, value := range req.Params.Context.Arguments {
				given[argument] = strings.TrimSpace(value)
			}
		}

		wait := time.NewTimer(completionTimeout)
		defer wait.Stop()
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		case <-wait.C:
			return completionResult(nil), nil
		case <-ctx.Done():
			return completionResult(nil), nil
		}

		ctx, cancel := context.WithTimeout(ctx, completionTimeout)
		defer cancel()
		values, err := complete(ctx, clients, req.Params.Ref, name, req.Params.Argument.Value, given)
		if err != nil {
			return completionResult(nil), nil
		}

		return completionResult(values), nil
	}
}

// completableArguments names the arguments of the served template or prompt a
// completion refers to.
func completableArguments(ref *mcp.CompleteReference, resources []ResourceSpec, prompts []PromptSpec) ([]string, error) {
	if ref == nil {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "the completion names no template or prompt"}
	}

	switch ref.Type {
	case "ref/resource":
		for _, spec := range resources {
			if spec.Template.URITemplate == ref.URI {
				return templateVariables(spec.Template.URITemplate), nil
			}
		}
	case "ref/prompt":
		for _, spec := range prompts {
			if spec.Prompt.Name == ref.Name {
				names := make([]string, 0, len(spec.Prompt.Arguments))
				for _, argument := range spec.Prompt.Arguments {
					names = append(names, argument.Name)
				}
				return names, nil
			}
		}
	}

	return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("this server has no %s %q", ref.Type, ref.URI+ref.Name)}
}

// complete lists the values an argument could take, given the arguments
// already chosen. An argument that depends on one not chosen yet, such as a
// repository before its project, has nothing to offer, and neither does one
// chosen as something no project or repository can be called.
func complete(ctx context.Context, c Clients, ref *mcp.CompleteReference, name, typed string, given map[string]string) ([]string, error) {
	project, repo := given["project"], given["repo"]
	if project != "" && openapi.ValidatePathSegment(project) != nil {
		return nil, nil
	}
	if repo != "" && openapi.ValidateRepository(project, repo) != nil {
		return nil, nil
	}

	switch name {
	case "project":
		projects, err := listing.Projects(ctx, c.OpenAPI, typed, completionPageSize)
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(projects))
		for _, project := range projects {
			keys = append(keys, value(project.Key))
		}
		return prefixFirst(keys, typed), nil

	case "repo":
		if project == "" {
			return nil, nil
		}
		repositories, err := listing.Repositories(ctx, c.OpenAPI, project, typed, completionPageSize)
		if err != nil {
			return nil, err
		}
		slugs := make([]string, 0, len(repositories))
		for _, repository := range repositories {
			slugs = append(slugs, value(repository.Slug))
		}
		return prefixFirst(slugs, typed), nil

	case "id":
		if project == "" || repo == "" {
			return nil, nil
		}
		if ref.Type == "ref/resource" && ref.URI == commitResource {
			return commitIDs(ctx, c, project, repo, typed)
		}
		return pullRequestIDs(ctx, c, project, repo, typed)

	case "path":
		if project == "" || repo == "" {
			return nil, nil
		}
		return paths(ctx, c, project, repo, given["at"], typed)

	case "at":
		if project == "" || repo == "" {
			return nil, nil
		}
		return refs(ctx, c, project, repo, typed)
	}

	return nil, nil
}

// pullRequestIDs offers the open pull requests of a repository, newest first.
func pullRequestIDs(ctx context.Context, c Clients, project, repo, typed string) ([]string, error) {
	pullRequests, err := pullrequestservice.NewService(c.HTTP).List(ctx,
		pullrequestservice.RepositoryRef{ProjectKey: project, Slug: repo},
		pullrequestservice.ListOptions{State: "OPEN", MaxResults: completionValues})
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(pullRequests))
	for _, pullRequest := range pullRequests {
		if id := strconv.FormatInt(pullRequest.ID, 10); strings.HasPrefix(id, strings.TrimPrefix(typed, "#")) {
			ids = append(ids, id)
		}
	}

	return ids, nil
}

// commitIDs offers the latest commits of a repository's default branch.
func commitIDs(ctx context.Context, c Clients, project, repo, typed string) ([]string, error) {
	commits, err := commitservice.NewService(c.OpenAPI).List(ctx,
		commitservice.RepositoryRef{ProjectKey: project, Slug: repo},
		commitservice.ListOptions{MaxResults: completionPageSize})
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(commits))
	for _, commit := range commits {
		if id := value(commit.Id); id != "" && strings.HasPrefix(strings.ToLower(id), strings.ToLower(typed)) {
			ids = append(ids, id)
		}
	}

	return ids, nil
}

// paths offers the entries of the directory being typed, a directory marked by
// the slash that lets the next keystroke continue into it.
func paths(ctx context.Context, c Clients, project, repo, at, typed string) ([]string, error) {
	directory, leaf := "", typed
	if slash := strings.LastIndexByte(typed, '/'); slash >= 0 {
		directory, leaf = typed[:slash+1], typed[slash+1:]
	}

	entries, err := listing.Directory(ctx, c.HTTP, project, repo, at, directory, listing.DirectoryEntries)
	if err != nil {
		return nil, err
	}

	offered := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !strings.HasPrefix(strings.ToLower(entry.Name), strings.ToLower(leaf)) {
			continue
		}
		candidate := directory + entry.Name
		if entry.Directory {
			candidate += "/"
		}
		offered = append(offered, candidate)
	}

	return offered, nil
}

// refs offers the branches and tags a typed prefix could become, branches
// first, most recently changed first.
func refs(ctx context.Context, c Clients, project, repo, typed string) ([]string, error) {
	branches, err := branchservice.NewService(c.OpenAPI).List(ctx,
		branchservice.RepositoryRef{ProjectKey: project, Slug: repo},
		branchservice.ListOptions{FilterText: typed, OrderBy: "MODIFICATION", MaxResults: completionPageSize})
	if err != nil {
		return nil, err
	}
	tags, err := tagservice.NewService(c.OpenAPI).List(ctx,
		tagservice.RepositoryRef{ProjectKey: project, Slug: repo},
		tagservice.ListOptions{FilterText: typed, OrderBy: "MODIFICATION", MaxResults: completionPageSize})
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(branches)+len(tags))
	for _, branch := range branches {
		names = append(names, value(branch.DisplayId))
	}
	for _, tag := range tags {
		names = append(names, value(tag.DisplayId))
	}

	return prefixFirst(names, typed), nil
}

// prefixFirst orders values that start with what was typed before the rest,
// drops empty values and duplicates, and keeps the listing's order otherwise.
// The rest are kept because Bitbucket matched them by name: "Platform
// Services" is project PS, and a person typing "Plat" means it.
func prefixFirst(values []string, typed string) []string {
	seen := map[string]bool{}
	var leading, trailing []string
	for _, candidate := range values {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if strings.HasPrefix(strings.ToLower(candidate), strings.ToLower(typed)) {
			leading = append(leading, candidate)
		} else {
			trailing = append(trailing, candidate)
		}
	}

	return append(leading, trailing...)
}

// completionResult caps an answer at completionValues, and says how many there
// were when it had to.
func completionResult(values []string) *mcp.CompleteResult {
	result := &mcp.CompleteResult{Completion: mcp.CompletionResultDetails{Values: []string{}}}
	if len(values) > completionValues {
		result.Completion.Total = len(values)
		result.Completion.HasMore = true
		values = values[:completionValues]
	}
	result.Completion.Values = append(result.Completion.Values, values...)

	return result
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}

	return strings.TrimSpace(*pointer)
}
