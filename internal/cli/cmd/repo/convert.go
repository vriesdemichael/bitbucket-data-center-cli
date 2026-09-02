package repocmd

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	browseservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/browse"
	commentservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/comment"
	reposettings "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/reposettings"
	repositoryservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/repository"
)

var (
	repoPermissionNames = []string{"REPO_READ", "REPO_WRITE", "REPO_ADMIN"}
	subjectKinds        = []string{"user", "group"}
	commentStates       = []string{"OPEN", "RESOLVED", "PENDING"}
	syncActions         = []string{"MERGE", "DISCARD"}
	syncRefStates       = []string{"AHEAD", "DIVERGED", "ORPHANED"}
)

func init() {
	detailEnums := map[string][]string{"repository.state": result.RepositoryStates}

	result.Declare("repo list", result.List[result.RepositorySummary](nil))
	result.Declare("repo create", result.For[SingleRepository](detailEnums))
	result.Declare("repo fork", result.For[SingleRepository](detailEnums))
	result.Declare("repo delete", result.For[RepositoryDeletion](nil))
	result.Declare("repo admin create", result.For[SingleRepository](detailEnums))
	result.Declare("repo admin fork", result.For[SingleRepository](detailEnums))
	result.Declare("repo admin update", result.For[SingleRepository](detailEnums))
	result.Declare("repo admin delete", result.For[RepositoryDeletion](nil))

	result.Declare("repo browse tree", result.For[Tree](nil))
	result.Declare("repo browse file", result.For[FileContent](nil))
	result.Declare("repo browse blame", result.For[FileContent](nil))
	result.Declare("repo browse raw", result.For[RawFile](nil))
	result.Declare("repo cat", result.For[RawFile](nil))
	result.Declare("repo browse history", result.For[FileHistory](nil))
	result.Declare("repo edit", result.For[FileEdit](nil))
	result.Declare("repo compare", result.For[Comparison](nil))
	result.Declare("repo archive", result.For[Archive](nil))

	result.Declare("clone", result.For[Clone](nil))
	result.Declare("repo clone", result.For[Clone](nil))

	commentEnums := map[string][]string{
		"context.type":   {"commit", "pull_request"},
		"comments.state": commentStates,
	}
	singleCommentEnums := map[string][]string{
		"context.type":  {"commit", "pull_request"},
		"comment.state": commentStates,
	}
	result.Declare("repo comment list", result.For[Comments](commentEnums))
	result.Declare("repo comment create", result.For[SingleComment](singleCommentEnums))
	result.Declare("repo comment update", result.For[SingleComment](singleCommentEnums))
	result.Declare("repo comment delete", result.For[CommentDeletion](map[string][]string{
		"context.type": {"commit", "pull_request"},
	}))

	listPermissionEnums := map[string][]string{
		"subject":            subjectKinds,
		"entries.permission": repoPermissionNames,
	}
	grantEnums := map[string][]string{"subject": subjectKinds, "permission": repoPermissionNames}
	revokeEnums := map[string][]string{"subject": subjectKinds}
	// The shallow aliases and the per-subject commands are the same code with
	// the same payload, so they declare the same schema.
	for _, prefix := range []string{
		"repo permissions",
		"repo settings security permissions users",
		"repo settings security permissions groups",
	} {
		result.Declare(prefix+" list", result.For[GrantedPermissions](listPermissionEnums))
		result.Declare(prefix+" grant", result.For[PermissionGrant](grantEnums))
		result.Declare(prefix+" revoke", result.For[PermissionRevocation](revokeEnums))
	}
	result.Declare("repo permissions show", result.For[EffectivePermissions](map[string][]string{
		"permissions.permission": repoPermissionNames,
	}))

	result.Declare("repo label list", result.For[Labels](nil))
	result.Declare("repo label add", result.For[LabelChange](nil))
	result.Declare("repo label remove", result.For[LabelChange](nil))
	result.Declare("repo watch", result.For[WatchState](nil))
	result.Declare("repo unwatch", result.For[WatchState](nil))

	result.Declare("repo default-task list", result.For[DefaultTasks](nil))
	result.Declare("repo default-task add", result.For[SingleDefaultTask](nil))
	result.Declare("repo default-task update", result.For[SingleDefaultTask](nil))
	result.Declare("repo default-task delete", result.For[DefaultTaskDeletion](nil))

	syncEnums := map[string][]string{
		"aheadRefs.state":    syncRefStates,
		"divergedRefs.state": syncRefStates,
		"orphanedRefs.state": syncRefStates,
	}
	result.Declare("repo sync", result.For[SyncTriggered](map[string][]string{"action": syncActions}))
	result.Declare("repo sync status", result.For[SyncStatus](syncEnums))
	result.Declare("repo sync enable", result.For[SyncStatus](syncEnums))
	result.Declare("repo sync disable", result.For[SyncStatus](syncEnums))

	result.Declare("repo ssh-key list", result.For[SSHKeys](map[string][]string{
		"keys.permission": {"REPO_READ", "REPO_WRITE"},
	}))
	result.Declare("repo ssh-key add", result.For[AddedSSHKey](map[string][]string{
		"key.permission": {"REPO_READ", "REPO_WRITE"},
	}))
	result.Declare("repo ssh-key remove", result.For[result.Status](nil))

	result.Declare("repo settings workflow webhooks list", result.For[Webhooks](nil))
	result.Declare("repo settings workflow webhooks create", result.For[WebhookChange](nil))
	result.Declare("repo settings workflow webhooks delete", result.For[WebhookDeletion](nil))

	result.Declare("repo settings pull-requests get", result.For[PullRequestSettings](nil))
	result.Declare("repo settings pull-requests update", result.For[PullRequestSettings](nil))
	result.Declare("repo settings pull-requests update-approvers", result.For[PullRequestSettings](nil))
	result.Declare("repo settings pull-requests set-strategy", result.For[PullRequestSettings](nil))
	result.Declare("repo settings pull-requests merge-checks list", result.For[MergeChecks](map[string][]string{
		"checks.refMatcher.type":       result.RefMatcherTypes,
		"checks.exemptRefMatcher.type": result.RefMatcherTypes,
	}))

	autoMergeEnums := map[string][]string{
		"restrictionState": {"UNRESTRICTED", "RESTRICTED", "RESTRICTED_MODIFIABLE"},
	}
	result.Declare("repo settings auto-merge get", result.For[AutoMergeSettings](autoMergeEnums))
	result.Declare("repo settings auto-merge set", result.For[AutoMergeSettings](autoMergeEnums))
	result.Declare("repo settings auto-merge delete", result.For[SettingsDeletion](map[string][]string{"setting": {"autoMerge"}}))
	result.Declare("repo settings auto-decline get", result.For[AutoDeclineSettings](nil))
	result.Declare("repo settings auto-decline set", result.For[AutoDeclineSettings](nil))
	result.Declare("repo settings auto-decline delete", result.For[SettingsDeletion](map[string][]string{"setting": {"autoDecline"}}))
}

