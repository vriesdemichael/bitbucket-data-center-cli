package outwriter

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
)

type failingWriter struct {
	err error
}

func (w failingWriter) Write(payload []byte) (int, error) {
	return 0, w.err
}

func TestRecorderPassesWritesThrough(t *testing.T) {
	buffer := &bytes.Buffer{}
	recorder := New(buffer)

	if _, err := io.WriteString(recorder, "hello"); err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if buffer.String() != "hello" {
		t.Fatalf("payload did not reach the writer: %q", buffer.String())
	}
	if err := recorder.Err(); err != nil {
		t.Fatalf("expected no recorded error, got %v", err)
	}
}

// TestRecorderKeepsTheFirstFailure is the point of the type: the command
// carries on writing, and the failure has to survive until someone can act on
// it.
func TestRecorderKeepsTheFirstFailure(t *testing.T) {
	first := errors.New("no space left on device")
	recorder := New(failingWriter{err: first})

	_, _ = io.WriteString(recorder, "one")
	_, _ = io.WriteString(recorder, "two")

	if !errors.Is(recorder.Err(), first) {
		t.Fatalf("expected the first failure to be kept, got %v", recorder.Err())
	}
}

// TestRecorderIgnoresAClosedPipe covers `bb pr list | head -5`, where the
// reader closing early is ordinary shell usage rather than a failure. Treating
// it as one would make every piped invocation look broken.
func TestRecorderIgnoresAClosedPipe(t *testing.T) {
	for name, err := range map[string]error{
		"EPIPE":         syscall.EPIPE,
		"ErrClosedPipe": io.ErrClosedPipe,
		"wrapped EPIPE": errors.New("write: " + syscall.EPIPE.Error()),
	} {
		t.Run(name, func(t *testing.T) {
			recorder := New(failingWriter{err: err})
			_, _ = io.WriteString(recorder, "payload")

			got := recorder.Err()
			// The wrapped case is a plain error carrying the same text, which
			// errors.Is cannot match; it is here to show the check is on the
			// error value and not on its message.
			if name == "wrapped EPIPE" {
				if got == nil {
					t.Fatal("a lookalike message must not be mistaken for EPIPE")
				}
				return
			}
			if got != nil {
				t.Fatalf("a closed pipe must not be reported, got %v", got)
			}
		})
	}
}

func TestRecorderToleratesNilWriter(t *testing.T) {
	recorder := New(nil)
	written, err := io.WriteString(recorder, "discarded")
	if err != nil || written != len("discarded") {
		t.Fatalf("a nil writer must discard cleanly, got %d, %v", written, err)
	}
	if recorder.Err() != nil {
		t.Fatalf("expected no recorded error, got %v", recorder.Err())
	}

	var absent *Recorder
	if _, err := io.WriteString(absent, "discarded"); err != nil {
		t.Fatalf("a nil recorder must discard cleanly, got %v", err)
	}
	if absent.Err() != nil {
		t.Fatal("a nil recorder has no error to report")
	}
}

func TestRecorderErrorNamesTheFailure(t *testing.T) {
	recorder := New(failingWriter{err: errors.New("disk quota exceeded")})
	_, _ = io.WriteString(recorder, "payload")

	if !strings.Contains(recorder.Err().Error(), "disk quota exceeded") {
		t.Fatalf("the underlying failure must survive: %v", recorder.Err())
	}
}
