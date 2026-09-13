package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"testing"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// TestAnInterruptIsReportedAsOne is #574: exit 12 was published and could not
// happen, because nothing handled an interrupt. main now cancels the command's
// context on the first one, and whatever the command returns after that is the
// interrupt's consequence rather than a failure of its own.
func TestAnInterruptIsReportedAsOne(t *testing.T) {
	t.Parallel()

	for name, testCase := range map[string]struct {
		returned func(ctx context.Context) error
		wantExit int
	}{
		"a failure the interrupt caused": {
			// A git subprocess killed by the context, say: nothing classified it.
			returned: func(ctx context.Context) error { return fmt.Errorf("git fetch: %w", ctx.Err()) },
			wantExit: 12,
		},
		"a mutation that had already gone": {
			// The transport's answer is the more specific one and is kept.
			returned: func(context.Context) error {
				return apperrors.New(apperrors.KindUnknownOutcome, "the POST reached the server", nil)
			},
			wantExit: 13,
		},
		"a command that finished anyway": {
			returned: func(context.Context) error { return nil },
			wantExit: 0,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)

			cmd := &cobra.Command{
				Use:           "bb",
				SilenceErrors: true,
				SilenceUsage:  true,
				RunE: func(cmd *cobra.Command, _ []string) error {
					// The interrupt arrives while the command runs.
					cancel()
					<-cmd.Context().Done()

					return testCase.returned(cmd.Context())
				},
			}
			cmd.SetContext(ctx)

			stderr := &bytes.Buffer{}
			if got := executeRootCommand(cmd, nil, io.Discard, stderr); got != testCase.wantExit {
				t.Fatalf("exit %d, want %d; stderr:\n%s", got, testCase.wantExit, stderr.String())
			}
		})
	}
}

// Without an interrupt nothing is reclassified: a command that failed on its
// own keeps its own kind.
func TestAFailureWithoutAnInterruptKeepsItsKind(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use:           "bb",
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(*cobra.Command, []string) error {
			return apperrors.New(apperrors.KindNotFound, "no such pull request", nil)
		},
	}
	cmd.SetContext(context.Background())

	if got := executeRootCommand(cmd, nil, io.Discard, io.Discard); got != 4 {
		t.Fatalf("exit %d, want 4", got)
	}
}
