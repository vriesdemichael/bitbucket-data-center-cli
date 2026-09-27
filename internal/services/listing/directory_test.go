package listing

import "testing"

func TestEncodeDirectoryKeepsTheSeparators(t *testing.T) {
	t.Parallel()

	// Escaped whole, the separators become %2F and the browse endpoint
	// refuses the request; escaped per segment they survive and a half-typed
	// word still cannot reach a different endpoint.
	got, err := encodeDirectory("src/main resources/a?b/")
	if err != nil {
		t.Fatalf("encodeDirectory returned %v", err)
	}
	if got != "src/main%20resources/a%3Fb" {
		t.Fatalf("encodeDirectory = %q", got)
	}

	if got, err := encodeDirectory(""); err != nil || got != "" {
		t.Fatalf("encodeDirectory of the root = (%q, %v), want the repository root", got, err)
	}

	if _, err := encodeDirectory("internal/../../etc"); err == nil {
		t.Fatal("a traversal was encoded instead of refused")
	}
}

func TestBrowsePathIsTheDirectoryEndpoint(t *testing.T) {
	t.Parallel()

	if got := browsePath("~alice", "my service", "internal/cli"); got != "/rest/api/latest/projects/~alice/repos/my%20service/browse/internal/cli" {
		t.Fatalf("browsePath = %q", got)
	}
	// The repository root, which the endpoint answers with a trailing slash.
	if got := browsePath("~alice", "my service", ""); got != "/rest/api/latest/projects/~alice/repos/my%20service/browse/" {
		t.Fatalf("browsePath of the root = %q", got)
	}
}
