package repocmd

import (
	"bytes"
	"encoding/base64"
	"testing"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
)

// TestRepoCatBase64EncodesBytesThatAreNotText is the guard on the encoding
// field.
//
// A JSON string cannot carry arbitrary bytes: Go's encoder substitutes U+FFFD
// for invalid UTF-8, so without this a binary file came back corrupted with
// nothing saying so.
func TestRepoCatBase64EncodesBytesThatAreNotText(t *testing.T) {
	t.Parallel()

	binary := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a, 0xff, 0xfe}

	converted := rawFileFrom(result.Repository{ProjectKey: "PRJ", Slug: "demo"}, "logo.png", "", binary)
	if converted.Encoding != "base64" {
		t.Fatalf("encoding = %q, want base64", converted.Encoding)
	}
	decoded, err := base64.StdEncoding.DecodeString(converted.Content)
	if err != nil {
		t.Fatalf("content did not decode as base64: %v", err)
	}
	if !bytes.Equal(decoded, binary) {
		t.Fatalf("the file did not survive the round trip: got %v, want %v", decoded, binary)
	}

	text := rawFileFrom(result.Repository{ProjectKey: "PRJ", Slug: "demo"}, "README.md", "", []byte("# hello\n"))
	if text.Encoding != "utf-8" {
		t.Fatalf("encoding = %q, want utf-8", text.Encoding)
	}
	if text.Content != "# hello\n" {
		t.Fatalf("content = %q, want the text unchanged", text.Content)
	}
}