// repositoryOf converts the repository service reference.
func repositoryOf(repo repositoryservice.RepositoryRef) result.Repository {
	return result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}
}

// settingsRepositoryOf converts the settings service reference.
func settingsRepositoryOf(repo reposettings.RepositoryRef) result.Repository {
	return result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}
}

// browseRepositoryOf converts the browse service reference.
func browseRepositoryOf(repo browseservice.RepositoryRef) result.Repository {
	return result.Repository{ProjectKey: repo.ProjectKey, Slug: repo.Slug}
}

// commentContextFrom converts the service's comment context.
func commentContextFrom(context commentservice.Context) CommentContext {
	return CommentContext{
		Type:          context.Type,
		ProjectKey:    context.ProjectKey,
		Slug:          context.RepositorySlug,
		CommitID:      context.CommitID,
		PullRequestID: context.PullRequestID,
	}
}

// permissionEntriesFrom converts what the permission list commands resolved.
func permissionEntriesFrom(entries []permissionEntry) []PermissionEntry {
	converted := make([]PermissionEntry, 0, len(entries))
	for _, entry := range entries {
		converted = append(converted, PermissionEntry{
			Name:        entry.name,
			DisplayName: entry.display,
			Permission:  entry.permission,
		})
	}

	return converted
}

// effectivePermissionsFrom orders the probe result rather than publishing the
// map it arrives in.
//
// Fixed order, increasing privilege, so two runs compare row by row rather than
// through a map whose iteration order Go deliberately randomises.
func effectivePermissionsFrom(probed map[string]bool) []EffectivePermission {
	converted := make([]EffectivePermission, 0, len(repoPermissionNames))
	for _, name := range repoPermissionNames {
		converted = append(converted, EffectivePermission{Permission: name, Granted: probed[name]})
	}

	return converted
}

