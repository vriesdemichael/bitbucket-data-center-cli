package execgit

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/git"
)

// TestACommandThatRunsTooLongSaysSo: bb's own time limit used to surface as git
// failing -- on Windows as "exit status 1" -- and as permanent, though nothing
// about running out of time is.
func TestACommandThatRunsTooLongSaysSo(t *testing.T) {
	t.Parallel()

	backend := New()
	backend.Timeout = 500 * time.Millisecond

	_, err := backend.run(context.Background(), runOptions{args: []string{"ls-remote", silentRemote(t)}})
	if apperrors.KindOf(err) != apperrors.KindTransient || !strings.Contains(apperrors.MessageOf(err), "did not finish within 0.5s") {
		t.Fatalf("got %v, want a transient error saying git did not finish within 0.5s", err)
	}
}

// TestAStalledTransferIsStoppedAndSaysSo: a clone runs as long as it reports
// progress, and is stopped once it has reported none for the timeout. What a
// stopped clone left behind is unknown, so the error is not transient: the
// directory wants checking before the clone is tried again.
func TestAStalledTransferIsStoppedAndSaysSo(t *testing.T) {
	t.Parallel()

	directory := filepath.Join(scratch(t), "clone")
	remote := silentRemote(t)
	backend := New()
	backend.Timeout = 500 * time.Millisecond

	started := time.Now()
	err := backend.Clone(context.Background(), remote, git.CloneOptions{Directory: directory})
	if apperrors.KindOf(err) != apperrors.KindUnknownOutcome || !strings.Contains(apperrors.MessageOf(err), "reported no progress for 0.5s") {
		t.Fatalf("got %v, want an unknown outcome saying the clone reported no progress for 0.5s", err)
	}
	if elapsed := time.Since(started); elapsed > 30*time.Second {
		t.Errorf("the stalled clone took %s to stop", elapsed)
	}
}

// TestAnInterruptedCommandIsCancelled: stopping bb stops git, and that is the
// operator's doing rather than a failure of git's.
func TestAnInterruptedCommandIsCancelled(t *testing.T) {
	t.Parallel()

	remote := silentRemote(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	time.AfterFunc(300*time.Millisecond, cancel)

	_, err := New().run(ctx, runOptions{args: []string{"ls-remote", remote}})
	if apperrors.KindOf(err) != apperrors.KindCancelled || !strings.Contains(apperrors.MessageOf(err), "was interrupted") {
		t.Fatalf("got %v, want a cancelled error saying git was interrupted", err)
	}
}

// TestProgressKeepsATransferGoing: the watch stops a transfer only once it has
// been quiet for a whole window, however long it has run in all.
func TestProgressKeepsATransferGoing(t *testing.T) {
	t.Parallel()

	const window = time.Second
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)

	watch := watchProgress(cancel, window)
	defer watch.stop()
	writer := watch.writer(io.Discard)

	// Three windows long, with a write every tenth of one.
	for range 30 {
		_, _ = writer.Write([]byte("Receiving objects:  50% (1/2)\r"))
		time.Sleep(window / 10)
	}
	if ctx.Err() != nil {
		t.Fatalf("a transfer still reporting progress was stopped: %v", context.Cause(ctx))
	}

	select {
	case <-ctx.Done():
		if !errors.Is(context.Cause(ctx), errStalled) {
			t.Fatalf("stopped for %v, want errStalled", context.Cause(ctx))
		}
	case <-time.After(10 * window):
		t.Fatal("a transfer that went quiet was not stopped")
	}
}

// TestAFailureSaysWhatWentWrongWithoutTheProgress: a transfer runs with
// --progress, and its meters say nothing about why it failed.
func TestAFailureSaysWhatWentWrongWithoutTheProgress(t *testing.T) {
	t.Parallel()

	stderr := "Cloning into 'repo'...\n" +
		"remote: Enumerating objects: 5, done.\n" +
		"remote: Counting objects:  20% (1/5)\rremote: Counting objects: 100% (5/5), done.\n" +
		"Receiving objects:  40% (2/5)\rReceiving objects:  60% (3/5), 1.20 MiB | 1.10 MiB/s\r\n" +
		"error: RPC failed; curl 18 transfer closed with outstanding read data remaining\n" +
		"fatal: early EOF\n"
	want := "Cloning into 'repo'...\n" +
		"error: RPC failed; curl 18 transfer closed with outstanding read data remaining\n" +
		"fatal: early EOF\n"

	if got := withoutProgress(stderr); got != want {
		t.Errorf("withoutProgress =\n%q\nwant\n%q", got, want)
	}
}

// silentRemote is a remote that accepts git's connection and never answers, as
// one does once the network in between has stopped moving: the request goes
// out, and git waits for a reply that never comes. It returns a URL to clone.
func silentRemote(t *testing.T) string {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	var mutex sync.Mutex
	var connections []net.Conn
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			mutex.Lock()
			connections = append(connections, connection)
			mutex.Unlock()

			// Read the request, and never answer it.
			go func() { _, _ = io.Copy(io.Discard, connection) }()
		}
	}()

	t.Cleanup(func() {
		_ = listener.Close()
		mutex.Lock()
		defer mutex.Unlock()
		for _, connection := range connections {
			_ = connection.Close()
		}
	})

	return "http://" + listener.Addr().String() + "/scm/PRJ/repo.git"
}

// scratch is a directory a stopped git may leave something open in. Its HTTP
// helper outlives it until the remote closes, and on Windows what the helper
// holds cannot be removed, so removal is best effort rather than a failure of
// the test, as t.TempDir would make it.
func scratch(t *testing.T) string {
	t.Helper()

	directory, err := os.MkdirTemp("", "bb-execgit-")
	if err != nil {
		t.Fatalf("scratch directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })

	return directory
}
