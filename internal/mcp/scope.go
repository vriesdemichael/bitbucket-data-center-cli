package mcp

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Scope confines an MCP server to one project, or to one repository within it.
//
// The zero value is unscoped, which is the default and what every existing
// deployment gets. ADR-039 already scopes a server to one Bitbucket instance
// with --host and to one token's rights with --token; this bounds it further,
// to the part of that instance an agent has business touching.
type Scope struct {
	ProjectKey string
	RepoSlug   string
}

// IsSet reports whether any scoping is in effect.
func (s Scope) IsSet() bool {
	return strings.TrimSpace(s.ProjectKey) != "" || strings.TrimSpace(s.RepoSlug) != ""
}

// String renders the scope for error messages and audit records.
func (s Scope) String() string {
	switch {
	case !s.IsSet():
		return ""
	case s.RepoSlug == "":
		return s.ProjectKey
	default:
		return s.ProjectKey + "/" + s.RepoSlug
	}
}

// ParseScope builds a Scope from the --project and --repo flag values.
//
// --repo accepts PROJECT/slug, or a bare slug when --project supplies the
// project. A bare slug with no project is rejected rather than guessed: a
// repository slug is not unique across projects, so guessing would silently
// scope to a repository the operator did not name.
func ParseScope(project, repo string) (Scope, error) {
	project = strings.TrimSpace(project)
	repo = strings.TrimSpace(repo)

	if repo == "" {
		return Scope{ProjectKey: project}, nil
	}

	if projectPart, slugPart, found := strings.Cut(repo, "/"); found {
		projectPart = strings.TrimSpace(projectPart)
		slugPart = strings.TrimSpace(slugPart)
		if projectPart == "" || slugPart == "" {
			return Scope{}, fmt.Errorf("--repo %q is not a PROJECT/slug pair", repo)
		}
		if project != "" && !strings.EqualFold(project, projectPart) {
			return Scope{}, fmt.Errorf("--repo %q names project %q, which conflicts with --project %q", repo, projectPart, project)
		}
		return Scope{ProjectKey: projectPart, RepoSlug: slugPart}, nil
	}

	if project == "" {
		return Scope{}, fmt.Errorf("--repo %q needs a project: pass --project, or write it as PROJECT/%s", repo, repo)
	}
	return Scope{ProjectKey: project, RepoSlug: repo}, nil
}

// scopeRule says how a tool's arguments relate to a project and repository.
//
// Every tool needs a rule, and scopeRuleFor has no default: a tool added
// without one fails TestEveryToolHasAScopeRule rather than silently escaping
// the boundary. That is the failure mode worth designing against, because a
// guard that skips the check when it cannot find a project argument reads like
// enforcement in review while permitting everything it does not understand.
type scopeRule int

const (
	// scopeProjectRepo: the tool takes project and repo, and both are required.
	// Under a scope they are injected when absent and rejected when they name
	// something else.
	scopeProjectRepo scopeRule = iota

	// scopeOptionalProjectRepo: the tool takes project and repo, but works
	// without them by widening to every repository the token can see.
	// list_pull_requests does this — omitting both selects dashboard mode.
	// Under a scope the arguments are injected, which turns the unbounded mode
	// into the bounded one rather than refusing the call.
	scopeOptionalProjectRepo

	// scopeProjectFilter: the tool takes project as a filter and has no repo
	// argument. Under a project scope the filter is pinned to it. Under a
	// repository scope the tool is withheld: pinning the project still lists
	// sibling repositories, and a filter the handler applies afterwards is not
	// a boundary.
	scopeProjectFilter

	// scopeUnbounded: the tool addresses a resource that is not project-scoped
	// in Bitbucket's API at all. Build statuses hang off a commit SHA, which is
	// global. There is no argument to constrain, so under any scope the tool is
	// withheld. Withholding it is the honest outcome; pretending a commit SHA
	// can be bounded to a project would be the dishonest one.
	scopeUnbounded
)

// scopeRules maps every tool to how it can be bounded.
//
// Hand-maintained, and pinned in both directions by TestEveryToolHasAScopeRule
// so it cannot drift from AllSpecs the way ADR-039's tool list once did.
var scopeRules = map[string]scopeRule{
	"get_pull_request":          scopeProjectRepo,
	"list_pull_requests":        scopeOptionalProjectRepo,
	"create_pull_request":       scopeProjectRepo,
	"update_pull_request":       scopeProjectRepo,
	"list_pr_comments":          scopeProjectRepo,
	"get_pr_diff":               scopeProjectRepo,
	"get_file_content":          scopeProjectRepo,
	"add_pr_comment":            scopeProjectRepo,
	"submit_pr_review":          scopeProjectRepo,
	"merge_pull_request":        scopeProjectRepo,
	"enable_auto_merge":         scopeProjectRepo,
	"disable_auto_merge":        scopeProjectRepo,
	"search_repositories":       scopeProjectFilter,
	"get_repository_clone_info": scopeProjectRepo,
	"list_branches":             scopeProjectRepo,
	"resolve_ref":               scopeProjectRepo,
	"list_tags":                 scopeProjectRepo,
	"create_tag":                scopeProjectRepo,
	"get_build_status":          scopeUnbounded,
	"set_build_status":          scopeUnbounded,
	"list_required_builds":      scopeProjectRepo,
	"list_commits":              scopeProjectRepo,
	"get_commit":                scopeProjectRepo,
	"compare_refs":              scopeProjectRepo,
}

