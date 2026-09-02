package projectcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/safederef"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
	projectservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/project"
)

// Project is one Bitbucket project.
//
// links and avatar are dropped: they are navigation for the Bitbucket web UI,
// and the avatar in particular arrives as a base64 data URI that can run to
// tens of kilobytes -- a payload larger than every other field put together,
// carrying nothing a caller acts on.
type Project struct {
	ID          int32  `json:"id,omitempty" jsonschema:"Project identifier."`
	Key         string `json:"key,omitempty" jsonschema:"Project key, which every other project command takes."`
	Name        string `json:"name,omitempty" jsonschema:"Display name."`
	Description string `json:"description,omitempty" jsonschema:"Description, when one was given."`
	Public      bool   `json:"public" jsonschema:"Whether the project is readable without authentication."`
	Type        string `json:"type,omitempty" jsonschema:"NORMAL for an ordinary project, PERSONAL for a user's own."`
	Scope       string `json:"scope,omitempty" jsonschema:"What the object is, when the instance reports it."`
}

// Projects is what `bb project list` returns.
type Projects struct {
	Projects []Project `json:"projects" jsonschema:"Matching projects. Empty rather than absent when nothing matched."`
}

// SingleProject is what `bb project get`, `create` and `update` return.
type SingleProject struct {
	Project Project `json:"project"`
}

// ProjectDeletion is what `bb project delete` reports.
type ProjectDeletion struct {
	result.Status
	Project string `json:"project" jsonschema:"Key of the project that was deleted."`
}

// PermissionEntry is one user or group holding a project permission.
type PermissionEntry struct {
	Name        string `json:"name" jsonschema:"Username for a user, group name for a group. This is what grant and revoke take."`
	DisplayName string `json:"displayName,omitempty" jsonschema:"Human-readable name. Falls back to name when the instance has none."`
	Permission  string `json:"permission,omitempty" jsonschema:"PROJECT_READ, PROJECT_WRITE or PROJECT_ADMIN."`
}

// GrantedPermissions is what the permission list commands return.
//
// subject names which kind of holder the entries are, and the entries are under
// one key rather than users or groups depending on the answer. The shallow
// alias -- `bb project permissions list [--group]` -- is one command that can
// report either, so a key that changed with the flag left it with no describable
// shape at all, and a consumer with two code paths for one command.
type GrantedPermissions struct {
	Project string            `json:"project" jsonschema:"Project key the permissions are on."`
	Subject string            `json:"subject" jsonschema:"user when the entries are users, group when they are groups."`
	Entries []PermissionEntry `json:"entries" jsonschema:"Holders of a project permission. Empty rather than absent when there are none."`
}

// PermissionGrant is what the permission grant commands report.
type PermissionGrant struct {
	result.Status
	Project    string `json:"project" jsonschema:"Project key the permission was granted on."`
	Subject    string `json:"subject" jsonschema:"user or group, matching what name refers to."`
	Name       string `json:"name" jsonschema:"Username or group name that was granted the permission."`
	Permission string `json:"permission" jsonschema:"PROJECT_READ, PROJECT_WRITE or PROJECT_ADMIN."`
}

// PermissionRevocation is what the permission revoke commands report.
//
// No permission field: revoking removes whatever the holder had, and Bitbucket
// does not report which level that was.
type PermissionRevocation struct {
	result.Status
	Project string `json:"project" jsonschema:"Project key the permission was revoked on."`
	Subject string `json:"subject" jsonschema:"user or group, matching what name refers to."`
	Name    string `json:"name" jsonschema:"Username or group name the permission was revoked from."`
}

// EffectivePermission is one permission level and whether the caller holds it.
type EffectivePermission struct {
	Permission string `json:"permission" jsonschema:"PROJECT_READ, PROJECT_WRITE or PROJECT_ADMIN."`
	Granted    bool   `json:"granted" jsonschema:"Whether the caller holds it."`
}

