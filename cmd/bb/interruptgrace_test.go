package main

import (
	"context"
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
	t.Parallel()

	t.Run("a command that cannot hear the cancellation is answered for", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		// Never closed: the command is blocked and will not return.
		finished := make(chan struct{})
		stopped := make(chan struct{})
		answered := make(chan struct{})

		go superviseInterrupt(ctx, finished, time.Millisecond, func() { close(stopped) }, func() { close(answered) })

		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("the first interrupt did not restore default signal handling, so a second one would be ignored too")
		}

		select {
		case <-answered:
		case <-time.After(5 * time.Second):
			t.Fatal("a blocked command was left without an answer; the terminal waits for a second interrupt that prints nothing")
		}
	})

	t.Run("a command that ends on its own answers for itself", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		finished := make(chan struct{})
		var answers atomic.Int32
		done := make(chan struct{})

		go func() {
			superviseInterrupt(ctx, finished, 50*time.Millisecond, func() {}, func() { answers.Add(1) })
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
		t.Parallel()

		finished := make(chan struct{})
		var answers atomic.Int32
		done := make(chan struct{})

		go func() {
			superviseInterrupt(context.Background(), finished, time.Millisecond, func() {}, func() { answers.Add(1) })
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
