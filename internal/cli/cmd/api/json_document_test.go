package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestAResponseUnderJSONIsOneDocument is what a caller parsing stdout gets for
// each kind of body under --json (ADR-075). A body that is not text used to come
// back as a string with each byte that is not UTF-8 replaced by U+FFFD, which no
// caller could turn back into the file; an empty one, a 204 to a DELETE, came
// back as nothing at all.
func TestAResponseUnderJSONIsOneDocument(t *testing.T) {
	t.Parallel()

	// A PNG signature, a NUL, bytes that are not UTF-8, and the whitespace a
	// trim would take.
	binary := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0x00, 0xff, 0xfe, ' ', '\n'}
	encoded := base64.StdEncoding.EncodeToString(binary)

	for _, testCase := range []struct {
		name        string
		contentType string
		body        []byte
		wantData    any
		wantMeta    map[string]any
	}{
		{"a body that is not text goes in as base64", "image/png", binary, encoded,
			map[string]any{"encoding": "base64", "contentType": "image/png"}},
		{"an undeclared body that is not UTF-8 is sniffed for its type", "", binary, encoded,
			map[string]any{"encoding": "base64", "contentType": "image/png"}},
		{"a JSON body is its value", "application/json", []byte(`{"id": 1}`), map[string]any{"id": float64(1)}, nil},
		{"other text is a string, trimmed", "text/plain", []byte("  hello\n"), "hello", nil},
		{"an empty body is null", "", nil, nil, nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			header := http.Header{}
			if testCase.contentType != "" {
				header.Set("Content-Type", testCase.contentType)
			}

			var out bytes.Buffer
			if err := writeResponse(&out, header, testCase.body, Dependencies{JSONEnabled: func() bool { return true }}); err != nil {
				t.Fatalf("writeResponse: %v", err)
			}

			var document map[string]any
			if err := json.Unmarshal(out.Bytes(), &document); err != nil {
				t.Fatalf("stdout is not one JSON document: %v\n%q", err, out.String())
			}
			if data, present := document["data"]; !present || !reflect.DeepEqual(data, testCase.wantData) {
				t.Fatalf("data = %#v (present %v), want %#v", data, present, testCase.wantData)
			}

			meta, _ := document["meta"].(map[string]any)
			for key, want := range testCase.wantMeta {
				if meta[key] != want {
					t.Fatalf("meta.%s = %#v, want %#v", key, meta[key], want)
				}
			}
			if testCase.wantMeta == nil {
				if _, present := meta["encoding"]; present {
					t.Fatalf("a text body carries meta.encoding: %v", meta)
				}
			}
		})
	}
}

// TestJSONRefusesToEncodeABodyOverTheCap: --json holds a body that is not text
// whole, and a third larger once encoded, so above the cap it refuses and says
// to drop --json. Streamed, it refuses as the body arrives, before holding it.
func TestJSONRefusesToEncodeABodyOverTheCap(t *testing.T) {
	t.Parallel()

	oversized := make([]byte, maxEncodedResponseBytes+1)
	header := http.Header{"Content-Type": []string{"application/octet-stream"}}

	err := writeResponse(io.Discard, header, oversized, Dependencies{JSONEnabled: func() bool { return true }})
	if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "without --json") {
		t.Fatalf("got %v, want a validation error saying to drop --json", err)
	}

	response := &streamedResponse{out: io.Discard, json: true}
	if err := response.Open(header); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := response.Write(oversized); !apperrors.IsKind(err, apperrors.KindValidation) {
		t.Fatalf("a streamed body over the cap was accepted: %v", err)
	}
	if held := response.held.Len(); held != 0 {
		t.Fatalf("held %d bytes before refusing them", held)
	}
}
