package ai

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	apperrors "github.com/vriesdemichael/bitbucket-data-center-cli/internal/domain/errors"
	bbskill "github.com/vriesdemichael/bitbucket-data-center-cli/skills/bb"
	bbbulkskill "github.com/vriesdemichael/bitbucket-data-center-cli/skills/bb-bulk"
)

type skillInfo struct {
	name    string
	content []byte
}

// Skill names one of the agent skills this binary carries.
//
// Exported so shell completion can offer them without writing the names down
// a second time: `bb ai skill install|remove|show [skill]` takes a positional,
// which cannot be an enum flag, and a list beside this one is a list that can
// disagree with what lookupSkill resolves.
type Skill struct {
	// Name is the canonical spelling, and the directory the file installs to.
	Name string
	// Aliases are the other spellings lookupSkill accepts. Not offered as
	// completions -- a shell showing both bulk and bb-bulk for one skill is
	// two candidates that do the same thing.
	Aliases []string
	// Summary is what the skill is for, shown beside the name.
	Summary string
	content []byte
}

// Rendered is the file `bb ai skill install` writes for this skill: the
// skill as the repository holds it, stamped with version.
//
// Exported for bb doctor, which tells a current skill from one an earlier bb
// installed by comparing the file with this.
func (skill Skill) Rendered(version string) string {
	return buildSkill(skillInfo{name: skill.Name, content: skill.content}, version)
}

// Repository is the skill as the repository holds it, without the stamp: what
// `npx skills add` installs.
//
// Exported for bb doctor, which reports such a copy as the fact it is rather
// than as an out-of-date one.
func (skill Skill) Repository() string {
	return string(skill.content)
}

// SkillLocation is a directory agents read skills from, under a project or a
// home directory.
type SkillLocation struct {
	// Name is what bb calls the location: agents for .agents/skills, the
	// convention most agents follow, and claude for .claude/skills, which
	// Claude Code reads instead.
	Name      string
	directory string
}

var (
	agentsSkills = SkillLocation{Name: "agents", directory: ".agents"}
	claudeSkills = SkillLocation{Name: "claude", directory: ".claude"}
)

// SkillLocations are the directories agents read skills from, under the
// working directory and, for --global, the home directory.
//
// Exported, with Path, for bb doctor, which looks for every skill in each of
// them: one list of places, so the command that writes a skill and the one
// that checks it cannot disagree about where it goes.
var SkillLocations = []SkillLocation{agentsSkills, claudeSkills}

// Path is where this skill's file is in location under base.
func (skill Skill) Path(base string, location SkillLocation) string {
	return location.path(base, skill.Name)
}

func (location SkillLocation) path(base, name string) string {
	return filepath.Join(base, location.directory, "skills", name, "SKILL.md")
}

// Skills are the skills bb ships, in the order `bb ai skill` documents them.
// The first is what an omitted argument resolves to.
var Skills = []Skill{
	{
		Name:    "bb",
		Summary: "Driving bb from a coding agent",
		content: bbskill.Content,
	},
	{
		Name:    "bb-bulk",
		Aliases: []string{"bulk"},
		Summary: "Planning and applying bulk changes with bb bulk",
		content: bbbulkskill.Content,
	},
}

func lookupSkill(name string) (skillInfo, error) {
	wanted := strings.ToLower(strings.TrimSpace(name))
	if wanted == "" {
		// No argument means the default skill, which is the first registered.
		return skillInfo{name: Skills[0].Name, content: Skills[0].content}, nil
	}

	for _, skill := range Skills {
		if strings.EqualFold(wanted, skill.Name) {
			return skillInfo{name: skill.Name, content: skill.content}, nil
		}
		for _, alias := range skill.Aliases {
			if strings.EqualFold(wanted, alias) {
				return skillInfo{name: skill.Name, content: skill.content}, nil
			}
		}
	}

	return skillInfo{}, apperrors.New(
		apperrors.KindValidation,
		fmt.Sprintf("unknown skill %q: supported skills are %s", name, strings.Join(skillSpellings(), ", ")),
		nil,
	)
}

// skillSpellings lists every name the argument accepts, canonical first, so
// the refusal names the alias the caller may have meant.
func skillSpellings() []string {
	spellings := make([]string, 0, len(Skills))
	for _, skill := range Skills {
		spellings = append(spellings, strconv.Quote(skill.Name))
		for _, alias := range skill.Aliases {
			spellings = append(spellings, strconv.Quote(alias))
		}
	}

	return spellings
}

