package result

// DefaultTask is one default checklist task.
//
// Shared by the project-scoped and repository-scoped commands: the same object
// through two endpoints, and two service packages that had each grown their own
// copy of it.
type DefaultTask struct {
	ID            int64      `json:"id,omitempty" jsonschema:"Task identifier, which update and delete address."`
	Description   string     `json:"description,omitempty" jsonschema:"The task text, which appears on every pull request the matchers cover."`
	SourceMatcher RefMatcher `json:"sourceMatcher,omitzero" jsonschema:"Which source branches the task applies to; type ANY_REF when it applies to all of them."`
	TargetMatcher RefMatcher `json:"targetMatcher,omitzero" jsonschema:"Which target branches the task applies to; type ANY_REF when it applies to all of them."`
}