// EffectivePermissions is what `bb project permissions show` returns.
//
// A list rather than an object keyed by permission name: the keys would be
// Bitbucket's SCREAMING_SNAKE constants, which is not how anything else bb
// emits names a field, and a fixed list is what --describe can state.
type EffectivePermissions struct {
	Project     string                `json:"project" jsonschema:"Project key the permissions were probed on."`
	Permissions []EffectivePermission `json:"permissions" jsonschema:"One entry per permission level, in increasing order of privilege."`
}

// Restrictions is what `bb project branch-restriction list` returns.
type Restrictions struct {
	Project      string               `json:"project" jsonschema:"Project key the restrictions are on."`
	Restrictions []result.Restriction `json:"restrictions" jsonschema:"Branch restrictions on the project. Empty rather than absent when there are none."`
}

// SingleRestriction is what `bb project branch-restriction get`, `create` and
// `update` return.
type SingleRestriction struct {
	Project     string             `json:"project" jsonschema:"Project key the restriction is on."`
	Restriction result.Restriction `json:"restriction"`
}

// RestrictionDeletion is what `bb project branch-restriction delete` reports.
type RestrictionDeletion struct {
	result.Status
	Project       string `json:"project" jsonschema:"Project key the restriction was on."`
	RestrictionID string `json:"restrictionId" jsonschema:"Identifier of the restriction that was deleted, as it was given on the command line."`
}

// DefaultTaskDeletion is what `bb project default-task delete` reports.
type DefaultTaskDeletion struct {
	result.Status
	Project string `json:"project" jsonschema:"Project key the task was on."`
	ID      string `json:"id" jsonschema:"Identifier of the task that was deleted, as it was given on the command line."`
}

// Webhooks is what `bb project webhook list` returns.
type Webhooks struct {
	Project  string           `json:"project" jsonschema:"Project key the webhooks are on."`
	Webhooks []result.Webhook `json:"webhooks" jsonschema:"Webhooks configured on the project. Empty rather than absent when there are none."`
}

// WebhookChange is what `bb project webhook create` and `update` report.
type WebhookChange struct {
	result.Status
	Project string         `json:"project" jsonschema:"Project key the webhook is on."`
	Webhook result.Webhook `json:"webhook"`
}

// WebhookDeletion is what `bb project webhook delete` reports.
type WebhookDeletion struct {
	result.Status
	Project   string `json:"project" jsonschema:"Project key the webhook was on."`
	WebhookID string `json:"webhookId" jsonschema:"Identifier of the webhook that was deleted, as it was given on the command line."`
}

var (
	projectTypes    = []string{"NORMAL", "PERSONAL"}
	permissionNames = []string{"PROJECT_READ", "PROJECT_WRITE", "PROJECT_ADMIN"}
	subjectKinds    = []string{"user", "group"}
)

func init() {
	projectEnums := map[string][]string{"project.type": projectTypes}

	result.Declare("project list", result.For[Projects](map[string][]string{"projects.type": projectTypes}))
	result.Declare("project get", result.For[SingleProject](projectEnums))
	result.Declare("project create", result.For[SingleProject](projectEnums))
	result.Declare("project update", result.For[SingleProject](projectEnums))
	result.Declare("project delete", result.For[ProjectDeletion](nil))

	listEnums := map[string][]string{
		"subject":            subjectKinds,
		"entries.permission": permissionNames,
	}
	grantEnums := map[string][]string{"subject": subjectKinds, "permission": permissionNames}
	revokeEnums := map[string][]string{"subject": subjectKinds}

	// The shallow aliases and the per-subject commands are the same code with
	// the same payload, so they declare the same schema rather than one
	// deferring to the other.
	for _, prefix := range []string{"project permissions", "project permissions users", "project permissions groups"} {
		result.Declare(prefix+" list", result.For[GrantedPermissions](listEnums))
		result.Declare(prefix+" grant", result.For[PermissionGrant](grantEnums))
		result.Declare(prefix+" revoke", result.For[PermissionRevocation](revokeEnums))
	}
	result.Declare("project permissions show", result.For[EffectivePermissions](map[string][]string{
		"permissions.permission": permissionNames,
	}))

	result.Declare("project branch-restriction list", result.For[Restrictions](map[string][]string{
		"restrictions.type":         result.RestrictionTypes,
		"restrictions.matcher.type": result.RefMatcherTypes,
		"restrictions.scope":        result.RestrictionScopes,
	}))
	singleRestrictionEnums := map[string][]string{
		"restriction.type":         result.RestrictionTypes,
		"restriction.matcher.type": result.RefMatcherTypes,
		"restriction.scope":        result.RestrictionScopes,
	}
	result.Declare("project branch-restriction get", result.For[SingleRestriction](singleRestrictionEnums))
	result.Declare("project branch-restriction create", result.For[SingleRestriction](singleRestrictionEnums))
	result.Declare("project branch-restriction update", result.For[SingleRestriction](singleRestrictionEnums))
	result.Declare("project branch-restriction delete", result.For[RestrictionDeletion](nil))

	result.Declare("project default-task list", result.List[result.DefaultTask](nil))
	result.Declare("project default-task add", result.For[result.DefaultTask](nil))
	result.Declare("project default-task update", result.For[result.DefaultTask](nil))
	result.Declare("project default-task delete", result.For[DefaultTaskDeletion](nil))

	result.Declare("project webhook list", result.For[Webhooks](nil))
	result.Declare("project webhook create", result.For[WebhookChange](nil))
	result.Declare("project webhook update", result.For[WebhookChange](nil))
	result.Declare("project webhook delete", result.For[WebhookDeletion](nil))

	// project webhook test and stats are deliberately not declared, for the same
	// reason as their repository counterparts: the service returns whatever
	// Bitbucket sent as an untyped any and the command pretty-prints it without
	// reading a field, so there is no shape to publish.
}

