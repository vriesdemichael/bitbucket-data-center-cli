package repocmd

import (
	"github.com/vriesdemichael/bitbucket-server-cli/internal/cli/result"
)

// SingleRepository is what `bb repo create`, `fork`, `admin create`,
// `admin fork` and `admin update` return.
type SingleRepository struct {
	Repository result.RepositoryDetail `json:"repository"`
}

// RepositoryDeletion is what `bb repo delete` and `bb repo admin delete`
// report.
type RepositoryDeletion struct {
	result.Status
	Repository result.Repository `json:"repository"`
}

// Tree is what `bb repo browse tree` returns.
type Tree struct {
	Repository result.Repository `json:"repository"`
	Path       string            `json:"path" jsonschema:"Directory that was listed, empty for the repository root."`
	Files      []string          `json:"files" jsonschema:"Paths under that directory, relative to the repository root. Empty rather than absent when there are none."`
}

// FileLine is one line of a file, with its attribution when blame was asked
// for.
type FileLine struct {
	Text   string `json:"text" jsonschema:"The line, without its terminator."`
	Author string `json:"author,omitempty" jsonschema:"Who last changed it. Only present for bb repo browse blame."`
}

// FileContent is what `bb repo browse file` and `bb repo browse blame` return.
//
// Both commands read the same endpoint, one with blame turned on, so both
// answer with lines. Before this the JSON path published whatever Bitbucket
// sent and the human path decoded one particular shape out of it, so the two
// renderings of the same response did not agree and neither had a contract.
//
// binary and complete are published because without them an empty lines array
// is three different answers wearing one face: an empty file, a binary file
// Bitbucket declined to render, and the first page of a file longer than the
// endpoint returns at once. The raw passthrough this replaced carried the
// upstream's binary and isLastPage markers, so dropping them lost information a
// caller had.
type FileContent struct {
	Repository result.Repository `json:"repository"`
	Path       string            `json:"path" jsonschema:"File that was read."`
	At         string            `json:"at,omitempty" jsonschema:"Commit or ref it was read at, when --at was given."`
	Binary     bool              `json:"binary" jsonschema:"Whether Bitbucket refused to render the file as text. True means lines is empty because the file is binary, not because it is empty."`
	Complete   bool              `json:"complete" jsonschema:"Whether lines is the whole file. False means Bitbucket returned one page of a longer file and the rest was not fetched."`
	Lines      []FileLine        `json:"lines" jsonschema:"The file's lines, in order. Empty rather than absent for an empty file -- and also empty when binary is true, which is why that field exists."`
}

// RawFile is what `bb repo cat` and `bb repo browse raw` return under --json.
//
// encoding exists because a JSON string cannot hold arbitrary bytes: Go's
// encoder replaces invalid UTF-8 with U+FFFD, which would return a corrupted
// file and say nothing about it. A file that is not valid UTF-8 is base64
// instead, and encoding says which was used so a caller does not have to guess.
type RawFile struct {
	Repository result.Repository `json:"repository"`
	Path       string            `json:"path" jsonschema:"File that was read."`
	At         string            `json:"at,omitempty" jsonschema:"Commit or ref it was read at, when --at was given."`
	Encoding   string            `json:"encoding" jsonschema:"How content is encoded: utf-8 for text, base64 for anything that is not valid UTF-8."`
	Content    string            `json:"content" jsonschema:"The file. Without --json the bytes go to stdout unwrapped, whatever they are."`
}

// FileHistory is what `bb repo browse history` returns.
type FileHistory struct {
	Repository result.Repository `json:"repository"`
	Path       string            `json:"path" jsonschema:"File the history is for."`
	Commits    []result.Commit   `json:"commits" jsonschema:"Commits that touched it, newest first. Empty rather than absent when there are none."`
}

// CloneUpstream is the remote a fork's clone points at its parent.
type CloneUpstream struct {
	Configured bool   `json:"configured" jsonschema:"Whether the remote was added. False for a repository that is not a fork, for one whose parent you cannot read, and whenever --no-upstream was passed."`
	Name       string `json:"name" jsonschema:"Remote name that was used or would have been."`
	URL        string `json:"url" jsonschema:"Parent repository's clone URL. Empty when there is no parent to point at."`
}

// CloneInput echoes back how the repository was named.
type CloneInput struct {
	Raw     string `json:"raw" jsonschema:"The argument as it was typed."`
	UsedURL bool   `json:"usedUrl" jsonschema:"Whether that argument was a clone URL rather than a PROJECT/slug."`
}

