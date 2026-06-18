package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	bbskill "github.com/vriesdemichael/bitbucket-server-cli/skills/bb"
)

// skillInstallPath is the universal agent skills path used by GitHub Copilot,
// Cursor, Codex, Cline, Amp, and most other agents.
const skillInstallPath = ".agents/skills/bb/SKILL.md"

func newSkillCommand(deps Dependencies) *cobra.Command {
	skillCmd := &cobra.Command{
		Use:   "skill",
		Short: "Agent skill distribution commands",
	}

	skillCmd.AddCommand(newSkillShowCommand(deps))
	skillCmd.AddCommand(newSkillInstallCommand(deps))
	skillCmd.AddCommand(newSkillRemoveCommand())

	return skillCmd
}

func newSkillShowCommand(deps Dependencies) *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the bb agent skill to stdout",
		Long: `Print the bb agent skill to stdout.

The skill is embedded in this binary at compile time, so it works with no
network connection and without the source repository present.

Redirect to the location your coding agent expects:

  bb ai skill show > .agents/skills/bb/SKILL.md

Most agents use .agents/skills/<name>/SKILL.md as the project-scoped path.
Some use agent-specific paths (e.g. .claude/skills/, .cursor/skills/).
Consult your agent's documentation if the above path does not work.

A baseline skill (fixed at release time) is also distributed via the open
agent skills ecosystem and can be installed without bb being present:

  npx skills add vriesdemichael/bitbucket-data-center-cli

The npx-installed file is a snapshot from the repository. Use this command
to get a skill that always matches your installed bb version.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			skill := buildSkill(deps.Version())
			_, err := fmt.Fprint(cmd.OutOrStdout(), skill)
			return err
		},
	}
}

func newSkillInstallCommand(deps Dependencies) *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Write the bb skill to the agent skills directory",
		Long: fmt.Sprintf(`Write the bb agent skill file to the appropriate directory.

Project scope (default):
  %s

Global scope (--global):
  ~/.agents/skills/bb/SKILL.md

The skill is embedded in this binary, so no network connection is required.
Re-run after upgrading bb to keep the skill file current.`, skillInstallPath),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, err := resolveInstallPath(global)
			if err != nil {
				return err
			}

			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to create skill directory", err)
			}

			skill := buildSkill(deps.Version())
			if err := os.WriteFile(dest, []byte(skill), 0o644); err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to write skill file", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Skill installed: %s\n", dest)
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Install to user-level path (~/.agents/skills/bb/SKILL.md)")
	return cmd
}

func newSkillRemoveCommand() *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove the installed bb skill file",
		RunE: func(cmd *cobra.Command, args []string) error {
			dest, err := resolveInstallPath(global)
			if err != nil {
				return err
			}

			if _, statErr := os.Stat(dest); os.IsNotExist(statErr) {
				fmt.Fprintf(cmd.OutOrStdout(), "Skill file not found: %s\n", dest)
				return nil
			}

			if err := os.Remove(dest); err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to remove skill file", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Skill removed: %s\n", dest)
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Remove from user-level path (~/.agents/skills/bb/SKILL.md)")
	return cmd
}

// resolveInstallPath returns the absolute target path for the skill file.
func resolveInstallPath(global bool) (string, error) {
	if !global {
		cwd, err := os.Getwd()
		if err != nil {
			return "", apperrors.New(apperrors.KindInternal, "failed to determine working directory", err)
		}
		return filepath.Join(cwd, skillInstallPath), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to determine home directory", err)
	}
	return filepath.Join(home, ".agents", "skills", "bb", "SKILL.md"), nil
}

// buildSkill returns the skill content with version substituted.
// The embedded template contains the literal marker {{BB_VERSION}} which is
// replaced with the running binary's version at generation time.
func buildSkill(version string) string {
	if version == "" {
		version = "dev"
	}
	return strings.ReplaceAll(string(bbskill.Content), "{{BB_VERSION}}", version)
}