// defaultTaskFrom converts one upstream default task.
func defaultTaskFrom(upstream reposettings.DefaultTask) result.DefaultTask {
	converted := result.DefaultTask{Description: safeString(upstream.Description)}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.SourceMatcher != nil {
		converted.SourceMatcher = result.DefaultTaskMatcher{
			ID:        safeString(upstream.SourceMatcher.Id),
			DisplayID: safeString(upstream.SourceMatcher.DisplayId),
		}
	}
	if upstream.TargetMatcher != nil {
		converted.TargetMatcher = result.DefaultTaskMatcher{
			ID:        safeString(upstream.TargetMatcher.Id),
			DisplayID: safeString(upstream.TargetMatcher.DisplayId),
		}
	}

	return converted
}

// defaultTasksFrom converts a list, preserving order and never returning nil.
func defaultTasksFrom(upstream []reposettings.DefaultTask) []result.DefaultTask {
	converted := make([]result.DefaultTask, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, defaultTaskFrom(one))
	}

	return converted
}

// defaultTaskValue converts the pointer the add and update calls return.
func defaultTaskValue(upstream *reposettings.DefaultTask) result.DefaultTask {
	if upstream == nil {
		return result.DefaultTask{}
	}

	return defaultTaskFrom(*upstream)
}

// syncStatusFrom converts the fork synchronisation status.
func syncStatusFrom(repo result.Repository, upstream *openapigenerated.RestRefSyncStatus) SyncStatus {
	converted := SyncStatus{
		Repository:   repo,
		AheadRefs:    []SyncedRef{},
		DivergedRefs: []SyncedRef{},
		OrphanedRefs: []SyncedRef{},
	}
	if upstream == nil {
		return converted
	}

	if upstream.Available != nil {
		converted.Available = *upstream.Available
	}
	if upstream.Enabled != nil {
		converted.Enabled = *upstream.Enabled
	}
	if upstream.LastSync != nil {
		converted.LastSync = *upstream.LastSync
	}
	if upstream.AheadRefs != nil {
		for _, ref := range *upstream.AheadRefs {
			entry := SyncedRef{ID: ref.Id, DisplayID: ref.DisplayId}
			if ref.State != nil {
				entry.State = string(*ref.State)
			}
			if ref.Tag != nil {
				entry.Tag = *ref.Tag
			}
			converted.AheadRefs = append(converted.AheadRefs, entry)
		}
	}
	if upstream.DivergedRefs != nil {
		for _, ref := range *upstream.DivergedRefs {
			entry := SyncedRef{ID: ref.Id, DisplayID: ref.DisplayId}
			if ref.State != nil {
				entry.State = string(*ref.State)
			}
			if ref.Tag != nil {
				entry.Tag = *ref.Tag
			}
			converted.DivergedRefs = append(converted.DivergedRefs, entry)
		}
	}
	if upstream.OrphanedRefs != nil {
		for _, ref := range *upstream.OrphanedRefs {
			entry := SyncedRef{ID: ref.Id, DisplayID: ref.DisplayId}
			if ref.State != nil {
				entry.State = string(*ref.State)
			}
			if ref.Tag != nil {
				entry.Tag = *ref.Tag
			}
			converted.OrphanedRefs = append(converted.OrphanedRefs, entry)
		}
	}

	return converted
}

// autoMergeSettingsFrom converts the auto-merge settings.
func autoMergeSettingsFrom(repo result.Repository, upstream *openapigenerated.RestAutoMergeRestrictedSettings) AutoMergeSettings {
	converted := AutoMergeSettings{Repository: repo}
	if upstream == nil {
		return converted
	}
	if upstream.Enabled != nil {
		converted.Enabled = *upstream.Enabled
	}
	if upstream.RestrictionState != nil {
		converted.RestrictionState = string(*upstream.RestrictionState)
	}

	return converted
}

// autoDeclineSettingsFrom converts the auto-decline settings.
func autoDeclineSettingsFrom(repo result.Repository, upstream *openapigenerated.RestAutoDeclineSettings) AutoDeclineSettings {
	converted := AutoDeclineSettings{Repository: repo}
	if upstream == nil {
		return converted
	}
	if upstream.Enabled != nil {
		converted.Enabled = *upstream.Enabled
	}
	if upstream.InactivityWeeks != nil {
		converted.InactivityWeeks = *upstream.InactivityWeeks
	}

	return converted
}

