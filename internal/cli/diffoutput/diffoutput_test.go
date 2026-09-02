package diffoutput

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	diffservice "github.com/vriesdemichael/bitbucket-data-center-cli/internal/services/diff"
)

var repository = result.Repository{ProjectKey: "PRJ", Slug: "payments"}

// TestOneShapeForEveryOutputMode is what this package exists for. The payload
// used to change key with the flag -- {"patch"}, {"names"} or {"stats"} -- so
// one command had three contracts and a consumer had to know which flag it had
// passed to know what it was reading.
func TestOneShapeForEveryOutputMode(t *testing.T) {
	t.Parallel()

	patch := From(repository, diffservice.OutputKindPatch, diffservice.Result{Patch: "diff --git a/x b/x\n"})
	if patch.Output != "patch" || patch.Patch == "" {
		t.Fatalf("patch = %+v", patch)
	}
	if patch.Names != nil || patch.Stats != nil {
		t.Fatalf("a patch run carried the other forms: %+v", patch)
	}

	// Raw is what the commands default to and patch is what --patch selects.
	// They produce the same output, so they report the same name rather than
	// publishing an internal distinction.
	if raw := From(repository, diffservice.OutputKindRaw, diffservice.Result{Patch: "x"}); raw.Output != "patch" {
		t.Fatalf("raw output = %q, want patch", raw.Output)
	}

	names := From(repository, diffservice.OutputKindNameOnly, diffservice.Result{Names: []string{"a.go", "b.go"}})
	if names.Output != "name-only" || names.Names == nil || len(*names.Names) != 2 || names.Patch != "" {
		t.Fatalf("names = %+v", names)
	}

	// Asserted on the encoded document, not the Go value. The earlier version of
	// this check read empty.Names != nil and passed while the key was being
	// dropped: omitempty discards an empty slice as readily as a nil one, so the
	// struct said "present and empty" and the JSON said nothing at all.
	encoded, err := json.Marshal(From(repository, diffservice.OutputKindNameOnly, diffservice.Result{}))
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(encoded, &document); err != nil {
		t.Fatalf("decode: %v", err)
	}
	list, present := document["names"]
	if !present {
		t.Fatalf("a name-only run with no differing files dropped the key: %s", encoded)
	}
	if entries, ok := list.([]any); !ok || len(entries) != 0 {
		t.Fatalf("names = %v, want an empty array", list)
	}

	// The other modes must not carry it at all, or the key stops meaning
	// "this was a name-only run".
	patchEncoded, err := json.Marshal(patch)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.Contains(string(patchEncoded), `"names"`) {
		t.Fatalf("a patch run carried names: %s", patchEncoded)
	}

	summary := map[string]any{"linesAdded": float64(10), "linesRemoved": float64(2)}
	stats := From(repository, diffservice.OutputKindStat, diffservice.Result{Stats: summary})
	if stats.Output != "stat" || stats.Stats["linesAdded"] != float64(10) {
		t.Fatalf("stats = %+v", stats)
	}

	// The repository is on every one of them.
	for _, converted := range []Diff{patch, names, stats} {
		if converted.Repository != repository {
			t.Fatalf("%q did not name the repository: %+v", converted.Output, converted)
		}
	}
}

// TestStatsSurviveThePointerTheGeneratedClientHandsBack covers the shape the
// service actually returns: Bitbucket declares the stats summary as an untyped
// value, so the generated client holds a pointer to an interface and there is
// no static type to assert on.
func TestStatsSurviveThePointerTheGeneratedClientHandsBack(t *testing.T) {
	t.Parallel()

	var boxed any = map[string]any{"linesAdded": float64(10)}
	converted := From(repository, diffservice.OutputKindStat, diffservice.Result{Stats: &boxed})
	if converted.Stats["linesAdded"] != float64(10) {
		t.Fatalf("stats = %+v, want the summary read through the pointer", converted.Stats)
	}

	// A run the server answered with nothing omits the key rather than
	// publishing a null, which is what the old payload did.
	if absent := From(repository, diffservice.OutputKindStat, diffservice.Result{}); absent.Stats != nil {
		t.Fatalf("an empty stats run produced %+v, want the key omitted", absent.Stats)
	}
}

func TestResolveOutputModeRefusesTwoForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                  string
		patch, stat, nameOnly bool
		want                  diffservice.OutputKind
		wantErr               bool
	}{
		{name: "no flags", want: diffservice.OutputKindRaw},
		{name: "patch", patch: true, want: diffservice.OutputKindPatch},
		{name: "stat", stat: true, want: diffservice.OutputKindStat},
		{name: "name-only", nameOnly: true, want: diffservice.OutputKindNameOnly},
		{name: "two at once", patch: true, stat: true, wantErr: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			mode, err := ResolveOutputMode(testCase.patch, testCase.stat, testCase.nameOnly)
			if testCase.wantErr {
				if err == nil {
					t.Fatal("expected an error for two output flags")
				}
				if apperrors.KindOf(err) != apperrors.KindValidation {
					t.Fatalf("kind = %v, want validation", apperrors.KindOf(err))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mode != testCase.want {
				t.Fatalf("mode = %v, want %v", mode, testCase.want)
			}
		})
	}
}

func TestWriteRendersBothWays(t *testing.T) {
	t.Parallel()

	t.Run("machine", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		err := Write(buffer, true, repository, diffservice.OutputKindPatch, diffservice.Result{Patch: "the patch\n"}, encodeInto)
		if err != nil {
			t.Fatalf("Write returned %v", err)
		}

		var decoded Diff
		if err := json.Unmarshal(buffer.Bytes(), &decoded); err != nil {
			t.Fatalf("machine output is not the published shape: %v\n%s", err, buffer.String())
		}
		if decoded.Output != "patch" || decoded.Patch != "the patch\n" {
			t.Fatalf("decoded = %+v", decoded)
		}
	})

	t.Run("human patch", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		if err := Write(buffer, false, repository, diffservice.OutputKindPatch, diffservice.Result{Patch: "no trailing newline"}, encodeInto); err != nil {
			t.Fatalf("Write returned %v", err)
		}
		// A patch that does not end in a newline gets one, so the next thing
		// the terminal prints starts on its own line.
		if buffer.String() != "no trailing newline\n" {
			t.Fatalf("human patch = %q", buffer.String())
		}
	})

	t.Run("human names", func(t *testing.T) {
		buffer := &bytes.Buffer{}
		if err := Write(buffer, false, repository, diffservice.OutputKindNameOnly, diffservice.Result{Names: []string{"a.go", "b.go"}}, encodeInto); err != nil {
			t.Fatalf("Write returned %v", err)
		}
		if buffer.String() != "a.go\nb.go\n" {
			t.Fatalf("human names = %q", buffer.String())
		}
	})
}

// encodeInto stands in for the envelope writer the commands pass, so the test
// can read what was handed to it.
func encodeInto(writer io.Writer, value any) error {
	return json.NewEncoder(writer).Encode(value)
}