// Clone is what `bb repo clone` and `bb clone` report.
type Clone struct {
	result.Status
	Repository      result.Repository `json:"repository"`
	CloneURL        string            `json:"cloneUrl" jsonschema:"URL git was pointed at."`
	Directory       string            `json:"directory" jsonschema:"Directory the working tree landed in."`
	NoUpstream      bool              `json:"noUpstream" jsonschema:"Whether --no-upstream was passed, which stops bb looking for a parent at all."`
	Upstream        CloneUpstream     `json:"upstream"`
	RepositoryInput CloneInput        `json:"repositoryInput"`
}

// CommentContext says which commit or pull request a repository comment is on.
type CommentContext struct {
	Type          string `json:"type" jsonschema:"commit or pull_request."`
	ProjectKey    string `json:"projectKey" jsonschema:"Project the repository belongs to."`
	Slug          string `json:"slug" jsonschema:"Repository slug."`
	CommitID      string `json:"commitId,omitempty" jsonschema:"Commit the comment is on. Present when type is commit."`
	PullRequestID string `json:"pullRequestId,omitempty" jsonschema:"Pull request the comment is on. Present when type is pull_request."`
}

// Comments is what `bb repo comment list` returns.
type Comments struct {
	Context  CommentContext `json:"context"`
	Comments []Comment      `json:"comments" jsonschema:"Comments in that context. Empty rather than absent when there are none."`
}

// SingleComment is what `bb repo comment create` and `update` return.
type SingleComment struct {
	Context CommentContext `json:"context"`
	Comment Comment        `json:"comment"`
}

// CommentDeletion is what `bb repo comment delete` reports.
type CommentDeletion struct {
	result.Status
	Context CommentContext `json:"context"`
	ID      string         `json:"id" jsonschema:"Identifier of the comment that was deleted, as it was given on the command line."`
	Version *int           `json:"version,omitempty" jsonschema:"Version the delete was performed against. Absent when Bitbucket did not report one."`
}

// CommentAnchor locates a repository comment in a diff.
type CommentAnchor struct {
	Path     string `json:"path,omitempty" jsonschema:"File the comment is anchored to."`
	SrcPath  string `json:"srcPath,omitempty" jsonschema:"Path before a rename, when the file was renamed."`
	Line     int32  `json:"line,omitempty" jsonschema:"Line within that file."`
	LineType string `json:"lineType,omitempty" jsonschema:"ADDED, REMOVED or CONTEXT."`
	FileType string `json:"fileType,omitempty" jsonschema:"FROM or TO, which side of the diff the line is on."`
	DiffType string `json:"diffType,omitempty" jsonschema:"COMMIT, EFFECTIVE or RANGE."`
	FromHash string `json:"fromHash,omitempty" jsonschema:"Commit the diff was taken from."`
	ToHash   string `json:"toHash,omitempty" jsonschema:"Commit the diff was taken to."`
}

// Comment is one comment on a commit or a pull request.
//
// The upstream object nests the entire pull request under its anchor, and every
// reply nests the same again. Only what identifies and locates the comment is
// published.
type Comment struct {
	ID           int64          `json:"id,omitempty" jsonschema:"Comment identifier."`
	Version      int32          `json:"version" jsonschema:"Optimistic-locking version. Pass it back when updating or deleting, or the call is refused. Always present: a never-edited comment is at version 0."`
	Text         string         `json:"text,omitempty" jsonschema:"The comment text."`
	State        string         `json:"state,omitempty" jsonschema:"OPEN, RESOLVED or PENDING."`
	Severity     string         `json:"severity,omitempty" jsonschema:"NORMAL for an ordinary comment, BLOCKER for a task."`
	Pending      bool           `json:"pending" jsonschema:"Whether this is an unpublished draft comment."`
	Resolved     bool           `json:"resolved" jsonschema:"Whether the thread this comment belongs to is resolved."`
	Anchored     bool           `json:"anchored" jsonschema:"Whether the comment is attached to a line rather than to the commit or pull request."`
	Reply        bool           `json:"reply" jsonschema:"Whether this comment is a reply to another rather than the root of a thread."`
	ParentID     int64          `json:"parentId,omitempty" jsonschema:"Comment this one replies to. Absent on a thread root, which is what resolve, reopen and react address -- so a caller holding a reply id follows this to reach the comment those commands take."`
	Anchor       *CommentAnchor `json:"anchor,omitempty" jsonschema:"Where in the diff it sits. Absent for a top-level comment."`
	Author       result.User    `json:"author,omitzero" jsonschema:"Who wrote it."`
	ReplyCount   int            `json:"replyCount" jsonschema:"Direct replies to this comment."`
	CreatedDate  int64          `json:"createdDate,omitempty" jsonschema:"When it was written, in milliseconds since the epoch."`
	UpdatedDate  int64          `json:"updatedDate,omitempty" jsonschema:"When it last changed, in milliseconds since the epoch."`
	ResolvedDate int64          `json:"resolvedDate,omitempty" jsonschema:"When it was resolved, in milliseconds since the epoch."`

	// Properties is left open, for the reason given on the pull request comment:
	// Bitbucket stores undocumented extras here, reactions among them.
	Properties map[string]any `json:"properties,omitempty" jsonschema:"Per-comment extras Bitbucket attaches, reactions among them. Left open because Bitbucket does not document what goes here."`
}

