package jsonoutput

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
)

func TestWriteSuccess(t *testing.T) {
	buffer := &bytes.Buffer{}

	err := Write(buffer, map[string]any{"status": "ok"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	output := buffer.String()
	if !strings.Contains(output, "\"version\": \"v2\"") {
		t.Fatalf("expected version field in output, got %s", output)
	}
	if !strings.Contains(output, "\"contract\": \"bb.machine\"") {
		t.Fatalf("expected contract field in output, got %s", output)
	}
	if !strings.Contains(output, "\"status\": \"ok\"") {
		t.Fatalf("expected payload field in output, got %s", output)
	}
}

func TestWriteMarshalFailure(t *testing.T) {
	err := Write(&bytes.Buffer{}, map[string]any{"invalid": func() {}})
	if err == nil {
		t.Fatal("expected marshal failure")
	}
	if apperrors.KindOf(err) != apperrors.KindInternal {
		t.Fatalf("expected internal kind, got %q", apperrors.KindOf(err))
	}
	if !strings.Contains(err.Error(), "failed to encode JSON output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteWriterFailure(t *testing.T) {
	err := Write(failingWriter{}, map[string]any{"status": "ok"})
	if err == nil {
		t.Fatal("expected write failure")
	}
	if apperrors.KindOf(err) != apperrors.KindInternal {
		t.Fatalf("expected internal kind, got %q", apperrors.KindOf(err))
	}
	if !strings.Contains(err.Error(), "failed to write JSON output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

type failingWriter struct{}

func (failingWriter) Write(_ []byte) (int, error) {
	return 0, errors.New("boom")
}

func TestEnvelopeSchemaFor(t *testing.T) {
	dataSchema := map[string]any{"type": "string"}
	schema := EnvelopeSchemaFor("test.schema.json", "Test Title", "Test description", dataSchema)

	if schema["$schema"] != jsonSchemaVersion {
		t.Errorf("expected $schema=%q, got %q", jsonSchemaVersion, schema["$schema"])
	}
	expected := SchemaBaseURL + "test.schema.json"
	if schema["$id"] != expected {
		t.Errorf("expected $id=%q, got %q", expected, schema["$id"])
	}
	if schema["title"] != "Test Title" {
		t.Errorf("unexpected title: %v", schema["title"])
	}
	if schema["description"] != "Test description" {
		t.Errorf("unexpected description: %v", schema["description"])
	}

	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties map")
	}
	for _, field := range []string{"version", "data", "meta"} {
		if _, ok := props[field]; !ok {
			t.Errorf("missing envelope property %q", field)
		}
	}
	if props["data"] == nil {
		t.Error("expected data property to be set")
	}

	req, ok := schema["required"].([]any)
	if !ok || len(req) != 3 {
		t.Fatalf("expected required=[version,data,meta], got %v", schema["required"])
	}
}

func TestWriteErrorEmitsClassifiedEnvelope(t *testing.T) {
	buffer := &bytes.Buffer{}
	if err := WriteError(buffer, apperrors.New(apperrors.KindConflict, "branch already exists", errors.New("409"))); err != nil {
		t.Fatalf("WriteError returned %v", err)
	}

	var envelope ErrorEnvelope
	if err := json.Unmarshal(buffer.Bytes(), &envelope); err != nil {
		t.Fatalf("expected parseable output, got %q (%v)", buffer.String(), err)
	}

	if envelope.Version != ContractVersion || envelope.Meta.Contract != ContractName {
		t.Fatalf("unexpected envelope header %+v", envelope)
	}
	if envelope.Error.Kind != string(apperrors.KindConflict) {
		t.Fatalf("expected conflict kind, got %q", envelope.Error.Kind)
	}
	if envelope.Error.ExitCode != 5 {
		t.Fatalf("expected exit_code 5, got %d", envelope.Error.ExitCode)
	}
	// The cause is preserved; only the redundant kind prefix is dropped.
	if envelope.Error.Message != "branch already exists (409)" {
		t.Fatalf("unexpected message %q", envelope.Error.Message)
	}
}

func TestWriteErrorClassifiesPlainErrorsAsInternal(t *testing.T) {
	buffer := &bytes.Buffer{}
	if err := WriteError(buffer, errors.New("something broke")); err != nil {
		t.Fatalf("WriteError returned %v", err)
	}

	var envelope ErrorEnvelope
	if err := json.Unmarshal(buffer.Bytes(), &envelope); err != nil {
		t.Fatalf("expected parseable output, got %q (%v)", buffer.String(), err)
	}
	if envelope.Error.Kind != string(apperrors.KindInternal) || envelope.Error.ExitCode != 1 {
		t.Fatalf("expected internal/1 fallback, got %+v", envelope.Error)
	}
	if envelope.Error.Message != "something broke" {
		t.Fatalf("unexpected message %q", envelope.Error.Message)
	}
}

func TestWriteErrorIgnoresNil(t *testing.T) {
	buffer := &bytes.Buffer{}
	if err := WriteError(buffer, nil); err != nil {
		t.Fatalf("WriteError returned %v", err)
	}
	if buffer.String() != "" {
		t.Fatalf("expected no output for a nil error, got %q", buffer.String())
	}
}

func TestEnvelopesDoNotEscapeHTMLCharacters(t *testing.T) {
	errorBuffer := &bytes.Buffer{}
	if err := WriteError(errorBuffer, apperrors.New(apperrors.KindValidation, "run 'bb auth login <host>'", nil)); err != nil {
		t.Fatalf("WriteError returned %v", err)
	}

	dataBuffer := &bytes.Buffer{}
	if err := Write(dataBuffer, map[string]string{"hint": "<host>"}); err != nil {
		t.Fatalf("Write returned %v", err)
	}

	for name, output := range map[string]string{"error": errorBuffer.String(), "data": dataBuffer.String()} {
		if strings.Contains(output, `\u003c`) {
			t.Fatalf("%s envelope escaped angle brackets: %s", name, output)
		}
		if !strings.Contains(output, "<host>") {
			t.Fatalf("%s envelope lost the literal placeholder: %s", name, output)
		}
	}
}

func TestErrorEnvelopeSchemaMatchesTheTaxonomy(t *testing.T) {
	schema := ErrorEnvelopeSchema("output.error.schema.json")

	properties := schema["properties"].(map[string]any)
	errorSchema := properties["error"].(map[string]any)
	errorProperties := errorSchema["properties"].(map[string]any)

	kindEnum := errorProperties["kind"].(map[string]any)["enum"].([]any)
	if len(kindEnum) != len(apperrors.Kinds()) {
		t.Fatalf("schema lists %d kinds, taxonomy has %d", len(kindEnum), len(apperrors.Kinds()))
	}

	listed := map[string]bool{}
	for _, kind := range kindEnum {
		listed[kind.(string)] = true
	}
	for _, kind := range apperrors.Kinds() {
		if !listed[string(kind)] {
			t.Fatalf("kind %q is missing from the published schema", kind)
		}
	}

	// Every kind's exit code must be an allowed value, or the CLI can emit an
	// envelope that fails validation against its own published schema.
	codeEnum := errorProperties["exit_code"].(map[string]any)["enum"].([]any)
	allowed := map[int]bool{}
	for _, code := range codeEnum {
		allowed[code.(int)] = true
	}
	for _, kind := range apperrors.Kinds() {
		code := apperrors.ExitCode(apperrors.New(kind, "", nil))
		if !allowed[code] {
			t.Fatalf("exit code %d for kind %q is not in the schema enum", code, kind)
		}
	}
}

func TestWriteErrorWriterFailure(t *testing.T) {
	err := WriteError(failingWriter{}, apperrors.New(apperrors.KindTransient, "upstream unavailable", nil))
	if err == nil {
		t.Fatal("expected write failure")
	}
	if apperrors.KindOf(err) != apperrors.KindInternal {
		t.Fatalf("expected internal kind, got %q", apperrors.KindOf(err))
	}
	if !strings.Contains(err.Error(), "failed to write JSON error output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteListCarriesLimitReached(t *testing.T) {
	for _, reached := range []bool{true, false} {
		buffer := &bytes.Buffer{}
		if err := WriteList(buffer, []string{"a"}, reached); err != nil {
			t.Fatalf("WriteList returned %v", err)
		}

		var envelope struct {
			Meta struct {
				Contract     string `json:"contract"`
				LimitReached *bool  `json:"limit_reached"`
			} `json:"meta"`
		}
		if err := json.Unmarshal(buffer.Bytes(), &envelope); err != nil {
			t.Fatalf("expected a parseable envelope, got %q (%v)", buffer.String(), err)
		}
		if envelope.Meta.LimitReached == nil {
			t.Fatal("expected limit_reached to be present on a list envelope")
		}
		if *envelope.Meta.LimitReached != reached {
			t.Fatalf("limit_reached = %v, want %v", *envelope.Meta.LimitReached, reached)
		}
		if envelope.Meta.Contract != ContractName {
			t.Fatalf("unexpected contract %q", envelope.Meta.Contract)
		}
	}
}

// TestWriteOmitsLimitReached keeps the field meaningful: its presence is the
// signal that a result set is bounded, so a non-list command must not carry it.
func TestWriteOmitsLimitReached(t *testing.T) {
	buffer := &bytes.Buffer{}
	if err := Write(buffer, map[string]string{"a": "b"}); err != nil {
		t.Fatalf("Write returned %v", err)
	}

	if strings.Contains(buffer.String(), "limit_reached") {
		t.Fatalf("non-list envelope carries limit_reached: %s", buffer.String())
	}
}

func TestWriteListMarshalFailure(t *testing.T) {
	err := WriteList(&bytes.Buffer{}, map[string]any{"invalid": func() {}}, false)
	if err == nil {
		t.Fatal("expected marshal failure")
	}
	if !strings.Contains(err.Error(), "failed to encode JSON output") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWriteListWriterFailure(t *testing.T) {
	err := WriteList(failingWriter{}, []string{"a"}, true)
	if err == nil {
		t.Fatal("expected write failure")
	}
	if !strings.Contains(err.Error(), "failed to write JSON output") {
		t.Fatalf("unexpected error: %v", err)
	}
}
