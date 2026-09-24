package outline_test

import (
	"os"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/outline"
)

// The outline in the package documentation, derived the way a command's result
// is: from a Go type, with the enum applied afterwards.
func ExampleWrite() {
	type pullRequest struct {
		Description string `json:"description,omitempty" jsonschema:"Body text, when one was written."`
		State       string `json:"state" jsonschema:"OPEN, MERGED or DECLINED."`
	}
	type change struct {
		PullRequest pullRequest `json:"pullRequest" jsonschema:"The pull request as it stands after the change."`
	}

	schema, err := jsonschema.For[change](nil)
	if err != nil {
		panic(err)
	}
	schema.Properties["pullRequest"].Properties["state"].Enum = []any{"OPEN", "MERGED", "DECLINED"}

	if err := outline.Write(os.Stdout, outline.Member{Name: "data", Schema: schema}); err != nil {
		panic(err)
	}
	// Output:
	// data
	//   pullRequest     object                The pull request as it stands after the change.
	//     description?  string                Body text, when one was written.
	//     state         OPEN|MERGED|DECLINED
}
