package doctorcmd

import (
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/cmd/ai"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/cli/result"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/completionsetup"
	"github.com/vriesdemichael/bitbucket-data-center-cli/internal/config"
)

// Report is what `bb doctor --json` returns when there is nothing to fix.
//
// A run with issues writes no report. It ends in the failure envelope, whose
// error.details names every issue (ADR-075, ADR-086), so this payload only
// ever describes a run with nothing to fix and has no field for a problem.
type Report struct {
	Files      []File       `json:"files" jsonschema:"The stored, workspace and system configuration files, in that order."`
	Settings   []Setting    `json:"settings" jsonschema:"Every effective setting and where it came from."`
	Keyring    Keyring      `json:"keyring" jsonschema:"The OS keyring, checked only when keyring-backed storage is required."`
	Completion []Completion `json:"completion" jsonschema:"Shell completion for each shell installed, and for any other shell something of bb's was found for."`
	Skills     []AgentSkill `json:"skills" jsonschema:"Each agent skill bb carries, in every place an agent reads it from: .agents/skills and .claude/skills, under the working directory and under the home directory."`
}

// File is one configuration file.
type File struct {
	Tier     string   `json:"tier" jsonschema:"Which configuration file this is."`
	Path     string   `json:"path,omitempty" jsonschema:"Where the file is. Absent when no workspace file was found, or the stored file's location could not be worked out."`
	PathFrom string   `json:"pathFrom" jsonschema:"What chose the path: the variable that set it, default, search for a workspace file found above the working directory, or machine for the fixed system location."`
	Read     bool     `json:"read" jsonschema:"Whether bb reads this file at all."`
	NotRead  string   `json:"notRead,omitempty" jsonschema:"Why bb does not read it, when it does not."`
	Exists   bool     `json:"exists" jsonschema:"Whether the file is there."`
	Secrets  []Secret `json:"secrets" jsonschema:"Hosts with a plaintext credential in this file's insecure_secrets. Never the credential itself."`
}

// Secret says which plaintext credentials a file holds for a host.
type Secret struct {
	Host     string `json:"host" jsonschema:"The host the credential is for."`
	Token    bool   `json:"token" jsonschema:"Whether a plaintext token is held."`
	Password bool   `json:"password" jsonschema:"Whether a plaintext password is held."`
}

// Setting is one effective setting.
type Setting struct {
	Name       string   `json:"name" jsonschema:"The setting, named as its configuration key where it has one."`
	Value      string   `json:"value,omitempty" jsonschema:"The effective value. Never present for a secret."`
	Secret     bool     `json:"secret" jsonschema:"Whether this is a token or password, whose value is never reported."`
	Configured bool     `json:"configured" jsonschema:"Whether anything sets it. False means the built-in default applies."`
	Source     Source   `json:"source" jsonschema:"Where the effective value came from."`
	Shadowed   []Source `json:"shadowed" jsonschema:"Other places that set it and lost to source."`
}

// Source is where a setting came from.
type Source struct {
	Kind string `json:"kind" jsonschema:"What kind of place: a flag, the environment, a .env file, a value passed by the program running bb, one of the three configuration files, the Windows registry, the OS keyring, or the built-in default."`
	Name string `json:"name,omitempty" jsonschema:"The flag, variable, key, keyring entry or registry value."`
	Path string `json:"path,omitempty" jsonschema:"The file, .env file or registry key it was found in."`
}

// Keyring reports on the OS keyring.
type Keyring struct {
	Required   bool   `json:"required" jsonschema:"Whether keyring-backed storage is required."`
	RequiredBy Source `json:"requiredBy,omitzero" jsonschema:"What requires it."`
	Checked    bool   `json:"checked" jsonschema:"Whether the keyring was probed. Only when it is required."`
	Reachable  bool   `json:"reachable" jsonschema:"Whether the probe reached it."`
}

// Completion is where completion is set up for one shell.
type Completion struct {
	Shell     string            `json:"shell" jsonschema:"The shell."`
	Installed bool              `json:"installed" jsonschema:"Whether the shell is on PATH. One that is not is listed only when something of bb's was found for it."`
	Places    []CompletionPlace `json:"places" jsonschema:"Every place completion is set up for this shell. Empty when it is set up nowhere."`
}

// CompletionPlace is one place completion is set up.
type CompletionPlace struct {
	Kind    string `json:"kind" jsonschema:"setup for what bb completion install writes, script for a script saved from bb completion <shell>, package for the script a package manager installed, startup for a startup file somebody added bb completion to by hand."`
	Scope   string `json:"scope" jsonschema:"Whose shells read it: user for the person running bb, all-users for everyone on the machine. A package's script is all-users."`
	Edition string `json:"edition,omitempty" jsonschema:"Which PowerShell reads it. Absent for the other shells."`
	Path    string `json:"path" jsonschema:"The file."`
	Current bool   `json:"current" jsonschema:"Whether it is exactly what this bb writes: bb completion install's loader as it is now, or the script bb completion <shell> prints. A script that is not has fallen behind, which is an issue, so a report never holds one. False for a package's script and a startup file, which bb does not write."`
}

