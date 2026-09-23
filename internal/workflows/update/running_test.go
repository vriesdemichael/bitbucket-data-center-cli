package update

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"
)

// runningBinary is a copy of this test binary, started from the path an update
// replaces: the position bb update is always in, since it replaces the file it
// was started from.
type runningBinary struct {
	input  io.WriteCloser
	lines  <-chan string
	exited <-chan struct{}
	err    *error
}

// startRunningBinary starts the binary at path and waits until it runs. It is
// killed and waited for when the test ends, whatever the test did, so no copy
// outlives it.
func startRunningBinary(t *testing.T, path string) *runningBinary {
	t.Helper()

	command := exec.Command(path)
	command.Env = append(os.Environ(), runningBinaryVariable+"=1")
	input, err := command.StdinPipe()
	if err != nil {
		t.Fatalf("stdin of %s: %v", path, err)
	}
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("stdout of %s: %v", path, err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start %s: %v", path, err)
	}

	var waitErr error
	exited := make(chan struct{})
	go func() {
		waitErr = command.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-exited
	})

	// Buffered beyond the few lines it writes, so the reader never blocks on a
	// test that has stopped listening.
	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			lines <- scanner.Text()
		}
	}()

	running := &runningBinary{input: input, lines: lines, exited: exited, err: &waitErr}
	running.expect(t, "running")

	return running
}

func (running *runningBinary) expect(t *testing.T, want string) {
	t.Helper()

	select {
	case line, open := <-running.lines:
		if !open {
			t.Fatalf("the running binary exited instead of saying %q", want)
		}
		if line != want {
			t.Fatalf("the running binary said %q, want %q", line, want)
		}
	case <-time.After(time.Minute):
		t.Fatalf("the running binary did not say %q within a minute", want)
	}
}

// answers checks the binary is still running.
func (running *runningBinary) answers(t *testing.T) {
	t.Helper()

	if _, err := io.WriteString(running.input, "still there?\n"); err != nil {
		t.Fatalf("write to the running binary: %v", err)
	}
	running.expect(t, "still running")
}

// exit lets the binary finish and waits until it has.
func (running *runningBinary) exit(t *testing.T) {
	t.Helper()

	if err := running.input.Close(); err != nil {
		t.Fatalf("close the running binary's input: %v", err)
	}
	select {
	case <-running.exited:
		if *running.err != nil {
			t.Fatalf("the running binary failed: %v", *running.err)
		}
	case <-time.After(time.Minute):
		t.Fatal("the running binary did not exit within a minute of its input closing")
	}
}

// TestInstallBinaryReplacesTheRunningBinary replaces a binary while it runs, in
// the way this operating system allows, and checks what the running process and
// the install directory are left with.
func TestInstallBinaryReplacesTheRunningBinary(t *testing.T) {
	t.Parallel()

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locate the test executable: %v", err)
	}
	original, err := os.ReadFile(self)
	if err != nil {
		t.Fatalf("read the test executable: %v", err)
	}

	dir := t.TempDir()
	name := binaryFileName(runtime.GOOS)
	target := filepath.Join(dir, name)
	if err := os.WriteFile(target, original, 0o755); err != nil {
		t.Fatalf("install the copy: %v", err)
	}

	running := startRunningBinary(t, target)

	if err := installBinary(runtime.GOOS, target, []byte("new bb"), 0o755); err != nil {
		t.Fatalf("installBinary: %v", err)
	}
	assertHolds(t, target, "new bb")
	// Still running from the file it was started from.
	running.answers(t)

	setAside, others := splitSetAside(t, dir, name)
	if !slices.Equal(others, []string{name}) {
		t.Fatalf("install directory holds %v beside what was set aside, want only %s", others, name)
	}

	if runtime.GOOS != "windows" {
		// Renamed over: the process holds the old file, and no name is left
		// pointing at it.
		if len(setAside) != 0 {
			t.Fatalf("set aside %v, want nothing: the old binary is replaced in one rename", setAside)
		}
		return
	}

	// Windows will rename a running binary but not delete it, so the old one
	// waits beside the new one.
	if len(setAside) != 1 {
		t.Fatalf("set aside %v, want the one binary that was replaced", setAside)
	}
	aside := filepath.Join(dir, setAside[0])
	held, err := os.ReadFile(aside)
	if err != nil {
		t.Fatalf("read %s: %v", aside, err)
	}
	if !bytes.Equal(held, original) {
		t.Fatalf("%s is not the binary that was replaced", aside)
	}

	// The next run cannot delete it while this one runs from it, and passes
	// over it.
	osFileSystem().removeLeftovers(target)
	if _, err := os.Stat(aside); err != nil {
		t.Fatalf("the cleanup removed a binary a running process holds: %v", err)
	}

	running.exit(t)

	// Once nothing runs from it, the next run's cleanup deletes it. Retried
	// briefly: a virus scanner can hold a file a process has just let go of.
	for deadline := time.Now().Add(30 * time.Second); ; {
		osFileSystem().removeLeftovers(target)
		entries := directoryEntries(t, dir)
		if slices.Equal(entries, []string{name}) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("30 seconds after the process exited the install directory still holds %v, want only %s", entries, name)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
