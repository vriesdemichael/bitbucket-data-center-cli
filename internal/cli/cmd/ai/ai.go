package ai

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
)

// Dependencies holds the external collaborators injected by the root command.
type Dependencies struct {
	// Version returns the bb binary version string (set via ldflags at build time).
	// It is a func rather than a plain string so the root command can bind it lazily
	// after setting cmd.Version post-construction.
	Version func() string
	// LoadConfig loads the resolved AppConfig from environment and stored
	// credentials, steered by the --host and --token flags of `mcp serve`.
	LoadConfig func(config.Overrides) (config.AppConfig, error)
	// WriteJSON serialises v to w as indented JSON.
	WriteJSON func(w io.Writer, v any) error
	// JSONEnabled reports whether --json was passed. The skill commands report
	// an outcome and a resolved path, which is a data payload rather than a
	// document, so they owe the caller an envelope (ADR-014).
	JSONEnabled func() bool
}

// New returns the top-level `bb ai` command group.
func New(deps Dependencies) *cobra.Command {
	if deps.LoadConfig == nil {
		deps.LoadConfig = func(config.Overrides) (config.AppConfig, error) {
			return config.AppConfig{}, apperrors.New(apperrors.KindInternal, "ai command dependency LoadConfig is not configured", nil)
		}
	}
	if deps.WriteJSON == nil {
		deps.WriteJSON = func(io.Writer, any) error {
			return apperrors.New(apperrors.KindInternal, "ai command dependency WriteJSON is not configured", nil)
		}
	}

	aiCmd := &cobra.Command{
		Use:   "ai",
		Short: "AI-first tooling: MCP server and agent skill distribution",
	}

	aiCmd.AddCommand(newMCPCommand(deps))
	aiCmd.AddCommand(newSkillCommand(deps))

	return aiCmd
}

// jsonEnabled reports whether machine output was asked for, treating an
// unwired dependency as no.
func (deps Dependencies) jsonEnabled() bool {
	return deps.JSONEnabled != nil && deps.JSONEnabled()
}
