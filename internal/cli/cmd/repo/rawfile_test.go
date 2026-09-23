package repocmd

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// rawFileCommands are the two commands that write a file's bytes.
var rawFileCommands = [][]string{
	{"repo", "cat", "big.bin"},
	{"repo", "browse", "raw", "big.bin"},
}

// errDiskFull is what the failing standard output answers with.
var errDiskFull = errors.New("no space left on device")

// failingOutput takes the first accepted bytes and fails every write after.
type failingOutput struct {
	accepted int
	written  int
	// refused counts the writes of the file that failed. The file is all NUL
	// bytes, which tells its writes from the usage text the test root prints
	// to the same writer once the command has failed.
	refused int
}

func (output *failingOutput) Write(chunk []byte) (int, error) {
	if output.written >= output.accepted {
		if bytes.IndexByte(chunk, 0) >= 0 {
			output.refused++
		}

		return 0, errDiskFull
	}
	output.written += len(chunk)

	return len(chunk), nil
}

// TestAFileThatCannotBeWrittenToStdoutIsReported: bb repo cat and bb repo
// browse raw wrote the file and threw the write's error away. A failed write is
// reported now, and ends the download rather than reading the rest of the file
// into a stream nothing takes.
// mock-inventory: unreachable-state — a standard output that fails part-way
// through a file, which the live suite cannot arrange; the server only supplies
// the bytes, and TestLiveRawFileCommandsWriteTheBytesExactly covers the rest.
func TestAFileThatCannotBeWrittenToStdoutIsReported(t *testing.T) {
	t.Parallel()

	body := make([]byte, 512<<10)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/raw/big.bin") {
			http.NotFound(writer, request)

			return
		}
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = writer.Write(body)
	}))
	defer server.Close()

	for _, command := range rawFileCommands {
		output := &failingOutput{accepted: 1000}
		setup := testSetup{Host: server.URL, Token: "token", ProjectKey: "PRJ", RepoSlug: "demo", Out: output}

		_, err := executeTestCLIWith(t, setup, command...)
		if !apperrors.IsKind(err, apperrors.KindInternal) || !errors.Is(err, errDiskFull) {
			t.Fatalf("%s: got %v, want the failed write reported", strings.Join(command, " "), err)
		}
		if output.refused != 1 {
			t.Fatalf("%s: the file was written %d times after a write failed; the download should stop at the first", strings.Join(command, " "), output.refused)
		}
	}
}

// TestAFileOverWhatJSONHoldsSaysToDropIt: under --json the file is held in
// memory, so it is capped, and a file over the cap is refused before a byte of
// it is read -- with the way out, since without --json it streams whatever its
// size.
// mock-inventory: transport-fault — a server declaring a file over the cap
// without sending it; the subject is bb's cap, and a real file that size would
// cost the live suite 64 MB to prove the same line.
func TestAFileOverWhatJSONHoldsSaysToDropIt(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if !strings.HasSuffix(request.URL.Path, "/raw/big.bin") {
			http.NotFound(writer, request)

			return
		}
		writer.Header().Set("Content-Length", strconv.Itoa(maxHeldFileBytes+1))
		writer.WriteHeader(http.StatusOK)
		writer.(http.Flusher).Flush()
		<-request.Context().Done()
	}))
	defer server.Close()

	for _, command := range rawFileCommands {
		setup := testSetup{Host: server.URL, Token: "token", ProjectKey: "PRJ", RepoSlug: "demo"}

		out, err := executeTestCLIWith(t, setup, append([]string{"--json"}, command...)...)
		if !apperrors.IsKind(err, apperrors.KindValidation) || !strings.Contains(err.Error(), "big.bin is larger than 64 MiB") ||
			!strings.Contains(err.Error(), "drop --json to stream the file to stdout") {
			t.Fatalf("%s: got %v, want the cap named and the way round it\noutput: %s", strings.Join(command, " "), err, out)
		}
	}
}