func newSkillCommand(deps Dependencies) *cobra.Command {
	skillCmd := &cobra.Command{
		Use:   "skill",
		Short: "Agent skill distribution commands",
	}

	skillCmd.AddCommand(newSkillShowCommand(deps))
	skillCmd.AddCommand(newSkillInstallCommand(deps))
	skillCmd.AddCommand(newSkillRemoveCommand(deps))

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

Most agents read .agents/skills/<name>/SKILL.md, and Claude Code reads
.claude/skills/<name>/SKILL.md; bb ai skill install writes both. For an agent
that reads a path of its own, consult its documentation.

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
		Short: "Write an agent skill to the agent skills directories",
		Long: `Write an agent skill file (defaults to "bb", supports "bulk" / "bb-bulk") where
coding agents read it: .agents/skills, which most agents read, and
.claude/skills, which Claude Code reads instead.

Project scope (default):
  .agents/skills/<skill>/SKILL.md
  .claude/skills/<skill>/SKILL.md

Global scope (--global), for every project of yours:
  ~/.agents/skills/<skill>/SKILL.md
  ~/.claude/skills/<skill>/SKILL.md

The skill is embedded in this binary, so no network connection is required.
Re-run after upgrading bb to keep the skill files current.`,
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

			paths, err := resolveInstallPaths(skill, global)
			if err != nil {
				return err
			}

			rendered := buildSkill(skill, deps.Version())
			for _, dest := range paths {
				// The skill is installed into the invoking user's own agent
				// configuration, so it needs no group or world access.
				if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
					return apperrors.New(apperrors.KindInternal, "failed to create skill directory", err)
				}
				if err := os.WriteFile(dest, []byte(rendered), 0o600); err != nil {
					return apperrors.New(apperrors.KindInternal, "failed to write skill file", err)
				}
			}

			if deps.jsonEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SkillFile{
					Status: "installed",
					Skill:  skill.name,
					Path:   paths[0],
					Paths:  paths,
					Scope:  installScope(global),
				})
			}

			for _, dest := range paths {
				fmt.Fprintf(cmd.OutOrStdout(), "Skill installed: %s\n", dest)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Install for every project of yours (~/.agents/skills and ~/.claude/skills)")
	return cmd
}

func newSkillRemoveCommand(deps Dependencies) *cobra.Command {
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

			paths, err := resolveInstallPaths(skill, global)
			if err != nil {
				return err
			}

			removed := make([]string, 0, len(paths))
			for _, dest := range paths {
				if _, statErr := os.Stat(dest); os.IsNotExist(statErr) {
					continue
				}
				if err := os.Remove(dest); err != nil {
					return apperrors.New(apperrors.KindInternal, "failed to remove skill file", err)
				}
				removed = append(removed, dest)
			}

			if len(removed) == 0 {
				// Not an error: removing something already absent leaves the
				// caller in the state they asked for. The status says which
				// of the two happened, which the English sentence also did.
				if deps.jsonEnabled() {
					return deps.WriteJSON(cmd.OutOrStdout(), SkillFile{
						Status: "not_found",
						Skill:  skill.name,
						Path:   paths[0],
						Paths:  paths,
						Scope:  installScope(global),
					})
				}

				fmt.Fprintf(cmd.OutOrStdout(), "Skill file not found: %s\n", strings.Join(paths, ", "))
				return nil
			}

			if deps.jsonEnabled() {
				return deps.WriteJSON(cmd.OutOrStdout(), SkillFile{
					Status: "removed",
					Skill:  skill.name,
					Path:   removed[0],
					Paths:  removed,
					Scope:  installScope(global),
				})
			}

			for _, dest := range removed {
				fmt.Fprintf(cmd.OutOrStdout(), "Skill removed: %s\n", dest)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&global, "global", false, "Remove from every project of yours (~/.agents/skills and ~/.claude/skills)")
	return cmd
}

// resolveInstallPaths returns the files a skill is installed to, one in each of
// SkillLocations, the .agents one first.
func resolveInstallPaths(skill skillInfo, global bool) ([]string, error) {
	base, err := installBase(global)
	if err != nil {
		return nil, err
	}

	paths := make([]string, 0, len(SkillLocations))
	for _, location := range SkillLocations {
		paths = append(paths, location.path(base, skill.name))
	}

	return paths, nil
}

// installBase is the directory the skill locations are under: the working
// directory for a project, the home directory for every project of the user.
func installBase(global bool) (string, error) {
	if !global {
		cwd, err := os.Getwd()
		if err != nil {
			return "", apperrors.New(apperrors.KindInternal, "failed to determine working directory", err)
		}
		return cwd, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", apperrors.New(apperrors.KindInternal, "failed to determine home directory", err)
	}
	return home, nil
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

// installScope names where the skill file lives, because the caller cannot
// infer it from the path alone: project scope resolves relative to the working
// directory and global scope under the home directory.
func installScope(global bool) string {
	if global {
		return "global"
	}
	return "project"
}
