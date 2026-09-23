package completion

import (
	"context"
	"strings"

	aicmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/ai"
	projectcmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/project"
	repocmd "github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/repo"
	bbmcp "github.com/vriesdemichael/bitbucket-data-center-cli/internal/mcp"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi"
)

func init() {
	register(KindTokenPermission, tokenPermissionSource)
	register(KindWebhookEvent, webhookEventSource)
	register(KindPermission, permissionSource)
	register(KindMergeStrategy, mergeStrategySource)
	register(KindSkill, skillSource)
	register(KindMCPTool, mcpToolSource)
}

// The vocabularies in this file are closed sets somebody else fixed: Bitbucket
// for a token permission and a webhook event, bb for its own skills and MCP
// tools. None of them can be an enum flag. enumflag holds one string and
// validates the whole of it, and every slot here is either a positional --
// which has no pflag.Value to hang a validator on -- or a list, where
// "REPO_READ,REPO_WRITE" is a correct value that enumflag would refuse
// wholesale. So each is a source, and where the same set is written down twice
// a governance test holds the copies together.

// TokenRepositoryPermissions and TokenProjectPermissions are what
// `bb auth token create --permission` accepts, read off a running Bitbucket
// rather than from the spec: RestAccessTokenRequest types permissions as a
// bare []string, and the server is the only thing that knows the set.
//
// Bitbucket refuses everything else at the endpoint -- "Tokens may not have
// the following permission: ADMIN" for an instance permission, "ACCOUNT_READ
// is not a valid permission" for a name it does not know -- so an invented
// value here is a completion that produces a 400 rather than a token.
//
// They are two slices because the scope narrows the set. A repository-scoped
// token takes only the repository three; a user- or project-scoped token takes
// all six.
var (
	TokenRepositoryPermissions = []string{"REPO_READ", "REPO_WRITE", "REPO_ADMIN"}
	TokenProjectPermissions    = []string{"PROJECT_READ", "PROJECT_WRITE", "PROJECT_ADMIN"}
)

var tokenPermissionDescriptions = map[string]string{
	"REPO_READ":     "Read repositories in the token's scope",
	"REPO_WRITE":    "Push to repositories in the token's scope",
	"REPO_ADMIN":    "Administer repositories in the token's scope",
	"PROJECT_READ":  "Read every repository in the project",
	"PROJECT_WRITE": "Push to every repository in the project",
	"PROJECT_ADMIN": "Administer the project",
}

// tokenPermissionSource offers the permissions a token of this scope can hold.
//
// The scope is the --user / --project / --repo flag on `bb auth token`, which
// is persistent and therefore readable from the command being completed. It
// matters: PROJECT_READ on a repository-scoped token is refused with
// "Permissions for REPOSITORY scope must be one of PROJECT_READ", a message
// that helps nobody, and the three values it does accept are the ones offered
// instead.
func tokenPermissionSource(_ context.Context, _ *Environment, request Request) (Result, error) {
	allowed := TokenRepositoryPermissions
	if !repositoryScopedToken(request) {
		allowed = append(append([]string{}, TokenRepositoryPermissions...), TokenProjectPermissions...)
	}

	return describedList(allowed, tokenPermissionDescriptions), nil
}

// repositoryScopedToken reports a `bb auth token` invocation aimed at one
// repository.
//
// --repo alone, because the three scope flags are exclusive and the command
// refuses a line that names more than one: with --project also given there is
// no scope to narrow to, and the full set is the honest answer.
func repositoryScopedToken(request Request) bool {
	if request.Command == nil {
		return false
	}

	named := func(name string) bool {
		flag := request.Command.Flags().Lookup(name)

		return flag != nil && strings.TrimSpace(flag.Value.String()) != ""
	}

	return named("repo") && !named("project") && !named("user")
}

// WebhookEvents are the event keys a Bitbucket webhook can subscribe to.
//
// Written down because nothing serves them: there is no endpoint that lists
// the events, so the set was established by creating a webhook for each key
// against a live instance and reading the subscription back.
//
// Doing that rather than transcribing the documentation is what this list is
// worth. Atlassian's event payload page documents pr:to_ref_updated and
// pr:reviewer:changes_requested; the server rejects both with "the event
// ${validatedValue} is unknown", and the key it does take for the third
// reviewer outcome is pr:reviewer:needs_work. Bitbucket validates the key, so
// a documented-but-wrong suggestion here fails the whole create.
//
// Ordered by what a webhook is usually for rather than alphabetically, and the
// order is kept: the push event comes first because it is the default the
// create commands carry.
var WebhookEvents = []string{
	"repo:refs_changed",
	"repo:modified",
	"repo:forked",
	"repo:comment:added",
	"repo:comment:edited",
	"repo:comment:deleted",
	"repo:secret_detected",
	"mirror:repo_synchronized",
	"project:modified",
	"pr:opened",
	"pr:from_ref_updated",
	"pr:modified",
	"pr:merged",
	"pr:declined",
	"pr:deleted",
	"pr:reviewer:updated",
	"pr:reviewer:approved",
	"pr:reviewer:unapproved",
	"pr:reviewer:needs_work",
	"pr:comment:added",
	"pr:comment:edited",
	"pr:comment:deleted",
}

