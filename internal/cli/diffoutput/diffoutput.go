// Package diffoutput renders a diff, in both machine and human form.
//
// It exists because there were two copies of this: one in the diff commands and
// one in `bb pr diff`, which the help text calls an alias for `bb diff pr`. The
// copies had already drifted -- passing two output flags produced a different
// error message depending on which of the two "same" commands you ran. An alias
// that is a second implementation is not an alias.
package diffoutput

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
)

// Diff is what the diff commands return.
//
// One shape for all three output modes, with output saying which was asked for
// and the matching field filled in. Before this the payload changed key with
// the flag -- {"patch": ...}, {"names": ...} or {"stats": ...} -- so one command
// had three contracts and a consumer had to know which flag it had passed to
// know what it was reading.
type Diff struct {
	Repository result.Repository `json:"repository"`
	Output     string            `json:"output" jsonschema:"Which form was produced: patch, stat or name-only."`
	Patch      string            `json:"patch,omitempty" jsonschema:"The unified diff, when output is patch."`
	// Names is a pointer because omitempty drops an empty slice as readily as a
	// nil one, and the two mean different things here: absent says this was not
	// a name-only run, an empty array says it was and nothing differs. A caller
	// branching on the key would otherwise read "no diff" as "wrong mode".
	Names *[]string `json:"names,omitempty" jsonschema:"Paths that differ, when output is name-only. Present and possibly empty for a name-only run, absent for any other."`
	// Stats is left open. Bitbucket declares the diff stats summary as an
	// untyped value in its own specification -- the generated client renders it
	// as interface{} -- so bb has nothing to promise about its fields.
	Stats map[string]any `json:"stats,omitempty" jsonschema:"Per-file addition and deletion counts, when output is stat. Absent when the server returned no summary, which output still reports as stat. Left open because Bitbucket declares this summary as an untyped value."`
}

// Outputs is the closed set of forms a diff command can produce.
var Outputs = []string{"patch", "stat", "name-only"}

func init() {
	enums := map[string][]string{"output": Outputs}

	result.Declare("diff refs", result.For[Diff](enums))
	result.Declare("diff commit", result.For[Diff](enums))
	result.Declare("diff pr", result.For[Diff](enums))
	result.Declare("pr diff", result.For[Diff](enums))
}

// ResolveOutputMode turns the three output flags into one mode.
func ResolveOutputMode(patch, stat, nameOnly bool) (diffservice.OutputKind, error) {
	selected := 0
	mode := diffservice.OutputKindRaw
	if patch {
		selected++
		mode = diffservice.OutputKindPatch
	}
	if stat {
		selected++
		mode = diffservice.OutputKindStat
	}
	if nameOnly {
		selected++
		mode = diffservice.OutputKindNameOnly
	}
	if selected > 1 {
		return "", apperrors.New(apperrors.KindValidation, "only one of --patch, --stat, or --name-only may be specified", nil)
	}

	return mode, nil
}

// Write renders a diff result, as a machine document or for a reader.
func Write(
	writer io.Writer,
	asJSON bool,
	repository result.Repository,
	mode diffservice.OutputKind,
	diff diffservice.Result,
	writeJSON func(io.Writer, any) error,
) error {
	if asJSON {
		return writeJSON(writer, From(repository, mode, diff))
	}

	switch mode {
	case diffservice.OutputKindNameOnly:
		for _, name := range diff.Names {
			fmt.Fprintln(writer, name)
		}

		return nil
	case diffservice.OutputKindStat:
		return writeJSON(writer, statsOf(diff))
	default:
		fmt.Fprint(writer, diff.Patch)
		if diff.Patch != "" && !strings.HasSuffix(diff.Patch, "\n") {
			fmt.Fprintln(writer)
		}

		return nil
	}
}

// From converts a service result into the published shape.
func From(repository result.Repository, mode diffservice.OutputKind, diff diffservice.Result) Diff {
	converted := Diff{Repository: repository, Output: outputName(mode)}

	switch mode {
	case diffservice.OutputKindNameOnly:
		names := diff.Names
		if names == nil {
			names = []string{}
		}
		converted.Names = &names
	case diffservice.OutputKindStat:
		converted.Stats = statsOf(diff)
	default:
		converted.Patch = diff.Patch
	}

	return converted
}

// outputName names the mode the way the payload reports it.
//
// Raw and patch are the same output -- raw is what the commands default to and
// patch is what --patch selects -- so both report patch rather than publishing
// an internal distinction that makes no difference to what is returned.
func outputName(mode diffservice.OutputKind) string {
	switch mode {
	case diffservice.OutputKindNameOnly:
		return "name-only"
	case diffservice.OutputKindStat:
		return "stat"
	default:
		return "patch"
	}
}

// statsOf reads the untyped stats summary as an object.
//
// A round trip rather than a type assertion: Bitbucket declares this summary as
// an untyped value, so the generated client hands back a *interface{} holding
// whatever was decoded, and there is no static type to assert on.
func statsOf(diff diffservice.Result) map[string]any {
	if diff.Stats == nil {
		return nil
	}

	raw, err := json.Marshal(diff.Stats)
	if err != nil {
		return nil
	}

	var stats map[string]any
	if err := json.Unmarshal(raw, &stats); err != nil {
		return nil
	}

	return stats
}