// pullRequestSettingsFrom reads the fields bb manipulates out of the open
// object Bitbucket returns.
//
// requiredApprovers arrives as {enabled, count} rather than a number, and count
// as either a string or a number depending on version, which is why the two are
// published as separate fields with one meaning each.
func pullRequestSettingsFrom(repo result.Repository, settings map[string]any) PullRequestSettings {
	converted := PullRequestSettings{Repository: repo}

	if value, ok := settings["requiredAllTasksComplete"].(bool); ok {
		converted.RequiredAllTasksComplete = &value
	}

	// Three shapes reach this, and only one of them comes from a JSON decode.
	// The modern object is {enabled, count}; the legacy fallback sends a bare
	// number; and when the PUT answers with an empty or non-JSON body the
	// service hands back the request map it sent, where the count is a Go int
	// rather than the float64 a decode produces. Reading only float64 and string
	// reported zero approvers immediately after setting two.
	switch section := settings["requiredApprovers"].(type) {
	case map[string]any:
		if enabled, ok := section["enabled"].(bool); ok {
			converted.RequiredApproversEnabled = &enabled
		}
		count := countOf(section["count"])
		converted.RequiredApprovers = &count
	case nil:
		// Not reported. Absent, rather than a count of zero that reads as "no
		// approvals required" on an instance that never answered the question.
	default:
		count := countOf(section)
		enabled := count > 0
		converted.RequiredApprovers = &count
		converted.RequiredApproversEnabled = &enabled
	}
	if mergeConfig, ok := settings["mergeConfig"].(map[string]any); ok {
		if defaultStrategy, ok := mergeConfig["defaultStrategy"].(map[string]any); ok {
			if id, ok := defaultStrategy["id"].(string); ok {
				converted.DefaultMergeStrategy = &id
			}
		}
		if strategies, ok := mergeConfig["strategies"].([]any); ok {
			// Empty rather than nil from here on: the configuration was
			// reported, so an empty list is the answer rather than a silence.
			listed := make([]MergeStrategy, 0, len(strategies))
			for _, entry := range strategies {
				strategy, ok := entry.(map[string]any)
				if !ok {
					continue
				}
				id, _ := strategy["id"].(string)
				name, _ := strategy["name"].(string)
				enabled, _ := strategy["enabled"].(bool)
				listed = append(listed, MergeStrategy{ID: id, Name: name, Enabled: enabled})
			}
			converted.MergeStrategies = &listed
		}
	}

	return converted
}

// countOf reads an approver count however it arrived.
//
// float64 is what a JSON decode gives, int is what the request map the service
// echoes back on an empty response body carries, and some instances send the
// number as a string.
func countOf(value any) int {
	switch count := value.(type) {
	case float64:
		return int(count)
	case float32:
		return int(count)
	case int:
		return count
	case int32:
		return int(count)
	case int64:
		return int(count)
	case json.Number:
		parsed, err := count.Int64()
		if err != nil {
			return 0
		}

		return int(parsed)
	case string:
		return parseCount(count)
	default:
		return 0
	}
}

// parseCount reads the approver count when the instance sends it as a string.
func parseCount(value string) int {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}

	count := 0
	for _, digit := range trimmed {
		if digit < '0' || digit > '9' {
			return 0
		}
		count = count*10 + int(digit-'0')
	}

	return count
}

