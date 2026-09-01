package result

// Repository names a repository in a payload.
//
// It exists because 49 payloads embedded a service's RepositoryRef directly,
// and those structs carry no JSON tags -- so bb published
// {"repository":{"ProjectKey":"PRJ","Slug":"payments"}}, Go field names and
// all, while every other field it emits is snake_case. That is what happens
// when the payload is whatever struct was in scope rather than something
// chosen: the internal type becomes the contract without anyone deciding it
// should.
//
// snake_case, because these are bb's own field names. Fields mirroring an
// upstream Bitbucket object keep the upstream spelling instead -- displayId,
// latestCommit -- so a reader comparing bb output against the Bitbucket API
// docs sees the same words.
type Repository struct {
	ProjectKey string `json:"project_key" jsonschema:"Project key the repository belongs to."`
	Slug       string `json:"slug" jsonschema:"Repository slug."`
}

// Status reports the outcome of a command whose result is the fact that it
// worked.
//
// Embedded rather than used alone, so a command can add what it acted on:
//
//	type Deletion struct {
//	    result.Status
//	    Tag string `json:"tag"`
//	}
//
// The value is "ok" across the surface today, with a handful of commands
// saying "deleted" instead. Both are kept rather than unified here, because
// changing what a command reports is a contract change and this type exists to
// stop shapes drifting, not to quietly re-spell them.
type Status struct {
	Status string `json:"status" jsonschema:"Outcome of the command. A failure reports an error envelope instead of this payload."`
}

// OK is the common case: the command did what it was asked.
func OK() Status {
	return Status{Status: "ok"}
}
