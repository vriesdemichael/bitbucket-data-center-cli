package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/jsonoutput"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// TestAGetIsWrittenAsItArrivesWithoutADeadline: a GET through bb api is held
// to the request timeout for each wait, not for the whole answer, and a body
// that is not text is written byte for byte as it arrives. It used to be read
// whole within the timeout, then trimmed and ended with a newline.
// mock-inventory: transport-fault — a server trickling a binary body out over
// several request timeouts; the subject is how bb api reads and writes it.
func TestAGetIsWrittenAsItArrivesWithoutADeadline(t *testing.T) {
	t.Parallel()

	const chunks = 20
	body := make([]byte, 0, chunks*64)
	for index := range chunks * 64 {
		body = append(body, byte(index*13))
	}
	body[0], body[len(body)-1] = '\n', ' '

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/octet-stream")
		writer.Header().Set("Content-Length", strconv.Itoa(len(body)))
		for index := range chunks {
			_, _ = writer.Write(body[index*64 : (index+1)*64])
			writer.(http.Flusher).Flush()
			time.Sleep(50 * time.Millisecond)
		}
	}))
	defer server.Close()

	const timeout = 300 * time.Millisecond
	deps := Dependencies{
		JSONEnabled:   func() bool { return false },
		DryRunEnabled: func() bool { return false },
		LoadConfig: func(config.Overrides) (config.AppConfig, error) {
			return config.AppConfig{BitbucketURL: server.URL, BitbucketToken: "test-token", RequestTimeout: timeout, RetryBackoff: time.Millisecond}, nil
		},
		WriteJSON: jsonoutput.Write,
	}

	cmd := New(deps)
	out := &timedOutput{}
	cmd.SetOut(out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"/rest/api/latest/projects/PRJ/repos/demo/raw/logo.bin"})

	started := time.Now()
	if err := cmd.Execute(); err != nil {
		t.Fatalf("a body that kept arriving failed: %v", err)
	}
	elapsed := time.Since(started)
	if elapsed < 2*timeout {
		t.Fatalf("the answer took %s; it has to outlast the timeout for this test to mean anything", elapsed)
	}
	if !bytes.Equal(out.written.Bytes(), body) {
		t.Fatalf("wrote %d bytes that differ from the %d served", out.written.Len(), len(body))
	}
	// Written as it arrived, not held until the end: the first bytes went out
	// long before the last had been sent.
	if first := out.first.Sub(started); first > elapsed/2 {
		t.Fatalf("the first bytes were written %s into a %s answer; the body was held rather than streamed", first, elapsed)
	}
}

// timedOutput records what was written and when the first of it was.
type timedOutput struct {
	written bytes.Buffer
	first   time.Time
}

func (output *timedOutput) Write(chunk []byte) (int, error) {
	if output.first.IsZero() {
		output.first = time.Now()
	}

	return output.written.Write(chunk)
}
