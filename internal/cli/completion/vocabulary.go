package completion

import (
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/enumflag"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/usage"
)

// placeholderKinds maps the name inside a Use line's <brackets> to what that
// argument accepts.
//
// The table is keyed by the placeholder alone, so a name has one meaning
// across the whole tree. That is what made the renames worth doing: <id> stood
// for nine different resources and <key> for two, and a table keyed by
// something ambiguous is a table that has to be keyed by command path instead
// -- which is the second list this design exists to avoid.
//
// A name that denotes something being created is KindFree. `branch delete
// <branch>` names a branch that exists; `branch create <name>` names one that
// does not, and offering the existing ones there would suggest exactly the
// values the command will reject.
var placeholderKinds = map[string]Kind{
	"access-key-id":                    KindAccessKey,
	"alias":                            KindHostAlias,
	"branch":                           KindBranch,
	"command":                          KindFree,
	"comment-id":                       KindPRComment,
	"commit":                           KindCommit,
	"condition-id":                     KindReviewerCondition,
	"description":                      KindFree,
	"directory":                        KindLocalDir,
	"emoji":                            KindEmoji,
	"endpoint":                         KindEndpoint,
	"external-id":                      KindFree,
	"from":                             KindCommit,
	"get|store|erase":                  KindGitHelperOp,
	"group":                            KindGroup,
	"host":                             KindHost,
	"id-or-fingerprint":                KindGPGKey,
	"json-config":                      KindFree,
	"key-file-or-text":                 KindLocalFile,
	"label":                            KindLabel,
	"name":                             KindFree,
	"<number> | <path> | <commit-sha>": KindRepoPath,
	// Everything after -- belongs to git, which completes its own flags.
	"-- <gitflags>":     KindFree,
	"operation-id":      KindBulkOperation,
	"path":              KindRepoPath,
	"permission":        KindPermission,
	"pr-id":             KindPullRequest,
	"PROJECT/slug":      KindRepository,
	"project-key":       KindProject,
	"ref":               KindRef,
	"report-key":        KindInsightReport,
	"repository":        KindRepository,
	"required-build-id": KindRequiredBuild,
	"restriction-id":    KindRestriction,
	"reviewer-group-id": KindReviewerGroup,
	"skill":             KindSkill,
	"ssh-key-id":        KindSSHKey,
	"status":            KindReviewStatus,
	"strategy-id":       KindMergeStrategy,
	"tag":               KindTag,
	"task-id":           KindDefaultTask,
	"to":                KindCommit,
	"token-id":          KindAccessToken,
	"url":               KindFree,
	"user-or-group":     KindUserOrGroup,
	"username":          KindUser,
	"webhook-id":        KindWebhook,
}

// flagKinds maps a flag name to what its value accepts.
//
// Keyed by name alone for the same reason, and the tree already reads that
// way: --repo is declared 37 separate times and means a repository selector
// in every one of them.
//
// Enum flags are absent on purpose. enumflag already holds the values its flag
// accepts and is the authority on them (ADR: #577), so install.go answers
// those from enumflag.Allowed without consulting this table.
var flagKinds = map[string]Kind{
	"access-key-id":              KindAccessKey,
	"at":                         KindCommit,
	"audit-file":                 KindLocalFile,
	"base":                       KindBranch,
	"base-url":                   KindFree,
	"body":                       KindFree,
	"branch":                     KindBranch,
	"build-number":               KindFree,
	"ca-file":                    KindLocalFile,
	"client-cert":                KindLocalFile,
	"client-key":                 KindLocalFile,
	"comment":                    KindFree,
	"comment-version":            KindFree,
	"commit":                     KindCommit,
	"commit-message":             KindFree,
	"config-file":                KindLocalFile,
	"content":                    KindFree,
	"count":                      KindFree,
	"credentials-username":       KindFree,
	"default-branch":             KindBranch,
	"deployment-sequence-number": KindFree,
	"description":                KindFree,
	"display-name":               KindFree,
	"duration-ms":                KindFree,
	"end-point":                  KindCommit,
	"env-key":                    KindEnvironmentKey,
	"env-name":                   KindFree,
	"env-url":                    KindFree,
	"event":                      KindWebhookEvent,
	"exclude":                    KindMCPTool,
	"expiry-days":                KindFree,
	"external-id":                KindFree,
	"field":                      KindFree,
	"file":                       KindLocalFile,
	"filter":                     KindFree,
	"from-plan":                  KindLocalFile,
	"from-ref":                   KindBranch,
	"from-repo":                  KindRepository,
	"group":                      KindGroup,
	"header":                     KindFree,
	"host":                       KindHost,
	"id":                         KindRepoComment,
	"inactivity-weeks":           KindFree,
	"index":                      KindFree,
	"input":                      KindLocalFile,
	"jira":                       KindFree,
	"key":                        KindBuildKey,
	"label":                      KindFree,
	"limit":                      KindFree,
	"line":                       KindFree,
	"link":                       KindFree,
	"log-format":                 KindLogFormat,
	"log-level":                  KindLogLevel,
	"matcher-display":            KindFree,
	"matcher-id":                 KindMatcherID,
	"message":                    KindFree,
	"name":                       KindFree,
	"output":                     KindLocalFile,
	"parent":                     KindFree,
	"parent-id":                  KindPRComment,
	"path":                       KindRepoPath,
	"permission":                 KindTokenPermission,
	"pr":                         KindPullRequest,
	"pr-version":                 KindFree,
	"prefix":                     KindFree,
	"project":                    KindProject,
	"raw-field":                  KindFree,
	"ref":                        KindCommit,
	"repo":                       KindRepository,
	"request-timeout":            KindFree,
	"retry-backoff":              KindFree,
	"retry-count":                KindFree,
	"reviewer-group":             KindReviewerGroup,
	"reviewers":                  KindUser,
	"search":                     KindFree,
	"since":                      KindCommit,
	"source-branch":              KindBranch,
	"source-commit":              KindCommit,
	"source-ref":                 KindBranch,
	"source-repo-id":             KindFree,
	"start":                      KindFree,
	"start-point":                KindCommit,
	"target-branch":              KindBranch,
	"target-ref":                 KindBranch,
	"target-repo-id":             KindFree,
	"text":                       KindFree,
	"title":                      KindFree,
	"to-ref":                     KindBranch,
	"tools":                      KindMCPTool,
	"until":                      KindCommit,
	"upstream-remote-name":       KindFree,
	"url":                        KindFree,
	"user":                       KindUser,
	"username":                   KindFree,
	"users":                      KindUser,
	"version":                    KindFree,
}