// PermissionEntry is one user or group holding a repository permission.
type PermissionEntry struct {
	Name        string `json:"name" jsonschema:"Username for a user, group name for a group. This is what grant and revoke take."`
	DisplayName string `json:"displayName,omitempty" jsonschema:"Human-readable name. Falls back to name when the instance has none."`
	Permission  string `json:"permission,omitempty" jsonschema:"REPO_READ, REPO_WRITE or REPO_ADMIN."`
}

// GrantedPermissions is what the repository permission list commands return.
//
// subject names which kind of holder the entries are, and they sit under one
// key rather than users or groups depending on the answer. The shallow alias
// picks the subject from --group, so a key that changed with the flag left one
// command with two shapes.
type GrantedPermissions struct {
	Repository result.Repository `json:"repository"`
	Subject    string            `json:"subject" jsonschema:"user when the entries are users, group when they are groups."`
	Entries    []PermissionEntry `json:"entries" jsonschema:"Holders of a repository permission. Empty rather than absent when there are none."`
}

// PermissionGrant is what the repository permission grant commands report.
type PermissionGrant struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Subject    string            `json:"subject" jsonschema:"user or group, matching what name refers to."`
	Name       string            `json:"name" jsonschema:"Username or group name that was granted the permission."`
	Permission string            `json:"permission" jsonschema:"REPO_READ, REPO_WRITE or REPO_ADMIN."`
}

// PermissionRevocation is what the repository permission revoke commands
// report.
type PermissionRevocation struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Subject    string            `json:"subject" jsonschema:"user or group, matching what name refers to."`
	Name       string            `json:"name" jsonschema:"Username or group name the permission was revoked from."`
}

// EffectivePermission is one permission level and whether the caller holds it.
type EffectivePermission struct {
	Permission string `json:"permission" jsonschema:"REPO_READ, REPO_WRITE or REPO_ADMIN."`
	Granted    bool   `json:"granted" jsonschema:"Whether the caller holds it."`
}

// EffectivePermissions is what `bb repo permissions show` returns.
type EffectivePermissions struct {
	Repository  result.Repository     `json:"repository"`
	Permissions []EffectivePermission `json:"permissions" jsonschema:"One entry per permission level, in increasing order of privilege."`
}

// Labels is what `bb repo label list` returns.
type Labels struct {
	Repository result.Repository `json:"repository"`
	Labels     []string          `json:"labels" jsonschema:"Labels on the repository. Empty rather than absent when there are none."`
}

// LabelChange is what `bb repo label add` and `remove` report.
type LabelChange struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Label      string            `json:"label" jsonschema:"The label that was added or removed."`
}

// WatchState is what `bb repo watch` and `bb repo unwatch` report.
type WatchState struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Watching   bool              `json:"watching" jsonschema:"Whether you are now watching the repository."`
}

// DefaultTasks is what `bb repo default-task list` returns.
type DefaultTasks struct {
	Repository result.Repository    `json:"repository"`
	Tasks      []result.DefaultTask `json:"tasks" jsonschema:"Default checklist tasks on the repository. Empty rather than absent when there are none."`
}

// SingleDefaultTask is what `bb repo default-task add` and `update` return.
type SingleDefaultTask struct {
	Repository result.Repository  `json:"repository"`
	Task       result.DefaultTask `json:"task"`
}

