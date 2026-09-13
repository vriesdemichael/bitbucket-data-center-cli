package ai

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// A damaged stored config stops `mcp serve` with the config's own error, not an
// internal one that says bb is broken (#567).
//
// Not parallel: the stored config is found through the environment.
func TestMCPServeReportsADamagedConfigAsItself(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("hosts: [\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("BB_CONFIG_PATH", path)
	t.Setenv("BB_DISABLE_STORED_CONFIG", "")

	cmd := New(testMCPDeps())
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)
	cmd.SetArgs([]string{"mcp", "serve"})

	err := cmd.Execute()
	if !apperrors.IsKind(err, apperrors.KindPermanent) {
		t.Fatalf("got %v, want the permanent error naming the config", err)
	}
}