// repo:secret_detected is the reason for the nolint: gosec reads an entry
// whose key says "secret" as a credential in source, and this one is the name
// of an event and a sentence about it.
//
//nolint:gosec // G101: an event key and its description, not a credential
var webhookEventDescriptions = map[string]string{
	"repo:refs_changed":        "A branch or tag was pushed, created or deleted",
	"repo:modified":            "The repository was renamed or moved",
	"repo:forked":              "The repository was forked",
	"repo:comment:added":       "A commit comment was added",
	"repo:comment:edited":      "A commit comment was edited",
	"repo:comment:deleted":     "A commit comment was deleted",
	"repo:secret_detected":     "A push carried something that looks like a secret",
	"mirror:repo_synchronized": "A mirror finished synchronising the repository",
	"project:modified":         "The project was renamed or its key changed",
	"pr:opened":                "A pull request was opened",
	"pr:from_ref_updated":      "The source branch of a pull request moved",
	"pr:modified":              "A pull request's title, description or target changed",
	"pr:merged":                "A pull request was merged",
	"pr:declined":              "A pull request was declined",
	"pr:deleted":               "A pull request was deleted",
	"pr:reviewer:updated":      "The reviewers of a pull request changed",
	"pr:reviewer:approved":     "A reviewer approved a pull request",
	"pr:reviewer:unapproved":   "A reviewer withdrew their approval",
	"pr:reviewer:needs_work":   "A reviewer marked a pull request as needing work",
	"pr:comment:added":         "A pull request comment was added",
	"pr:comment:edited":        "A pull request comment was edited",
	"pr:comment:deleted":       "A pull request comment was deleted",
}

func webhookEventSource(_ context.Context, _ *Environment, _ Request) (Result, error) {
	return describedList(WebhookEvents, webhookEventDescriptions), nil
}

var permissionDescriptions = map[string]string{
	"PROJECT_READ":  "Read every repository in the project",
	"PROJECT_WRITE": "Push to every repository in the project",
	"PROJECT_ADMIN": "Administer the project and its repositories",
	"REPO_READ":     "Clone and read the repository",
	"REPO_WRITE":    "Push to the repository",
	"REPO_ADMIN":    "Administer the repository",
}

// permissionSource offers the permissions the grant being completed accepts.
//
// The two sets share a placeholder and nothing else: `bb project permissions
// grant` takes PROJECT_*, `bb repo permissions grant` takes REPO_*, and each
// refuses the other's. Which one is being completed is read from the command,
// because the placeholder cannot say -- the alternative is renaming the
// argument in one of the two, and <project-permission> reads worse in help
// text than the wrong suggestion costs.
//
// Both slices come from the command packages that enforce them, so this
// answers with exactly what enumflag.Value will accept a moment later.
func permissionSource(_ context.Context, _ *Environment, request Request) (Result, error) {
	allowed := projectcmd.PermissionNames
	if topLevelCommand(request) == "repo" {
		allowed = repocmd.PermissionNames
	}

	return describedList(allowed, permissionDescriptions), nil
}

var mergeStrategyDescriptions = map[string]string{
	"no-ff":          "Always create a merge commit",
	"ff":             "Fast-forward when possible, merge commit otherwise",
	"ff-only":        "Fast-forward, or refuse the merge",
	"rebase-no-ff":   "Rebase the source, then always create a merge commit",
	"rebase-ff-only": "Rebase the source, then fast-forward or refuse",
	"squash":         "Squash to one commit, fast-forward when possible",
	"squash-ff-only": "Squash to one commit, or refuse the merge",
}