// DefaultTaskDeletion is what `bb repo default-task delete` reports.
type DefaultTaskDeletion struct {
	result.Status
	Repository result.Repository `json:"repository"`
	ID         string            `json:"id" jsonschema:"Identifier of the task that was deleted, as it was given on the command line."`
}

// SyncedRef is one ref the fork-synchronisation status reports on.
type SyncedRef struct {
	ID        string `json:"id" jsonschema:"Full ref name, for example refs/heads/main."`
	DisplayID string `json:"displayId" jsonschema:"Short ref name."`
	State     string `json:"state,omitempty" jsonschema:"AHEAD, DIVERGED or ORPHANED, matching the list it came from."`
	Tag       bool   `json:"tag" jsonschema:"Whether the ref is a tag rather than a branch."`
}

// SyncStatus is what `bb repo sync status`, `enable` and `disable` return.
type SyncStatus struct {
	Repository   result.Repository `json:"repository"`
	Available    bool              `json:"available" jsonschema:"Whether the repository can be synchronised at all, which needs it to be a fork."`
	Enabled      bool              `json:"enabled" jsonschema:"Whether automatic synchronisation is on."`
	LastSync     int64             `json:"lastSync,omitempty" jsonschema:"When it last synchronised, in milliseconds since the epoch."`
	AheadRefs    []SyncedRef       `json:"aheadRefs" jsonschema:"Refs the fork is ahead of upstream on. Empty rather than absent when there are none."`
	DivergedRefs []SyncedRef       `json:"divergedRefs" jsonschema:"Refs that have diverged and cannot fast-forward. Empty rather than absent when there are none."`
	OrphanedRefs []SyncedRef       `json:"orphanedRefs" jsonschema:"Refs that no longer exist upstream. Empty rather than absent when there are none."`
}

// SyncTriggered is what `bb repo sync` reports.
type SyncTriggered struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Ref        string            `json:"ref" jsonschema:"Ref that was synchronised."`
	Action     string            `json:"action" jsonschema:"MERGE or DISCARD, which decides what happens to local commits."`
}

// FileEdit is what `bb repo edit` returns.
type FileEdit struct {
	Repository result.Repository `json:"repository"`
	Path       string            `json:"path" jsonschema:"File that was written."`
	Branch     string            `json:"branch,omitempty" jsonschema:"Branch the commit landed on."`
	Commit     result.Commit     `json:"commit" jsonschema:"The commit the edit produced."`
}

// Comparison is what `bb repo compare` returns without --diff.
type Comparison struct {
	Repository result.Repository `json:"repository"`
	From       string            `json:"from" jsonschema:"Ref the comparison started at."`
	To         string            `json:"to" jsonschema:"Ref it ended at."`
	Changes    []Change          `json:"changes" jsonschema:"Files that differ between the two. Empty rather than absent when nothing does, and always empty with --diff, which asks for the patch instead."`
	Patch      string            `json:"patch,omitempty" jsonschema:"The unified diff, when --diff was passed."`
}

// Change is one file that differs between two refs.
type Change struct {
	Path       string `json:"path" jsonschema:"Path after the change."`
	SrcPath    string `json:"srcPath,omitempty" jsonschema:"Path before a rename or copy."`
	Type       string `json:"type,omitempty" jsonschema:"ADD, MODIFY, DELETE, COPY, MOVE or UNKNOWN."`
	NodeType   string `json:"nodeType,omitempty" jsonschema:"FILE, DIRECTORY or SUBMODULE."`
	Executable bool   `json:"executable,omitempty" jsonschema:"Whether the file is executable after the change."`
}

// Archive is what `bb repo archive` reports when it wrote a file.
//
// Nothing is reported when the archive went to stdout: the archive is the
// output then, and a JSON document beside it would be a second one.
type Archive struct {
	result.Status
	Repository result.Repository `json:"repository"`
	File       string            `json:"file" jsonschema:"Path the archive was written to."`
}

// SSHKey is one SSH key granted access to a repository or project.
type SSHKey struct {
	ID          int32  `json:"id,omitempty" jsonschema:"Key identifier, which remove takes."`
	Label       string `json:"label,omitempty" jsonschema:"Label the key was registered under."`
	Fingerprint string `json:"fingerprint,omitempty" jsonschema:"Fingerprint of the public key."`
	Text        string `json:"text,omitempty" jsonschema:"The public key itself."`
	Permission  string `json:"permission,omitempty" jsonschema:"REPO_READ or REPO_WRITE, the access the key is granted."`
}