// projectFrom converts one upstream project.
func projectFrom(upstream openapigenerated.RestProject) Project {
	converted := Project{
		Key:         safederef.String(upstream.Key),
		Name:        safederef.String(upstream.Name),
		Description: safederef.String(upstream.Description),
		Scope:       safederef.String(upstream.Scope),
	}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.Public != nil {
		converted.Public = *upstream.Public
	}
	if upstream.Type != nil {
		converted.Type = string(*upstream.Type)
	}

	return converted
}

// projectsFrom converts a list, preserving order and never returning nil.
func projectsFrom(upstream []openapigenerated.RestProject) []Project {
	converted := make([]Project, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, projectFrom(one))
	}

	return converted
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
// Fixed order, increasing privilege, so a reader comparing two runs is
// comparing rows rather than re-sorting a map whose iteration order Go
// deliberately randomises.
func effectivePermissionsFrom(probed map[string]bool) []EffectivePermission {
	converted := make([]EffectivePermission, 0, len(permissionNames))
	for _, name := range permissionNames {
		converted = append(converted, EffectivePermission{Permission: name, Granted: probed[name]})
	}

	return converted
}

// defaultTaskFrom converts one upstream default task.
func defaultTaskFrom(upstream projectservice.DefaultTask) result.DefaultTask {
	converted := result.DefaultTask{Description: safederef.String(upstream.Description)}
	if upstream.Id != nil {
		converted.ID = *upstream.Id
	}
	if upstream.SourceMatcher != nil {
		converted.SourceMatcher = result.DefaultTaskMatcher{
			ID:        safederef.String(upstream.SourceMatcher.Id),
			DisplayID: safederef.String(upstream.SourceMatcher.DisplayId),
		}
	}
	if upstream.TargetMatcher != nil {
		converted.TargetMatcher = result.DefaultTaskMatcher{
			ID:        safederef.String(upstream.TargetMatcher.Id),
			DisplayID: safederef.String(upstream.TargetMatcher.DisplayId),
		}
	}

	return converted
}

// defaultTasksFrom converts a list, preserving order and never returning nil.
func defaultTasksFrom(upstream []projectservice.DefaultTask) []result.DefaultTask {
	converted := make([]result.DefaultTask, 0, len(upstream))
	for _, one := range upstream {
		converted = append(converted, defaultTaskFrom(one))
	}

	return converted
}

// defaultTaskValue converts the pointer the add and update calls return.
func defaultTaskValue(upstream *projectservice.DefaultTask) result.DefaultTask {
	if upstream == nil {
		return result.DefaultTask{}
	}

	return defaultTaskFrom(*upstream)
}
