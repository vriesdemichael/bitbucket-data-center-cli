package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-server-cli/internal/domain/errors"
	bbskill "github.com/vriesdemichael/bitbucket-server-cli/skills/bb"
	bbbulkskill "github.com/vriesdemichael/bitbucket-server-cli/skills/bb-bulk"
)

type skillInfo struct {
	name    string
	content []byte
}

func lookupSkill(name string) (skillInfo, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "bb":
		return skillInfo{
			name:    "bb",
			content: bbskill.Content,
		}, nil
	case "bulk", "bb-bulk":
		return skillInfo{
			name:    "bb-bulk",
			content: bbbulkskill.Content,
		}, nil
	default:
		return skillInfo{}, apperrors.New(
			apperrors.KindValidation,
			fmt.Sprintf("unknown skill %q: supported skills are \"bb\", \"bulk\" (or \"bb-bulk\")", name),
			nil,
		)
	}
}

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
		Use:   "show [skill]",
		Short: "Print an agent skill to stdout",
		Long: `Print an agent skill to stdout (defaults to "bb", supports "bulk" / "bb-bulk").

The skill is embedded in this binary at compile time, so it works with no
network connection and without the source repository present.

Redirect to the location your coding agent expects:

  bb ai skill show > .agents/skills/bb/SKILL.md
  bb ai skill show bulk > .agents/skills/bb-bulk/SKILL.md

Most agents use .agents/skills/<name>/SKILL.md as the project-scoped path.
Some use agent-specific paths (e.g. .claude/skills/, .cursor/skills/).
Consult your agent's documentation if the above path does not work.

Baseline skills (fixed at release time) are also distributed via the open
agent skills ecosystem and can be installed without bb being present:

  npx skills add vriesdemichael/bitbucket-data-center-cli

The npx-installed files are snapshots from the repository. Use this command
to get a skill that always matches your installed bb version.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName := ""
			if len(args) > 0 {
				skillName = args[0]
			}
			skill, err := lookupSkill(skillName)
			if err != nil {
				return err
			}

			rendered := buildSkill(skill, deps.Version())
			_, err = fmt.Fprint(cmd.OutOrStdout(), rendered)
			return err
		},
	}
}

func newSkillInstallCommand(deps Dependencies) *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "install [skill]",
		Short: "Write an agent skill to the agent skills directory",
		Long: `Write an agent skill file to the appropriate directory (defaults to "bb", supports "bulk" / "bb-bulk").

Project scope (default):
  .agents/skills/<skill>/SKILL.md

Global scope (--global):
  ~/.agents/skills/<skill>/SKILL.md

The skill is embedded in this binary, so no network connection is required.
Re-run after upgrading bb to keep the skill file current.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName := ""
			if len(args) > 0 {
				skillName = args[0]
			}
			skill, err := lookupSkill(skillName)
			if err != nil {
				return err
			}

			dest, err := resolveInstallPath(skill, global)
			if err != nil {
				return err
			}

			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to create skill directory", err)
			}

			rendered := buildSkill(skill, deps.Version())
			if err := os.WriteFile(dest, []byte(rendered), 0o644); err != nil {
				return apperrors.New(apperrors.KindInternal, "failed to write skill file", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Skill installed: %s\n", dest)
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Install to user-level path (~/.agents/skills/<skill>/SKILL.md)")
	return cmd
}

func newSkillRemoveCommand() *cobra.Command {
	var global bool

	cmd := &cobra.Command{
		Use:   "remove [skill]",
		Short: "Remove an installed agent skill file",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName := ""
			if len(args) > 0 {
				skillName = args[0]
			}
			skill, err := lookupSkill(skillName)
			if err != nil {
				return err
			}

			dest, err := resolveInstallPath(skill, global)
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

	cmd.Flags().BoolVar(&global, "global", false, "Remove from user-level path (~/.agents/skills/<skill>/SKILL.md)")
	return cmd
}

// resolveInstallPath returns the absolute target path for the skill file.
func resolveInstallPath(skill skillInfo, global bool) (string, error) {
	relPath := filepath.Join(".agents", "skills", skill.name, "SKILL.md")
	if !global {
		cwd, err := os.Getwd()
		if err != nil {
			return "", apperrors.New(apperrors.KindInternal, "failed to determine working directory", err)
		}
		return filepath.Join(cwd, relPath), nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to determine home directory", err)
	}
	return filepath.Join(home, relPath), nil
}

// buildSkill returns the skill content stamped with the running binary's
// version.
//
// The stamp is appended here rather than substituted into a placeholder in the
// committed file. The repository copy is what `npx skills add` distributes, and
// the skill advertises that install path itself, so a `{{BB_VERSION}}` marker in
// the source shipped raw to anyone who followed the documented instructions.
// Nothing in the file can now be wrong when read unrendered.
func buildSkill(skill skillInfo, version string) string {
	if strings.TrimSpace(version) == "" {
		version = "dev"
	}

	content := strings.TrimRight(string(skill.content), "\n")

	return content + "\n\n---\n\nPrinted by `bb` " + version + ".\n"
}