// SSHKeys is what `bb repo ssh-key list` returns.
type SSHKeys struct {
	Keys []SSHKey `json:"keys" jsonschema:"Access keys in scope. Empty rather than absent when there are none."`
}

// AddedSSHKey is what `bb repo ssh-key add` returns.
type AddedSSHKey struct {
	Key SSHKey `json:"key"`
}

// Webhooks is what `bb repo settings workflow webhooks list` returns.
type Webhooks struct {
	Repository result.Repository `json:"repository"`
	Count      int               `json:"count" jsonschema:"How many webhooks are configured. Bitbucket reports this separately from the page, so it may exceed the number returned."`
	Webhooks   []result.Webhook  `json:"webhooks" jsonschema:"The webhooks. Empty rather than absent when there are none."`
}

// WebhookChange is what `bb repo settings workflow webhooks create` reports.
type WebhookChange struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Webhook    result.Webhook    `json:"webhook"`
}

// WebhookDeletion is what `bb repo settings workflow webhooks delete` reports.
type WebhookDeletion struct {
	result.Status
	Repository result.Repository `json:"repository"`
	WebhookID  string            `json:"webhookId" jsonschema:"Identifier of the webhook that was deleted, as it was given on the command line."`
}

// MergeStrategy is one merge strategy a repository offers.
type MergeStrategy struct {
	ID      string `json:"id" jsonschema:"Strategy id, which set-strategy takes: no-ff, ff, ff-only, rebase-no-ff, rebase-ff-only or squash."`
	Name    string `json:"name,omitempty" jsonschema:"Human-readable name."`
	Enabled bool   `json:"enabled" jsonschema:"Whether it is offered on this repository."`
}

// PullRequestSettings is what the repository pull-request settings commands
// return.
//
// Bitbucket answers with an open object whose shape moves between versions;
// these are the fields bb reads and writes. Anything else the instance reports
// is not published, because bb cannot say what it means or promise it will be
// there.
type PullRequestSettings struct {
	Repository               result.Repository `json:"repository"`
	RequiredApprovers        int               `json:"requiredApprovers" jsonschema:"How many approvals a merge needs. Zero when the check is off."`
	RequiredApproversEnabled bool              `json:"requiredApproversEnabled" jsonschema:"Whether the approval check is on at all. Distinct from a count of zero, which the instance may report while the check is off."`
	RequiredAllTasksComplete bool              `json:"requiredAllTasksComplete" jsonschema:"Whether every task must be resolved before a merge."`
	DefaultMergeStrategy     string            `json:"defaultMergeStrategy,omitempty" jsonschema:"Strategy a merge uses when none is named. Reported at the top level rather than as a flag on one entry, because Bitbucket names it even when it does not send the list."`
	MergeStrategies          []MergeStrategy   `json:"mergeStrategies" jsonschema:"Strategies the repository offers. Empty rather than absent when the instance reports none."`
}

// MergeChecks is what `bb repo settings pull-requests merge-checks list`
// returns.
type MergeChecks struct {
	Repository result.Repository           `json:"repository"`
	Checks     []result.RequiredBuildCheck `json:"checks" jsonschema:"Required-build merge checks on the repository. Empty rather than absent when there are none."`
}

// AutoMergeSettings is what the `bb repo settings auto-merge` commands return.
type AutoMergeSettings struct {
	Repository       result.Repository `json:"repository"`
	Enabled          bool              `json:"enabled" jsonschema:"Whether auto-merge may be armed on this repository."`
	RestrictionState string            `json:"restrictionState,omitempty" jsonschema:"Whether the project allows the repository to decide: UNRESTRICTED, RESTRICTED or RESTRICTED_MODIFIABLE."`
}

// AutoDeclineSettings is what the `bb repo settings auto-decline` commands
// return.
type AutoDeclineSettings struct {
	Repository      result.Repository `json:"repository"`
	Enabled         bool              `json:"enabled" jsonschema:"Whether inactive pull requests are declined automatically."`
	InactivityWeeks int32             `json:"inactivityWeeks,omitempty" jsonschema:"How many weeks of inactivity trigger it."`
}

// SettingsDeletion is what the auto-merge and auto-decline delete commands
// report.
//
// Deleting these settings does not turn the behaviour off: it removes the
// repository's own setting so the project's applies again.
type SettingsDeletion struct {
	result.Status
	Repository result.Repository `json:"repository"`
	Setting    string            `json:"setting" jsonschema:"Which setting was cleared: autoMerge or autoDecline."`
}