// mergeStrategySource offers the ids `bb repo settings pull-requests
// set-strategy` takes.
//
// openapi.MergeStrategies rather than a list of its own, because the same
// vocabulary is already the enum behind `bb pr auto-merge enable --strategy`.
// TestTheStrategyArgumentMatchesTheFlagThatTakesTheSameValues holds the
// argument to that flag, which is where the ids would otherwise drift: four
// disagreeing copies of this set is what #577 found.
func mergeStrategySource(_ context.Context, _ *Environment, _ Request) (Result, error) {
	return describedList(openapi.MergeStrategies, mergeStrategyDescriptions), nil
}

// skillSource offers the agent skills this binary carries.
//
// From the registry `bb ai skill show` resolves against, so a skill added
// there is completed without anything here changing.
func skillSource(_ context.Context, _ *Environment, _ Request) (Result, error) {
	candidates := make([]Candidate, 0, len(aicmd.Skills))
	for _, skill := range aicmd.Skills {
		candidates = append(candidates, Candidate{Value: skill.Name, Description: skill.Summary})
	}

	return Result{Candidates: candidates, KeepOrder: true}, nil
}

// mcpToolSource offers the tools `bb ai mcp serve` can expose, for the
// allowlist and the denylist alike.
//
// Both flags take every tool. --tools overrides the safety filter rather than
// intersecting with it, so a tool withheld without --yolo is a legitimate
// thing to name there, and --exclude has nothing to gain from a narrower set.
// The description says which are which.
//
// The commas are handled here rather than by the caller. --tools and --exclude
// are plain strings that the command splits itself, so pflag reports their
// type as "string" and the installer's multi-value path -- which is what
// splits the word at the last comma and carries the finished elements back --
// never runs for them. Without this a second name would complete against the
// whole of "list_branches,lis".
func mcpToolSource(_ context.Context, _ *Environment, request Request) (Result, error) {
	prefix, word := beforeLastComma(request.ToComplete)

	chosen := map[string]bool{}
	for _, name := range strings.Split(prefix, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			chosen[trimmed] = true
		}
	}

	specs := bbmcp.AllSpecs()
	candidates := make([]Candidate, 0, len(specs))
	for _, spec := range specs {
		if spec.Tool == nil || chosen[spec.Tool.Name] {
			continue
		}
		if !strings.HasPrefix(spec.Tool.Name, word) {
			continue
		}

		candidates = append(candidates, Candidate{
			Value:       prefix + spec.Tool.Name,
			Description: describeTool(spec),
		})
	}

	// NoSpace because a list is rarely one element, and the cursor sitting
	// against the value is what lets the next comma be typed without moving
	// it back.
	return Result{Candidates: candidates, NoSpace: true, KeepOrder: true}, nil
}

// describeTool says when a tool is available before saying what it does. The
// exposure is the question somebody building an allowlist has, and a
// description is cut at the end rather than the start.
func describeTool(spec bbmcp.Spec) string {
	description := firstSentence(spec.Tool.Description)
	if spec.Safe {
		return description
	}

	return strings.TrimSpace("needs --yolo. " + description)
}

func firstSentence(description string) string {
	if cut := strings.IndexRune(description, '.'); cut >= 0 {
		return strings.TrimSpace(description[:cut+1])
	}

	return strings.TrimSpace(description)
}

// beforeLastComma splits a comma-separated value into the part already
// finished, comma included, and the element being typed.
//
// The finished part is carried back on every candidate because a shell
// replaces the whole word: returning just the tool name would leave the line
// reading --tools get_commit with everything before it gone.
func beforeLastComma(toComplete string) (prefix, word string) {
	cut := strings.LastIndex(toComplete, ",")
	if cut < 0 {
		return "", toComplete
	}

	return toComplete[:cut+1], toComplete[cut+1:]
}

// topLevelCommand is the first word of the command being completed -- project,
// repo, pr -- which is what distinguishes two slots that share a placeholder.
func topLevelCommand(request Request) string {
	if request.Command == nil {
		return ""
	}

	path := strings.Fields(commandPath(request.Command))
	if len(path) == 0 {
		return ""
	}

	return path[0]
}

// describedList answers from a fixed set, each value carrying what it means.
//
// Like fixedSource, but with a description per value rather than one for the
// whole set: these are vocabularies whose values are opaque on their own --
// rebase-ff-only, pr:from_ref_updated -- and the description is the difference
// between a list and a list somebody can choose from.
func describedList(values []string, descriptions map[string]string) Result {
	candidates := make([]Candidate, 0, len(values))
	for _, value := range values {
		candidates = append(candidates, Candidate{Value: value, Description: descriptions[value]})
	}

	return Result{Candidates: candidates, KeepOrder: true}
}
