package browsecmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// Target is what `bb browse` worked out the caller was asking for.
//
// Every field is reported whether or not it applies to the kind, so a consumer
// can read one shape rather than branching on kind to know which keys exist.
// kind says which of them carry meaning.
type Target struct {
	Kind   string `json:"kind" jsonschema:"What was addressed: home, settings, releases, pull_request, commit or path."`
	Arg    string `json:"arg" jsonschema:"The positional argument as it was typed, empty when none was given."`
	Number int    `json:"number" jsonschema:"Pull request number. Zero unless kind is pull_request."`
	Path   string `json:"path" jsonschema:"Repository path. Empty unless kind is path."`
	Line   int    `json:"line" jsonschema:"Line within that path. Zero when no line was given."`
	Branch string `json:"branch" jsonschema:"Branch the path is browsed on, when --branch was given."`
	Commit string `json:"commit" jsonschema:"Commit addressed, or the commit a path is browsed at."`
	Blame  bool   `json:"blame" jsonschema:"Whether the URL opens the blame view."`
}

// Destination is what `bb browse` returns.
//
// url is the point of the command: with --json (or --no-browser) nothing is
// opened and the caller gets the address to do what it likes with.
type Destination struct {
	URL        string            `json:"url" jsonschema:"The Bitbucket URL the command resolved to."`
	Repository result.Repository `json:"repository"`
	Target     Target            `json:"target"`
}

func init() {
	result.Declare("browse", result.For[Destination](map[string][]string{
		"target.kind": {
			string(browseTargetHome),
			string(browseTargetSettings),
			string(browseTargetReleases),
			string(browseTargetPR),
			string(browseTargetCommit),
			string(browseTargetPath),
		},
	}))
}

// targetFrom converts the internal resolution result.
func targetFrom(target browseTarget) Target {
	return Target{
		Kind:   string(target.kind),
		Arg:    target.rawArg,
		Number: target.number,
		Path:   target.path,
		Line:   target.line,
		Branch: target.branch,
		Commit: target.commit,
		Blame:  target.blame,
	}
}
