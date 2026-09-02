package result

// DefaultTaskMatcher is which refs a default task applies to.
//
// Its own type rather than RefMatcher: the default-task endpoints report only
// an id and a display id, with no kind at all, so the shared matcher's type
// field would be permanently empty here.
type DefaultTaskMatcher struct {
	ID        string `json:"id,omitempty" jsonschema:"Matcher value: a branch name or a pattern."`
	DisplayID string `json:"displayId,omitempty" jsonschema:"Human-readable form of the same thing."`
}

// DefaultTask is one default checklist task.
//
// Shared by the project-scoped and repository-scoped commands: the same object
// through two endpoints, and two service packages that had each grown their own
// copy of it.
type DefaultTask struct {
	ID            int64              `json:"id,omitempty" jsonschema:"Task identifier, which update and delete address."`
	Description   string             `json:"description,omitempty" jsonschema:"The task text, which appears on every pull request the matchers cover."`
	SourceMatcher DefaultTaskMatcher `json:"sourceMatcher,omitzero" jsonschema:"Which source branches the task applies to. Absent when it applies to all."`
	TargetMatcher DefaultTaskMatcher `json:"targetMatcher,omitzero" jsonschema:"Which target branches the task applies to. Absent when it applies to all."`
}
