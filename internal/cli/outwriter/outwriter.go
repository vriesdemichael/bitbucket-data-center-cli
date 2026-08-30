// Package outwriter records the first failure from writing command output.
//
// Every command writes through fmt.Fprint* to the writer Cobra hands it, and
// those calls return an error nobody checks. Individually there is nothing to
// do about it: a command cannot report that stdout is broken by writing to
// stdout. But the failure still matters -- `bb pr list > file` on a full disk
// produced truncated output and exited 0, which is the same class of bug as an
// unchecked Close on a download.
//
// Wrapping the writer once, at the root, moves the check to the only place it
// can be acted on: after the command has finished, where an exit code is still
// available. That is why the errcheck configuration excludes fmt.Fprint* --
// the error is recorded here, not discarded.
package outwriter

import (
	"errors"
	"io"
	"syscall"
)

// Recorder passes writes through and remembers the first failure.
type Recorder struct {
	writer io.Writer
	err    error
}

// New wraps writer. A nil writer discards, so a caller does not have to guard.
func New(writer io.Writer) *Recorder {
	return &Recorder{writer: writer}
}

func (recorder *Recorder) Write(payload []byte) (int, error) {
	if recorder == nil || recorder.writer == nil {
		return len(payload), nil
	}

	written, err := recorder.writer.Write(payload)
	if err != nil && recorder.err == nil {
		recorder.err = err
	}

	return written, err
}

// Err reports the first write failure worth failing the command over.
//
// A closed pipe is not one. `bb pr list | head -5` closes the pipe as soon as
// head has what it wants, and every well-behaved CLI treats that as the reader
// being finished rather than as an error -- reporting it would make ordinary
// shell usage look broken.
func (recorder *Recorder) Err() error {
	if recorder == nil || recorder.err == nil {
		return nil
	}
	if errors.Is(recorder.err, syscall.EPIPE) || errors.Is(recorder.err, io.ErrClosedPipe) {
		return nil
	}

	return recorder.err
}
