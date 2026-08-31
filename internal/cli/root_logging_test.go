package cli

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

func TestApplyRuntimeFlagOverridesLoggingFlags(t *testing.T) {
	t.Setenv("BB_LOG_LEVEL", "")
	t.Setenv("BB_LOG_FORMAT", "")

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("log-level", "", "")
	cmd.Flags().String("log-format", "", "")

	if err := cmd.Flags().Set("log-level", "debug"); err != nil {
		t.Fatalf("set log-level: %v", err)
	}
	if err := cmd.Flags().Set("log-format", "jsonl"); err != nil {
		t.Fatalf("set log-format: %v", err)
	}

	if err := (&rootOptions{}).applyRuntimeFlagOverrides(cmd); err != nil {
		t.Fatalf("applyRuntimeFlagOverrides: %v", err)
	}

	if got := os.Getenv("BB_LOG_LEVEL"); got != "debug" {
		t.Fatalf("expected BB_LOG_LEVEL=debug, got %q", got)
	}
	if got := os.Getenv("BB_LOG_FORMAT"); got != "jsonl" {
		t.Fatalf("expected BB_LOG_FORMAT=jsonl, got %q", got)
	}
}