// AgentSkill is one agent skill in one place.
type AgentSkill struct {
	Skill    string `json:"skill" jsonschema:"The skill."`
	Scope    string `json:"scope" jsonschema:"project for the working directory, user for the home directory, which bb ai skill install writes to with --global."`
	Location string `json:"location" jsonschema:"agents for .agents/skills, which most agents read; claude for .claude/skills, which Claude Code reads."`
	Path     string `json:"path" jsonschema:"The skill's file in this place, whether or not it is there."`
	State    string `json:"state" jsonschema:"not_installed; current for exactly what bb ai skill install writes now; repository for the repository's copy, which npx skills add installs. Any other file -- an earlier bb's, or an edited one -- is an issue, so a report never holds one."`
}

func init() {
	kinds := config.SourceKinds()

	shells := make([]string, 0, len(completionsetup.Shells))
	for _, shell := range completionsetup.Shells {
		shells = append(shells, string(shell))
	}
	skills := make([]string, 0, len(ai.Skills))
	for _, skill := range ai.Skills {
		skills = append(skills, skill.Name)
	}
	locations := make([]string, 0, len(ai.SkillLocations))
	for _, location := range ai.SkillLocations {
		locations = append(locations, location.Name)
	}

	result.Declare("doctor", result.For[Report](map[string][]string{
		"files.tier":                {config.TierStored, config.TierWorkspace, config.TierSystem},
		"settings.source.kind":      kinds,
		"settings.shadowed.kind":    kinds,
		"keyring.requiredBy.kind":   kinds,
		"completion.shell":          shells,
		"completion.places.kind":    {placeSetup, placeScript, placePackage, placeStartup},
		"completion.places.scope":   {string(completionsetup.CurrentUser), string(completionsetup.AllUsers)},
		"completion.places.edition": completionsetup.Editions(),
		"skills.skill":              skills,
		"skills.scope":              {scopeProject, scopeUser},
		"skills.location":           locations,
		"skills.state":              {skillNotInstalled, skillCurrent, skillRepository},
	}))
}

func reportFrom(found findings) Report {
	diagnosis := found.diagnosis

	report := Report{
		Files:    make([]File, 0, len(diagnosis.Files)),
		Settings: make([]Setting, 0, len(diagnosis.Settings)),
		Keyring: Keyring{
			Required:   diagnosis.Keyring.Required,
			RequiredBy: sourceFrom(diagnosis.Keyring.RequiredBy),
			Checked:    diagnosis.Keyring.Checked,
			Reachable:  diagnosis.Keyring.Reachable,
		},
		Completion: make([]Completion, 0, len(found.completion)),
		Skills:     make([]AgentSkill, 0, len(found.skills)),
	}

	for _, file := range diagnosis.Files {
		report.Files = append(report.Files, fileFrom(file))
	}
	for _, setting := range diagnosis.Settings {
		report.Settings = append(report.Settings, settingFrom(setting))
	}
	for _, completion := range found.completion {
		report.Completion = append(report.Completion, completionFrom(completion))
	}
	for _, install := range found.skills {
		report.Skills = append(report.Skills, AgentSkill{
			Skill:    install.skill.Name,
			Scope:    install.scope,
			Location: install.location,
			Path:     install.path,
			State:    install.state,
		})
	}

	return report
}

func completionFrom(completion shellCompletion) Completion {
	published := Completion{
		Shell:     string(completion.shell),
		Installed: completion.installed,
		Places:    make([]CompletionPlace, 0, len(completion.places)),
	}
	for _, place := range completion.places {
		published.Places = append(published.Places, CompletionPlace{
			Kind:    place.kind,
			Scope:   string(place.scope),
			Edition: place.edition,
			Path:    place.path,
			Current: place.current,
		})
	}

	return published
}

func fileFrom(file config.DiagnosedFile) File {
	published := File{
		Tier:     file.Tier,
		Path:     file.Path,
		PathFrom: file.PathFrom,
		Read:     file.Read,
		NotRead:  file.NotRead,
		Exists:   file.Exists,
		Secrets:  make([]Secret, 0, len(file.Secrets)),
	}
	for _, secret := range file.Secrets {
		published.Secrets = append(published.Secrets, Secret{Host: secret.Host, Token: secret.Token, Password: secret.Password})
	}

	return published
}

func settingFrom(setting config.DiagnosedSetting) Setting {
	published := Setting{
		Name:       setting.Name,
		Value:      setting.Value,
		Secret:     setting.Secret,
		Configured: setting.Configured,
		Source:     sourceFrom(setting.Source),
		Shadowed:   make([]Source, 0, len(setting.Shadowed)),
	}
	if published.Secret {
		published.Value = ""
	}
	for _, shadowed := range setting.Shadowed {
		published.Shadowed = append(published.Shadowed, sourceFrom(shadowed))
	}

	return published
}

func sourceFrom(source config.SettingSource) Source {
	return Source{Kind: source.Kind, Name: source.Name, Path: source.Path}
}