// withheldUnderScope reports whether a scope removes a tool altogether.
//
// A withheld tool is dropped from tools/list as well as refused on call. An
// agent offered a tool that always fails wastes a turn discovering that, and a
// catalogue that advertises what the server will not do is a worse description
// of the server than one that does not.
func withheldUnderScope(toolName string, scope Scope) bool {
	if !scope.IsSet() {
		return false
	}
	switch scopeRules[toolName] {
	case scopeUnbounded:
		return true
	case scopeProjectFilter:
		return scope.RepoSlug != ""
	default:
		return false
	}
}

// applyScope rewrites a tool call's arguments so they fall inside the scope, or
// reports why they cannot.
//
// Injecting the scope when an argument is absent rather than demanding the
// agent supply it is deliberate: the agent does not need to know the server is
// bounded, and a call it would have made unbounded becomes a bounded one
// instead of an error it has to interpret.
func applyScope(toolName string, arguments json.RawMessage, scope Scope) (json.RawMessage, error) {
	if !scope.IsSet() {
		return arguments, nil
	}

	rule, ok := scopeRules[toolName]
	if !ok {
		// Unreachable through AllSpecs, which the tests pin. Refusing rather
		// than passing through keeps the failure closed if it ever is reached.
		return nil, fmt.Errorf("tool %q has no scope rule; refusing to dispatch it under --project/--repo", toolName)
	}

	if withheldUnderScope(toolName, scope) {
		switch rule {
		case scopeUnbounded:
			return nil, fmt.Errorf("%s is not available when the server is scoped to %s: it addresses a commit, which Bitbucket does not scope to a project", toolName, scope)
		default:
			return nil, fmt.Errorf("%s is not available when the server is scoped to a single repository", toolName)
		}
	}

	decoded := map[string]any{}
	if len(arguments) > 0 {
		if err := json.Unmarshal(arguments, &decoded); err != nil {
			return nil, fmt.Errorf("cannot read tool arguments to enforce scoping: %w", err)
		}
	}

	if err := bindScopedArgument(decoded, "project", scope.ProjectKey, scope); err != nil {
		return nil, err
	}
	if rule != scopeProjectFilter {
		if err := bindScopedArgument(decoded, "repo", scope.RepoSlug, scope); err != nil {
			return nil, err
		}
	}

	rewritten, err := json.Marshal(decoded)
	if err != nil {
		return nil, fmt.Errorf("cannot rewrite tool arguments for scoping: %w", err)
	}
	return rewritten, nil
}

// bindScopedArgument pins one argument to the scope, rejecting a conflict.
//
// An empty want means the scope does not constrain this argument — a project
// scope leaves repo free — so the argument is left as the caller supplied it.
func bindScopedArgument(arguments map[string]any, key, want string, scope Scope) error {
	if want == "" {
		return nil
	}

	raw, present := arguments[key]
	if !present {
		arguments[key] = want
		return nil
	}

	got, ok := raw.(string)
	if !ok {
		return fmt.Errorf("%s must be a string to be checked against the server scope %s", key, scope)
	}
	if strings.TrimSpace(got) == "" {
		arguments[key] = want
		return nil
	}
	if !strings.EqualFold(strings.TrimSpace(got), want) {
		return fmt.Errorf("access denied: %s %q is outside the scope this MCP server is confined to (%s)", key, got, scope)
	}

	// Normalise to the configured spelling so the audit record and the API call
	// agree on one form.
	arguments[key] = want
	return nil
}

// scopedTarget extracts the project and repository a call is aimed at, for the
// audit record. It reads the arguments after scoping has been applied, so the
// record shows what was actually dispatched.
func scopedTarget(arguments json.RawMessage) (project, repo string) {
	if len(arguments) == 0 {
		return "", ""
	}
	decoded := map[string]any{}
	if err := json.Unmarshal(arguments, &decoded); err != nil {
		return "", ""
	}
	project, _ = decoded["project"].(string)
	repo, _ = decoded["repo"].(string)
	return project, repo
}
