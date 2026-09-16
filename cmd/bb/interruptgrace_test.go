package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestAnInterruptIsAnsweredEvenWhenTheCommandIsBlocked is the other half of
// #574's exit 12.
//
// Cancelling the context answers for every command that watches it. A command
// blocked on a read does not watch it -- stdin for `bb api --input -`,
// `auth login --token-stdin`, the git credential helper -- and the first
// interrupt then looked like nothing happening at all. v4.0.0 ended the process
// on the first interrupt, so waiting for a second one is a regression.
func TestAnInterruptIsAnsweredEvenWhenTheCommandIsBlocked(t *testing.T) {
	t.Run("a command that cannot hear the cancellation is answered for", func(t *testing.T) {
		answered := make(chan []string, 1)
		swapInterruptAnswer(t, func(args []string, _, _ io.Writer) {
			answered <- args
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Never closed: the command is blocked and will not return.
		finished := make(chan struct{})
		stopped := make(chan struct{})

		go superviseInterrupt(ctx, finished, time.Millisecond, func() { close(stopped) }, []string{"api", "--json"}, io.Discard, io.Discard)

		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("the first interrupt did not restore default signal handling, so a second one would be ignored too")
		}

		select {
		case args := <-answered:
			if len(args) != 2 || args[0] != "api" {
				t.Fatalf("the answer did not get the invocation's arguments: %v", args)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a blocked command was left without an answer; the terminal waits for a second interrupt that prints nothing")
		}
	})

	t.Run("a command that ends on its own answers for itself", func(t *testing.T) {
		var answers atomic.Int32
		swapInterruptAnswer(t, func([]string, io.Writer, io.Writer) { answers.Add(1) })

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		finished := make(chan struct{})
		done := make(chan struct{})

		go func() {
			superviseInterrupt(ctx, finished, 50*time.Millisecond, func() {}, nil, io.Discard, io.Discard)
			close(done)
		}()

		// The command returns inside the grace period, as one that watches its
		// context does: executeRootCommand reports the cancellation itself, and
		// a second answer here would exit before it could.
		close(finished)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the supervisor outlived the command it was watching")
		}

		if got := answers.Load(); got != 0 {
			t.Fatalf("answered %d times for a command that answered for itself", got)
		}
	})

	t.Run("a run that is never interrupted leaves nothing behind", func(t *testing.T) {
		var answers atomic.Int32
		swapInterruptAnswer(t, func([]string, io.Writer, io.Writer) { answers.Add(1) })

		finished := make(chan struct{})
		done := make(chan struct{})

		go func() {
			superviseInterrupt(context.Background(), finished, time.Millisecond, func() {}, nil, io.Discard, io.Discard)
			close(done)
		}()

		close(finished)

		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("the supervisor did not return when the command finished")
		}

		if got := answers.Load(); got != 0 {
			t.Fatalf("answered %d times without an interrupt", got)
		}
	})
}

// TestTheInterruptAnswerIsTheOneACancelledRunWrites keeps main's answer and
// executeRootCommand's the same: kind cancelled, exit 12, the line on stderr,
// and the envelope on stdout only when machine output was asked for.
func TestTheInterruptAnswerIsTheOneACancelledRunWrites(t *testing.T) {
	t.Parallel()

	t.Run("text output", func(t *testing.T) {
		t.Parallel()

		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

		if code := writeInterruptAnswer([]string{"pr", "list"}, stdout, stderr); code != 12 {
			t.Errorf("exit code %d, want 12", code)
		}
		if stdout.Len() != 0 {
			t.Errorf("stdout is not a machine contract here, but carried: %s", stdout)
		}
		if !strings.Contains(stderr.String(), "interrupted") {
			t.Errorf("stderr does not say what happened: %s", stderr)
		}
	})

	t.Run("machine output", func(t *testing.T) {
		t.Parallel()

		stdout, stderr := &bytes.Buffer{}, &bytes.Buffer{}

		if code := writeInterruptAnswer([]string{"pr", "list", "--json"}, stdout, stderr); code != 12 {
			t.Errorf("exit code %d, want 12", code)
		}

		var envelope struct {
			Error struct {
				Kind     string `json:"kind"`
				ExitCode int    `json:"exitCode"`
			} `json:"error"`
		}
		if err := json.Unmarshal(stdout.Bytes(), &envelope); err != nil {
			t.Fatalf("stdout is not the failure envelope: %v\n%s", err, stdout)
		}
		if envelope.Error.Kind != "cancelled" || envelope.Error.ExitCode != 12 {
			t.Errorf("envelope says %q / %d, want cancelled / 12", envelope.Error.Kind, envelope.Error.ExitCode)
		}
		if !strings.Contains(stderr.String(), "interrupted") {
			t.Errorf("stderr does not say what happened: %s", stderr)
		}
	})
}

// swapInterruptAnswer replaces the answer for one test. The real one calls
// os.Exit, which would end the test binary.
func swapInterruptAnswer(t *testing.T, answer func([]string, io.Writer, io.Writer)) {
	t.Helper()

	original := interruptAnswer
	interruptAnswer = answer
	t.Cleanup(func() { interruptAnswer = original })
}
