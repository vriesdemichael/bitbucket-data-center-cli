package repocmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	openapigenerated "github.com/vriesdemichael/bitbucket-data-center-cli/internal/openapi/generated"
)

// TestAnArchiveThatBreaksOffLeavesNoFile: an archive download that fails
// part-way leaves the target as it was. Written into place, it left a
// truncated archive there, and had already truncated the file that was there
// before the first byte arrived.
// mock-inventory: transport-fault — a connection dropped mid-archive, which no
// live Bitbucket does on request; the subject is what bb repo archive leaves.
func TestAnArchiveThatBreaksOffLeavesNoFile(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/repos/demo/archive") {
			http.NotFound(writer, request)

			return
		}
		writer.Header().Set("Content-Length", "100000")
		_, _ = writer.Write(make([]byte, 40_000))
		writer.(http.Flusher).Flush()
		panic(http.ErrAbortHandler)
	}))
	defer server.Close()

	directory := t.TempDir()
	target := filepath.Join(directory, "demo.zip")
	if err := os.WriteFile(target, []byte("last week's archive"), 0o600); err != nil {
		t.Fatalf("write the existing archive: %v", err)
	}

	setup := testSetup{Host: server.URL, Token: "token", ProjectKey: "PRJ", RepoSlug: "demo"}
	out, err := executeTestCLIWith(t, setup, "repo", "archive", "--output", target)
	if !apperrors.IsKind(err, apperrors.KindTransient) || !strings.Contains(err.Error(), "failed to stream the repository archive") {
		t.Fatalf("got %v, want the broken download reported as transient\noutput: %s", err, out)
	}

	listed, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read the directory: %v", err)
	}
	if len(listed) != 1 || listed[0].Name() != "demo.zip" {
		t.Fatalf("a failed archive download left %v in the directory", listed)
	}
	if kept, _ := os.ReadFile(target); string(kept) != "last week's archive" {
		t.Fatalf("a failed archive download changed the file already there to %d bytes", len(kept))
	}
}

// TestTheArchiveIsRequestedUnderTheRESTPath: the generated builder resolves its
// path against the server URL, and without a trailing slash drops that URL's
// last segment, /rest, so every archive request reached the web UI's 404 page.
func TestTheArchiveIsRequestedUnderTheRESTPath(t *testing.T) {
	t.Parallel()

	format := "zip"
	for _, host := range []string{"https://bitbucket.example", "https://bitbucket.example/", "https://example.com/bitbucket"} {
		got, err := archiveRequestURL(host, "PROJ", "repo", &openapigenerated.GetArchiveParams{Format: &format})
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		want := strings.TrimRight(host, "/") + "/rest/api/latest/projects/PROJ/repos/repo/archive?format=zip"
		if got != want {
			t.Fatalf("%s: archive URL %s, want %s", host, got, want)
		}
	}
}