// fileLinesFrom decodes a browse response into lines, with attribution when
// blame was asked for.
//
// Bitbucket answers /browse with the file's lines. With blame=true it adds a
// blame array alongside them -- "the blame will be returned for the file as
// well", in the spec's words -- where each entry names an author and the run of
// lines that author last touched, by starting line number and length.
//
// The previous reader decoded blame as an object with one author and applied
// that author to every line. Against a real server the decode failed outright,
// because blame is a list, so the human path fell through to printing the raw
// JSON and the machine path published it. That is what made bb repo browse
// blame return nothing once both paths were made to read one value.
func fileLinesFrom(content []byte) (lines []FileLine, binary bool, complete bool) {
	lines = []FileLine{}
	if len(content) == 0 {
		return lines, false, true
	}

	var structured struct {
		Lines []struct {
			Text string `json:"text"`
		} `json:"lines"`
		Blame []struct {
			Author struct {
				Name string `json:"name"`
			} `json:"author"`
			LineNumber   int `json:"lineNumber"`
			SpannedLines int `json:"spannedLines"`
		} `json:"blame"`
		// Bitbucket answers a binary file with {"binary": true} and no lines,
		// and pages a long one with isLastPage. Both were visible while the
		// response was passed through raw.
		Binary     *bool `json:"binary"`
		IsLastPage *bool `json:"isLastPage"`
	}
	if err := json.Unmarshal(content, &structured); err != nil {
		return lines, false, true
	}

	binary = structured.Binary != nil && *structured.Binary
	// Absent means the endpoint did not page this response, which is the usual
	// case and means what was returned is the file.
	complete = structured.IsLastPage == nil || *structured.IsLastPage

	for _, line := range structured.Lines {
		lines = append(lines, FileLine{Text: line.Text})
	}

	// lineNumber is 1-based and spannedLines counts the run starting there. A
	// span reaching past the end is clamped rather than dropped: the file and
	// its blame are two reads of the same commit, but not the same request.
	for _, span := range structured.Blame {
		for offset := range span.SpannedLines {
			index := span.LineNumber - 1 + offset
			if index < 0 || index >= len(lines) {
				break
			}
			lines[index].Author = span.Author.Name
		}
	}

	return lines, binary, complete
}

// changesFrom converts the compare result, never returning nil.
func changesFrom(upstream []openapigenerated.RestChange) []Change {
	converted := make([]Change, 0, len(upstream))
	for _, one := range upstream {
		entry := Change{}
		if one.Path != nil && one.Path.Components != nil {
			entry.Path = strings.Join(*one.Path.Components, "/")
		}
		if one.SrcPath != nil && one.SrcPath.Components != nil {
			entry.SrcPath = strings.Join(*one.SrcPath.Components, "/")
		}
		if one.Type != nil {
			entry.Type = string(*one.Type)
		}
		if one.NodeType != nil {
			entry.NodeType = string(*one.NodeType)
		}
		if one.Executable != nil {
			entry.Executable = *one.Executable
		}
		converted = append(converted, entry)
	}

	return converted
}

// sshKeysFrom converts the access keys a repository or project grants.
func sshKeysFrom(upstream []openapigenerated.RestSshAccessKey) []SSHKey {
	converted := make([]SSHKey, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, sshKeyFrom(one))
	}

	return converted
}

// sshKeyFrom converts one access key.
func sshKeyFrom(upstream openapigenerated.RestSshAccessKey) SSHKey {
	converted := SSHKey{}
	if upstream.Key != nil {
		if upstream.Key.Id != nil {
			converted.ID = *upstream.Key.Id
		}
		converted.Label = safeString(upstream.Key.Label)
		converted.Fingerprint = safeString(upstream.Key.Fingerprint)
		converted.Text = safeString(upstream.Key.Text)
	}
	if upstream.Permission != nil {
		converted.Permission = string(*upstream.Permission)
	}

	return converted
}

// intPointer widens the version Bitbucket reports on a delete.
//
// The service returns *int32 and the payload publishes *int, because absent
// means "the server did not say which version was deleted" and zero would be a
// claim that it did.
func intPointer(value *int32) *int {
	if value == nil {
		return nil
	}
	widened := int(*value)

	return &widened
}

// fileEditFrom converts the commit an edit produced.
func fileEditFrom(repo result.Repository, path string, branch string, commit *openapigenerated.RestCommit) FileEdit {
	converted := FileEdit{Repository: repo, Path: path, Branch: branch}
	if commit != nil {
		converted.Commit = result.CommitFrom(*commit)
	}

	return converted
}

// rawFileFrom wraps a file's bytes for machine output.
//
// Text goes out as a string, which is what a caller wants and what jq can read.
// Anything else goes out base64: a JSON string cannot carry arbitrary bytes, and
// Go's encoder silently substitutes U+FFFD for invalid UTF-8, so the alternative
// is returning a corrupted file with nothing saying so.
func rawFileFrom(repo result.Repository, path string, at string, content []byte) RawFile {
	converted := RawFile{Repository: repo, Path: path, At: at, Encoding: "utf-8"}
	if utf8.Valid(content) {
		converted.Content = string(content)

		return converted
	}

	converted.Encoding = "base64"
	converted.Content = base64.StdEncoding.EncodeToString(content)

	return converted
}