// exceptions are the slots the two tables above get wrong, keyed by command
// path and slot: "project create <project-key>", "build set --key".
//
// Every entry is a name meaning something different in one place than it does
// everywhere else, and each says which. The list is short by design -- a long
// one would mean the names are not carrying their meaning and should be
// changed instead.
var exceptions = map[string]Kind{
	// Creating names something that does not exist yet.
	"project create <project-key>": KindFree,
	"repo label add <label>":       KindFree,
	"auth alias add <alias>":       KindFree,

	// --key is a build status key under bb build, and a deployment key under
	// bb deployment.
	"deployment create --key": KindDeploymentKey,
	"deployment delete --key": KindDeploymentKey,
	"deployment get --key":    KindDeploymentKey,

	// --parent is a comment id here and a build key under bb build.
	"repo comment create --parent": KindRepoComment,

	// A local branch bb is about to create, named after the pull request's
	// source branch when it is left out.
	"pr checkout --branch": KindFree,

	// The repository is being created, so its default branch is a name, not
	// one of the branches a repository that does not exist yet does not have.
	"repo create --default-branch":       KindFree,
	"repo admin create --default-branch": KindFree,

	// An inline comment is anchored to a file the pull request changes, not
	// to any path in the repository.
	"pr comment add --path":  KindPRDiffPath,
	"pr comment list --path": KindPRDiffPath,
}

// DeclaredPositional resolves a positional argument, and reports false when
// no table covers its placeholder.
//
// Exported because the governance test asks the same question the installer
// does, of the same tables: a test that walked the tree with its own copy of
// the rules would pass while the installer did something else.
func DeclaredPositional(commandPath, placeholder string) (Kind, bool) {
	if kind, ok := exceptions[commandPath+" "+placeholder]; ok {
		return kind, true
	}

	kind, ok := placeholderKinds[usage.Name(placeholder)]

	return kind, ok
}

// DeclaredFlag resolves a flag's value.
//
// An enum flag answers for itself, so it is declared whatever the table says
// -- enumflag holds the values the flag accepts and validates against them.
func DeclaredFlag(commandPath string, flag *pflag.Flag) (Kind, bool) {
	if flag == nil {
		return "", false
	}

	if flag.Value.Type() == "bool" {
		return KindFree, true
	}

	if _, isEnum := enumflag.Allowed(flag); isEnum {
		return KindEnum, true
	}

	if kind, ok := exceptions[commandPath+" --"+flag.Name]; ok {
		return kind, true
	}

	kind, ok := flagKinds[flag.Name]

	return kind, ok
}

// commandOwnsItsCompletion reports the commands Cobra generates and answers
// for itself. bb neither declares their arguments nor should override them.
func commandOwnsItsCompletion(cmd *cobra.Command) bool {
	switch cmd.Name() {
	case "help", "completion", cobra.ShellCompRequestCmd, cobra.ShellCompNoDescRequestCmd:
		return true
	default:
		return false
	}
}
