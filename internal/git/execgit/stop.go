package execgit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// Why bb stopped a git command, as the cause of the context it ran under.
var (
	errTimedOut = errors.New("git did not finish in time")
	errStalled  = errors.New("git reported no progress")
)

// waitDelay bounds the wait for git's output once git has been stopped, or has
// exited. Stopping git does not end a helper it started -- git-remote-https,
// ssh -- and one still blocked on the network holds the output pipes open, so
// without a bound the wait would last as long as that read.
const waitDelay = 2 * time.Second

// progressWatch stops a transfer that has gone quiet. Git is run with
// --progress, so it reports progress while it transfers, and every write it
// makes counts as some: a clone may run as long as the repository takes, but
// not on after git has stopped hearing from the other end.
type progressWatch struct {
	start time.Time
	// last is when git last wrote, as the time since start, so the watch runs
	// on the monotonic clock rather than on a wall clock that can jump.
	last atomic.Int64
	done chan struct{}
}

// watchProgress cancels with errStalled once nothing has been written through
// the watch for window.
func watchProgress(cancel context.CancelCauseFunc, window time.Duration) *progressWatch {
	watch := &progressWatch{start: time.Now(), done: make(chan struct{})}

	go func() {
		ticker := time.NewTicker(max(min(window/4, time.Second), time.Millisecond))
		defer ticker.Stop()

		for {
			select {
			case <-watch.done:
				return
			case <-ticker.C:
				if watch.quietFor() >= window {
					cancel(errStalled)
					return
				}
			}
		}
	}()

	return watch
}

func (watch *progressWatch) touch() {
	watch.last.Store(int64(time.Since(watch.start)))
}

func (watch *progressWatch) quietFor() time.Duration {
	return time.Since(watch.start) - time.Duration(watch.last.Load())
}

func (watch *progressWatch) stop() {
	close(watch.done)
}

// writer passes writes through to inner, counting each as progress.
func (watch *progressWatch) writer(inner io.Writer) io.Writer {
	return progressWriter{watch: watch, inner: inner}
}

type progressWriter struct {
	watch *progressWatch
	inner io.Writer
}

func (writer progressWriter) Write(p []byte) (int, error) {
	writer.watch.touch()

	return writer.inner.Write(p)
}

// stopped says why git did not finish when it was stopped rather than failed
// on its own. Git reports only that it was killed -- on Windows as exit status
// 1, which reads as though git had failed.
//
// Running out of time is transient, and an interrupt is cancelled, as for a
// request (apperrors.KindCancelled). A command that changes something may have
// got part of the way -- a clone leaves a partial directory behind -- so its
// outcome is unknown: it has to be checked rather than repeated.
func (backend *Backend) stopped(options runOptions, cause, err error) error {
	command := "git " + strings.Join(redactAll(options.args), " ")

	outOfTime := apperrors.KindTransient
	if options.changes {
		outOfTime = apperrors.KindUnknownOutcome
	}

	switch {
	case errors.Is(cause, errStalled):
		return apperrors.New(outOfTime, fmt.Sprintf("%s reported no progress for %s, so bb stopped it", command, seconds(backend.Timeout)), err)
	case errors.Is(cause, errTimedOut):
		return apperrors.New(outOfTime, fmt.Sprintf("%s did not finish within %s, so bb stopped it", command, seconds(backend.Timeout)), err)
	case errors.Is(cause, context.DeadlineExceeded):
		return apperrors.New(outOfTime, fmt.Sprintf("%s ran out of time", command), err)
	default:
		return apperrors.New(apperrors.KindCancelled, fmt.Sprintf("%s was interrupted", command), err)
	}
}

// seconds is a duration as a number of seconds, 60s or 0.5s, which is how a
// person reads a timeout; Duration.String gives 1m0s.
func seconds(duration time.Duration) string {
	return strconv.FormatFloat(duration.Seconds(), 'f', -1, 64) + "s"
}

// redactAll redacts each argument of a command for a message.
func redactAll(args []string) []string {
	redacted := make([]string, len(args))
	for index, arg := range args {
		redacted[index] = redact(arg)
	}

	return redacted
}

// progressLine is a line of git's progress output, finished or not, as
// "Receiving objects: 100% (5/5), 1.2 MiB | 3 MiB/s, done." or
// "remote: Enumerating objects: 5, done.".
var progressLine = regexp.MustCompile(`^(remote: )?[A-Z][a-z]+( [a-z]+)*: +(\d+% \(\d+/\d+\)|\d+)(,.*)?$`)

// withoutProgress is what git wrote to stderr less its progress meters, which
// a transfer writes since it runs with --progress, and which say nothing about
// why it failed. A meter rewrites its line in place with carriage returns; each
// is dropped whole.
func withoutProgress(output string) string {
	var kept []string
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimRight(line, "\r")
		if at := strings.LastIndex(line, "\r"); at >= 0 {
			line = line[at+1:]
		}
		if progressLine.MatchString(strings.TrimSpace(line)) {
			continue
		}
		kept = append(kept, line)
	}

	return strings.Join(kept, "\n")
}
