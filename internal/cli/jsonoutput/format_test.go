package jsonoutput

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"gopkg.in/yaml.v3"
)

// TestEveryWriterEncodesInTheFormatItsWriterCarries holds each of the four
// document writers to ADR-095: the document is the same, only its encoding
// follows the writer, and a writer carrying nothing gets JSON as it always has.
func TestEveryWriterEncodesInTheFormatItsWriterCarries(t *testing.T) {
	t.Parallel()

	writers := map[string]func(writer io.Writer) error{
		"Write":      func(writer io.Writer) error { return Write(writer, map[string]any{"id": 7, "name": "yes"}) },
		"WriteList":  func(writer io.Writer) error { return WriteList(writer, []string{"a", "b"}, true) },
		"WriteBytes": func(writer io.Writer) error { return WriteBytes(writer, []byte{0, 1, 2}, "image/png") },
		"WriteError": func(writer io.Writer) error {
			return WriteError(writer, apperrors.New(apperrors.KindNotFound, "no such pull request", nil))
		},
	}

	for name, write := range writers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var unbound bytes.Buffer
			if err := write(&unbound); err != nil {
				t.Fatalf("unbound: %v", err)
			}
			var fromJSON any
			if err := json.Unmarshal(unbound.Bytes(), &fromJSON); err != nil {
				t.Fatalf("a writer carrying no settings did not get JSON: %v\n%s", err, unbound.String())
			}

			var asYAML bytes.Buffer
			if err := write(Bind(&asYAML, Settings{Format: FormatYAML})); err != nil {
				t.Fatalf("yaml: %v", err)
			}
			if json.Valid(asYAML.Bytes()) {
				t.Fatalf("a writer carrying YAML got JSON:\n%s", asYAML.String())
			}

			var fromYAML any
			if err := yaml.Unmarshal(asYAML.Bytes(), &fromYAML); err != nil {
				t.Fatalf("not YAML: %v\n%s", err, asYAML.String())
			}
			if !reflect.DeepEqual(throughJSON(fromJSON), throughJSON(fromYAML)) {
				t.Fatalf("--yaml is not the --json document:\njson: %#v\nyaml: %#v", fromJSON, fromYAML)
			}

			var asJSON bytes.Buffer
			if err := write(Bind(&asJSON, Settings{Format: FormatJSON})); err != nil {
				t.Fatalf("json: %v", err)
			}
			if !bytes.Equal(asJSON.Bytes(), unbound.Bytes()) {
				t.Fatalf("a writer bound to JSON wrote something else than an unbound one:\n%s\n%s", asJSON.String(), unbound.String())
			}
		})
	}
}

// TestBindReplacesRatherThanStacks: a writer bound twice carries the second
// settings, so a command tree executed twice in one process cannot end up
// wrapped in both answers.
func TestBindReplacesRatherThanStacks(t *testing.T) {
	t.Parallel()

	var buffer bytes.Buffer
	twice := Bind(Bind(&buffer, Settings{Format: FormatYAML}), Settings{Format: FormatJSON})

	if got := settingsOf(twice).Format; got != FormatJSON {
		t.Fatalf("rebinding kept %q, want the new settings", got)
	}
	if inner := twice.(*boundWriter).Writer; inner != &buffer {
		t.Fatalf("rebinding wrapped the bound writer instead of replacing it")
	}
}

// throughJSON gives a decoded value the types encoding/json would, so a YAML
// int and a JSON float64 holding the same number compare equal.
func throughJSON(value any) any {
	encoded, _ := json.Marshal(value)
	var decoded any
	_ = json.Unmarshal(encoded, &decoded)
	return decoded
}
