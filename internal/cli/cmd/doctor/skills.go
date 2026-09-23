package doctorcmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/ai"
)

// What an agent skill's file can be, in one place, when nothing needs fixing.
const (
	skillNotInstalled = "not_installed"
	// skillCurrent is exactly what bb ai skill install writes now.
	skillCurrent = "current"
	// skillRepository is the repository's copy, as npx skills add installs
	// it: the skill this bb carries, without the stamp that names the bb.
	skillRepository = "repository"
)

// The scopes a skill is installed in: a project, which is the working
// directory, and the user, whose home directory bb ai skill install writes to
// with --global.
const (
	scopeProject = "project"
	scopeUser    = "user"
)

// skillInstall is one agent skill in one place.
type skillInstall struct {
	skill    ai.Skill
	scope    string
	location string
	path     string
	// state is one of the skill states, and empty for a file that is none of
	// them, which problem and issue then describe.
	state   string
	problem string
	issue   *issue
}

// inspectSkills looks for every skill in every place an agent reads it from:
// each skill location, under the working directory and the home directory.
//
// A directory that cannot be worked out is left out: bb ai skill install could
// not have written there either. So is the working directory when it is the
// home directory, whose skills are the user's: the same files, reported once.
func inspectSkills(machine Machine, version string) []skillInstall {
	type scoped struct {
		scope     string
		directory string
	}

	bases := []scoped{}
	project, projectErr := machine.WorkingDirectory()
	home, homeErr := machine.System.HomeDir()
	if projectErr == nil && (homeErr != nil || !sameDirectory(project, home)) {
		bases = append(bases, scoped{scope: scopeProject, directory: project})
	}
	if homeErr == nil {
		bases = append(bases, scoped{scope: scopeUser, directory: home})
	}

	found := []skillInstall{}
	for _, skill := range ai.Skills {
		for _, base := range bases {
			for _, location := range ai.SkillLocations {
				found = append(found, inspectSkill(skill, base.scope, location.Name, skill.Path(base.directory, location), version))
			}
		}
	}

	return found
}

// sameDirectory asks the file system rather than comparing names, which differ
// in case on Windows and through a symbolic link anywhere.
func sameDirectory(first, second string) bool {
	firstInfo, firstErr := os.Stat(first)
	secondInfo, secondErr := os.Stat(second)
	if firstErr != nil || secondErr != nil {
		return filepath.Clean(first) == filepath.Clean(second)
	}

	return os.SameFile(firstInfo, secondInfo)
}

func inspectSkill(skill ai.Skill, scope, location, path, version string) skillInstall {
	install := skillInstall{skill: skill, scope: scope, location: location, path: path}

	content, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		install.state = skillNotInstalled
		return install
	case err != nil:
		install.problem = readFailure(err)
		install.issue = skillIssue(install)
		return install
	}

	// Line endings aside: a project's skill is often committed, and a checkout
	// on Windows may have given it CRLF.
	switch withLF(string(content)) {
	case withLF(skill.Rendered(version)):
		install.state = skillCurrent
	case withLF(skill.Repository()):
		install.state = skillRepository
	default:
		install.problem = "not what this bb installs, but an earlier bb's or an edited copy; " + reinstall(skill, scope) + " replaces it"
		install.issue = skillIssue(install)
	}

	return install
}

// skillIssue names the issue by skill, scope and location, because a scope
// holds a copy for Claude Code beside the one for every other agent.
func skillIssue(install skillInstall) *issue {
	return &issue{
		key:     "skill/" + install.skill.Name + "/" + install.scope + "/" + install.location,
		message: install.path + ": " + install.problem,
		summary: "the " + install.skill.Name + " skill",
	}
}

// reinstall is the command that writes this bb's skill over whatever is there,
// with the shortest name the skill answers to.
func reinstall(skill ai.Skill, scope string) string {
	name := skill.Name
	if len(skill.Aliases) > 0 {
		name = skill.Aliases[0]
	}

	command := "bb ai skill install " + name
	if scope == scopeUser {
		command += " --global"
	}

	return command
}

func skillIssues(skills []skillInstall) []issue {
	found := []issue{}
	for _, install := range skills {
		if install.issue != nil {
			found = append(found, *install.issue)
		}
	}

	return found
}

// writeSkills lists, for each skill, the places it is installed in, with the
// path saying which scope and location each is. The JSON report has every
// place, installed or not.
func writeSkills(w io.Writer, skills []skillInstall) {
	fmt.Fprintln(w, "Agent skills")
	if len(skills) == 0 {
		fmt.Fprintln(w, "  neither the working directory nor the home directory could be worked out")
		return
	}

	width := 0
	for _, install := range skills {
		width = max(width, len(install.skill.Name))
	}
	indent := strings.Repeat(" ", 2+width+2)

	for start := 0; start < len(skills); {
		name := skills[start].skill.Name

		lines := []string{}
		end := start
		for ; end < len(skills) && skills[end].skill.Name == name; end++ {
			install := skills[end]
			switch install.state {
			case skillNotInstalled:
				continue
			case skillRepository:
				lines = append(lines, "installed from the repository in "+install.path)
			default:
				lines = append(lines, "installed in "+install.path)
			}
			if install.problem != "" {
				lines = append(lines, "problem: "+install.problem)
			}
		}
		if len(lines) == 0 {
			lines = append(lines, "not installed")
		}

		fmt.Fprintf(w, "  %-*s  %s\n", width, name, lines[0])
		for _, line := range lines[1:] {
			fmt.Fprintf(w, "%s%s\n", indent, line)
		}

		start = end
	}
}
